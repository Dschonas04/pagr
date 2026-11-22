// Pagr: Content-Management-System für Websites. Seiten und Beiträge in
// Markdown mit Entwürfen und Versionen, Medienbibliothek, Menü, Sitemap.
// Die Redaktion liegt unter /admin und braucht eine Anmeldung, die Website
// selbst ist öffentlich.
package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type App struct {
	exportSperre sync.Mutex // ein Export nach dem anderen
	// verwaltung: Änderungen an Konten, Rollen und Anmeldung laufen nacheinander.
	verwaltung Sperre
	admins     []string // Vorgabe des Betreibers
	db         *pgxpool.Pool
	sso        *SSO
	v          Vorlagen
	site       *siteVorlagen
	medien     string
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := &App{
		db:     datenbank(ctx, umgebung("PAGR_DB", "postgres://pagr:pagr@localhost:5432/pagr?sslmode=disable")),
		sso:    NeuesSSO("Pagr", umgebung("PAGR_SSO_DATEI", "./einstellungen/sso.json"), "PAGR_SSO_AUS"),
		v:      ladeVorlagen(),
		site:   ladeSite(),
		medien: umgebung("PAGR_MEDIEN", "./medien"),
	}
	a.admins = adminListe(umgebung("PAGR_ADMIN", ""))
	a.sso.ErstesKonto = a.ersterAdmin
	a.sso.KontoWeg = a.adminEntziehen
	a.sso.Frei = []string{"/statisch/", "/healthz"}
	a.sso.Startseite = "/admin"
	if err := os.MkdirAll(a.medien, 0o750); err != nil {
		log.Fatalf("Medien: %v", err)
	}

	adresse := ":" + umgebung("PAGR_PORT", "8360")
	srv := &http.Server{Addr: adresse, Handler: a.wege(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("Pagr %s lauscht auf %s", version, adresse)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	<-ctx.Done()
	ende, abbruch := context.WithTimeout(context.Background(), 10*time.Second)
	defer abbruch()
	srv.Shutdown(ende)
}

func (a *App) wege() http.Handler {
	// Redaktion: hinter der Anmeldung.
	adm := http.NewServeMux()
	adm.Handle("GET /statisch/", statisch())
	a.sso.Wege(adm, a.angemeldet)
	adm.HandleFunc("GET /admin", a.seitenListe)
	adm.HandleFunc("GET /admin/seite/{id}", a.seiteFormular)
	adm.HandleFunc("POST /admin/seite", a.seiteSpeichern)
	adm.HandleFunc("POST /admin/seite/{id}/loeschen", a.seiteLoeschen)
	adm.HandleFunc("GET /admin/seite/{id}/versionen", a.versionen)
	adm.HandleFunc("GET /admin/seite/{id}/vergleich/{v}", a.versionVergleich)
	adm.HandleFunc("POST /admin/markdown", a.markdownVorschau)
	adm.HandleFunc("GET /admin/personen", a.nurAdmin(a.personenSeite))
	adm.HandleFunc("POST /admin/personen", a.personSetzen)
	adm.HandleFunc("GET /admin/weiterleitungen", a.nurRedaktion(a.weiterleitungen))
	adm.HandleFunc("POST /admin/weiterleitungen", a.nurRedaktion(a.weiterleitungSpeichern))
	adm.HandleFunc("POST /admin/weiterleitungen/loeschen", a.nurRedaktion(a.weiterleitungLoeschen))
	adm.HandleFunc("GET /admin/export", a.nurRedaktion(a.export))
	adm.HandleFunc("GET /admin/vorschau/{id}", a.vorschau)
	adm.HandleFunc("GET /admin/medien", a.medienListe)
	adm.HandleFunc("POST /admin/medien", a.medienHochladen)
	adm.HandleFunc("POST /admin/medien/{id}/loeschen", a.nurRedaktion(a.medienLoeschen))
	adm.HandleFunc("GET /admin/einstellungen", a.nurAdmin(a.einstellungen))
	adm.HandleFunc("POST /admin/einstellungen", a.nurAdmin(a.einstellungenSpeichern))
	redaktion := nurAngemeldet(a.sso, adm)

	// Website: öffentlich.
	pub := http.NewServeMux()
	pub.HandleFunc("GET /healthz", gesund)
	pub.HandleFunc("GET /favicon.svg", logo)
	pub.HandleFunc("GET /favicon.ico", logo)
	pub.Handle("GET /statisch/", statisch())
	pub.HandleFunc("GET /medien/{id}", a.medium)
	pub.HandleFunc("GET /sitemap.xml", a.sitemap)
	pub.HandleFunc("GET /robots.txt", a.robots)
	pub.HandleFunc("GET /feed.xml", a.feed)
	pub.HandleFunc("GET /", a.oeffentlich)

	return sicherheitskopf(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/admin" || strings.HasPrefix(p, "/admin/") || strings.HasPrefix(p, "/sso/") || strings.HasPrefix(p, "/api/") {
			redaktion.ServeHTTP(w, r)
			return
		}
		pub.ServeHTTP(w, r)
	}))
}

func (a *App) angemeldet(h http.HandlerFunc) http.HandlerFunc {
	// Anmeldung und Konten verwalten nur Admins; vor der Einrichtung, wer den
	// Code aus dem Log hat.
	darf := func(w http.ResponseWriter, r *http.Request) bool {
		if !a.sso.Geschuetzt() && a.sso.EinrichtungErlaubt(w, r) {
			return true
		}
		if _, ok := a.sso.Wer(r); !ok {
			http.Error(w, "Bitte anmelden", http.StatusUnauthorized)
			return false
		}
		if !a.istAdmin(r) {
			http.Error(w, "Nur für Admins", http.StatusForbidden)
			return false
		}
		return true
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if !inhaltZuerst(w, r) {
			return
		}
		// Lesen und Testen ändern nichts und brauchen keine Sperre.
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.URL.Path == "/api/sso/testen" {
			if darf(w, r) {
				h(w, r)
			}
			return
		}
		// Der Anbieter wird vor der Sperre gefragt, und nur von Admins.
		if r.URL.Path == "/api/sso/einstellungen" {
			if !darf(w, r) {
				return
			}
			roh, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "Die Anfrage ist unvollständig", http.StatusBadRequest)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(roh))
			var ok bool
			if r, ok = a.sso.ausstellerVorab(w, r, roh); !ok {
				return
			}
		}
		// Erst sperren, dann prüfen: bis zum Speichern kann niemand die Rechte
		// entziehen, auf die sich die Prüfung stützt.
		a.verwaltung.fuehreAus(w, r, func(w http.ResponseWriter, r *http.Request) {
			if darf(w, r) {
				h(w, r)
			}
		})
	}
}

func (a *App) daten(r *http.Request, titel, nav string, d any) Daten {
	n, _ := a.sso.Wer(r)
	p, err := a.person(r)
	if err != nil {
		log.Printf("personen: %v", err)
	}
	return Daten{App: "Pagr", Titel: titel, Nav: nav, Nutzer: n, Admin: p.Rolle == rolleAdmin, Redaktion: p.darfVeroeffentlichen(), D: d}
}

func (a *App) wer(r *http.Request) string {
	n, _ := a.sso.Wer(r)
	if n.Name != "" {
		return n.Name
	}
	return n.Nutzer
}
