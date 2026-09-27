package store

import (
	"database/sql"
	"errors"

	"tga/internal/domain"
)

// ErrNoRaw means derivation was requested before raw samples were committed.
var ErrNoRaw = errors.New("raw_samples layer is required before analysis")

// Decide records a human accept/reject/adjust/note decision. Adjustments are
// parameter overrides for a specific stage and never mutate the underlying
// result; conflict resolution is itself recorded as a decision history.
func (s *Store) Decide(projectID, targetKind, targetID, analysisID, action,
	reason, reviewer string, adjust map[string]float64, actor string) (*domain.Decision, error) {
	p, err := s.LoadProject(projectID)
	if err != nil {
		return nil, err
	}
	switch action {
	case "accept", "reject", "adjust", "note":
	default:
		return nil, errors.New("unknown action: " + action)
	}
	if !targetExists(p, targetKind, targetID, analysisID) {
		return nil, ErrNotFound
	}
	d := &domain.Decision{
		ProjectID: projectID, TargetKind: targetKind, TargetID: targetID,
		AnalysisID: analysisID, Action: action, Reason: reason,
		Reviewer: reviewer, Adjust: adjust,
	}
	d.ID = domain.ShortID("dec", map[string]any{
		"project": projectID, "target": targetID, "action": action,
		"reason": reason, "reviewer": reviewer, "adjust": adjust,
		"after": decisionCount(p, targetID)})
	err = s.tx(func(tx *sql.Tx) error {
		var seq int64
		if err := tx.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM events
		 WHERE project_id=?`, projectID).Scan(&seq); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO decisions
		 (id,project_id,at_event_seq,target_kind,target_id,analysis_id,action,
		  reason,reviewer,adjust,created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			d.ID, projectID, seq, targetKind, targetID, analysisID, action,
			reason, reviewer, mustJSON(adjust), s.now()); err != nil {
			return err
		}
		_, e := s.appendEventTx(tx, projectID, "decision_made", actor,
			map[string]any{"decision": jsonMap(d)})
		return e
	})
	if err != nil {
		return nil, err
	}
	return d, nil
}

// UndoDecision reverses a prior adjudication with a recorded undo event; the
// original decision is retained and marked undone rather than deleted.
func (s *Store) UndoDecision(projectID, decisionID, reviewer, actor string) (*domain.Decision, error) {
	p, err := s.LoadProject(projectID)
	if err != nil {
		return nil, err
	}
	var target *domain.Decision
	for _, d := range p.Decisions {
		if d.ID == decisionID {
			target = d
		}
	}
	if target == nil {
		return nil, ErrNotFound
	}
	undoID := domain.ShortID("undo", map[string]any{"decision": decisionID, "reviewer": reviewer})
	err = s.tx(func(tx *sql.Tx) error {
		if _, e := tx.Exec(`UPDATE decisions SET undone=1, undone_by=? WHERE id=?`,
			undoID, decisionID); e != nil {
			return e
		}
		_, e := s.appendEventTx(tx, projectID, "decision_undone", actor,
			map[string]any{"decision_id": decisionID, "undo_id": undoID,
				"reviewer": reviewer})
		return e
	})
	if err != nil {
		return nil, err
	}
	target.Undone = true
	target.UndoneBy = undoID
	return target, nil
}

func decisionCount(p *Project, targetID string) int {
	n := 0
	for _, d := range p.Decisions {
		if d.TargetID == targetID {
			n++
		}
	}
	return n
}

func targetExists(p *Project, kind, id, analysisID string) bool {
	for _, a := range p.Analyses {
		if a.ID != analysisID {
			continue
		}
		switch kind {
		case "stage":
			for _, st := range a.Result.Stages {
				if st.ID == id {
					return true
				}
			}
		case "residue":
			for _, r := range a.Result.Residues {
				if r.ID == id {
					return true
				}
			}
		}
	}
	return false
}
