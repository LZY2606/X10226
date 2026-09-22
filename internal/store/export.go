package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

// Bundle is the portable, self-contained export. Identity is deterministic
// (ids + seq order), so a fresh database reproduces the exact state.
type Bundle struct {
	Schema      string          `json:"schema"`
	ExportedAt  string          `json:"exported_at"`
	ProjectSeq  int             `json:"project_seq"`
	Projects    []projectRow    `json:"projects"`
	Events      []eventRow      `json:"events"`
	Submissions []submissionRow `json:"submissions"`
	Derivations []derivationRow `json:"derivations"`
	Decisions   []decisionRow   `json:"decisions"`
}

type projectRow struct {
	ID, Name, Material, Note, CreatedAt string
	Seq                                 int
}
type eventRow struct {
	ID, ProjectID, Type, At, Payload string
	Seq                              int
}
type submissionRow struct {
	ID, ProjectID, Layer, Kind, ParentID, SourceName, PayloadHash string
	Mutation, Note, CreatedAt, Data                               string
	Partial                                                       bool
	Seq                                                           int
}
type derivationRow struct {
	ID, ProjectID, EventID, Data string
	Seq                          int
}
type decisionRow struct {
	ID, ProjectID, EventID, TargetKind, TargetKey, DerivationID string
	Action, Adjustments, Reason, SupersedesID, CreatedAt        string
	Active                                                      bool
	Seq                                                         int
}

// Export serialises the entire database deterministically.
func (s *Store) Export() (*Bundle, error) {
	b := &Bundle{Schema: "tga-review-bundle-v1", ExportedAt: s.now().Format(time.RFC3339Nano)}
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM projects`).Scan(&b.ProjectSeq); err != nil {
		// project_seq table is authoritative
		_ = s.db.QueryRow(`SELECT COALESCE(n,0) FROM project_seq`).Scan(&b.ProjectSeq)
	}
	rows, err := s.db.Query(`SELECT id, seq, name, material, note, created_at FROM projects ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r projectRow
		if err := rows.Scan(&r.ID, &r.Seq, &r.Name, &r.Material, &r.Note, &r.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		b.Projects = append(b.Projects, r)
	}
	rows.Close()

	if b.Events, err = loadEvents(s.db); err != nil {
		return nil, err
	}
	if b.Submissions, err = loadSubmissions(s.db); err != nil {
		return nil, err
	}
	if b.Derivations, err = loadDerivations(s.db); err != nil {
		return nil, err
	}
	if b.Decisions, err = loadDecisions(s.db); err != nil {
		return nil, err
	}
	return b, nil
}

func loadEvents(db *sql.DB) ([]eventRow, error) {
	rows, err := db.Query(`SELECT id, project_id, seq, type, at, payload FROM events ORDER BY project_id, seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []eventRow
	for rows.Next() {
		var r eventRow
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Seq, &r.Type, &r.At, &r.Payload); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadSubmissions(db *sql.DB) ([]submissionRow, error) {
	rows, err := db.Query(`SELECT id, project_id, seq, layer, kind, parent_id,
		source_name, payload_hash, partial, mutation, note, created_at, data
		FROM submissions ORDER BY project_id, seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []submissionRow
	for rows.Next() {
		var r submissionRow
		var partial int
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Seq, &r.Layer, &r.Kind,
			&r.ParentID, &r.SourceName, &r.PayloadHash, &partial, &r.Mutation,
			&r.Note, &r.CreatedAt, &r.Data); err != nil {
			return nil, err
		}
		r.Partial = partial == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadDerivations(db *sql.DB) ([]derivationRow, error) {
	rows, err := db.Query(`SELECT id, project_id, seq, event_id, data FROM derivations ORDER BY project_id, seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []derivationRow
	for rows.Next() {
		var r derivationRow
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Seq, &r.EventID, &r.Data); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadDecisions(db *sql.DB) ([]decisionRow, error) {
	rows, err := db.Query(`SELECT id, project_id, seq, event_id, target_kind, target_key,
		derivation_id, action, adjustments, reason, supersedes_id, active, created_at
		FROM decisions ORDER BY project_id, seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []decisionRow
	for rows.Next() {
		var r decisionRow
		var active int
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Seq, &r.EventID, &r.TargetKind,
			&r.TargetKey, &r.DerivationID, &r.Action, &r.Adjustments, &r.Reason,
			&r.SupersedesID, &active, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Active = active == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

// Marshal returns canonical JSON bytes.
func (b *Bundle) Marshal() ([]byte, error) { return json.MarshalIndent(b, "", "  ") }
