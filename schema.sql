-- Datenbankschema von Pagr (Entwurf).
CREATE TABLE IF NOT EXISTS seiten (
	id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	eltern        uuid REFERENCES seiten(id) ON DELETE SET NULL,
	art           text NOT NULL DEFAULT 'seite',   -- seite | beitrag
	sprache       text NOT NULL DEFAULT 'de',
	pfad          text NOT NULL,                   -- /ueber-uns
	titel         text NOT NULL,
	inhalt        jsonb NOT NULL DEFAULT '[]',     -- Blöcke
	beschreibung  text NOT NULL DEFAULT '',
	status        text NOT NULL DEFAULT 'entwurf', -- entwurf | geplant | veroeffentlicht
	veroeffentlicht_ab timestamptz,
	geaendert     timestamptz NOT NULL DEFAULT now(),
	UNIQUE (sprache, pfad)
);

CREATE TABLE IF NOT EXISTS versionen (
	id        bigserial PRIMARY KEY,
	seite     uuid NOT NULL REFERENCES seiten(id) ON DELETE CASCADE,
	titel     text NOT NULL,
	inhalt    jsonb NOT NULL,
	erstellt  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS medien (
	id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
	name      text NOT NULL,
	typ       text NOT NULL,
	groesse   bigint NOT NULL,
	alt       text NOT NULL DEFAULT '',
	erstellt  timestamptz NOT NULL DEFAULT now()
);
