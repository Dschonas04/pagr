package main

import (
	"net/http"
	"net/url"
	"strings"
)

type Weiterleitung struct{ Von, Nach string }

// zielGueltig: ein Pfad dieser Website oder eine ganze http(s)-Adresse.
// Alles, was ein Browser als fremden Host lesen könnte (//host, /\host), fällt raus.
func zielGueltig(z string) bool {
	if strings.HasPrefix(z, "/") {
		return !strings.HasPrefix(z, "//") && pfadMuster.MatchString(z)
	}
	if strings.ContainsAny(z, "\\\r\n\t ") {
		return false
	}
	u, err := url.Parse(z)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}

func (a *App) weiterleitungen(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `SELECT von, nach FROM weiterleitungen ORDER BY von`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var l []Weiterleitung
	for rows.Next() {
		var x Weiterleitung
		if err := rows.Scan(&x.Von, &x.Nach); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		l = append(l, x)
	}
	a.v.zeigen(w, r, "weiterleitungen", a.daten(r, "Weiterleitungen", "weiterleitungen", map[string]any{"Liste": l}))
}

func (a *App) weiterleitungSpeichern(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	const hier = "/admin/weiterleitungen"
	von, nach := strings.TrimSpace(r.FormValue("von")), strings.TrimSpace(r.FormValue("nach"))
	if !pfadMuster.MatchString(von) || von == "/" || strings.HasPrefix(von, "//") || reserviert(von) || !zielGueltig(nach) || von == nach {
		zurueck(w, r, hier, "Von ist ein Pfad, das Ziel ein Pfad oder eine https-Adresse")
		return
	}
	if _, err := a.db.Exec(r.Context(), `INSERT INTO weiterleitungen VALUES ($1, $2) ON CONFLICT (von) DO UPDATE SET nach = $2`, von, nach); err != nil {
		zurueck(w, r, hier, "Fehler: "+err.Error())
		return
	}
	zurueck(w, r, hier, "Weiterleitung gespeichert")
}

func (a *App) weiterleitungLoeschen(w http.ResponseWriter, r *http.Request) {
	if !pruefeFormular(w, r) {
		return
	}
	if _, err := a.db.Exec(r.Context(), `DELETE FROM weiterleitungen WHERE von = $1`, r.FormValue("von")); err != nil {
		zurueck(w, r, "/admin/weiterleitungen", "Fehler: "+err.Error())
		return
	}
	zurueck(w, r, "/admin/weiterleitungen", "Weiterleitung gelöscht")
}
