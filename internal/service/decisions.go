package service

import (
	"context"

	"tgareview/internal/domain"
)

// DecideRequest is a human verdict from the review page.
type DecideRequest struct {
	Kind     domain.DecisionKind `json:"kind"`
	Target   string              `json:"target_kind"`
	TargetID string              `json:"target_id"`
	Basis    string              `json:"basis"`
	Note     string              `json:"note"`
	StartS   *float64            `json:"start_s,omitempty"`
	EndS     *float64            `json:"end_s,omitempty"`
	Actor    string              `json:"actor"`
}

// Decide commits an accept/reject/adjust verdict. Contradictory verdicts open a
// conflict that must later be resolved; every step is history, never overwritten.
func (s *Service) Decide(ctx context.Context, pid string, r DecideRequest) (int64, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadState(ctx, pid)
	if err != nil {
		return 0, "", err
	}
	hadConflict := false
	for _, c := range state.Conflicts {
		if c.Open && c.ID == domain.ShortID("conflict", r.TargetID) {
			hadConflict = true
		}
	}
	d := domain.DecisionPayload{
		Kind: r.Kind, TargetKind: r.Target, TargetID: r.TargetID,
		SeqRef: state.Seq, Basis: r.Basis, Note: r.Note,
		StartS: r.StartS, EndS: r.EndS,
	}
	if _, err := s.commit(ctx, state, domain.EvDecision, r.Actor, "", decisionWrap{d: &d}); err != nil {
		return 0, "", err
	}
	cid := ""
	if !hadConflict {
		if c, ok := state.Conflicts[domain.ShortID("conflict", r.TargetID)]; ok && c.Open {
			cid = c.ID
		}
	}
	return state.Seq, cid, nil
}

// ResolveConflict commits the chosen candidate of an open conflict. Rejected
// alternatives remain stored with their reasons.
func (s *Service) ResolveConflict(ctx context.Context, pid, conflictID, chosenID, reason, actor string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadState(ctx, pid)
	if err != nil {
		return 0, err
	}
	c, ok := state.Conflicts[conflictID]
	if !ok {
		return 0, ErrUnknownConflict
	}
	if !c.Open {
		return 0, ErrConflictClosed
	}
	var losers []string
	for _, t := range c.TargetIDs {
		if t != chosenID {
			losers = append(losers, t)
		}
	}
	ev, err := s.commit(ctx, state, domain.EvConflictResolved, actor, "",
		domain.ConflictResolvedPayload{
			ConflictID: conflictID, ChosenID: chosenID, LoserIDs: losers, Reason: reason,
		})
	if err != nil {
		return 0, err
	}
	return ev.Seq, nil
}

// UndoEvent negates a previous event. History is preserved; dependent results
// become invalid/stale, never silently replaced by the newest computation.
func (s *Service) UndoEvent(ctx context.Context, pid, eventID, reason, actor string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadState(ctx, pid)
	if err != nil {
		return 0, err
	}
	found := false
	for _, ev := range state.Events {
		if ev.EventID == eventID {
			found = true
		}
	}
	if !found {
		return 0, ErrUnknownEvent
	}
	ev, err := s.commit(ctx, state, domain.EvUndo, actor, eventID,
		domain.UndoPayload{UndoneEventID: eventID, Reason: reason})
	if err != nil {
		return 0, err
	}
	return ev.Seq, nil
}
