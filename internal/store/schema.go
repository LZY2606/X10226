package store

const schema = `
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	nonce TEXT NOT NULL,
	created_at TEXT NOT NULL,
	seq INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS events (
	project_id TEXT NOT NULL,
	seq INTEGER NOT NULL,
	id TEXT NOT NULL,
	kind TEXT NOT NULL,
	occurred_at TEXT NOT NULL,
	actor TEXT NOT NULL,
	payload TEXT NOT NULL,
	refs_json TEXT NOT NULL DEFAULT '[]',
	PRIMARY KEY (project_id, seq)
);

-- Immutable raw-input versions. Never updated after insert.
CREATE TABLE IF NOT EXISTS sources (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	version INTEGER NOT NULL,
	parent_id TEXT NOT NULL DEFAULT '',
	origin TEXT NOT NULL,
	submitted_at TEXT NOT NULL,
	data_json TEXT NOT NULL,
	diag_json TEXT NOT NULL DEFAULT '[]',
	UNIQUE (project_id, kind, version)
);

-- Rejected import fragments (half-failure evidence): input preserved.
CREATE TABLE IF NOT EXISTS fragments (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	batch_id TEXT NOT NULL,
	kind_hint TEXT NOT NULL,
	origin TEXT NOT NULL,
	raw_snippet TEXT NOT NULL,
	diag_json TEXT NOT NULL,
	occurred_at TEXT NOT NULL
);

-- Immutable computed artifacts, content-addressed.
CREATE TABLE IF NOT EXISTS derived (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	algo_version TEXT NOT NULL,
	param_fingerprint TEXT NOT NULL,
	input_ids_json TEXT NOT NULL,
	produced_by_event INTEGER NOT NULL,
	payload_json TEXT NOT NULL
);

-- Which derived artifact each kind points to at each event (time travel).
CREATE TABLE IF NOT EXISTS pointers (
	project_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	as_of_event INTEGER NOT NULL,
	derived_id TEXT NOT NULL,
	PRIMARY KEY (project_id, kind, as_of_event)
);

CREATE TABLE IF NOT EXISTS batches (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	seq INTEGER NOT NULL,
	committed_json TEXT NOT NULL DEFAULT '[]',
	rejected_json TEXT NOT NULL DEFAULT '[]',
	occurred_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events ON events(project_id, seq);
CREATE INDEX IF NOT EXISTS idx_sources ON sources(project_id, kind, version);
CREATE INDEX IF NOT EXISTS idx_pointers ON pointers(project_id, kind, as_of_event);
`
