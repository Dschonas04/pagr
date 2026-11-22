package main

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRedaktion(t *testing.T) {
	adresse := os.Getenv("PAGR_TEST_DB")
	if adresse == "" {
		t.Skip("PAGR_TEST_DB ist nicht gesetzt")
	}
	c, err := pgxpool.ParseConfig(adresse)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(c.ConnConfig.Database, "pruef") {
		t.Fatal("PAGR_TEST_DB muss auf eine Datenbank pruef* zeigen")
	}
	ctx := context.Background()
	db := datenbank(ctx, adresse)
	defer db.Close()
	if _, err := db.Exec(ctx, `TRUNCATE seiten, versionen, weiterleitungen, personen CASCADE`); err != nil {
		t.Fatal(err)
	}
	a := &App{db: db, v: ladeVorlagen(), site: ladeSite(), medien: t.TempDir(), admins: []string{"konto:chefin"},
		sso: NeuesSSO("Pagr", filepath.Join(t.TempDir(), "sso.json"), "PAGR_TEST_SSO_AUS")}
	token := strings.Repeat("a", 32)
	anmelden := func(name string) []*http.Cookie {
		k := ssoKonto{Name: name, Hash: ssoPasswortHash("nur-fuer-den-test")}
		a.sso.konten = append(a.sso.konten, k)
		w := httptest.NewRecorder()
		a.sso.lokalAnmelden(w, httptest.NewRequest("POST", "/", nil), k)
		return append(w.Result().Cookies(), &http.Cookie{Name: "formular", Value: token})
	}
	chefin, autorin, andere := anmelden("chefin"), anmelden("autorin"), anmelden("andere")
	ruf := func(kekse []*http.Cookie, methode, pfad string, f url.Values) *httptest.ResponseRecorder {
		if f == nil {
			f = url.Values{}
		}
		f.Set("token", token)
		r := httptest.NewRequest(methode, pfad, strings.NewReader(f.Encode()))
		if methode != "GET" {
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", "http://"+r.Host)
		}
		for _, k := range kekse {
			r.AddCookie(k)
		}
		w := httptest.NewRecorder()
		a.wege().ServeHTTP(w, r)
		return w
	}
	id := func(pfad string) (id string) {
		if err := db.QueryRow(ctx, `SELECT id::text FROM seiten WHERE pfad=$1`, pfad).Scan(&id); err != nil {
			t.Fatalf("%s: %v", pfad, err)
		}
		return
	}
	lesen := func(pfad string) *httptest.ResponseRecorder { return ruf(nil, "GET", pfad, nil) }
	seite := func(id, pfad, inhalt, status string) url.Values {
		return url.Values{"id": {id}, "titel": {"Titel " + pfad}, "pfad": {pfad}, "inhalt": {inhalt}, "status": {status}}
	}

	// Autorin: eigene Entwürfe, mehr nicht.
	if w := ruf(autorin, "POST", "/admin/seite", seite("", "/vorab", "x", "veroeffentlicht")); w.Code != http.StatusForbidden {
		t.Errorf("Autorin veröffentlicht: %d", w.Code)
	}
	ruf(autorin, "POST", "/admin/seite", seite("", "/entwurf", "erste", "entwurf"))
	entwurf := id("/entwurf")
	if w := ruf(andere, "POST", "/admin/seite", seite(entwurf, "/entwurf", "fremd", "entwurf")); w.Code != http.StatusForbidden {
		t.Errorf("fremder Entwurf geändert: %d", w.Code)
	}
	if w := ruf(andere, "POST", "/admin/seite/"+entwurf+"/loeschen", nil); w.Code != http.StatusForbidden {
		t.Errorf("fremder Entwurf gelöscht: %d", w.Code)
	}
	ruf(chefin, "POST", "/admin/seite", seite(entwurf, "/entwurf", "zweite", "veroeffentlicht"))
	if w := ruf(autorin, "POST", "/admin/seite", seite(entwurf, "/entwurf", "heimlich", "entwurf")); w.Code != http.StatusForbidden {
		t.Errorf("Autorin ändert Veröffentlichtes: %d", w.Code)
	}
	var verfasser string
	if err := db.QueryRow(ctx, `SELECT verfasser FROM seiten WHERE id::text=$1`, entwurf).Scan(&verfasser); err != nil || verfasser != "autorin" {
		t.Errorf("Verfasserin verloren: %q %v", verfasser, err)
	}
	for _, p := range []string{"/admin/export", "/admin/weiterleitungen", "/admin/personen"} {
		if w := ruf(autorin, "GET", p, nil); w.Code != http.StatusForbidden {
			t.Errorf("Autorin kommt an %s: %d", p, w.Code)
		}
	}

	// Eine alte Fassung zu laden ändert nichts an der Website.
	var v string
	if err := db.QueryRow(ctx, `SELECT id::text FROM versionen WHERE seite::text=$1 ORDER BY id LIMIT 1`, entwurf).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if b := ruf(chefin, "GET", "/admin/seite/"+entwurf+"?version="+v, nil).Body.String(); !strings.Contains(b, "erste</textarea>") {
		t.Error("alte Fassung nicht im Formular")
	}
	if !strings.Contains(lesen("/entwurf").Body.String(), "zweite") {
		t.Error("Laden einer Fassung hat die Website geändert")
	}

	// Geplantes ist nirgends zu sehen.
	f := seite("", "/beitraege/spaeter", "GEHEIM-BIS-MORGEN", "veroeffentlicht")
	f.Set("art", "beitrag")
	f.Set("wann", time.Now().Add(24*time.Hour).Format("2006-01-02T15:04"))
	ruf(chefin, "POST", "/admin/seite", f)
	id("/beitraege/spaeter")
	if lesen("/beitraege/spaeter").Code != http.StatusNotFound {
		t.Error("Geplantes ist öffentlich")
	}
	for _, p := range []string{"/sitemap.xml", "/feed.xml"} {
		if w := lesen(p); w.Code != 200 || strings.Contains(w.Body.String(), "spaeter") {
			t.Errorf("%s: %d, oder Geplantes steht darin", p, w.Code)
		}
	}
	w := ruf(chefin, "GET", "/admin/export", nil)
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("Export %d: %v", w.Code, err)
	}
	namen := map[string]bool{}
	for _, d := range z.File {
		namen[d.Name] = true
	}
	if !namen["entwurf/index.html"] || namen["beitraege/spaeter/index.html"] || !namen["statisch/site.css"] {
		t.Errorf("Export: %v", namen)
	}

	// Ein neuer Pfad lässt den alten weiterleiten.
	ruf(chefin, "POST", "/admin/seite", seite(entwurf, "/neuer-name", "zweite", "veroeffentlicht"))
	if w := lesen("/entwurf"); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/neuer-name" {
		t.Errorf("Weiterleitung: %d %s", w.Code, w.Header().Get("Location"))
	}

	// Übersetzungen verweisen aufeinander.
	u := seite("", "/en/new-name", "second", "veroeffentlicht")
	u.Set("sprache", "en")
	u.Set("uebersetzung_von", entwurf)
	ruf(chefin, "POST", "/admin/seite", u)
	if !strings.Contains(lesen("/neuer-name").Body.String(), `href="/en/new-name"`) || !strings.Contains(lesen("/en/new-name").Body.String(), `<html lang="en">`) {
		t.Error("Übersetzung fehlt")
	}
}

