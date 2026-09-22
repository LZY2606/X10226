package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"tga-review/internal/domain"
)

// DecisionRequest is a review action from the web UI.
type DecisionRequest struct {
	TargetKind string `json:"target_kind"`
	TargetKey  string `json:"target_key"`
	Action     string `json:"action"` // accept | reject | undo
	Reason     string `json:"reason"`
	UndoID     string `json:"undo_id"`
}

// Decide appends a human adjudication event. Derivations are immutable; the
// decision becomes visible through the read projection. "undo" appends a
// compensating event and deactivates the prior decision instead of deleting.
func (s *Store) Decide(pid string, req DecisionRequest) (*domain.Decision, error) {
	if req.Action != "accept" && req.Action != "reject" && req.Action != "undo" {
		return nil, fmt.Errorf("不支持的裁定动作 %q", req.Action)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	d, err := latestDerivationLocked(tx, pid)
	if err != nil || d == nil {
		return nil, fmt.Errorf("尚无派生结果可裁定")
	}
	if req.Action != "undo" && !validTarget(d, req.TargetKind, req.TargetKey) {
		return nil, fmt.Errorf("目标 %s/%s 不属于当前派生版本", req.TargetKind, req.TargetKey)
	}

	seq, err := nextSeqLocked(tx, "decisions", pid)
	if err != nil {
		return nil, nil
	}
	dec := domain.Decision{
		ID: domain.DecisionStableID(pid, seq), Seq: seq, ProjectID: pid,
		TargetKind: req.TargetKind, TargetKey: req.TargetKey,
		DerivationID: d.ID, Action: req.Action, Reason: req.Reason,
		Active: true, CreatedAt: s.now(),
	}
	if req.Action == "undo" {
		undone, err := decisionByIDLocked(tx, pid, req.UndoID)
		if err != nil || undone == nil {
			return nil, fmt.Errorf("找不到要撤销的决定 %q", req.UndoID)
		}
		dec.TargetKind = undone.TargetKind
		dec.TargetKey = undone.TargetKey
		dec.DerivationID = undone.DerivationID
		dec.SupersedesID = undone.ID
		if _, err := tx.Exec(`UPDATE decisions SET active=0 WHERE id=?`, undone.ID); err != nil {
			return nil, err
		}
	} else {
		prior, err := activeDecisionForLocked(tx, pid, req.TargetKind, req.TargetKey)
		if err != nil {
			return nil, err
		}
		if prior != nil {
			dec.SupersedesID = prior.ID
			if _, err := tx.Exec(`UPDATE decisions SET active=0 WHERE id=?`, prior.ID); err != nil {
				return nil, err
			}
		}
	}

	eseq, err := nextEventSeqLocked(tx, pid)
	if err != nil {
		return nil, err
	}
	dec.EventID = domain.EventStableID(pid, eseq)
	if _, err := tx.Exec(`INSERT INTO decisions
		(id, project_id, seq, event_id, target_kind, target_key, derivation_id,
		 action, adjustments, reason, supersedes_id, active, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		dec.ID, pid, seq, dec.EventID, dec.TargetKind, dec.TargetKey, dec.DerivationID,
		dec.Action, "{}", dec.Reason, dec.SupersedesID, 1,
		dec.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	evtType := "decision_" + req.Action
	payload := map[string]any{
		"decision_id": dec.ID, "target_kind": dec.TargetKind,
		"target_key": dec.TargetKey, "derivation_id": dec.DerivationID,
		"reason": dec.Reason,
	}
	if dec.SupersedesID != "" {
		payload["supersedes_id"] = dec.SupersedesID
	}
	if _, err := appendEvent(tx, pid, eseq, evtType, s.now(), payload); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &dec, nil
}

func validTarget(d *domain.Derivation, kind, key string) bool {
	switch kind {
	case "candidate":
		for _, c := range d.Stages {
			if c.Key == key {
				return true
			}
		}
	case "residue":
		for _, r := range d.Residues {
			if r.Key == key {
				return true
			}
		}
	}
	return false
}

func decisionByIDLocked(tx *sql.Tx, pid, id string) (*domain.Decision, error) {
	row := tx.QueryRow(`SELECT id, project_id, seq, event_id, target_kind, target_key,
		derivation_id, action, adjustments, reason, supersedes_id, active, created_at
		FROM decisions WHERE project_id=? AND id=?`, pid, id)
	d, err := scanDecision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

func activeDecisionForLocked(tx *sql.Tx, pid, kind, key string) (*domain.Decision, error) {
	row := tx.QueryRow(`SELECT id, project_id, seq, event_id, target_kind, target_key,
		derivation_id, action, adjustments, reason, supersedes_id, active, created_at
		FROM decisions WHERE project_id=? AND target_kind=? AND target_key=? AND active=1
		ORDER BY seq DESC LIMIT 1`, pid, kind, key)
	d, err := scanDecision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}

// activeDecisionsAtLocked returns decisions active at (and after replay to) a
// given event sequence. atSeq < 0 means current head.
func activeDecisionsAtLocked(tx *sql.Tx, pid string, atSeq int) ([]domain.Decision, error) {
	q := `SELECT id, project_id, seq, event_id, target_kind, target_key,
		derivation_id, action, adjustments, reason, supersedes_id, active, created_at
		FROM decisions d WHERE project_id=?`
	args := []any{pid}
	// project decisions onto their event sequence for time travel
	rows, err := tx.Query(q+` ORDER BY seq`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all, err := scanDecisions(rows)
	if err != nil {
		return nil, err
	}
	// event seq per decision
	eseq := map[string]int{}
	erows, err := tx.Query(`SELECT seq, payload FROM events WHERE project_id=? AND type LIKE 'decision_%'`, pid)
	if err != nil {
		return nil, err
	}
	defer erows.Close()
	for erows.Next() {
		var seq int
		var pl string
		if err := erows.Scan(&seq, &pl); err != nil {
			return nil, err
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(pl), &m)
		if id, ok := m["decision_id"].(string); ok {
			eseq[id] = seq
		}
	}
	var out []domain.Decision
	for _, d := range all {
		if atSeq >= 0 && eseq[d.ID] > atSeq {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDecisions(rows *sql.Rows) ([]domain.Decision, error) {
	var out []domain.Decision
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func scanDecision(rs rowScanner) (*domain.Decision, error) {
	var d domain.Decision
	var adj, ct string
	var active int
	if err := rs.Scan(&d.ID, &d.ProjectID, &d.Seq, &d.EventID, &d.TargetKind,
		&d.TargetKey, &d.DerivationID, &d.Action, &adj, &d.Reason,
		&d.SupersedesID, &active, &ct); err != nil {
		return nil, err
	}
	d.Active = active == 1
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, ct)
	_ = json.Unmarshal([]byte(adj), &d.Adjustments)
	return &d, nil
}

// Decisions returns the full decision history (including superseded/undo).
func (s *Store) Decisions(pid string) ([]domain.Decision, error) {
	rows, err := s.db.Query(`SELECT id, project_id, seq, event_id, target_kind, target_key,
		derivation_id, action, adjustments, reason, supersedes_id, active, created_at
		FROM decisions WHERE project_id=? ORDER BY seq`, pid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDecisions(rows)
}
