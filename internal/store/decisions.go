package store

import (
	"context"
	"encoding/json"
	"fmt"

	"tgaaudit/internal/domain"
)

// Decide records a human accept/reject/adjust decision. A second decision on
// the same target supersedes the first; if verdicts conflict a conflict is
// raised and must be explicitly resolved.
func (s *Store) Decide(ctx context.Context, projectID, by, targetKind, targetID,
	verdict, rationale string, adjustment map[string]float64) (*domain.Decision, *domain.Conflict, error) {
	// Target must be a known node (checked against projection).
	var exists int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(1) FROM nodes WHERE project_id=? AND id=?`,
		projectID, targetID).Scan(&exists); err != nil {
		return nil, nil, err
	}
	if targetKind == "node" && exists == 0 {
		return nil, nil, fmt.Errorf("decision target %s is not a known node", targetID)
	}
	var prevID string
	_ = s.db.QueryRowContext(ctx,
		`SELECT id FROM decisions WHERE project_id=? AND target_id=?
		 AND undone_event_seq IS NULL ORDER BY event_seq DESC LIMIT 1`,
		projectID, targetID).Scan(&prevID)

	ev, err := s.appendEvent(ctx, projectID, domain.EvDecision, map[string]any{
		"by": by, "target_kind": targetKind, "target_id": targetID,
		"verdict": verdict, "rationale": rationale, "adjustment": adjustment,
		"supersedes": prevID,
	})
	if err != nil {
		return nil, nil, err
	}
	dec := domain.Decision{
		ID:       deterministicID("dec", projectID, targetID, fmt.Sprint(ev.Seq)),
		EventSeq: ev.Seq, At: ev.At, By: by, TargetKind: targetKind,
		TargetID: targetID, Verdict: verdict, Rationale: rationale,
		Adjustment: adjustment, Supersedes: prevID,
	}
	adj, _ := json.Marshal(adjustment)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO decisions(project_id,id,event_seq,at,by_name,target_kind,
		   target_id,verdict,rationale,adjustment,supersedes)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		projectID, dec.ID, ev.Seq, ev.At, by, targetKind, targetID, verdict,
		rationale, string(adj), prevID); err != nil {
		return nil, nil, err
	}

	var conflict *domain.Conflict
	if prevID != "" {
		var oldVerdict string
		if err := s.db.QueryRowContext(ctx,
			`SELECT verdict FROM decisions WHERE project_id=? AND id=?`,
			projectID, prevID).Scan(&oldVerdict); err == nil &&
			verdictsConflict(oldVerdict, verdict) {
			c := domain.Conflict{
				ID: deterministicID("cf", projectID, prevID, dec.ID),
				At: ev.At, DecisionA: prevID, DecisionB: dec.ID, TargetID: targetID,
			}
			if _, err := s.appendEvent(ctx, projectID, domain.EvConflict, c); err != nil {
				return nil, nil, err
			}
			if _, err := s.db.ExecContext(ctx,
				`INSERT INTO conflicts(project_id,id,at,decision_a,decision_b,target_id)
				 VALUES(?,?,?,?,?,?)`,
				projectID, c.ID, c.At, c.DecisionA, c.DecisionB, c.TargetID); err != nil {
				return nil, nil, err
			}
			// mark the newer decision as conflicted pending resolution
			_, _ = s.db.ExecContext(ctx,
				`UPDATE decisions SET conflict_id=? WHERE project_id=? AND id=?`,
				c.ID, projectID, dec.ID)
			dec.ConflictID = c.ID
			conflict = &c
		}
	}
	return &dec, conflict, nil
}

func verdictsConflict(a, b string) bool {
	opposite := map[string]string{
		domain.VerdictAccepted: domain.VerdictRejected,
		domain.VerdictRejected: domain.VerdictAccepted,
	}
	return opposite[a] == b
}

// ResolveConflict records the winning decision and reason.
func (s *Store) ResolveConflict(ctx context.Context, projectID, conflictID,
	winnerID, reason string) (*DerivationSnapshot, error) {
	var c domain.Conflict
	pjStr := ""
	err := s.db.QueryRowContext(ctx,
		`SELECT id,at,decision_a,decision_b,target_id FROM conflicts
		 WHERE project_id=? AND id=?`, projectID, conflictID).Scan(
		&c.ID, &c.At, &c.DecisionA, &c.DecisionB, &c.TargetID)
	if err != nil {
		return nil, err
	}
	if winnerID != c.DecisionA && winnerID != c.DecisionB {
		return nil, fmt.Errorf("winner must be one of the conflicting decisions")
	}
	c.Winner = winnerID
	c.Reason = reason
	ev, err := s.appendEvent(ctx, projectID, domain.EvResolved, c)
	if err != nil {
		return nil, err
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE conflicts SET resolved_event_seq=?, winner=?, reason=?
		 WHERE project_id=? AND id=?`,
		ev.Seq, winnerID, reason, projectID, conflictID)
	if err != nil {
		return nil, err
	}
	_ = pjStr
	return s.Snapshot(ctx, projectID, 0)
}

// Undo logically reverses a decision event. The original event is retained;
// the undo is itself an event, so history stays replayable.
func (s *Store) Undo(ctx context.Context, projectID string, decisionSeq int,
	reason string) (*DerivationSnapshot, error) {
	var id, kind string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, verdict FROM decisions WHERE project_id=? AND event_seq=?`,
		projectID, decisionSeq).Scan(&id, &kind)
	if err != nil {
		return nil, fmt.Errorf("no decision at event seq %d: %w", decisionSeq, err)
	}
	ev, err := s.appendEvent(ctx, projectID, domain.EvUndid, map[string]any{
		"undone_seq": decisionSeq, "kind": "decision", "reason": reason,
	})
	if err != nil {
		return nil, err
	}
	u := domain.UndoRecord{ID: deterministicID("undo", projectID, fmt.Sprint(ev.Seq)),
		At: ev.At, EventSeq: ev.Seq, Kind: "decision",
		Reason: reason, UndoneSeq: decisionSeq}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO undos(project_id,id,at,event_seq,undone_event_seq,kind,reason)
		 VALUES(?,?,?,?,?,?,?)`,
		projectID, u.ID, u.At, ev.Seq, decisionSeq, "decision", reason); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE decisions SET undone_event_seq=? WHERE project_id=? AND event_seq=?`,
		ev.Seq, projectID, decisionSeq); err != nil {
		return nil, err
	}
	return s.Snapshot(ctx, projectID, 0)
}
