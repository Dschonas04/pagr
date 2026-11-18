package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

type Seite struct {
	ID                string
	Art               string
	Pfad              string
	Titel             string
	Inhalt            string
	Beschreibung      string
	Status            string
	ImMenue           bool
	MenueReihe        int
	VeroeffentlichtAm *time.Time
	Verfasser         string
	Erstellt          time.Time
	Geaendert         time.Time
}

func (s Seite) Oeffentlich() bool { return s.Status == "veroeffentlicht" }

// Markdown ohne rohes HTML: was in der Redaktion steht, kann kein Skript in
// die Website schreiben.
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM, extension.Typographer),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

func mdHTML(markdown string) template.HTML {
	var b bytes.Buffer
	if err := md.Convert([]byte(markdown), &b); err != nil {
		return template.HTML(template.HTMLEscapeString(markdown))
	}
	return template.HTML(b.String())
}

const seitenSpalten = `id::text, art, pfad, titel, inhalt, beschreibung, status, im_menue, menue_reihe,
	veroeffentlicht_am, verfasser, erstellt, geaendert`

type zeile interface{ Scan(...any) error }

func scanSeite(r zeile) (Seite, error) {
	var s Seite
	err := r.Scan(&s.ID, &s.Art, &s.Pfad, &s.Titel, &s.Inhalt, &s.Beschreibung, &s.Status, &s.ImMenue, &s.MenueReihe,
		&s.VeroeffentlichtAm, &s.Verfasser, &s.Erstellt, &s.Geaendert)
	return s, err
}

func (a *App) seiten(ctx context.Context, wo string, args ...any) ([]Seite, error) {
	rows, err := a.db.Query(ctx, "SELECT "+seitenSpalten+" FROM seiten "+wo, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var l []Seite
	for rows.Next() {
		s, err := scanSeite(rows)
		if err != nil {
			return nil, err
		}
		l = append(l, s)
	}
	return l, rows.Err()
}

var pfadMuster = regexp.MustCompile(`^/[a-z0-9\-/]*$`)

// pfadAus macht aus einem Titel oder einer Eingabe einen sauberen Pfad.
func pfadAus(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", " ", "-", "_", "-").Replace(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '/' {
			b.WriteRune(r)
		}
	}
	p := "/" + strings.Trim(regexp.MustCompile(`[-/]{2,}`).ReplaceAllStringFunc(b.String(), func(x string) string { return x[:1] }), "-/")
	return p
}

// ---------- Redaktion ----------

func (a *App) seitenListe(w http.ResponseWriter, r *http.Request) {
	l, err := a.seiten(r.Context(), "ORDER BY art DESC, im_menue DESC, menue_reihe, geaendert DESC")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.v.zeigen(w, r, "liste", a.daten(r, "Seiten", "seiten", map[string]any{"Seiten": l}))
}

