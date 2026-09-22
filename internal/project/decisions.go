package project

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"tga-lineage/internal/identity"
	"tga-lineage/internal/model"
)

// Decide records accept/reject/adjust verdicts. A second, contradictory
// verdict on the same target does not overwrite the first: both stay visible
// and a conflict decision is appended. Retraction is itself an event; the old
// verdict remains in history and can be revisited by rewinding to its seq.
func (s *Service) Decide(ctx context.Context, pid, target, verdict, note string, paramsUID string) (*State, model.Decision, error) {
	st, err := s.State(ctx, pid)
	if err != nil {
		return nil, model.Decision{}, err
	}
	if !targetExists(st, target) {
		return nil, model.Decision{}, fmt.Errorf("no result with identity %q in project", target)
	}
	d := model.Decision{
		UID:        identity.Hash("dec_", pid, target, verdict, strconv.FormatInt(st.Seq+1, 10), note),
		TargetUID:  target,
		TargetKind: targetKind(st, target),
		Verdict:    verdict,
		Note:       note,
		ParamsUID:  paramsUID,
	}
	// conflict detection against the latest effective decision
	if eff := st.EffectiveDecision(target); eff != nil && contradicts(eff.Verdict, verdict) {
		d.ConflictsWith = eff.UID
	}
	if d.ConflictsWith != "" {
		conflict := d
		conflict.UID = identity.Hash("cnf_", d.UID)
		conflict.Verdict = model.DecisionConflict
		if _, err := s.store.AppendAt(ctx, pid, model.EvtDecisionMade, lastEventUID(st),
			s.clock.Now().UTC().Format(time.RFC3339),
			"conflicting decision recorded",
			model.DecisionPayload{Decision: conflict}); err != nil {
			return nil, model.Decision{}, err
		}
		st.Decisions = append(st.Decisions, conflict)
	}
	if _, err := s.store.AppendAt(ctx, pid, model.EvtDecisionMade, lastEventUID(st),
		s.clock.Now().UTC().Format(time.RFC3339),
		"decision: "+verdict,
		model.DecisionPayload{Decision: d}); err != nil {
		return nil, model.Decision{}, err
	}
	if verdict == model.DecisionAdjusted && paramsUID != "" {
		if err := s.resolveAfterAdjust(ctx, pid, paramsUID, note); err != nil {
			return nil, model.Decision{}, err
		}
	}
	out, err := s.State(ctx, pid)
	return out, d, err
}

// Retract appends a retraction decision; the original decision remains.
func (s *Service) Retract(ctx context.Context, pid, decisionUID, note string) (*State, error) {
	st, err := s.State(ctx, pid)
	if err != nil {
		return nil, err
	}
	var target string
	for _, d := range st.Decisions {
		if d.UID == decisionUID {
			target = d.TargetUID
		}
	}
	if target == "" {
		return nil, fmt.Errorf("decision %q not found", decisionUID)
	}
	r := model.Decision{
		UID:       identity.Hash("ret_", pid, decisionUID, strconv.FormatInt(st.Seq+1, 10)),
		TargetUID: target, TargetKind: "decision", Verdict: model.DecisionRetracted,
		Note: "retract " + decisionUID + ": " + note,
	}
	if _, err := s.store.AppendAt(ctx, pid, model.EvtDecisionMade, lastEventUID(st),
		s.clock.Now().UTC().Format(time.RFC3339), "decision retracted",
		model.DecisionPayload{Decision: r}); err != nil {
		return nil, err
	}
	return s.State(ctx, pid)
}

// ResolveConflict explicitly resolves a conflict and is written to history.
func (s *Service) ResolveConflict(ctx context.Context, pid, target, keepVerdict, note string) (*State, error) {
	st, err := s.State(ctx, pid)
	if err != nil {
		return nil, err
	}
	d := model.Decision{
		UID:       identity.Hash("rsl_", pid, target, keepVerdict, strconv.FormatInt(st.Seq+1, 10)),
		TargetUID: target, TargetKind: targetKind(st, target),
		Verdict: keepVerdict, Note: note,
	}
	if _, err := s.store.AppendAt(ctx, pid, model.EvtConflictResolved, lastEventUID(st),
		s.clock.Now().UTC().Format(time.RFC3339), "conflict resolved: "+keepVerdict,
		model.DecisionPayload{Decision: d}); err != nil {
		return nil, err
	}
	return s.State(ctx, pid)
}

func (s *Service) resolveAfterAdjust(ctx context.Context, pid, paramsUID, note string) error {
	st, err := s.State(ctx, pid)
	if err != nil {
		return err
	}
	return s.recompute(ctx, pid, st, lastEventUID(st), "recomputed after parameter adjustment: "+note)
}

func contradicts(a, b string) bool {
	if a == b || a == model.DecisionRetracted || b == model.DecisionRetracted {
		return false
	}
	if a == model.DecisionConflict || b == model.DecisionConflict {
		return false
	}
	neg := map[string]string{
		model.DecisionAccepted: model.DecisionRejected,
		model.DecisionRejected: model.DecisionAccepted,
	}
	return neg[a] == b
}

func targetExists(st *State, uid string) bool {
	if _, ok := st.Arts[uid]; ok {
		return true
	}
	for _, a := range st.Arts {
		for _, sg := range a.Stages {
			if sg.UID == uid {
				return true
			}
		}
		for _, r := range a.Residuals {
			if r.UID == uid {
				return true
			}
		}
	}
	return false
}

func targetKind(st *State, uid string) string {
	if a, ok := st.Arts[uid]; ok {
		return a.Kind
	}
	for _, a := range st.Arts {
		for _, sg := range a.Stages {
			if sg.UID == uid {
				return "stage"
			}
		}
		for _, r := range a.Residuals {
			if r.UID == uid {
				return "residual"
			}
		}
	}
	return "unknown"
}
