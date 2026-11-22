package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"net/http"
	"time"
)

type rssEintrag struct {
	Titel        string `xml:"title"`
	Adresse      string `xml:"link"`
	GUID         string `xml:"guid"`
	Wann         string `xml:"pubDate,omitempty"`
	Beschreibung string `xml:"description"`
}

type rss struct {
	XMLName      xml.Name     `xml:"rss"`
	Fassung      string       `xml:"version,attr"`
	Titel        string       `xml:"channel>title"`
	Adresse      string       `xml:"channel>link"`
	Beschreibung string       `xml:"channel>description"`
	Eintraege    []rssEintrag `xml:"channel>item"`
}

func (a *App) feedXML(ctx context.Context, basis string) ([]byte, error) {
	l, err := a.seiten(ctx, "WHERE art = 'beitrag' AND "+sichtbar+" ORDER BY veroeffentlicht_am DESC LIMIT 30")
	if err != nil {
		return nil, err
	}
	w := a.werte(ctx)
	f := rss{Fassung: "2.0", Titel: w["titel"], Adresse: basis + "/", Beschreibung: w["untertitel"]}
	for _, s := range l {
		e := rssEintrag{Titel: s.Titel, Adresse: basis + s.Pfad, GUID: basis + s.Pfad, Beschreibung: s.Beschreibung}
		if s.VeroeffentlichtAm != nil {
			e.Wann = s.VeroeffentlichtAm.Format(time.RFC1123Z)
		}
		f.Eintraege = append(f.Eintraege, e)
	}
	b := bytes.NewBufferString(xml.Header)
	if err := xml.NewEncoder(b).Encode(f); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (a *App) feed(w http.ResponseWriter, r *http.Request) {
	b, err := a.feedXML(r.Context(), a.basis(r))
	if err != nil {
		http.Error(w, "Der Feed geht gerade nicht", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write(b)
}
