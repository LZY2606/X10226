package store

import (
	"encoding/json"
	"errors"
	"time"

	"tga-review/internal/domain"
)

// ErrNotFound is returned when a project/entity does not exist.
var ErrNotFound = errors.New("not found")

// AddSubmission appends an immutable source submission as a new version and
// produces a fresh derivation. Existing layers get a parent link; originals
// are never overwritten.
func (s *Store) AddSubmission(pid string, sub domain.Submission, _ bool) (*domain.Submission, *domain.Derivation, error) {
	return s.addSubmissionInternal(pid, sub)
}

func (s *Store) addSubmissionInternal(pid string, sub domain.Submission) (*domain.Submission, *domain.Derivation, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	if _, err := s.getProjectLocked(tx, pid); err != nil {
		return nil, nil, err
	}
	seq, err := nextSeqLocked(tx, "submissions", pid)
	if err != nil {
		return nil, nil, err
	}
	parent, _ := latestSubmissionLocked(tx, pid, sub.Layer)
	sub.ProjectID = pid
	sub.Seq = seq
	sub.ID = domain.SubmissionStableID(pid, seq)
	sub.CreatedAt = s.now()
	if parent != nil {
		sub.ParentID = parent.ID
		if sub.Kind == "import" || sub.Kind == "" {
			sub.Kind = "revision"
		}
	} else if sub.Kind == "" {
		sub.Kind = "import"
	}
	if sub.SourceName == "" {
		sub.SourceName = string(sub.Layer) + ".csv"
	}
	if sub.PayloadHash == "" {
		raw, _ := json.Marshal(sub)
		sub.PayloadHash = domain.Fingerprint(string(raw))
	}
	data, _ := json.Marshal(sub)
	if _, err := tx.Exec(`INSERT INTO submissions
		(id, project_id, seq, layer, kind, parent_id, source_name, payload_hash,
		 partial, mutation, note, created_at, data)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sub.ID, pid, seq, string(sub.Layer), sub.Kind, sub.ParentID, sub.SourceName,
		sub.PayloadHash, boolInt(sub.Partial), sub.Mutation, sub.Note,
		sub.CreatedAt.Format(time.RFC3339Nano), string(data)); err != nil {
		return nil, nil, err
	}
	eseq, err := nextEventSeqLocked(tx, pid)
	if err != nil {
		return nil, nil, err
	}
	eid, err := appendEvent(tx, pid, eseq, "submission_committed", s.now(), map[string]any{
		"submission_id": sub.ID, "layer": string(sub.Layer), "kind": sub.Kind,
		"parent_id": sub.ParentID, "partial": sub.Partial, "mutation": sub.Mutation,
		"payload_hash": sub.PayloadHash, "source_name": sub.SourceName,
		"diagnostic_count": len(sub.Diags),
	})
	if err != nil {
		return nil, nil, err
	}
	d, err := deriveLocked(tx, pid, eid, s.now())
	if err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &sub, d, nil
}

// Submissions returns all submissions for a project in version order.
func (s *Store) Submissions(pid string) ([]domain.Submission, error) {
	rows, err := s.db.Query(
		`SELECT data FROM submissions WHERE project_id=? ORDER BY seq`, pid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Submission
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var sub domain.Submission
		if err := json.Unmarshal([]byte(data), &sub); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// GetSubmission returns one immutable submission.
func (s *Store) GetSubmission(pid, id string) (*domain.Submission, error) {
	var data string
	err := s.db.QueryRow(`SELECT data FROM submissions WHERE project_id=? AND id=?`, pid, id).
		Scan(&data)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var sub domain.Submission
	if err := json.Unmarshal([]byte(data), &sub); err != nil {
		return nil, err
	}
	return &sub, nil
}
