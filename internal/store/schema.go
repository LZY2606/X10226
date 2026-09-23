package store

const schemaSQL = `
CREATE TABLE IF NOT EXISTS projects (
	id           TEXT PRIMARY KEY,
	name         TEXT NOT NULL,
	kind         TEXT NOT NULL,        -- review | counterexample
	description  TEXT NOT NULL DEFAULT '',
	created_at   TEXT NOT NULL,
	created_event TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
	id          TEXT PRIMARY KEY,
	project_id  TEXT NOT NULL REFERENCES projects(id),
	seq         INTEGER NOT NULL,
	kind        TEXT NOT NULL,
	at          TEXT NOT NULL,
	actor       TEXT NOT NULL,
	payload     TEXT NOT NULL,
	prev_hash   TEXT NOT NULL,
	hash        TEXT NOT NULL,
	reverted_by TEXT NOT NULL DEFAULT '',
	UNIQUE(project_id, seq)
);
CREATE INDEX IF NOT EXISTS events_project ON events(project_id, seq);

CREATE TABLE IF NOT EXISTS raw_batches (
	id              TEXT PRIMARY KEY,
	project_id      TEXT NOT NULL REFERENCES projects(id),
	layer           TEXT NOT NULL,
	payload         TEXT NOT NULL,
	supersedes      TEXT NOT NULL DEFAULT '',
	import_seq      INTEGER NOT NULL,
	committed_at    TEXT NOT NULL,
	partial_failure TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS raw_batches_project ON raw_batches(project_id, layer, import_seq);

CREATE TABLE IF NOT EXISTS raw_items (
	id          TEXT PRIMARY KEY,
	batch_id    TEXT NOT NULL REFERENCES raw_batches(id),
	project_id  TEXT NOT NULL REFERENCES projects(id),
	layer       TEXT NOT NULL,
	line_no     INTEGER NOT NULL,
	fields      TEXT NOT NULL,
	status      TEXT NOT NULL,
	diagnostic  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS raw_items_batch ON raw_items(batch_id, line_no);
CREATE INDEX IF NOT EXISTS raw_items_project ON raw_items(project_id, layer);

CREATE TABLE IF NOT EXISTS artifacts (
	id            TEXT PRIMARY KEY,
	project_id    TEXT NOT NULL REFERENCES projects(id),
	kind          TEXT NOT NULL,
	subject       TEXT NOT NULL DEFAULT '',
	algo_version  TEXT NOT NULL,
	param_fp      TEXT NOT NULL,
	sources       TEXT NOT NULL,
	status        TEXT NOT NULL,
	created_event TEXT NOT NULL,
	superseded_by TEXT NOT NULL DEFAULT '',
	payload       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS artifacts_project ON artifacts(project_id, kind, status);
CREATE INDEX IF NOT EXISTS artifacts_created ON artifacts(created_event);

CREATE TABLE IF NOT EXISTS decisions (
	id            TEXT PRIMARY KEY,
	project_id    TEXT NOT NULL REFERENCES projects(id),
	target_kind   TEXT NOT NULL,
	target_id     TEXT NOT NULL,
	verdict       TEXT NOT NULL,
	reason        TEXT NOT NULL,
	adjustments   TEXT NOT NULL DEFAULT '{}',
	artifact_id   TEXT NOT NULL,
	conflict_key  TEXT NOT NULL DEFAULT '',
	superseded_by TEXT NOT NULL DEFAULT '',
	event_id      TEXT NOT NULL,
	at            TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS decisions_target ON decisions(project_id, target_kind, target_id);
`
