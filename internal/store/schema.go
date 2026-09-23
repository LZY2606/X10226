package store

const schema = `
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
	project_id TEXT NOT NULL,
	seq INTEGER NOT NULL,
	id TEXT NOT NULL,
	type TEXT NOT NULL,
	payload TEXT NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY (project_id, seq)
);
CREATE TABLE IF NOT EXISTS sources (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	layer TEXT NOT NULL,
	version INTEGER NOT NULL,
	parent_id TEXT,
	format TEXT NOT NULL,
	raw TEXT NOT NULL,
	payload TEXT NOT NULL,
	diag TEXT NOT NULL,
	accepted INTEGER NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	status TEXT NOT NULL,
	submitted_at TEXT NOT NULL,
	fragments TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS params_versions (
	project_id TEXT NOT NULL,
	version INTEGER NOT NULL,
	fingerprint TEXT NOT NULL,
	parent_fp TEXT,
	json TEXT NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY (project_id, version)
);
CREATE TABLE IF NOT EXISTS nodes (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	kind TEXT NOT NULL,
	algo TEXT NOT NULL,
	algo_version TEXT NOT NULL,
	param_fp TEXT NOT NULL,
	inputs TEXT NOT NULL,
	payload TEXT NOT NULL,
	diag TEXT NOT NULL,
	status TEXT NOT NULL,
	current INTEGER NOT NULL,
	superseded_by TEXT,
	created_event_seq INTEGER NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS node_edges (
	project_id TEXT NOT NULL,
	from_id TEXT NOT NULL,
	to_id TEXT NOT NULL,
	PRIMARY KEY (project_id, from_id, to_id)
);
CREATE TABLE IF NOT EXISTS decisions (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	seq INTEGER NOT NULL,
	kind TEXT NOT NULL,
	target TEXT,
	payload TEXT NOT NULL,
	node_ids TEXT NOT NULL,
	param_fp TEXT NOT NULL,
	rationale TEXT,
	supersedes TEXT,
	live INTEGER NOT NULL,
	created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS conflicts (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	seq INTEGER NOT NULL,
	stage_key TEXT NOT NULL,
	decision_ids TEXT NOT NULL,
	status TEXT NOT NULL,
	note TEXT,
	created_at TEXT NOT NULL,
	resolved_at TEXT
);
CREATE TABLE IF NOT EXISTS counters (
	project_id TEXT NOT NULL,
	name TEXT NOT NULL,
	value INTEGER NOT NULL,
	PRIMARY KEY (project_id, name)
);
CREATE TABLE IF NOT EXISTS archive_entries (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	scenario TEXT NOT NULL,
	title TEXT NOT NULL,
	status TEXT NOT NULL,
	changed_source TEXT,
	before_json TEXT,
	after_json TEXT,
	rules_json TEXT,
	scope_json TEXT,
	adopted INTEGER NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sources_project ON sources(project_id, layer, version);
CREATE INDEX IF NOT EXISTS idx_nodes_project ON nodes(project_id, kind, current);
CREATE INDEX IF NOT EXISTS idx_decisions_project ON decisions(project_id, seq);
`
