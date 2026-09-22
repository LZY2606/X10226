package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"tga-review/internal/domain"
)

type Store struct{ DB *sql.DB }

func New(db *sql.DB) *Store { return &Store{DB: db} }

func (s *Store) CreateProject(p domain.Project) error {
	_, err := s.DB.Exec(`INSERT INTO projects(id,name,sample_id,note,created_at) VALUES(?,?,?,?,?)`,
		p.ID, p.Name, p.SampleID, p.Note, p.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetProject(id string) (domain.Project, error) {
	row := s.DB.QueryRow(`SELECT id,name,sample_id,note,created_at FROM projects WHERE id=?`, id)
	var p domain.Project
	var created string
	if err := row.Scan(&p.ID, &p.Name, &p.SampleID, &p.Note, &created); err != nil {
		return p, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return p, nil
}

func (s *Store) ListProjects() ([]domain.Project, error) {
	rows, err := s.DB.Query(`SELECT id,name,sample_id,note,created_at FROM projects ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		var created string
		if err := rows.Scan(&p.ID, &p.Name, &p.SampleID, &p.Note, &created); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, p)
	}
	return out, nil
}

// AppendEvent writes one event and returns its sequence.
func (s *Store) AppendEvent(projectID, typ string, at time.Time, actor string, payload any) (domain.Event, error) {
	raw, _ := json.Marshal(payload)
	res, err := s.DB.Exec(`INSERT INTO events(project_id,type,at,actor,payload) VALUES(?,?,?,?,?)`,
		projectID, typ, at.Format(time.RFC3339Nano), actor, string(raw))
	if err != nil {
		return domain.Event{}, err
	}
	seq, _ := res.LastInsertId()
	return domain.Event{Seq: seq, ProjectID: projectID, Type: typ, At: at, Actor: actor, Payload: raw}, nil
}

func (s *Store) ListEvents(projectID string) ([]domain.Event, error) {
	rows, err := s.DB.Query(`SELECT seq,project_id,type,at,actor,payload FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var at, payload string
		if err := rows.Scan(&e.Seq, &e.ProjectID, &e.Type, &at, &e.Actor, &payload); err != nil {
			return nil, err
		}
		e.At, _ = time.Parse(time.RFC3339Nano, at)
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) MaxEventSeq() (int64, error) {
	var n sql.NullInt64
	if err := s.DB.QueryRow(`SELECT MAX(seq) FROM events`).Scan(&n); err != nil {
		return 0, err
	}
	return n.Int64, nil
}

func (s *Store) InsertRecords(recs []domain.SourceRecord) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.Prepare(`INSERT INTO source_records
(id,project_id,kind,layer_index,line_no,raw_text,fields,parent_id,revision,status,diagnostic,import_seq,created_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	for _, r := range recs {
		fields, _ := json.Marshal(r.Fields)
		if _, err := stmt.Exec(r.ID, r.ProjectID, r.Kind, r.LayerIndex, r.LineNo, r.RawText,
			string(fields), r.ParentID, r.Revision, r.Status, r.Diagnostic, r.ImportSeq,
			r.CreatedAt.Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("insert record %s: %w", r.ID, err)
		}
	}
	return tx.Commit()
}

func (s *Store) ListRecords(projectID string) ([]domain.SourceRecord, error) {
	rows, err := s.DB.Query(`SELECT id,project_id,kind,layer_index,line_no,raw_text,fields,parent_id,revision,status,diagnostic,import_seq,created_at
FROM source_records WHERE project_id=? ORDER BY import_seq,kind,layer_index,line_no`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SourceRecord
	for rows.Next() {
		var r domain.SourceRecord
		var fields, created string
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Kind, &r.LayerIndex, &r.LineNo, &r.RawText,
			&fields, &r.ParentID, &r.Revision, &r.Status, &r.Diagnostic, &r.ImportSeq, &created); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(fields), &r.Fields)
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, r)
	}
	return out, nil
}

// PutArtifact inserts only if the stable id is new; identical recomputation is
// a no-op so identities survive deletion of the db + re-import replay.
func (s *Store) PutArtifact(a domain.Artifact) error {
	sourceIDs, _ := json.Marshal(a.SourceIDs)
	deps, _ := json.Marshal(a.DependsOn)
	_, err := s.DB.Exec(`INSERT INTO artifacts
(id,project_id,kind,family,label,algo_version,param_fp,source_ids,depends_on,created_seq,payload)
VALUES(?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(project_id,id) DO NOTHING`,
		a.ID, a.ProjectID, a.Kind, a.Family, a.Label, a.AlgoVersion, a.ParamFP,
		string(sourceIDs), string(deps), a.CreatedSeq, string(a.Payload))
	return err
}

func scanArtifacts(rows *sql.Rows) ([]domain.Artifact, error) {
	var out []domain.Artifact
	for rows.Next() {
		var a domain.Artifact
		var sourceIDs, deps, payload string
		if err := rows.Scan(&a.ID, &a.ProjectID, &a.Kind, &a.Family, &a.Label, &a.AlgoVersion,
			&a.ParamFP, &sourceIDs, &deps, &a.CreatedSeq, &payload); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(sourceIDs), &a.SourceIDs)
		_ = json.Unmarshal([]byte(deps), &a.DependsOn)
		a.Payload = json.RawMessage(payload)
		out = append(out, a)
	}
	return out, nil
}

func (s *Store) ListArtifacts(projectID string) ([]domain.Artifact, error) {
	rows, err := s.DB.Query(`SELECT id,project_id,kind,family,label,algo_version,param_fp,source_ids,depends_on,created_seq,payload
FROM artifacts WHERE project_id=? ORDER BY family,created_seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

func (s *Store) PutArchiveRun(r domain.ArchiveRun) error {
	_, err := s.DB.Exec(`INSERT INTO archive_runs(id,baseline_project_id,perturbation,changed_source,project_id,report,created_at)
VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
		r.ID, r.BaselineProjectID, r.Perturbation, r.ChangedSource, r.ProjectID, string(r.Report),
		r.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListArchiveRuns() ([]domain.ArchiveRun, error) {
	rows, err := s.DB.Query(`SELECT id,baseline_project_id,perturbation,changed_source,project_id,report,created_at FROM archive_runs ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ArchiveRun
	for rows.Next() {
		var r domain.ArchiveRun
		var created, report string
		if err := rows.Scan(&r.ID, &r.BaselineProjectID, &r.Perturbation, &r.ChangedSource,
			&r.ProjectID, &report, &created); err != nil {
			return nil, err
		}
		r.Report = json.RawMessage(report)
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, r)
	}
	return out, nil
}

// WipeProject deletes every row of a project (used by round-trip tests / UI
// "delete local database then re-import" demonstration).
func (s *Store) WipeProject(projectID string) error {
	for _, q := range []string{
		`DELETE FROM artifacts WHERE project_id=?`,
		`DELETE FROM source_records WHERE project_id=?`,
		`DELETE FROM events WHERE project_id=?`,
		`DELETE FROM projects WHERE id=?`,
	} {
		if _, err := s.DB.Exec(q, projectID); err != nil {
			return err
		}
	}
	return nil
}
