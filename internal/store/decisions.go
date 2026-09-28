package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tgareview/internal/domain"
)

// SaveDecision records a decision row linked to its journal event.
func (s *Store) SaveDecision(ctx context.Context, tx *sql.Tx, ev domain.Event, d DecisionRec) error {
	d.ID = domain.ContentID("dec", domain.CanonicalJSON(map[string]any{
		"project": ev.ProjectID, "seq": ev.Seq, "candidate": d.CandidateID, "action": d.Action,
	}))
	d.ProjectID = ev.ProjectID
	d.EventSeq = ev.Seq
	d.EventID = ev.ID
	d.CreatedAt = ev.At
	_, err := tx.ExecContext(ctx,
		`INSERT INTO decisions(id,project_id,event_seq,event_id,candidate_id,candidate_kind,action,
		   rationale,params,active,revoked_by_event,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,1,'',?)`,
		d.ID, d.ProjectID, d.EventSeq, d.EventID, d.CandidateID, d.CandidateKind, d.Action,
		d.Rationale, string(jsonRaw(d.Params)), ev.At.Format(timeFmt))
	return err
}

// DecisionRec is the mutable-projection row for one human decision.
type DecisionRec struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"project_id"`
	EventSeq      int64          `json:"event_seq"`
	EventID       string         `json:"event_id"`
	CandidateID   string         `json:"candidate_id"`
	CandidateKind string         `json:"candidate_kind"`
	Action        string         `json:"action"`
	Rationale     string         `json:"rationale"`
	Params        map[string]any `json:"params"`
	Active        bool           `json:"active"`
	RevokedBy     string         `json:"revoked_by_event"`
	CreatedAt     string         `json:"created_at"`
}

// RevokeDecision marks a prior decision inactive (undo never deletes history).
func (s *Store) RevokeDecision(ctx context.Context, tx *sql.Tx, projectID, candidateID string, revokeEventID string) (string, error) {
	res, err := tx.ExecContext(ctx,
		`UPDATE decisions SET active=0, revoked_by_event=? WHERE project_id=? AND candidate_id=? AND active=1`,
		revokeEventID, projectID, candidateID)
	if err != nil {
		return "", err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return "", ErrNotFound
	}
	return "", nil
}

// ListDecisions returns all decisions (active and inactive) newest first.
func (s *Store) ListDecisions(ctx context.Context, projectID string) ([]DecisionRec, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,event_seq,event_id,candidate_id,candidate_kind,action,rationale,params,active,revoked_by_event,created_at
		 FROM decisions WHERE project_id=? ORDER BY event_seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DecisionRec
	for rows.Next() {
		var d DecisionRec
		var params string
		var active int
		if err := rows.Scan(&d.ID, &d.EventSeq, &d.EventID, &d.CandidateID, &d.CandidateKind,
			&d.Action, &d.Rationale, &params, &active, &d.RevokedBy, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.ProjectID = projectID
		d.Active = active == 1
		_ = json.Unmarshal([]byte(params), &d.Params)
		out = append(out, d)
	}
	return out, rows.Err()
}

// ActiveDecisionAtSeq returns the decision effective for candidateID at seq.
func (s *Store) ActiveDecisionAtSeq(ctx context.Context, projectID, candidateID string, seq int64) (*DecisionRec, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,event_seq,event_id,candidate_id,candidate_kind,action,rationale,params,active,revoked_by_event,created_at
		 FROM decisions WHERE project_id=? AND candidate_id=? AND event_seq<=? ORDER BY event_seq`, projectID, candidateID, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rec *DecisionRec
	for rows.Next() {
		var d DecisionRec
		var params string
		var active int
		if err := rows.Scan(&d.ID, &d.EventSeq, &d.EventID, &d.CandidateID, &d.CandidateKind,
			&d.Action, &d.Rationale, &params, &active, &d.RevokedBy, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.ProjectID = projectID
		_ = json.Unmarshal([]byte(params), &d.Params)
		if active == 1 {
			dd := d
			rec = &dd
		} else {
			rec = nil
		}
	}
	if rec == nil {
		return nil, sql.ErrNoRows
	}
	return rec, nil
}

// ensure unused import guard
var _ = errors.Is
