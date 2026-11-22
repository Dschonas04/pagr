package main

import (
	"net/http"
)

// Rollen: Autoren schreiben eigene Entwürfe, die Redaktion veröffentlicht,
// Admins verwalten dazu die App.
const (
	rolleAutor     = "autor"
	rolleRedaktion = "redaktion"
	rolleAdmin     = "admin"
)

type Person struct{ Kennung, Name, Rolle string }

func (p Person) darfVeroeffentlichen() bool {
	return p.Rolle == rolleRedaktion || p.Rolle == rolleAdmin
}

// darfAendern: Autoren nur ihre eigenen Entwürfe.
func (p Person) darfAendern(s Seite) bool {
	return p.darfVeroeffentlichen() || (p.Kennung != "" && s.Besitzer == p.Kennung && s.Status == "entwurf")
}

func (a *App) person(r *http.Request) (Person, error) {
	n, ok := a.sso.Wer(r)
	if !ok {
		if a.sso.notaus {
			return Person{Rolle: rolleAdmin}, nil
		}
		return Person{}, nil
	}
	p := Person{Kennung: n.Kennung, Name: a.wer(r)}
	if err := a.db.QueryRow(r.Context(), `INSERT INTO personen (kennung, name) VALUES ($1, $2)
		ON CONFLICT (kennung) DO UPDATE SET name = $2, zuletzt = now() RETURNING rolle`, p.Kennung, p.Name).Scan(&p.Rolle); err != nil {
		return Person{}, err
	}
	if a.istAdmin(r) {
		p.Rolle = rolleAdmin
	}
	return p, nil
}

func (a *App) nurRedaktion(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p, err := a.person(r); err != nil || !p.darfVeroeffentlichen() {
			http.Error(w, "Nur für die Redaktion", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func (a *App) personenSeite(w http.ResponseWriter, r *http.Request) {
	rows, err := a.db.Query(r.Context(), `SELECT p.kennung, p.name,
		CASE WHEN p.kennung = ANY($1::text[]) OR EXISTS (SELECT 1 FROM admins a WHERE a.kennung = p.kennung) THEN 'admin' ELSE p.rolle END
		FROM personen p ORDER BY p.name, p.kennung`, append([]string{}, a.admins...))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	var l []Person
	for rows.Next() {
		var p Person
		if err := rows.Scan(&p.Kennung, &p.Name, &p.Rolle); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		l = append(l, p)
	}
	a.v.zeigen(w, r, "personen", a.daten(r, "Personen", "einstellungen", map[string]any{"Personen": l}))
}

// personSetzen läuft unter der Sperre der Verwaltung.
func (a *App) personSetzen(w http.ResponseWriter, r *http.Request) {
	if !inhaltZuerst(w, r) {
		return
	}
	a.verwaltung.fuehreAus(w, r, a.personAendern)
}

func (a *App) personAendern(w http.ResponseWriter, r *http.Request) {
	if !a.istAdmin(r) {
		http.Error(w, "Nur für Admins", http.StatusForbidden)
		return
	}
	if !pruefeFormular(w, r) {
		return
	}
	rolle := r.FormValue("rolle")
	if rolle != rolleAutor && rolle != rolleRedaktion {
		zurueck(w, r, "/admin/personen", "Diese Rolle gibt es nicht")
		return
	}
	if _, err := a.db.Exec(r.Context(), `UPDATE personen SET rolle = $2 WHERE kennung = $1`, r.FormValue("kennung"), rolle); err != nil {
		zurueck(w, r, "/admin/personen", "Fehler: "+err.Error())
		return
	}
	zurueck(w, r, "/admin/personen", "Rolle gespeichert")
}