func (a *App) seiteFormular(w http.ResponseWriter, r *http.Request) {
	s := Seite{Art: r.URL.Query().Get("art"), Status: "entwurf"}
	if s.Art != "beitrag" {
		s.Art = "seite"
	}
	if id := r.PathValue("id"); id != "neu" {
		var err error
		s, err = scanSeite(a.db.QueryRow(r.Context(), "SELECT "+seitenSpalten+" FROM seiten WHERE id::text=$1", id))
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	medien, _ := a.medienAlle(r.Context())
	a.v.zeigen(w, r, "seite", a.daten(r, s.Titel, "seiten", map[string]any{"S": s, "Medien": medien}))
}

func (a *App) seiteSpeichern(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	s := Seite{ID: r.FormValue("id"), Art: r.FormValue("art"), Titel: strings.TrimSpace(r.FormValue("titel")),
		Inhalt: strings.ReplaceAll(r.FormValue("inhalt"), "\r\n", "\n"), Beschreibung: strings.TrimSpace(r.FormValue("beschreibung")),
		Status: r.FormValue("status"), ImMenue: r.FormValue("im_menue") == "ja", MenueReihe: zahl(r.FormValue("menue_reihe"))}
	if s.Art != "beitrag" {
		s.Art = "seite"
	}
	if s.Status != "veroeffentlicht" {
		s.Status = "entwurf"
	}
	ruck := "/admin/seite/neu?art=" + s.Art
	if s.ID != "" {
		ruck = "/admin/seite/" + s.ID
	}
	if s.Titel == "" {
		zurueck(w, r, ruck, "Der Titel fehlt")
		return
	}
	pfad := strings.TrimSpace(r.FormValue("pfad"))
	if pfad == "" {
		pfad = s.Titel
		if s.Art == "beitrag" {
			pfad = "beitraege/" + pfad
		}
	}
	if pfad != "/" {
		pfad = pfadAus(pfad)
	}
	if !pfadMuster.MatchString(pfad) || strings.HasPrefix(pfad, "/admin") || strings.HasPrefix(pfad, "/sso") ||
		strings.HasPrefix(pfad, "/api") || strings.HasPrefix(pfad, "/statisch") || strings.HasPrefix(pfad, "/medien") {
		zurueck(w, r, ruck, "Dieser Pfad ist nicht möglich: "+pfad)
		return
	}
	s.Pfad = pfad
	wer := a.wer(r)
	var id string
	var err error
	if s.ID == "" {
		err = a.db.QueryRow(r.Context(), `INSERT INTO seiten (art, pfad, titel, inhalt, beschreibung, status, im_menue, menue_reihe, verfasser,
			veroeffentlicht_am) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9, CASE WHEN $6='veroeffentlicht' THEN now() END) RETURNING id::text`,
			s.Art, s.Pfad, s.Titel, s.Inhalt, s.Beschreibung, s.Status, s.ImMenue, s.MenueReihe, wer).Scan(&id)
	} else {
		id = s.ID
		_, err = a.db.Exec(r.Context(), `UPDATE seiten SET art=$2, pfad=$3, titel=$4, inhalt=$5, beschreibung=$6, status=$7,
			im_menue=$8, menue_reihe=$9, verfasser=$10, geaendert=now(),
			veroeffentlicht_am = CASE WHEN $7='veroeffentlicht' THEN COALESCE(veroeffentlicht_am, now()) END
			WHERE id::text=$1`, id, s.Art, s.Pfad, s.Titel, s.Inhalt, s.Beschreibung, s.Status, s.ImMenue, s.MenueReihe, wer)
	}
	if err != nil {
		if strings.Contains(err.Error(), "seiten_pfad_key") {
			zurueck(w, r, ruck, "Den Pfad "+s.Pfad+" gibt es schon")
			return
		}
		zurueck(w, r, ruck, "Fehler: "+err.Error())
		return
	}
	// Jede Fassung wird festgehalten, die letzten 50 je Seite bleiben.
	a.db.Exec(r.Context(), `INSERT INTO versionen (seite, titel, inhalt, verfasser) SELECT $1::uuid, $2, $3, $4
		WHERE NOT EXISTS (SELECT 1 FROM versionen WHERE seite=$1::uuid AND inhalt=$3 AND titel=$2
		                  AND id = (SELECT max(id) FROM versionen WHERE seite=$1::uuid))`, id, s.Titel, s.Inhalt, wer)
	a.db.Exec(r.Context(), `DELETE FROM versionen WHERE seite=$1::uuid AND id NOT IN
		(SELECT id FROM versionen WHERE seite=$1::uuid ORDER BY id DESC LIMIT 50)`, id)
	hinweis := "Entwurf gespeichert"
	if s.Status == "veroeffentlicht" {
		hinweis = "Veröffentlicht unter " + s.Pfad
	}
	zurueck(w, r, "/admin/seite/"+id, hinweis)
}

func (a *App) seiteLoeschen(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	a.db.Exec(r.Context(), `DELETE FROM seiten WHERE id::text=$1`, r.PathValue("id"))
	zurueck(w, r, "/admin", "Seite gelöscht")
}

type Version struct {
	ID        int64
	Titel     string
	Inhalt    string
	Verfasser string
	Erstellt  time.Time
}

func (a *App) versionen(w http.ResponseWriter, r *http.Request) {
	s, err := scanSeite(a.db.QueryRow(r.Context(), "SELECT "+seitenSpalten+" FROM seiten WHERE id::text=$1", r.PathValue("id")))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rows, err := a.db.Query(r.Context(), `SELECT id, titel, inhalt, verfasser, erstellt FROM versionen WHERE seite=$1::uuid ORDER BY id DESC`, s.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()
	var l []Version
	for rows.Next() {
		var v Version
		if rows.Scan(&v.ID, &v.Titel, &v.Inhalt, &v.Verfasser, &v.Erstellt) == nil {
			l = append(l, v)
		}
	}
	a.v.zeigen(w, r, "versionen", a.daten(r, "Versionen", "seiten", map[string]any{"S": s, "Versionen": l}))
}

func (a *App) versionZurueck(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	id := r.PathValue("id")
	tag, err := a.db.Exec(r.Context(), `UPDATE seiten s SET titel=v.titel, inhalt=v.inhalt, geaendert=now()
		FROM versionen v WHERE s.id::text=$1 AND v.seite=s.id AND v.id::text=$2`, id, r.PathValue("v"))
	if err != nil || tag.RowsAffected() == 0 {
		zurueck(w, r, "/admin/seite/"+id+"/versionen", "Version nicht gefunden")
		return
	}
	zurueck(w, r, "/admin/seite/"+id, "Version zurückgeholt, noch nicht neu gespeichert")
}

func (a *App) vorschau(w http.ResponseWriter, r *http.Request) {
	s, err := scanSeite(a.db.QueryRow(r.Context(), "SELECT "+seitenSpalten+" FROM seiten WHERE id::text=$1", r.PathValue("id")))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.seiteAusliefern(w, r, s, true)
}

// ---------- Medien ----------

type Medium struct {
	ID       string
	Name     string
	Typ      string
	Groesse  int64
	Erstellt time.Time
}

func (m Medium) Bild() bool { return strings.HasPrefix(m.Typ, "image/") }
func (m Medium) GroesseText() string {
	switch {
	case m.Groesse > 1<<20:
		return fmt.Sprintf("%.1f MB", float64(m.Groesse)/(1<<20))
	case m.Groesse > 1<<10:
		return fmt.Sprintf("%d KB", m.Groesse>>10)
	}
	return fmt.Sprintf("%d B", m.Groesse)
}

func (a *App) medienAlle(ctx context.Context) ([]Medium, error) {
	rows, err := a.db.Query(ctx, `SELECT id::text, name, typ, groesse, erstellt FROM medien ORDER BY erstellt DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var l []Medium
	for rows.Next() {
		var m Medium
		if rows.Scan(&m.ID, &m.Name, &m.Typ, &m.Groesse, &m.Erstellt) == nil {
			l = append(l, m)
		}
	}
	return l, nil
}

func (a *App) medienListe(w http.ResponseWriter, r *http.Request) {
	l, _ := a.medienAlle(r.Context())
	a.v.zeigen(w, r, "medien", a.daten(r, "Medien", "medien", map[string]any{"Medien": l}))
}

// Erlaubt sind Bilder, PDF und einfache Dokumente. SVG nicht: ein SVG ist ein
// Dokument mit Skripten auf dem Ursprung der Website.
var medienTypen = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp",
	".avif": "image/avif", ".pdf": "application/pdf", ".txt": "text/plain; charset=utf-8", ".zip": "application/zip",
	".mp4": "video/mp4", ".webm": "video/webm", ".mp3": "audio/mpeg",
}

func (a *App) medienHochladen(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		zurueck(w, r, "/admin/medien", "Datei zu groß (höchstens 64 MB)")
		return
	}
	if !pruefeFormular(w, r) {
		return
	}
	f, kopf, err := r.FormFile("datei")
	if err != nil {
		zurueck(w, r, "/admin/medien", "Keine Datei gewählt")
		return
	}
	defer f.Close()
	endung := strings.ToLower(filepath.Ext(kopf.Filename))
	typ, ok := medienTypen[endung]
	if !ok {
		zurueck(w, r, "/admin/medien", "Dieser Dateityp ist nicht erlaubt: "+endung)
		return
	}
	var id string
	name := filepath.Base(kopf.Filename)
	if err := a.db.QueryRow(r.Context(), `INSERT INTO medien (name, typ, groesse) VALUES ($1, $2, $3) RETURNING id::text`,
		name, typ, kopf.Size).Scan(&id); err != nil {
		zurueck(w, r, "/admin/medien", "Fehler: "+err.Error())
		return
	}
	ziel, err := os.Create(filepath.Join(a.medien, id))
	if err == nil {
		_, err = io.Copy(ziel, f)
		ziel.Close()
	}
	if err != nil {
		a.db.Exec(r.Context(), `DELETE FROM medien WHERE id::text=$1`, id)
		zurueck(w, r, "/admin/medien", "Speichern ging nicht: "+err.Error())
		return
	}
	zurueck(w, r, "/admin/medien", "Hochgeladen: "+name)
}

func (a *App) medienLoeschen(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	id := r.PathValue("id")
	if tag, _ := a.db.Exec(r.Context(), `DELETE FROM medien WHERE id::text=$1`, id); tag.RowsAffected() == 1 {
		os.Remove(filepath.Join(a.medien, id))
	}
	zurueck(w, r, "/admin/medien", "Gelöscht")
}

func (a *App) medium(w http.ResponseWriter, r *http.Request) {
	var m Medium
	if err := a.db.QueryRow(r.Context(), `SELECT id::text, name, typ FROM medien WHERE id::text=$1`, r.PathValue("id")).
		Scan(&m.ID, &m.Name, &m.Typ); err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(filepath.Join(a.medien, m.ID))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", m.Typ)
	art := "inline"
	if !m.Bild() && m.Typ != "application/pdf" && !strings.HasPrefix(m.Typ, "video/") && !strings.HasPrefix(m.Typ, "audio/") {
		art = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(art, map[string]string{"filename": m.Name}))
	w.Header().Set("Cache-Control", "public, max-age=86400")
	h := sha256.Sum256([]byte(m.ID))
	w.Header().Set("ETag", `"`+hex.EncodeToString(h[:8])+`"`)
	st, _ := f.Stat()
	http.ServeContent(w, r, "", st.ModTime(), f)
}

// ---------- Einstellungen ----------

func (a *App) werte(ctx context.Context) map[string]string {
	m := map[string]string{}
	rows, err := a.db.Query(ctx, `SELECT schluessel, wert FROM einstellungen`)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			m[k] = v
		}
	}
	return m
}

func (a *App) einstellungen(w http.ResponseWriter, r *http.Request) {
	a.v.zeigen(w, r, "einstellungen", a.daten(r, "Einstellungen", "einstellungen", map[string]any{"W": a.werte(r.Context())}))
}

func (a *App) einstellungenSpeichern(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	for _, k := range []string{"titel", "untertitel", "fuss", "adresse"} {
		a.db.Exec(r.Context(), `INSERT INTO einstellungen VALUES ($1, $2) ON CONFLICT (schluessel) DO UPDATE SET wert=$2`,
			k, strings.TrimSpace(r.FormValue(k)))
	}
	zurueck(w, r, "/admin/einstellungen", "Gespeichert")
}

func zahl(s string) int {
	var n int
	fmt.Sscan(strings.TrimSpace(s), &n)
	return n
}
