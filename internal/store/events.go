package store

import (
	"database/sql"
	"encoding/json"

	"gotga/internal/domain"
)

func (s *Store) CreateProject(id, name, ts, meta string) error {
	_, err := s.DB.Exec(`INSERT INTO projects(id,name,created_ts,meta) VALUES(?,?,?,?)`, id, name, ts, meta)
	return err
}

func (s *Store) ProjectExists(id string) (bool, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM projects WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

// AppendEvent inserts an event atomically and returns its seq.
func (s *Store) AppendEvent(e *domain.Event) (int64, error) {
	res, err := s.DB.Exec(
		`INSERT INTO events(id,project_id,kind,payload,revokes,actor,note,ts) VALUES(?,?,?,?,?,?,?,?)`,
		e.ID, e.ProjectID, string(e.Kind), e.Payload, e.Revokes, e.Actor, e.Note, e.TS)
	if err != nil {
		return 0, err
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	e.Seq = seq
	return seq, nil
}

func (s *Store) Events(projectID string) ([]domain.Event, error) {
	rows, err := s.DB.Query(`SELECT seq,id,project_id,kind,payload,COALESCE(revokes,''),COALESCE(actor,''),COALESCE(note,''),ts FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		if err := rows.Scan(&e.Seq, &e.ID, &e.ProjectID, &e.Kind, &e.Payload, &e.Revokes, &e.Actor, &e.Note, &e.TS); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) InsertDoc(d *domain.RawDoc, bornSeq int64) error {
	var maxSeq sql.NullInt64
	_ = s.DB.QueryRow(`SELECT MAX(doc_seq) FROM documents WHERE project_id=? AND kind=?`, d.ProjectID, string(d.Kind)).Scan(&maxSeq)
	d.Seq = int(maxSeq.Int64) + 1
	diags, _ := json.Marshal(d.Diags)
	_, err := s.DB.Exec(`INSERT INTO documents(id,project_id,kind,label,source,doc_seq,parent_id,born_seq,raw_text,payload,diags,ingest_ts)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, string(d.Kind), d.Label, d.Source, d.Seq, d.ParentID, bornSeq, d.RawText, d.Payload, string(diags), d.IngestTS)
	return err
}

func (s *Store) Doc(id string) (*domain.RawDoc, error) {
	var d domain.RawDoc
	var kind, diagJSON string
	err := s.DB.QueryRow(`SELECT id,project_id,kind,COALESCE(label,''),COALESCE(source,''),doc_seq,COALESCE(parent_id,''),raw_text,payload,diags,ingest_ts
		FROM documents WHERE id=?`, id).
		Scan(&d.ID, &d.ProjectID, &kind, &d.Label, &d.Source, &d.Seq, &d.ParentID, &d.RawText, &d.Payload, &diagJSON, &d.IngestTS)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Kind = domain.DocKind(kind)
	_ = json.Unmarshal([]byte(diagJSON), &d.Diags)
	return &d, nil
}

// LatestDoc returns the most recent document of a kind (immutable revisions).
func (s *Store) LatestDoc(projectID string, kind domain.DocKind) (*domain.RawDoc, error) {
	var d domain.RawDoc
	var kd, diagJSON string
	err := s.DB.QueryRow(`SELECT id,project_id,kind,COALESCE(label,''),COALESCE(source,''),doc_seq,COALESCE(parent_id,''),raw_text,payload,diags,ingest_ts
		FROM documents WHERE project_id=? AND kind=? ORDER BY doc_seq DESC LIMIT 1`, projectID, string(kind)).
		Scan(&d.ID, &d.ProjectID, &kd, &d.Label, &d.Source, &d.Seq, &d.ParentID, &d.RawText, &d.Payload, &diagJSON, &d.IngestTS)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Kind = kind
	_ = json.Unmarshal([]byte(diagJSON), &d.Diags)
	return &d, nil
}

func (s *Store) AllDocs(projectID string) ([]*domain.RawDoc, error) {
	rows, err := s.DB.Query(`SELECT id,project_id,kind,COALESCE(label,''),COALESCE(source,''),doc_seq,COALESCE(parent_id,''),raw_text,payload,diags,ingest_ts
		FROM documents WHERE project_id=? ORDER BY kind,doc_seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.RawDoc
	for rows.Next() {
		var d domain.RawDoc
		var kind, diagJSON string
		if err := rows.Scan(&d.ID, &d.ProjectID, &kind, &d.Label, &d.Source, &d.Seq, &d.ParentID, &d.RawText, &d.Payload, &diagJSON, &d.IngestTS); err != nil {
			return nil, err
		}
		d.Kind = domain.DocKind(kind)
		_ = json.Unmarshal([]byte(diagJSON), &d.Diags)
		out = append(out, &d)
	}
	return out, rows.Err()
}

// LatestDocAsOf returns the newest layer born at or before seq.
func (s *Store) LatestDocAsOf(projectID string, kind domain.DocKind, seq int64) (*domain.RawDoc, error) {
	var d domain.RawDoc
	var kd, diagJSON string
	err := s.DB.QueryRow(`SELECT id,project_id,kind,COALESCE(label,''),COALESCE(source,''),doc_seq,COALESCE(parent_id,''),raw_text,payload,diags,ingest_ts
		FROM documents WHERE project_id=? AND kind=? AND born_seq<=? ORDER BY doc_seq DESC LIMIT 1`, projectID, string(kind), seq).
		Scan(&d.ID, &d.ProjectID, &kd, &d.Label, &d.Source, &d.Seq, &d.ParentID, &d.RawText, &d.Payload, &diagJSON, &d.IngestTS)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Kind = kind
	_ = json.Unmarshal([]byte(diagJSON), &d.Diags)
	return &d, nil
}

// InsertEventAtSeq is used by archive import to preserve seq and identity.
func (s *Store) InsertEventAtSeq(e *domain.Event) error {
	_, err := s.DB.Exec(
		`INSERT INTO events(seq,id,project_id,kind,payload,revokes,actor,note,ts) VALUES(?,?,?,?,?,?,?,?,?)`,
		e.Seq, e.ID, e.ProjectID, string(e.Kind), e.Payload, e.Revokes, e.Actor, e.Note, e.TS)
	return err
}
