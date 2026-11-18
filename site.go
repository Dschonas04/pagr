package main

import (
	"encoding/xml"
	"html/template"
	"log"
	"net/http"
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
	Titel, Untertitel, Fuss string
	Menue                   []Seite
	Aktuell                 string
	S                       Seite
	HTML                    template.HTML
	Beitraege               []Seite
	Vorschau                bool
}

func (a *App) rahmen(r *http.Request) siteDaten {
	w := a.werte(r.Context())
	menue, _ := a.seiten(r.Context(), "WHERE status='veroeffentlicht' AND im_menue ORDER BY menue_reihe, titel")
	return siteDaten{Titel: w["titel"], Untertitel: w["untertitel"], Fuss: w["fuss"], Menue: menue, Aktuell: r.URL.Path}
}

func (a *App) oeffentlich(w http.ResponseWriter, r *http.Request) {
	pfad := strings.TrimSuffix(r.URL.Path, "/")
	if pfad == "" {
		pfad = "/"
	}
	s, err := scanSeite(a.db.QueryRow(r.Context(), "SELECT "+seitenSpalten+" FROM seiten WHERE pfad=$1 AND status='veroeffentlicht'", pfad))
	if err != nil {
		d := a.rahmen(r)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		a.site.fehlt.Execute(w, d)
		return
	}
	a.seiteAusliefern(w, r, s, false)
}

func (a *App) seiteAusliefern(w http.ResponseWriter, r *http.Request, s Seite, vorschau bool) {
	d := a.rahmen(r)
	d.S, d.HTML, d.Vorschau = s, mdHTML(s.Inhalt), vorschau
	d.Aktuell = s.Pfad
	// Die Seite /beitraege (oder jede Seite mit [beitraege] im Text) listet die Beiträge.
	if s.Pfad == "/beitraege" || strings.Contains(s.Inhalt, "[beitraege]") {
		d.Beitraege, _ = a.seiten(r.Context(), "WHERE art='beitrag' AND status='veroeffentlicht' ORDER BY veroeffentlicht_am DESC LIMIT 100")
		d.HTML = template.HTML(strings.ReplaceAll(string(d.HTML), "[beitraege]", ""))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if vorschau {
		w.Header().Set("Cache-Control", "no-store")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=60")
	}
	if err := a.site.seite.Execute(w, d); err != nil {
		log.Printf("Website: %v", err)
	}
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

func (a *App) sitemap(w http.ResponseWriter, r *http.Request) {
	l, _ := a.seiten(r.Context(), "WHERE status='veroeffentlicht' ORDER BY pfad")
	type url struct {
		Loc     string `xml:"loc"`
		LastMod string `xml:"lastmod"`
	}
	type urlset struct {
		XMLName xml.Name `xml:"urlset"`
		NS      string   `xml:"xmlns,attr"`
		URLs    []url    `xml:"url"`
	}
	b := a.basis(r)
	u := urlset{NS: "http://www.sitemaps.org/schemas/sitemap/0.9"}
	for _, s := range l {
		u.URLs = append(u.URLs, url{b + s.Pfad, s.Geaendert.Format("2006-01-02")})
	}
	w.Header().Set("Content-Type", "application/xml")
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(u)
}

func (a *App) robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("User-agent: *\nDisallow: /admin\nDisallow: /sso/\nSitemap: " + a.basis(r) + "/sitemap.xml\n"))
}
