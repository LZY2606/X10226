package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"tga-review/internal/domain"
)

func (s *Store) InsertNode(ctx context.Context, n domain.Node, eventSeq int) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO nodes(id,project_id,kind,algo,algo_version,param_fp,inputs,payload,diag,status,current,superseded_by,created_event_seq,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		n.ID, n.ProjectID, string(n.Kind), n.Algo, n.AlgoVer, n.ParamFP,
		string(jb(n.Inputs)), string(jb(n.Payload)), string(jb(n.Diag)), n.Status,
		boolInt(n.Current), n.SupersedBy, eventSeq, n.CreatedAt)
	if err != nil {
		return err
	}
	for _, in := range n.Inputs {
		if _, err := s.db.ExecContext(ctx,
			"INSERT OR IGNORE INTO node_edges(project_id,from_id,to_id) VALUES(?,?,?)",
			n.ProjectID, in, n.ID); err != nil {
			return err
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

const nodeCols = `id,project_id,kind,algo,algo_version,param_fp,inputs,payload,diag,status,current,superseded_by,created_event_seq,created_at`

func scanNode(row interface{ Scan(...any) error }) (domain.Node, error) {
	var n domain.Node
	var kind, inputs, payload, diag, sup sql.NullString
	var current int
	var seq int
	if err := row.Scan(&n.ID, &n.ProjectID, &kind, &n.Algo, &n.AlgoVer, &n.ParamFP,
		&inputs, &payload, &diag, &n.Status, &current, &sup, &seq, &n.CreatedAt); err != nil {
		return n, err
	}
	n.Kind = domain.NodeKind(kind.String)
	_ = json.Unmarshal([]byte(inputs.String), &n.Inputs)
	_ = json.Unmarshal([]byte(payload.String), &n.Payload)
	_ = json.Unmarshal([]byte(diag.String), &n.Diag)
	n.Current = current != 0
	n.SupersedBy = sup.String
	n.CreatedEventSeq = seq
	return n, nil
}

// SetCurrentForKind marks all nodes of a kind stale, optionally recording the
// node that superseded them.
func (s *Store) SetCurrentForKind(ctx context.Context, projectID string, kind domain.NodeKind, current bool, supersedBy string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE nodes SET current=?, superseded_by=COALESCE(NULLIF(?, ''), superseded_by) WHERE project_id=? AND kind=? AND current=1",
		boolInt(current), supersedBy, projectID, string(kind))
	return err
}

func (s *Store) CurrentNode(ctx context.Context, projectID string, kind domain.NodeKind) (*domain.Node, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+nodeCols+` FROM nodes WHERE project_id=? AND kind=? AND current=1 ORDER BY created_event_seq DESC LIMIT 1`,
		projectID, string(kind))
	n, err := scanNode(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) NodeByID(ctx context.Context, id string) (*domain.Node, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+nodeCols+` FROM nodes WHERE id=?`, id)
	n, err := scanNode(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *Store) NodesAtEvent(ctx context.Context, projectID string, seq int) ([]domain.Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+nodeCols+` FROM nodes WHERE project_id=? AND created_event_seq<=? ORDER BY created_event_seq, kind`,
		projectID, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectNodes(rows)
}

func (s *Store) AllNodes(ctx context.Context, projectID string) ([]domain.Node, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+nodeCols+` FROM nodes WHERE project_id=? ORDER BY created_event_seq, kind`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectNodes(rows)
}

type nodeScanner interface {
	Scan(...any) error
	Next() bool
	Err() error
}

func collectNodes(rows *sql.Rows) ([]domain.Node, error) {
	var out []domain.Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// ---- decisions ----

func (s *Store) InsertDecision(ctx context.Context, d domain.Decision) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO decisions(id,project_id,seq,kind,target,payload,node_ids,param_fp,rationale,supersedes,live,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, d.Seq, d.Kind, d.Target, string(jb(d.Payload)),
		string(jb(d.NodeIDs)), d.ParamFP, d.Rationale, d.Supersedes, boolInt(d.Live), d.CreatedAt)
	return err
}

func (s *Store) SetDecisionLive(ctx context.Context, id string, live bool, supersedes string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE decisions SET live=?, supersedes=? WHERE id=?", boolInt(live), supersedes, id)
	return err
}

const decisionCols = `id,project_id,seq,kind,target,payload,node_ids,param_fp,rationale,supersedes,live,created_at`

func scanDecision(row interface{ Scan(...any) error }) (domain.Decision, error) {
	var d domain.Decision
	var target, payload, nodeIDs, rationale, sup sql.NullString
	var live int
	if err := row.Scan(&d.ID, &d.ProjectID, &d.Seq, &d.Kind, &target, &payload, &nodeIDs,
		&d.ParamFP, &rationale, &sup, &live, &d.CreatedAt); err != nil {
		return d, err
	}
	d.Target = target.String
	_ = json.Unmarshal([]byte(payload.String), &d.Payload)
	_ = json.Unmarshal([]byte(nodeIDs.String), &d.NodeIDs)
	d.Rationale = rationale.String
	d.Supersedes = sup.String
	d.Live = live != 0
	return d, nil
}

func (s *Store) AllDecisions(ctx context.Context, projectID string) ([]domain.Decision, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+decisionCols+` FROM decisions WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ---- conflicts ----

func (s *Store) InsertConflict(ctx context.Context, c domain.Conflict) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO conflicts(id,project_id,seq,stage_key,decision_ids,status,note,created_at,resolved_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		c.ID, c.ProjectID, c.Seq, c.StageKey, string(jb(c.DecisionIDs)), c.Status, c.Note, c.CreatedAt, c.ResolvedAt)
	return err
}

func (s *Store) UpdateConflict(ctx context.Context, id, status, resolvedAt string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE conflicts SET status=?, resolved_at=? WHERE id=?", status, resolvedAt, id)
	return err
}

func (s *Store) AllConflicts(ctx context.Context, projectID string) ([]domain.Conflict, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,project_id,seq,stage_key,decision_ids,status,note,created_at,COALESCE(resolved_at,'') FROM conflicts WHERE project_id=? ORDER BY seq`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Conflict
	for rows.Next() {
		var c domain.Conflict
		var ids, note string
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.Seq, &c.StageKey, &ids, &c.Status, &note, &c.CreatedAt, &c.ResolvedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(ids), &c.DecisionIDs)
		c.Note = note
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- archive ----

func (s *Store) InsertArchiveEntry(ctx context.Context, e domain.ArchiveEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO archive_entries(id,project_id,scenario,title,status,changed_source,before_json,after_json,rules_json,scope_json,adopted,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.ProjectID, e.Scenario, e.Title, e.Status, e.ChangedSource,
		string(jb(e.Before)), string(jb(e.After)), string(jb(e.Rules)), string(jb(e.Scope)),
		boolInt(e.Adopted), e.CreatedAt)
	return err
}

func (s *Store) ArchiveEntries(ctx context.Context, projectID string) ([]domain.ArchiveEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,project_id,scenario,title,status,changed_source,before_json,after_json,rules_json,scope_json,adopted,created_at
		 FROM archive_entries WHERE project_id=? ORDER BY created_at, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ArchiveEntry
	for rows.Next() {
		var e domain.ArchiveEntry
		var before, after, rules, scope, changed sql.NullString
		var adopted int
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Scenario, &e.Title, &e.Status, &changed,
			&before, &after, &rules, &scope, &adopted, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.ChangedSource = changed.String
		_ = json.Unmarshal([]byte(before.String), &e.Before)
		_ = json.Unmarshal([]byte(after.String), &e.After)
		_ = json.Unmarshal([]byte(rules.String), &e.Rules)
		_ = json.Unmarshal([]byte(scope.String), &e.Scope)
		e.Adopted = adopted != 0
		out = append(out, e)
	}
	return out, rows.Err()
}

// SupersedeNode marks one node stale and records its replacement.
func (s *Store) SupersedeNode(ctx context.Context, oldID, newID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE nodes SET current=0, superseded_by=? WHERE id=?", newID, oldID)
	return err
}
