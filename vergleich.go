package main

import (
	"net/http"
	"strings"
)

type VergleichZeile struct{ Art, Text string }

const vergleichMax = 2000 // Zeilen je Seite, darüber wird nicht gerechnet

// vergleichen: zeilenweise über die längste gemeinsame Folge.
func vergleichen(alt, neu string) ([]VergleichZeile, bool) {
	a, b := strings.Split(alt, "\n"), strings.Split(neu, "\n")
	if len(a) > vergleichMax || len(b) > vergleichMax {
		return nil, false
	}
	breite := len(b) + 1
	g := make([]uint16, (len(a)+1)*breite)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				g[i*breite+j] = g[(i+1)*breite+j+1] + 1
			} else {
				g[i*breite+j] = max(g[(i+1)*breite+j], g[i*breite+j+1])
			}
		}
	}
	var l []VergleichZeile
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			l = append(l, VergleichZeile{"", a[i]})
			i, j = i+1, j+1
		case g[(i+1)*breite+j] >= g[i*breite+j+1]:
			l = append(l, VergleichZeile{"minus", a[i]})
			i++
		default:
			l = append(l, VergleichZeile{"plus", b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		l = append(l, VergleichZeile{"minus", a[i]})
	}
	for ; j < len(b); j++ {
		l = append(l, VergleichZeile{"plus", b[j]})
	}
	return l, true
}

func (a *App) versionVergleich(w http.ResponseWriter, r *http.Request) {
	s, err := a.seiteMit(r.Context(), r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var v Version
	if err := a.db.QueryRow(r.Context(), `SELECT id, titel, inhalt, verfasser, erstellt FROM versionen WHERE id::text = $1 AND seite = $2::uuid`,
		r.PathValue("v"), s.ID).Scan(&v.ID, &v.Titel, &v.Inhalt, &v.Verfasser, &v.Erstellt); err != nil {
		http.NotFound(w, r)
		return
	}
	zeilen, ok := vergleichen(v.Inhalt, s.Inhalt)
	a.v.zeigen(w, r, "vergleich", a.daten(r, "Vergleich", "seiten", map[string]any{"S": s, "V": v, "Zeilen": zeilen, "Geht": ok}))
}
