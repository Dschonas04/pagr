-- Datenbankschema von Pagr. Idempotent: wird bei jedem Start eingespielt.
CREATE TABLE IF NOT EXISTS seiten (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	art           text NOT NULL DEFAULT 'seite',     -- seite | beitrag
	pfad          text NOT NULL UNIQUE,              -- /ueber-uns, Beiträge unter /beitraege/<name>
	titel         text NOT NULL,
	inhalt        text NOT NULL DEFAULT '',          -- Markdown
	beschreibung  text NOT NULL DEFAULT '',
	status        text NOT NULL DEFAULT 'entwurf',   -- entwurf | veroeffentlicht
	im_menue      boolean NOT NULL DEFAULT false,
	menue_reihe   int NOT NULL DEFAULT 0,
	veroeffentlicht_am timestamptz,
	verfasser     text NOT NULL DEFAULT '',
	erstellt      timestamptz NOT NULL DEFAULT now(),
	geaendert     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS versionen (
	id        bigserial PRIMARY KEY,
	seite     uuid NOT NULL REFERENCES seiten(id) ON DELETE CASCADE,
	titel     text NOT NULL,
	inhalt    text NOT NULL,
	verfasser text NOT NULL DEFAULT '',
	erstellt  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS versionen_seite ON versionen(seite, erstellt DESC);

CREATE TABLE IF NOT EXISTS medien (
	id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	name      text NOT NULL,
	typ       text NOT NULL,
	groesse   bigint NOT NULL,
	erstellt  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS einstellungen (
	schluessel text PRIMARY KEY,
	wert       text NOT NULL
);
INSERT INTO einstellungen VALUES ('titel', 'Meine Website'), ('untertitel', ''), ('fuss', '')
	ON CONFLICT DO NOTHING;
INSERT INTO seiten (pfad, titel, inhalt, status, im_menue, veroeffentlicht_am)
	VALUES ('/', 'Willkommen', E'Diese Website läuft mit **Pagr**.\n\nInhalte pflegst du unter [/admin](/admin).', 'veroeffentlicht', true, now())
	ON CONFLICT DO NOTHING;
