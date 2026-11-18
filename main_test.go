package main

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSeitenRendern(t *testing.T) {
	v := ladeVorlagen()
	jetzt := time.Now()
	s := Seite{ID: "s1", Art: "beitrag", Pfad: "/beitraege/hallo", Titel: "Hallo", Inhalt: "# Hi\n\n**fett**", Status: "veroeffentlicht",
		VeroeffentlichtAm: &jetzt, Erstellt: jetzt, Geaendert: jetzt, ImMenue: true}
	m := []Medium{{ID: "m1", Name: "bild.png", Typ: "image/png", Groesse: 2048, Erstellt: jetzt}}
	faelle := map[string]map[string]any{
		"liste":         {"Seiten": []Seite{s}},
		"seite":         {"S": s, "Medien": m},
		"versionen":     {"S": s, "Versionen": []Version{{ID: 1, Titel: "Hallo", Inhalt: "alt", Erstellt: jetzt}}},
		"medien":        {"Medien": m},
		"einstellungen": {"W": map[string]string{"titel": "Test"}},
	}
	for name, d := range faelle {
		rec := httptest.NewRecorder()
		v.zeigen(rec, httptest.NewRequest("GET", "/", nil), name, Daten{App: "Pagr", D: d})
		if b := rec.Body.String(); !strings.Contains(b, "</html>") {
			t.Errorf("%s bricht ab: %s", name, b[max(0, len(b)-300):])
		}
	}
	site := ladeSite()
	for _, d := range []siteDaten{{Titel: "Web", Menue: []Seite{s}, S: s, HTML: mdHTML(s.Inhalt), Beitraege: []Seite{s}, Vorschau: true}, {Titel: "Web"}} {
		var b strings.Builder
		if err := site.seite.Execute(&b, d); err != nil || !strings.Contains(b.String(), "</html>") {
			t.Errorf("Website: %v", err)
		}
	}
	var b strings.Builder
	if err := site.fehlt.Execute(&b, siteDaten{Titel: "Web"}); err != nil {
		t.Error(err)
	}
}

func TestMarkdownOhneSkript(t *testing.T) {
	h := string(mdHTML("Hallo <script>alert(1)</script> **x** [a](javascript:alert(1))"))
	if strings.Contains(h, "<script>") || strings.Contains(h, `href="javascript`) {
		t.Fatalf("Skript durchgelassen: %s", h)
	}
	if !strings.Contains(h, "<strong>x</strong>") {
		t.Fatal(h)
	}
}

func TestPfadAus(t *testing.T) {
	for ein, aus := range map[string]string{"Über uns": "/ueber-uns", "beitraege/Neues Jahr!": "/beitraege/neues-jahr", "  /a//b  ": "/a/b"} {
		if p := pfadAus(ein); p != aus {
			t.Errorf("%q: %q, erwartet %q", ein, p, aus)
		}
	}
}
