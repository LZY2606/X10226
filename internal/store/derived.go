package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"tgareview/internal/domain"
)

func (s *Store) PutDerived(d domain.Derived) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO derived
		 (id,project_id,kind,algo_version,param_fingerprint,input_ids_json,produced_by_event,payload_json)
		 VALUES(?,?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, d.Kind, d.AlgoVersion, d.ParamFingerprint,
		mustJSON(d.InputIDs), d.ProducedByEvent, d.Payload)
	return err
}

func (s *Store) GetDerived(id string) (domain.Derived, error) {
	var d domain.Derived
	var inputs, payload string
	err := s.db.QueryRow(
		`SELECT id,project_id,kind,algo_version,param_fingerprint,input_ids_json,produced_by_event,payload_json
		 FROM derived WHERE id=?`, id).Scan(
		&d.ID, &d.ProjectID, &d.Kind, &d.AlgoVersion, &d.ParamFingerprint,
		&inputs, &d.ProducedByEvent, &payload)
	if err == sql.ErrNoRows {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	_ = json.Unmarshal([]byte(inputs), &d.InputIDs)
	d.Payload = payload
	return d, nil
}

func (s *Store) SetPointer(projectID, kind string, asOfEvent int64, derivedID string) error {
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO pointers(project_id,kind,as_of_event,derived_id)
		 VALUES(?,?,?,?)`, projectID, kind, asOfEvent, derivedID)
	return err
}

func (s *Store) PointerAt(projectID, kind string, seq int64) (string, error) {
	var id string
	err := s.db.QueryRow(
		`SELECT derived_id FROM pointers
		 WHERE project_id=? AND kind=? AND as_of_event<=?
		 ORDER BY as_of_event DESC LIMIT 1`, projectID, kind, seq).Scan(&id)
	if err == sql.ErrNoRows {
		return "", ErrNotFound
	}
	return id, err
}

func (s *Store) LatestPointer(projectID, kind string) (string, int64, error) {
	var id string
	var seq int64
	err := s.db.QueryRow(
		`SELECT derived_id, as_of_event FROM pointers
		 WHERE project_id=? AND kind=? ORDER BY as_of_event DESC LIMIT 1`,
		projectID, kind).Scan(&id, &seq)
	if err == sql.ErrNoRows {
		return "", 0, ErrNotFound
	}
	return id, seq, err
}

type BatchRow struct {
	ID        string
	ProjectID string
	Seq       int64
	Committed []string
	Rejected  []string
}

func (s *Store) PutBatch(b BatchRow) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO batches(id,project_id,seq,committed_json,rejected_json,occurred_at)
		 VALUES(?,?,?,?,?,?)`,
		b.ID, b.ProjectID, b.Seq, mustJSON(b.Committed), mustJSON(b.Rejected),
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) AllBatches(projectID string) ([]BatchRow, error) {
	rows, err := s.db.Query(
		`SELECT id,project_id,seq,committed_json,rejected_json FROM batches
		 WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BatchRow
	for rows.Next() {
		var b BatchRow
		var c, r string
		if err := rows.Scan(&b.ID, &b.ProjectID, &b.Seq, &c, &r); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(c), &b.Committed)
		_ = json.Unmarshal([]byte(r), &b.Rejected)
		out = append(out, b)
	}
	return out, nil
}
