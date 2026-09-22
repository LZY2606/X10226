package app

import (
	"context"
	"fmt"

	"tga-review/internal/domain"
	"tga-review/internal/store"
)

// Decide records a human accept/reject/adjust/uncertain decision on a stage or
// residue. Conflicting decisions on the same target are both retained; a later
// conflict_resolved event names the winner.
func (s *Service) Decide(ctx context.Context, projectID, reviewer string, d domain.DecisionPayload) (string, error) {
	st, err := s.Store.Replay(ctx, projectID, 0)
	if err != nil {
		return "", err
	}
	if st.CurrentRun == nil {
		return "", fmt.Errorf("no current run to decide on")
	}
	if d.TargetType != "stage" && d.TargetType != "residue" {
		return "", fmt.Errorf("target_type must be stage or residue")
	}
	conflictOf := ""
	if d.TargetType == "stage" {
		if !stageExists(st.CurrentRun, d.TargetID) {
			return "", fmt.Errorf("unknown stage %s", d.TargetID)
		}
	} else {
		if st.CurrentRun.Residue.ID != d.TargetID {
			return "", fmt.Errorf("unknown residue %s", d.TargetID)
		}
	}
	if prev := priorDecision(st, d.TargetID); prev != "" && d.Action != prevAction(st, d.TargetID) {
		conflictOf = prev
	}
	d.RunID = st.CurrentRun.RunID
	ev, err := s.appendEvent(ctx, projectID, domain.EvtDecision, reviewer, d, "", conflictOf)
	if err != nil {
		return "", err
	}
	if conflictOf != "" {
		// automatic conflict record: the new decision is the challenger; the
		// reviewer must resolve explicitly (ResolveConflict) — here we simply
		// leave both visible and return the linkage in the event metadata.
		_ = conflictOf
	}
	return ev.ID, nil
}

// ResolveConflict records which of two decisions wins; history keeps both.
func (s *Service) ResolveConflict(ctx context.Context, projectID, reviewer, objectID, firstID, secondID, winner, resolution string) (string, error) {
	if winner != "first" && winner != "second" {
		return "", fmt.Errorf("winner must be first or second")
	}
	p := domain.ConflictResolvedPayload{
		ObjectID: objectID, FirstDecision: firstID, SecondDecision: secondID,
		Winner: winner, Resolution: resolution,
	}
	ev, err := s.appendEvent(ctx, projectID, domain.EvtConflict, reviewer, p, "", "")
	return ev.ID, err
}

// Undo reverses a previous event (decision, params revision, run). The
// reversed event stays in the ledger; replay simply skips it.
func (s *Service) Undo(ctx context.Context, projectID, reviewer, eventID, reason string) (string, error) {
	st, err := s.Store.Replay(ctx, projectID, 0)
	if err != nil {
		return "", err
	}
	for _, ev := range st.Events {
		if ev.ID == eventID {
			if ev.Type == domain.EvtProjectCreated || ev.Type == domain.EvtBatchImported {
				return "", fmt.Errorf("cannot undo %s events (raw inputs are immutable)", ev.Type)
			}
			p := domain.UndoPayload{ReversedEventID: eventID, Reason: reason}
			rev, err := s.appendEvent(ctx, projectID, domain.EvtUndo, reviewer, p, eventID, "")
			return rev.ID, err
		}
	}
	return "", fmt.Errorf("event %s not found", eventID)
}

func stageExists(run *domain.DerivedSet, id string) bool {
	for _, st := range run.Stages {
		if st.ID == id {
			return true
		}
	}
	return false
}

func priorDecision(st *store.State, target string) string {
	for i := len(st.Decisions) - 1; i >= 0; i-- {
		d := st.Decisions[i]
		if !d.Undone && d.Payload.TargetID == target {
			return d.EventID
		}
	}
	return ""
}

func prevAction(st *store.State, target string) string {
	for i := len(st.Decisions) - 1; i >= 0; i-- {
		d := st.Decisions[i]
		if !d.Undone && d.Payload.TargetID == target {
			return d.Payload.Action
		}
	}
	return ""
}
