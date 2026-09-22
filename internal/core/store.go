package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  params_json TEXT NOT NULL,
  seed INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY,
  project_id TEXT NOT NULL,
  type TEXT NOT NULL,
  payload TEXT NOT NULL,
  at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS layers (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent_id TEXT NOT NULL DEFAULT '',
  raw TEXT NOT NULL,
  parsed TEXT NOT NULL,
  status TEXT NOT NULL,
  diagnostics TEXT NOT NULL DEFAULT '[]',
  submitted_at INTEGER NOT NULL,
  seq INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS derived (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  algo_version TEXT NOT NULL,
  params_fp TEXT NOT NULL,
  source_ids TEXT NOT NULL,
  payload TEXT NOT NULL,
  status TEXT NOT NULL,
  stale_since_seq INTEGER NOT NULL DEFAULT 0,
  created_seq INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS decisions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  action TEXT NOT NULL,
  params TEXT NOT NULL DEFAULT '',
  reviewer TEXT NOT NULL,
  rationale TEXT NOT NULL,
  seq INTEGER NOT NULL,
  undone_by_seq INTEGER NOT NULL DEFAULT 0,
  superseded_by_seq INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS ix_events_proj ON events(project_id, seq);
CREATE INDEX IF NOT EXISTS ix_layers_proj ON layers(project_id, kind, version);
CREATE INDEX IF NOT EXISTS ix_derived_proj ON derived(project_id, created_seq);
CREATE INDEX IF NOT EXISTS ix_dec_proj ON decisions(project_id, seq);
`

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(off)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// CreateProject inserts the project and its root event.
func (s *Store) CreateProject(ctx context.Context, p Params, clock Clock, name string, seed int64) (string, int64, error) {
	id := "proj:" + hashParts("project", name, fmt.Sprint(clock.Now()), fmt.Sprint(seed))
	now := clock.Now()
	pb, _ := canonicalJSON(p)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO projects(id,name,created_at,params_json,seed) VALUES(?,?,?,?,?)`,
		id, name, now, string(pb), seed); err != nil {
		return "", 0, err
	}
	seq, err := appendEventTx(ctx, tx, id, EvProject, map[string]any{"name": name, "seed": seed}, now)
	if err != nil {
		return "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return id, seq, nil
}

func (s *Store) ProjectExists(id string) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM projects WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

func (s *Store) ListProjects(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,created_at FROM projects ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, name string
		var at int64
		if err := rows.Scan(&id, &name, &at); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"id": id, "name": name, "created_at": at})
	}
	return out, rows.Err()
}

// CurrentParams returns the latest parameter set.
func (s *Store) CurrentParams(ctx context.Context, projectID string) (Params, error) {
	var js string
	if err := s.DB.QueryRowContext(ctx,
		`SELECT params_json FROM projects WHERE id=?`, projectID).Scan(&js); err != nil {
		return Params{}, err
	}
	var p Params
	return p, json.Unmarshal([]byte(js), &p)
}

func (s *Store) SetParamsTx(ctx context.Context, tx *sql.Tx, projectID string, p Params) error {
	b, _ := canonicalJSON(p)
	_, err := tx.ExecContext(ctx, `UPDATE projects SET params_json=? WHERE id=?`, string(b), projectID)
	return err
}

