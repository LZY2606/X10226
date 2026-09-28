package engine

import (
	"fmt"

	"tga/internal/domain"
)

// Decide records a human accept/reject/adjust/comment on a stage, residue or
// derivation. A contradictory decision does not overwrite the prior one: the
// prior is superseded and a conflict event records both, so resolution and
// undo are themselves history.
func (e *Engine) Decide(targetType, targetID, action, reason, actor string, params map[string]any) (*domain.Decision, []*domain.Event) {
	if actor == "" {
		actor = "reviewer"
	}
	var prevStatus string
	var conflictID int64
	for _, d := range e.decisions {
		if d.Active && d.TargetType == targetType && d.TargetID == targetID {
			prevStatus = d.NewStatus
			if contradicts(d.Action, action) {
				conflictID = d.EventSeq
			}
		}
	}

	id := domain.StableID("dec", []any{"v1", targetType, targetID, action, reason, len(e.events) + 1})
	d := &domain.Decision{
		ID: id, ProjectID: e.Project.ID, EventSeq: int64(len(e.events) + 1),
		TargetType: targetType, TargetID: targetID, Action: action,
		PrevStatus: prevStatus, NewStatus: newStatus(action, prevStatus),
		Parameter: params, Reason: reason, Actor: actor, Active: true,
	}
	ev := e.buildEvent(e.Project.ID, domain.EvDecision, map[string]any{
		"target_type": targetType, "target_id": targetID, "action": action,
		"reason": reason, "actor": actor,
	}, nil)
	putDecision(ev, d)
	e.commit(ev)
	events := []*domain.Event{ev}

	if conflictID != 0 {
		var oldID string
		for _, od := range e.decisions {
			if od.EventSeq == conflictID && od.TargetID == targetID {
				oldID = od.ID
			}
		}
		cev := e.buildEvent(e.Project.ID, domain.EvConflict, map[string]any{
			"target_type": targetType, "target_id": targetID,
			"superseded_decision": oldID, "new_seq": ev.Seq,
			"resolution": "latest_explicit_decision_wins; prior preserved for audit",
		}, []string{oldID, id})
		e.commit(cev)
		events = append(events, cev)
	}

	// A parameter adjustment from the review page becomes a real parameter
	// revision and triggers dependency-scoped recomputation.
	if action == "adjust" && len(params) > 0 {
		if np, ok := paramsToParams(params); ok {
			events = append(events, e.ReviseParams(np)...)
		}
	}
	return d, events
}

// UndoDecision marks a decision inactive and records the reversal.
func (e *Engine) UndoDecision(decisionID, reason string) *domain.Event {
	d := e.decisions[decisionID]
	if d == nil {
		return nil
	}
	ev := e.buildEvent(e.Project.ID, domain.EvUndo, map[string]any{
		"target_decision": decisionID, "reason": reason,
		"restored_status": d.PrevStatus,
	}, []string{decisionID})
	return e.commit(ev)
}

func contradicts(a, b string) bool {
	if a == b || a == "comment" || b == "comment" || a == "adjust" || b == "adjust" {
		return false
	}
	return (a == "accept" && b == "reject") || (a == "reject" && b == "accept")
}

func newStatus(action, prev string) string {
	switch action {
	case "accept":
		return "accepted"
	case "reject":
		return "rejected"
	case "adjust":
		if prev != "" {
			return prev
		}
		return "adjusted"
	}
	return prev
}

func paramsToParams(in map[string]any) (domain.Params, bool) {
	p := domain.DefaultParams()
	if cur, ok := in["gas_delay_s"]; ok {
		if f, ok := toFloat(cur); ok {
			p.GasDelayS = f
		}
	}
	if cur, ok := in["temp_lag_c"]; ok {
		if f, ok := toFloat(cur); ok {
			p.TempLagC = f
		}
	}
	if cur, ok := in["min_prominence"]; ok {
		if f, ok := toFloat(cur); ok {
			p.MinProminence = f
		}
	}
	if cur, ok := in["min_stage_loss_pct"]; ok {
		if f, ok := toFloat(cur); ok {
			p.MinStageLossPct = f
		}
	}
	if cur, ok := in["smoothing_window_n"]; ok {
		if f, ok := toFloat(cur); ok {
			p.SmoothingWindowN = int(f)
		}
	}
	if cur, ok := in["gap_max_bridge_n"]; ok {
		if f, ok := toFloat(cur); ok {
			p.GapMaxBridgeN = int(f)
		}
	}
	if cur, ok := in["noise_monotonic_window_n"]; ok {
		if f, ok := toFloat(cur); ok {
			p.NoiseMonotonicWindowN = int(f)
		}
	}
	return p, true
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	}
	return 0, false
}

var _ = fmt.Sprintf
