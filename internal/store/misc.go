package store

import (
	"encoding/json"

	"gotga/internal/domain"
)

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

func unmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

type BatchRow struct {
	ID       string `json:"id"`
	EventSeq int64  `json:"event_seq"`
	Partial  bool   `json:"partial"`
	Accepted string `json:"accepted"`
	Rejected string `json:"rejected"`
	RawText  string `json:"raw_text"`
	TS       string `json:"ts"`
}

func (s *Store) SaveBatchFor(projectID string, b BatchRow) error {
	_, err := s.DB.Exec(`INSERT INTO batches(id,project_id,event_seq,partial,accepted,rejected,raw_text,ts) VALUES(?,?,?,?,?,?,?,?)`,
		b.ID, projectID, b.EventSeq, b.Partial, b.Accepted, b.Rejected, b.RawText, b.TS)
	return err
}

func (s *Store) Batches(projectID string) ([]BatchRow, error) {
	rows, err := s.DB.Query(`SELECT id,event_seq,partial,accepted,rejected,raw_text,ts FROM batches WHERE project_id=? ORDER BY event_seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BatchRow
	for rows.Next() {
		var b BatchRow
		var partial int
		if err := rows.Scan(&b.ID, &b.EventSeq, &partial, &b.Accepted, &b.Rejected, &b.RawText, &b.TS); err != nil {
			return nil, err
		}
		b.Partial = partial == 1
		out = append(out, b)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nilIfNil(p *domain.Params) any {
	if p == nil {
		return nil
	}
	return mustJSON(p)
}

func (s *Store) PutDecision(projectID string, d *domain.Decision) error {
	_, err := s.DB.Exec(`INSERT INTO decisions(id,project_id,event_seq,subject_type,subject_id,verdict,reason,revoked,conflict_with,resolved_by,winner,adjustment,ts)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, projectID, d.EventSeq, d.SubjectType, d.SubjectID, d.Verdict, d.Reason, boolInt(d.Revoked),
		mustJSON(d.ConflictWith), d.ResolvedBy, boolInt(d.Winner), nilIfNil(d.Adjustment), d.TS)
	return err
}

func (s *Store) Decisions(projectID string) ([]domain.Decision, error) {
	rows, err := s.DB.Query(`SELECT id,event_seq,subject_type,subject_id,verdict,COALESCE(reason,''),revoked,COALESCE(conflict_with,'[]'),COALESCE(resolved_by,''),winner,ts FROM decisions WHERE project_id=? ORDER BY event_seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Decision
	for rows.Next() {
		var d domain.Decision
		var revoked, winner int
		var cw string
		if err := rows.Scan(&d.ID, &d.EventSeq, &d.SubjectType, &d.SubjectID, &d.Verdict, &d.Reason, &revoked, &cw, &d.ResolvedBy, &winner, &d.TS); err != nil {
			return nil, err
		}
		d.Revoked = revoked == 1
		d.Winner = winner == 1
		_ = json.Unmarshal([]byte(cw), &d.ConflictWith)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) RevokeDecision(projectID, decisionID, resolvedBy string) error {
	_, err := s.DB.Exec(`UPDATE decisions SET revoked=1, resolved_by=? WHERE project_id=? AND id=?`, resolvedBy, projectID, decisionID)
	return err
}

func (s *Store) SetKV(projectID, key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO kv(project_id,key,value) VALUES(?,?,?) ON CONFLICT(project_id,key) DO UPDATE SET value=excluded.value`, projectID, key, value)
	return err
}

func (s *Store) GetKV(projectID, key string) (string, error) {
	var v string
	err := s.DB.QueryRow(`SELECT value FROM kv WHERE project_id=? AND key=?`, projectID, key).Scan(&v)
	return v, err
}

func (s *Store) Projects() ([]string, error) {
	rows, err := s.DB.Query(`SELECT id FROM projects ORDER BY created_ts,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