func TestZielGueltig(t *testing.T) {
	for z, soll := range map[string]bool{"/ok": true, "https://example.org/a": true, "//fremd.example": false, `/\fremd.example`: false, "javascript:alert(1)": false, "https://a@fremd.example": false} {
		if zielGueltig(z) != soll {
			t.Errorf("%q", z)
		}
	}
}

func TestVergleichen(t *testing.T) {
	l, ok := vergleichen("a\nb\nc", "a\nx\nc")
	var s string
	for _, z := range l {
		s += z.Art + ":" + z.Text + ","
	}
	if !ok || s != ":a,minus:b,plus:x,:c," {
		t.Errorf("%v %s", ok, s)
	}
}

func TestVerkleinert(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bild")
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1000, 500)), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	klein, _, err := verkleinert(p, "image/jpeg", "480")
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(klein)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if c, err := jpeg.DecodeConfig(f); err != nil || c.Width != 480 || c.Height != 240 {
		t.Errorf("%v %v", c, err)
	}
	if _, _, err := verkleinert(p, "image/jpeg", "999999"); err == nil {
		t.Error("beliebige Breite angenommen")
	}
	// Orientierung 6, einmal heil und einmal mit kaputtem Versatz.
	exif := []byte("Exif\x00\x00MM\x00*\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x06\x00\x00")
	bild := append([]byte{0xff, 0xd8, 0xff, 0xe1, 0, byte(len(exif) + 2)}, exif...)
	if !gedreht(bytes.NewReader(bild)) {
		t.Error("Drehung übersehen")
	}
	copy(bild[6+10:], []byte{0xff, 0xff, 0xff, 0xff})
	if gedreht(bytes.NewReader(bild)) {
		t.Error("kaputtes EXIF")
	}
}
