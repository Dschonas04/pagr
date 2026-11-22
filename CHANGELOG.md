# Änderungen

## Unveröffentlicht

- Sicherheit: Mit einem Konto enden seine Rechte; geprüft wird erst, wenn die Anfrage vollständig da ist.
- Sicherheit: Admin wird man nur mit dem Einrichtungscode oder über `PAGR_ADMIN`, nicht durch die erste Anmeldung. Kennungen von eigenen Konten und SSO sind getrennt.
- Oberfläche überarbeitet: Kopfzeile, Tabellen und Felder am Handy, Anmeldeseite in den Farben der App.
- Logo auf der Anmeldeseite wird wieder ausgeliefert, der Hinweis zur Ersteinrichtung stimmt.
- Sicherheit: Anmeldung, Konten und Verwaltung nur noch für Admins.
- Seiten und Beiträge in Markdown (GFM, ohne rohes HTML), Pfade aus dem Titel, Entwurf und Veröffentlichen, Vorschau für Entwürfe.
- Versionen: jede gespeicherte Fassung, die letzten 50 je Seite, mit Zurückholen.
- Medienbibliothek: Bilder, PDF, Video, Audio, ZIP bis 64 MB; SVG und HTML werden abgelehnt. Einfügen per Klick im Editor.
- Menü aus Seiten, Beitragsliste unter `/beitraege` oder per `[beitraege]`, Sitemap, robots.txt, Beschreibung und Open Graph.
- Redaktion unter `/admin` hinter der Anmeldung (Keycloak oder eigene Konten), Website öffentlich.
- Grundgerüst: Server mit Startseite, /healthz und /api/version, Schemaentwurf, Docker, Prüfung in GitHub Actions.
