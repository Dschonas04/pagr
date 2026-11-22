package main

import (
	"archive/zip"
	"bytes"
	"context"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var weiterSeite = template.Must(template.New("weiter").Parse(`<!doctype html><html lang="de"><meta charset="utf-8">
<meta http-equiv="refresh" content="0; url={{.}}"><link rel="canonical" href="{{.}}"><a href="{{.}}">Weiter</a></html>
`))

// export liefert die veröffentlichte Website als ZIP für jeden Webserver.
func (a *App) export(w http.ResponseWriter, r *http.Request) {
	a.exportSperre.Lock()
	defer a.exportSperre.Unlock()
	t, err := os.CreateTemp("", "pagr-export-*.zip")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer os.Remove(t.Name())
	defer t.Close()
	if err := a.exportSchreiben(r.Context(), a.basis(r), t); err != nil {
		log.Printf("export: %v", err)
		http.Error(w, "Der Export ging nicht", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="website.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", time.Time{}, t)
}

func (a *App) exportSchreiben(ctx context.Context, basis string, ziel io.Writer) error {
	z := zip.NewWriter(ziel)
	schreiben := func(name string, b []byte) error {
		f, err := z.Create(name)
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		return err
	}
	datei := func(pfad string) string { return strings.TrimPrefix(strings.TrimSuffix(pfad, "/")+"/index.html", "/") }

	seiten, err := a.seiten(ctx, "WHERE "+sichtbar+" ORDER BY pfad")
	if err != nil {
		return err
	}
	belegt := map[string]bool{}
	for _, s := range seiten {
		b, err := a.seiteHTML(ctx, basis, s, false)
		if err != nil {
			return err
		}
		if err := schreiben(datei(s.Pfad), b); err != nil {
			return err
		}
		belegt[datei(s.Pfad)] = true
	}

	rows, err := a.db.Query(ctx, `SELECT von, nach FROM weiterleitungen`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var von, nach string
		if err := rows.Scan(&von, &nach); err != nil {
			return err
		}
		if belegt[datei(von)] {
			continue
		}
		var b bytes.Buffer
		if err := weiterSeite.Execute(&b, nach); err != nil {
			return err
		}
		if err := schreiben(datei(von), b.Bytes()); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	var fehlt bytes.Buffer
	if err := a.site.fehlt.Execute(&fehlt, a.rahmen(ctx, "/404", "")); err != nil {
		return err
	}
	if err := schreiben("404.html", fehlt.Bytes()); err != nil {
		return err
	}
	karte, err := a.sitemapXML(ctx, basis)
	if err != nil {
		return err
	}
	if err := schreiben("sitemap.xml", karte); err != nil {
		return err
	}
	f, err := a.feedXML(ctx, basis)
	if err != nil {
		return err
	}
	if err := schreiben("feed.xml", f); err != nil {
		return err
	}
	if err := schreiben("robots.txt", []byte("User-agent: *\nAllow: /\nSitemap: "+basis+"/sitemap.xml\n")); err != nil {
		return err
	}
	for von, nach := range map[string]string{"web/statisch/site.css": "statisch/site.css", "web/statisch/zeichen.svg": "favicon.svg"} {
		b, err := webDateien.ReadFile(von)
		if err != nil {
			return err
		}
		if err := schreiben(nach, b); err != nil {
			return err
		}
	}

	// Nur Medien, die auf einer veröffentlichten Seite stehen.
	medien, err := a.medienAlle(ctx)
	if err != nil {
		return err
	}
	for _, m := range medien {
		gebraucht := false
		for _, s := range seiten {
			gebraucht = gebraucht || strings.Contains(s.Inhalt, "/medien/"+m.ID)
		}
		if !gebraucht {
			continue
		}
		q, err := os.Open(filepath.Join(a.medien, m.ID))
		if os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		f, err := z.Create("medien/" + m.ID)
		if err == nil {
			_, err = io.Copy(f, q)
		}
		q.Close()
		if err != nil {
			return err
		}
	}
	return z.Close()
}