func appendEventTx(ctx context.Context, tx *sql.Tx, projectID, typ string, payload any, at int64) (int64, error) {
	b, err := canonicalJSON(payload)
	if err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO events(project_id,type,payload,at) VALUES(?,?,?,?)`,
		projectID, typ, string(b), at)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) AppendEvent(ctx context.Context, projectID, typ string, payload any, at int64) (int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	seq, err := appendEventTx(ctx, tx, projectID, typ, payload, at)
	if err != nil {
		return 0, err
	}
	return seq, tx.Commit()
}

func (s *Store) InsertLayerTx(ctx context.Context, tx *sql.Tx, l *Layer) error {
	pb, err := canonicalJSON(l.Parsed)
	if err != nil {
		return err
	}
	db := jsonD(l.Diagnostics)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO layers(id,project_id,kind,version,parent_id,raw,parsed,status,diagnostics,submitted_at,seq)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		l.ID, l.ProjectID, l.Kind, l.Version, l.ParentID, l.Raw, string(pb),
		l.Status, db, l.SubmittedAt, l.Seq)
	return err
}

func (s *Store) InsertDerivedTx(ctx context.Context, tx *sql.Tx, d *Derived) error {
	sb, _ := canonicalJSON(d.SourceIDs)
	_, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO derived(id,project_id,kind,algo_version,params_fp,source_ids,payload,status,stale_since_seq,created_seq)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, d.Kind, d.AlgoVersion, d.ParamsFP, string(sb),
		string(d.Payload), d.Status, d.StaleSinceSeq, d.CreatedSeq)
	return err
}

func (s *Store) InsertDecisionTx(ctx context.Context, tx *sql.Tx, d *Decision) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO decisions(id,project_id,target_id,action,params,reviewer,rationale,seq,undone_by_seq,superseded_by_seq)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, d.TargetID, d.Action, d.Params, d.Reviewer, d.Rationale,
		d.Seq, d.UndoneBySeq, d.SupersededBySeq)
	return err
}

func jsonD(ds []Diagnostic) string {
	if ds == nil {
		ds = []Diagnostic{}
	}
	b, _ := canonicalJSON(ds)
	return string(b)
}

// queryer allows sharing queries between *sql.DB and *sql.Tx.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

const layerCols = `id,project_id,kind,version,parent_id,raw,parsed,status,diagnostics,submitted_at,seq`

func scanLayer(row *sql.Row) (*Layer, error) {
	var l Layer
	var parent, parsed, diags string
	if err := row.Scan(&l.ID, &l.ProjectID, &l.Kind, &l.Version, &parent,
		&l.Raw, &parsed, &l.Status, &diags, &l.SubmittedAt, &l.Seq); err != nil {
		return nil, err
	}
	l.ParentID = parent
	l.Parsed = json.RawMessage(parsed)
	json.Unmarshal([]byte(diags), &l.Diagnostics)
	return &l, nil
}

func (s *Store) LatestLayerTx(ctx context.Context, q queryer, projectID, kind string) (*Layer, error) {
	row := q.QueryRowContext(ctx,
		`SELECT `+layerCols+` FROM layers WHERE project_id=? AND kind=? ORDER BY version DESC LIMIT 1`,
		projectID, kind)
	l, err := scanLayer(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func (s *Store) LayerByID(ctx context.Context, id string) (*Layer, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+layerCols+` FROM layers WHERE id=?`, id)
	l, err := scanLayer(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func (s *Store) ListLayers(ctx context.Context, projectID string) ([]*Layer, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+layerCols+` FROM layers WHERE project_id=? ORDER BY seq,kind`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Layer
	for rows.Next() {
		var l Layer
		var parent, parsed, diags string
		if err := rows.Scan(&l.ID, &l.ProjectID, &l.Kind, &l.Version, &parent,
			&l.Raw, &parsed, &l.Status, &diags, &l.SubmittedAt, &l.Seq); err != nil {
			return nil, err
		}
		l.ParentID = parent
		l.Parsed = json.RawMessage(parsed)
		json.Unmarshal([]byte(diags), &l.Diagnostics)
		out = append(out, &l)
	}
	return out, rows.Err()
}

// AllLayersAtSeq returns the newest layer of each kind visible at seq (time travel).
func (s *Store) AllLayersAtSeq(ctx context.Context, projectID string, seq int64) (map[string]*Layer, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+layerCols+` FROM layers a
		 WHERE project_id=? AND seq<=?
		   AND version=(SELECT MAX(version) FROM layers b
		               WHERE b.project_id=a.project_id AND b.kind=a.kind AND b.seq<=?)
		 ORDER BY kind`, projectID, seq, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*Layer{}
	for rows.Next() {
		var l Layer
		var parent, parsed, diags string
		if err := rows.Scan(&l.ID, &l.ProjectID, &l.Kind, &l.Version, &parent,
			&l.Raw, &parsed, &l.Status, &diags, &l.SubmittedAt, &l.Seq); err != nil {
			return nil, err
		}
		l.ParentID = parent
		l.Parsed = json.RawMessage(parsed)
		json.Unmarshal([]byte(diags), &l.Diagnostics)
		out[l.Kind] = &l
	}
	return out, rows.Err()
}

const derCols = `id,project_id,kind,algo_version,params_fp,source_ids,payload,status,stale_since_seq,created_seq`

func scanDerived(scanner interface{ Scan(...any) error }) (*Derived, error) {
	var d Derived
	var sources, payload string
	var stale sql.NullInt64
	if err := scanner.Scan(&d.ID, &d.ProjectID, &d.Kind, &d.AlgoVersion,
		&d.ParamsFP, &sources, &payload, &d.Status, &stale, &d.CreatedSeq); err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(sources), &d.SourceIDs)
	d.Payload = json.RawMessage(payload)
	d.StaleSinceSeq = stale.Int64
	return &d, nil
}

func (s *Store) ListDerived(ctx context.Context, projectID string) ([]*Derived, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+derCols+` FROM derived WHERE project_id=? ORDER BY created_seq,kind,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Derived
	for rows.Next() {
		d, err := scanDerived(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MarkStale marks every current derived row depending on changedSourceIDs.
// Returns the number of stale-marked rows.
func (s *Store) MarkStale(ctx context.Context, projectID string, changedSourceIDs []string, seq int64) (int64, error) {
	if len(changedSourceIDs) == 0 {
		return 0, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var n int64
	rows, err := tx.QueryContext(ctx,
		`SELECT id, source_ids FROM derived WHERE project_id=? AND status='current'`, projectID)
	if err != nil {
		return 0, err
	}
	type pair struct{ id, sources string }
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.id, &p.sources); err != nil {
			rows.Close()
			return 0, err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	changed := map[string]bool{}
	for _, id := range changedSourceIDs {
		changed[id] = true
	}
	for _, p := range pairs {
		var src []string
		json.Unmarshal([]byte(p.sources), &src)
		hit := false
		for _, id := range src {
			if changed[id] {
				hit = true
				break
			}
		}
		if hit {
			r, err := tx.ExecContext(ctx,
				`UPDATE derived SET status='stale', stale_since_seq=? WHERE id=? AND status='current'`, seq, p.id)
			if err != nil {
				return 0, err
			}
			nn, _ := r.RowsAffected()
			n += nn
		}
	}
	return n, tx.Commit()
}

// MarkAllCurrentStale expires current results on parameter revision.
func (s *Store) MarkAllCurrentStale(ctx context.Context, projectID string, seq int64) (int64, error) {
	res, err := s.DB.ExecContext(ctx,
		`UPDATE derived SET status='stale', stale_since_seq=? WHERE project_id=? AND status='current'`,
		seq, projectID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// MarkDecision marks one decision field (undo / supersede).
func (s *Store) MarkDecision(ctx context.Context, id, field string, seq int64) error {
	if field != "undone_by_seq" && field != "superseded_by_seq" {
		return fmt.Errorf("bad field %s", field)
	}
	_, err := s.DB.ExecContext(ctx,
		`UPDATE decisions SET `+field+`=? WHERE id=?`, seq, id)
	return err
}

func (s *Store) DecisionByID(ctx context.Context, id string) (*Decision, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+decCols+` FROM decisions WHERE id=?`, id)
	d, err := scanDecision(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

const decCols = `id,project_id,target_id,action,params,reviewer,rationale,seq,undone_by_seq,superseded_by_seq`

func scanDecision(row *sql.Row) (*Decision, error) {
	var d Decision
	if err := row.Scan(&d.ID, &d.ProjectID, &d.TargetID, &d.Action,
		&d.Params, &d.Reviewer, &d.Rationale, &d.Seq,
		&d.UndoneBySeq, &d.SupersededBySeq); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) ActiveDecisionsAt(ctx context.Context, projectID string, seq int64) ([]*Decision, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+decCols+` FROM decisions
		 WHERE project_id=? AND seq<=? AND undone_by_seq=0 AND superseded_by_seq=0
		 ORDER BY seq`, projectID, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Decision
	for rows.Next() {
		var d Decision
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.TargetID, &d.Action,
			&d.Params, &d.Reviewer, &d.Rationale, &d.Seq,
			&d.UndoneBySeq, &d.SupersededBySeq); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

func (s *Store) Events(ctx context.Context, projectID string) ([]Event, error) {
	return s.eventsUpTo(ctx, projectID, 0)
}

func (s *Store) eventsUpTo(ctx context.Context, projectID string, upTo int64) ([]Event, error) {
	q := `SELECT seq,project_id,type,payload,at FROM events WHERE project_id=?`
	args := []any{projectID}
	if upTo > 0 {
		q += ` AND seq<=?`
		args = append(args, upTo)
	}
	q += ` ORDER BY seq`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var pl string
		if err := rows.Scan(&e.Seq, &e.ProjectID, &e.Type, &pl, &e.At); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(pl)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) MaxSeq(ctx context.Context) (int64, error) {
	var n sql.NullInt64
	if err := s.DB.QueryRowContext(ctx, `SELECT MAX(seq) FROM events`).Scan(&n); err != nil {
		return 0, err
	}
	return n.Int64, nil
}
