# Pagr

Content-Management-System für Websites: Seiten, Beiträge und Medien pflegen, sauber ausliefern.

**Stand: Grundgerüst.** Der Server startet und zeigt eine Startseite, die
Funktionen unten sind geplant.

## Geplant

* **Seiten und Beiträge** mit Blockeditor, Entwürfen und zeitgesteuerter Veröffentlichung
* **Versionen** jeder Seite, Vergleich und Zurückholen
* **Medienbibliothek** mit Bildgrößen und WebP, Ablage lokal oder in S3 (MinIO)
* **Menüs und Seitenbaum**, sprechende Adressen, Weiterleitungen
* **Designs** als Go-Templates, ein schlichtes Standarddesign dabei
* **Mehrsprachig**: jede Seite in mehreren Sprachen, Sprachwahl im Menü
* **SEO**: Titel, Beschreibung, Sitemap, Open Graph, schnelle Auslieferung mit Cache
* **Rollen** Autor, Redaktion, Admin; Anmeldung über Keycloak
* **Statischer Export** der ganzen Website als Alternative zur Live-Auslieferung

## Schritte

1. Datenmodell, Seiten anlegen und als HTML ausliefern
2. Redaktionsoberfläche mit Blockeditor und Entwürfen
3. Medienbibliothek
4. Menüs, Designs, Sitemap
5. Rollen und Keycloak
6. Versionen, Mehrsprachigkeit, statischer Export

## Starten

```bash
cp .env.beispiel .env            # Kennwort für die Datenbank eintragen
docker compose up -d --build
```

Danach unter `http://<host>:8360`. Im Homelab kommt TLS über Caddy mit
der Homelab-CA davor, der Dienst selbst lauscht dann auf `127.0.0.1:28360`.

## Entwickeln

```bash
go run .
go test ./...
```

Go 1.27.4, Datenbank PostgreSQL. Das Schema steht in `schema.sql`.

## Lizenz

MIT, siehe [LICENSE](LICENSE).
