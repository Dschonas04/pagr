package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"html/template"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Die öffentliche Website: eigene Vorlagen unter web/site, ohne die Leiste der
// Redaktion.
type siteVorlagen struct {
	seite, fehlt *template.Template
}

func ladeSite() *siteVorlagen {
	f := template.FuncMap{"tag": func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Local().Format("02.01.2006")
	}}
	laden := func(n string) *template.Template {
		return template.Must(template.New("rahmen.html").Funcs(f).ParseFS(webDateien, "web/site/rahmen.html", "web/site/"+n+".html"))
	}
	return &siteVorlagen{seite: laden("seite"), fehlt: laden("fehlt")}
}

type siteDaten struct {
	Titel, Untertitel, Fuss  string
	Design, Sprache, Adresse string
	Menue                    []Seite
	Uebersetzungen           []Seite
	Aktuell                  string
	S                        Seite
	HTML                     template.HTML
	Beitraege                []Seite
	Vorschau                 bool
}

func (a *App) hauptsprache(ctx context.Context) string {
	if s := a.werte(ctx)["sprache"]; slices.Contains(Sprachen, s) {
		return s
	}
	return "de"
}

// rahmen: Kopf und Menü in der Sprache der Seite, leer heißt Hauptsprache.
func (a *App) rahmen(ctx context.Context, pfad, sprache string) siteDaten {
	w := a.werte(ctx)
	if sprache == "" {
		if sprache = w["sprache"]; !slices.Contains(Sprachen, sprache) {
			sprache = "de"
		}
	}
	menue, err := a.seiten(ctx, "WHERE "+sichtbar+" AND im_menue AND sprache=$1 ORDER BY menue_reihe, titel", sprache)
	if err != nil {
		log.Printf("Website, Menü: %v", err)
	}
	return siteDaten{Titel: w["titel"], Untertitel: w["untertitel"], Fuss: w["fuss"], Design: w["design"], Sprache: sprache,
		Menue: menue, Aktuell: pfad}
}

func (a *App) oeffentlich(w http.ResponseWriter, r *http.Request) {
	pfad := strings.TrimSuffix(r.URL.Path, "/")
	if pfad == "" {
		pfad = "/"
	}
	s, err := scanSeite(a.db.QueryRow(r.Context(), "SELECT "+seitenSpalten+" FROM seiten WHERE pfad=$1 AND "+sichtbar, pfad))
	if err != nil {
		var nach string
		if a.db.QueryRow(r.Context(), `SELECT nach FROM weiterleitungen WHERE von=$1`, pfad).Scan(&nach) == nil && zielGueltig(nach) {
			w.Header().Set("Cache-Control", "public, max-age=60")
			http.Redirect(w, r, nach, http.StatusMovedPermanently)
			return
		}
		d := a.rahmen(r.Context(), r.URL.Path, "")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		if err := a.site.fehlt.Execute(w, d); err != nil {
			log.Printf("Website: %v", err)
		}
		return
	}
	a.seiteAusliefern(w, r, s, false)
}

func (a *App) seiteHTML(ctx context.Context, basis string, s Seite, vorschau bool) ([]byte, error) {
	d := a.rahmen(ctx, s.Pfad, s.Sprache)
	d.S, d.HTML, d.Vorschau, d.Adresse = s, mdHTML(s.Inhalt), vorschau, basis+s.Pfad
	var err error
	// Die Seite /beitraege (oder jede Seite mit [beitraege] im Text) listet die Beiträge ihrer Sprache.
	if s.Pfad == "/beitraege" || strings.Contains(s.Inhalt, "[beitraege]") {
		if d.Beitraege, err = a.seiten(ctx, "WHERE art='beitrag' AND "+sichtbar+" AND sprache=$1 ORDER BY veroeffentlicht_am DESC LIMIT 100", s.Sprache); err != nil {
			return nil, err
		}
		d.HTML = template.HTML(strings.ReplaceAll(string(d.HTML), "[beitraege]", ""))
	}
	wurzel := s.UebersetzungVon
	if wurzel == "" {
		wurzel = s.ID
	}
	if d.Uebersetzungen, err = a.seiten(ctx, "WHERE (id::text=$1 OR uebersetzung_von::text=$1) AND id::text<>$2 AND "+sichtbar+" ORDER BY sprache", wurzel, s.ID); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	if err := a.site.seite.Execute(&b, d); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (a *App) seiteAusliefern(w http.ResponseWriter, r *http.Request, s Seite, vorschau bool) {
	b, err := a.seiteHTML(r.Context(), a.basis(r), s, vorschau)
	if err != nil {
		log.Printf("Website: %v", err)
		http.Error(w, "Die Seite geht gerade nicht", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if vorschau {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=60")
	}
	w.Write(b)
}

func (a *App) basis(r *http.Request) string {
	if b := strings.TrimRight(a.werte(r.Context())["adresse"], "/"); b != "" {
		return b
	}
	schema := "http"
	if ssoSicher(r) {
		schema = "https"
	}
	return schema + "://" + r.Host
}

func (a *App) sitemapXML(ctx context.Context, basis string) ([]byte, error) {
	l, err := a.seiten(ctx, "WHERE "+sichtbar+" ORDER BY pfad")
	if err != nil {
		return nil, err
	}
	type eintrag struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	}
	type urlset struct {
		XMLName xml.Name  `xml:"urlset"`
		NS      string    `xml:"xmlns,attr"`
		URLs    []eintrag `xml:"url"`
	}
	u := urlset{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, s := range l {
		u.URLs = append(u.URLs, eintrag{basis + s.Pfad, s.Geaendert.Format("2006-01-02")})
	}
	b := bytes.NewBufferString(xml.Header)
	if err := xml.NewEncoder(b).Encode(u); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (a *App) sitemap(w http.ResponseWriter, r *http.Request) {
	b, err := a.sitemapXML(r.Context(), a.basis(r))
	if err != nil {
		http.Error(w, "Die Sitemap geht gerade nicht", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Write(b)
}

func (a *App) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("User-agent: *\nDisallow: /admin\nDisallow: /sso/\nSitemap: " + a.basis(r) + "/sitemap.xml\n"))
}
