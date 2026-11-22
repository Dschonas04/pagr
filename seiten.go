package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
	Besitzer          string
	Sprache           string
	UebersetzungVon   string
}

// sichtbar ist die eine Bedingung für alles Öffentliche: veröffentlicht und an der Reihe.
const sichtbar = "status='veroeffentlicht' AND veroeffentlicht_am <= now()"

func (s Seite) Geplant() bool {
	return s.Status == "veroeffentlicht" && s.VeroeffentlichtAm != nil && s.VeroeffentlichtAm.After(time.Now())
}
func (s Seite) Oeffentlich() bool { return s.Status == "veroeffentlicht" && !s.Geplant() }

var Sprachen = []string{"de", "en", "fr", "es", "it", "nl", "pl", "tr", "uk"}

// reserviert: Pfade, die der App gehören.
func reserviert(pfad string) bool {
	erstes, _, _ := strings.Cut(strings.TrimPrefix(pfad, "/"), "/")
	switch erstes {
	case "admin", "sso", "api", "statisch", "medien", "healthz":
		return true
	}
	return false
}

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
	veroeffentlicht_am, verfasser, erstellt, geaendert, besitzer, sprache, COALESCE(uebersetzung_von::text, '')`

type zeile interface{ Scan(...any) error }

func scanSeite(r zeile) (Seite, error) {
	var s Seite
	err := r.Scan(&s.ID, &s.Art, &s.Pfad, &s.Titel, &s.Inhalt, &s.Beschreibung, &s.Status, &s.ImMenue, &s.MenueReihe,
		&s.VeroeffentlichtAm, &s.Verfasser, &s.Erstellt, &s.Geaendert,
		&s.Besitzer, &s.Sprache, &s.UebersetzungVon)
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

func (a *App) seiteMit(ctx context.Context, id string) (Seite, error) {
	return scanSeite(a.db.QueryRow(ctx, "SELECT "+seitenSpalten+" FROM seiten WHERE id::text=$1", id))
}

// markdownVorschau für den Editor, mit demselben Markdown wie die Website.
func (a *App) markdownVorschau(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if !pruefeFormular(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, string(mdHTML(r.PostFormValue("inhalt"))))
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
	p, err := a.person(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	q := r.URL.Query()
	s := Seite{Art: q.Get("art"), Status: "entwurf", Besitzer: p.Kennung, Sprache: a.hauptsprache(r.Context())}
	if s.Art != "beitrag" {
		s.Art = "seite"
	}
	var alteFassung *Version
	if id := r.PathValue("id"); id != "neu" {
		if s, err = a.seiteMit(r.Context(), id); err != nil {
			http.NotFound(w, r)
			return
		}
		// Eine alte Fassung kommt nur ins Formular. Live wird sie erst mit dem Speichern.
		if v := q.Get("version"); v != "" {
			var alt Version
			if err := a.db.QueryRow(r.Context(), `SELECT id, titel, inhalt, erstellt FROM versionen WHERE seite=$1::uuid AND id::text=$2`, s.ID, v).
				Scan(&alt.ID, &alt.Titel, &alt.Inhalt, &alt.Erstellt); err != nil {
				http.NotFound(w, r)
				return
			}
			s.Titel, s.Inhalt, alteFassung = alt.Titel, alt.Inhalt, &alt
		}
	}
	medien, err := a.medienAlle(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	originale, err := a.seiten(r.Context(), "WHERE uebersetzung_von IS NULL AND id::text <> $1 ORDER BY titel", s.ID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	a.v.zeigen(w, r, "seite", a.daten(r, s.Titel, "seiten", map[string]any{
		"S": s, "Medien": medien, "Originale": originale, "Sprachen": Sprachen, "Alt": alteFassung,
		"Darf": p.darfAendern(s),
	}))
}

func (a *App) seiteSpeichern(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	p, err := a.person(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s := Seite{ID: r.FormValue("id"), Art: r.FormValue("art"), Titel: strings.TrimSpace(r.FormValue("titel")),
		Inhalt: strings.ReplaceAll(r.FormValue("inhalt"), "\r\n", "\n"), Beschreibung: strings.TrimSpace(r.FormValue("beschreibung")),
		Status: r.FormValue("status"), ImMenue: r.FormValue("im_menue") == "ja", MenueReihe: zahl(r.FormValue("menue_reihe")),
		Sprache: r.FormValue("sprache"), UebersetzungVon: r.FormValue("uebersetzung_von")}
	if s.Art != "beitrag" {
		s.Art = "seite"
	}
	if s.Status != "veroeffentlicht" {
		s.Status = "entwurf"
	}
	darf := p.darfVeroeffentlichen()
	if !darf {
		if s.Status != "entwurf" {
			http.Error(w, "Veröffentlichen kann nur die Redaktion", http.StatusForbidden)
			return
		}
		s.ImMenue, s.MenueReihe = false, 0
	}
	var vorher Seite
	if s.ID != "" {
		if vorher, err = a.seiteMit(r.Context(), s.ID); err != nil {
			http.NotFound(w, r)
			return
		}
		if !p.darfAendern(vorher) {
			http.Error(w, "Das ist nicht dein Entwurf", http.StatusForbidden)
			return
		}
	}
	haupt := a.hauptsprache(r.Context())
	if !slices.Contains(Sprachen, s.Sprache) {
		s.Sprache = haupt
		if vorher.ID != "" {
			s.Sprache = vorher.Sprache
		}
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
		if s.Sprache != haupt {
			pfad = s.Sprache + "/" + pfad
		}
	}
	if pfad != "/" {
		pfad = pfadAus(pfad)
	}
	if !pfadMuster.MatchString(pfad) || reserviert(pfad) {
		zurueck(w, r, ruck, "Dieser Pfad ist nicht möglich: "+pfad)
		return
	}
	s.Pfad = pfad
	var wann *time.Time
	if v := r.FormValue("wann"); v != "" && s.Status == "veroeffentlicht" {
		t, err := time.ParseInLocation("2006-01-02T15:04", v, time.Local)
		if err != nil {
			zurueck(w, r, ruck, "Der Zeitpunkt stimmt nicht")
			return
		}
		wann = &t
	}
	if s.UebersetzungVon != "" {
		o, err := a.seiteMit(r.Context(), s.UebersetzungVon)
		if err != nil || o.ID == s.ID || o.UebersetzungVon != "" {
			zurueck(w, r, ruck, "Die Seite, von der das die Übersetzung ist, passt nicht")
			return
		}
	}

	wer := a.wer(r)
	tx, err := a.db.Begin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer tx.Rollback(r.Context())
	var id string
	if s.ID == "" {
		err = tx.QueryRow(r.Context(), `INSERT INTO seiten (art, pfad, titel, inhalt, beschreibung, status, im_menue, menue_reihe, verfasser,
			veroeffentlicht_am, besitzer, sprache, uebersetzung_von)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9, CASE WHEN $6='veroeffentlicht' THEN COALESCE($10::timestamptz, now()) END, $11, $12, NULLIF($13, '')::uuid)
			RETURNING id::text`,
			s.Art, s.Pfad, s.Titel, s.Inhalt, s.Beschreibung, s.Status, s.ImMenue, s.MenueReihe, wer,
			wann, p.Kennung, s.Sprache, s.UebersetzungVon).Scan(&id)
	} else {
		// Die Bedingung steht auch im UPDATE: veröffentlicht jemand den Entwurf
		// inzwischen, ändert der Autor ihn nicht mehr. Ohne Zeitpunkt heißt sofort.
		err = tx.QueryRow(r.Context(), `UPDATE seiten SET art=$2, pfad=$3, titel=$4, inhalt=$5, beschreibung=$6, status=$7,
			im_menue=$8, menue_reihe=$9, verfasser=CASE WHEN verfasser='' THEN $10 ELSE verfasser END, geaendert=now(),
			veroeffentlicht_am = CASE WHEN $7='veroeffentlicht'
				THEN COALESCE($11::timestamptz, LEAST(COALESCE(veroeffentlicht_am, now()), now())) END,
			sprache=$12, uebersetzung_von=NULLIF($13, '')::uuid
			WHERE id::text=$1 AND ($14 OR (besitzer=$15 AND status='entwurf')) RETURNING id::text`,
			s.ID, s.Art, s.Pfad, s.Titel, s.Inhalt, s.Beschreibung, s.Status, s.ImMenue, s.MenueReihe, wer,
			wann, s.Sprache, s.UebersetzungVon, darf, p.Kennung).Scan(&id)
	}
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			http.Error(w, "Die Seite hat sich inzwischen geändert", http.StatusConflict)
		case strings.Contains(err.Error(), "seiten_pfad_key"):
			zurueck(w, r, ruck, "Den Pfad "+s.Pfad+" gibt es schon")
		default:
			zurueck(w, r, ruck, "Fehler: "+err.Error())
		}
		return
	}
	type befehl struct {
		sql  string
		args []any
	}
	var dazu []befehl
	if s.Status == "veroeffentlicht" {
		if vorher.Oeffentlich() && vorher.Pfad != s.Pfad && vorher.Pfad != "/" {
			// Der alte Pfad führt weiter, auch was schon auf ihn zeigte.
			dazu = append(dazu,
				befehl{`UPDATE weiterleitungen SET nach=$2 WHERE nach=$1`, []any{vorher.Pfad, s.Pfad}},
				befehl{`INSERT INTO weiterleitungen VALUES ($1, $2) ON CONFLICT (von) DO UPDATE SET nach=$2`, []any{vorher.Pfad, s.Pfad}})
		}
		dazu = append(dazu, befehl{`DELETE FROM weiterleitungen WHERE von=$1`, []any{s.Pfad}})
	}
	// Jede Fassung wird festgehalten, die letzten 50 je Seite bleiben.
	dazu = append(dazu,
		befehl{`INSERT INTO versionen (seite, titel, inhalt, verfasser) SELECT $1::uuid, $2, $3, $4
			WHERE NOT EXISTS (SELECT 1 FROM versionen WHERE seite=$1::uuid AND inhalt=$3 AND titel=$2
			                  AND id = (SELECT max(id) FROM versionen WHERE seite=$1::uuid))`, []any{id, s.Titel, s.Inhalt, wer}},
		befehl{`DELETE FROM versionen WHERE seite=$1::uuid AND id NOT IN
			(SELECT id FROM versionen WHERE seite=$1::uuid ORDER BY id DESC LIMIT 50)`, []any{id}})
	for _, b := range dazu {
		if _, err := tx.Exec(r.Context(), b.sql, b.args...); err != nil {
			zurueck(w, r, ruck, "Fehler: "+err.Error())
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		zurueck(w, r, ruck, "Fehler: "+err.Error())
		return
	}
	hinweis := "Entwurf gespeichert"
	if s.Status == "veroeffentlicht" {
		hinweis = "Veröffentlicht unter " + s.Pfad
		if wann != nil && wann.After(time.Now()) {
			hinweis = "Geplant für " + wann.Format("02.01.2006 15:04")
		}
	}
	zurueck(w, r, "/admin/seite/"+id, hinweis)
}

func (a *App) seiteLoeschen(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	p, err := a.person(r)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	e, err := a.db.Exec(r.Context(), `DELETE FROM seiten WHERE id::text=$1 AND ($2 OR (besitzer=$3 AND status='entwurf'))`,
		r.PathValue("id"), p.darfVeroeffentlichen(), p.Kennung)
	if err != nil {
		zurueck(w, r, "/admin", "Fehler: "+err.Error())
		return
	}
	if e.RowsAffected() == 0 {
		http.Error(w, "Das ist nicht dein Entwurf", http.StatusForbidden)
		return
	}
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
	tag, err := a.db.Exec(r.Context(), `DELETE FROM medien WHERE id::text=$1`, id)
	if err != nil {
		zurueck(w, r, "/admin/medien", "Fehler: "+err.Error())
		return
	}
	if tag.RowsAffected() == 1 {
		os.Remove(filepath.Join(a.medien, id))
		for _, b := range bildBreiten {
			os.Remove(filepath.Join(a.medien, id+"-"+strconv.Itoa(b)))
		}
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
	pfad := filepath.Join(a.medien, m.ID)
	etikett := m.ID
	if b := r.URL.Query().Get("b"); b != "" && m.Bild() {
		if p, typ, err := verkleinert(pfad, m.Typ, b); err == nil {
			pfad, m.Typ, etikett = p, typ, m.ID+"-"+b
		}
	}
	f, err := os.Open(pfad)
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
	h := sha256.Sum256([]byte(etikett))
	w.Header().Set("ETag", `"`+hex.EncodeToString(h[:8])+`"`)
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
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
	a.v.zeigen(w, r, "einstellungen", a.daten(r, "Einstellungen", "einstellungen", map[string]any{"W": a.werte(r.Context()), "Sprachen": Sprachen}))
}

func (a *App) einstellungenSpeichern(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	for _, k := range []string{"titel", "untertitel", "fuss", "adresse", "design", "sprache"} {
		v := strings.TrimSpace(r.FormValue(k))
		switch {
		case k == "design" && v != "klar" && v != "magazin":
			v = ""
		case k == "sprache" && !slices.Contains(Sprachen, v):
			v = "de"
		}
		if _, err := a.db.Exec(r.Context(), `INSERT INTO einstellungen VALUES ($1, $2) ON CONFLICT (schluessel) DO UPDATE SET wert=$2`, k, v); err != nil {
			zurueck(w, r, "/admin/einstellungen", "Fehler: "+err.Error())
			return
		}
	}
	zurueck(w, r, "/admin/einstellungen", "Gespeichert")
}

func zahl(s string) int {
	var n int
	fmt.Sscan(strings.TrimSpace(s), &n)
	return n
}
