package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"tga-review/internal/domain"
	"tga-review/internal/ingest"
)

// DecisionRequest is a human verdict from the review UI.
type DecisionRequest struct {
	TargetID   string         `json:"target_id"`
	State      string         `json:"state"` // accepted | rejected | adjusted
	Reason     string         `json:"reason"`
	Adjustment map[string]any `json:"adjustment"`
}

// Decide records a human verdict. Accepting mutually exclusive alternatives
// within an ambiguous group raises a first-class conflict; both verdicts are
// retained until explicit resolution. Undo appends an undo event.
func (e *Engine) Decide(ctx context.Context, projectID string, req DecisionRequest) (*domain.Decision, error) {
	st, err := e.loadState(ctx, projectID)
	if err != nil {
		return nil, err
	}
	target := findTarget(st, req.TargetID)
	if target == nil {
		return nil, fmt.Errorf("target %q not found among current candidates/residue", req.TargetID)
	}
	if !validState(req.State) {
		return nil, fmt.Errorf("invalid decision state %q", req.State)
	}
	dec := domain.Decision{
		TargetID: req.TargetID, State: req.State, Actor: e.actor,
		At: e.clk.Now(), Reason: req.Reason, Adjustment: req.Adjustment,
	}
	if req.State == domain.CandAdjusted {
		dec.TargetID = adjustedID(req.TargetID, req.Adjustment)
		dec.SupersedesID = req.TargetID
	}
	decisions := latestDecisions(st)
	group := conflictGroup(st, req.TargetID)
	conflictWith := ""
	if req.State == domain.CandAccepted && group != "" {
		for id, prev := range decisions {
			if id == req.TargetID || prev.State != domain.CandAccepted {
				continue
			}
			if conflictGroup(st, id) == group {
				conflictWith = id
			}
		}
	}
	dec.GroupID = group
	dec.ConflictWith = conflictWith
	decisions[dec.TargetID] = dec

	kind := domain.EvCandAccepted
	switch req.State {
	case domain.CandRejected:
		kind = domain.EvCandRejected
	case domain.CandAdjusted:
		kind = domain.EvCandAdjusted
	}
	snap := e.snapshotOf(st, st.Sources)
	snap.Decisions = decisions
	ev := &domain.Event{ProjectID: projectID, Kind: kind, At: e.clk.Tick(),
		Actor: e.actor, Snap: snap,
		Payload: map[string]any{"project_id": projectID, "target_id": dec.TargetID,
			"state": dec.State, "reason": dec.Reason, "supersedes": dec.SupersedesID,
			"group": group, "conflict_with": conflictWith}}
	seq, err := e.st.AppendEvent(ctx, ev)
	if err != nil {
		return nil, err
	}
	dec.EventSeq = seq
	if conflictWith != "" {
		// mark both sides as conflict, keep both verdicts
		decisions[dec.TargetID] = dec
		if other, ok := decisions[conflictWith]; ok {
			other.State = domain.CandConflict
			other.ConflictWith = dec.TargetID
			decisions[conflictWith] = other
		}
		confSnap := e.snapshotOf(st, st.Sources)
		confSnap.Decisions = decisions
		_, err = e.st.AppendEvent(ctx, &domain.Event{
			ProjectID: projectID, Kind: domain.EvConflict, At: e.clk.Tick(),
			Actor: e.actor, Snap: confSnap,
			Payload: map[string]any{
				"project_id": projectID, "group": group,
				"sides": []string{conflictWith, dec.TargetID},
				"message": "mutually exclusive candidates accepted; both retained pending resolution",
			}})
		if err != nil {
			return nil, err
		}
	}
	return &dec, nil
}

// ResolveConflict settles a conflict group in favor of winnerID and records
// the losing side as rejected. Nothing is deleted.
func (e *Engine) ResolveConflict(ctx context.Context, projectID, group, winnerID, reason string) error {
	st, err := e.loadState(ctx, projectID)
	if err != nil {
		return err
	}
	decisions := latestDecisions(st)
	var sides []string
	for id, d := range decisions {
		if d.GroupID == group {
			sides = append(sides, id)
			if id != winnerID {
				d.State = domain.CandRejected
				d.Reason = "conflict resolved against this candidate: " + reason
				decisions[id] = d
			} else {
				d.State = domain.CandAccepted
				d.Reason = "conflict resolved in favor: " + reason
				decisions[id] = d
			}
		}
	}
	sort.Strings(sides)
	snap := e.snapshotOf(st, st.Sources)
	snap.Decisions = decisions
	_, err = e.st.AppendEvent(ctx, &domain.Event{
		ProjectID: projectID, Kind: domain.EvConflictResolved, At: e.clk.Tick(),
		Actor: e.actor, Snap: snap,
		Payload: map[string]any{"project_id": projectID, "group": group,
			"winner": winnerID, "sides": sides, "reason": reason}})
	return err
}

// Undo reverses the most recent human decision event. It appends an undo
// event rather than deleting history, so the prior conclusion is reproducible.
func (e *Engine) Undo(ctx context.Context, projectID, targetID string) error {
	st, err := e.loadState(ctx, projectID)
	if err != nil {
		return err
	}
	decisions := latestDecisions(st)
	removed := ""
	if targetID == "" {
		// undo latest decision by event seq
		var latest int64
		for id, d := range decisions {
			if d.EventSeq > latest {
				latest, removed = d.EventSeq, id
			}
		}
	} else {
		if _, ok := decisions[targetID]; !ok {
			return fmt.Errorf("no decision for %q", targetID)
		}
		removed = targetID
	}
	if removed == "" {
		return fmt.Errorf("nothing to undo")
	}
	delete(decisions, removed)
	snap := e.snapshotOf(st, st.Sources)
	snap.Decisions = decisions
	_, err = e.st.AppendEvent(ctx, &domain.Event{
		ProjectID: projectID, Kind: domain.EvUndo, At: e.clk.Tick(),
		Actor: e.actor, Snap: snap,
		Payload: map[string]any{"project_id": projectID, "target_id": removed,
			"message": "decision undone; underlying derived node left intact"}})
	return err
}

// Judgement records a machine-readable counterexample judgement event.
func (e *Engine) Judgement(ctx context.Context, projectID string, j map[string]any) error {
	st, err := e.loadState(ctx, projectID)
	if err != nil {
		return err
	}
	j["project_id"] = projectID
	snap := e.snapshotOf(st, st.Sources)
	snap.Decisions = latestDecisions(st)
	_, err = e.st.AppendEvent(ctx, &domain.Event{
		ProjectID: projectID, Kind: domain.EvCaseJudgement, At: e.clk.Tick(),
		Actor: "casepack", Snap: snap, Payload: j})
	return err
}

func latestDecisions(st *State) map[string]domain.Decision {
	out := map[string]domain.Decision{}
	if len(st.Events) > 0 {
		last := st.Events[len(st.Events)-1].Snap
		if last != nil {
			for k, d := range last.Decisions {
				out[k] = d
			}
		}
	}
	return out
}

func validState(s string) bool {
	switch s {
	case domain.CandAccepted, domain.CandRejected, domain.CandAdjusted:
		return true
	}
	return false
}

func findTarget(st *State, id string) targetLike {
	if st.Residue != nil && st.Residue.ID == id {
		return &residueTarget{r: st.Residue}
	}
	for i := range st.Stages {
		if st.Stages[i].ID == id {
			return &stageTarget{s: &st.Stages[i]}
		}
		for j := range st.Stages[i].Candidates {
			if st.Stages[i].Candidates[j].ID == id {
				return &stageTarget{s: &st.Stages[i].Candidates[j]}
			}
		}
	}
	return nil
}

type targetLike interface{ id() string }
type stageTarget struct{ s *domain.Stage }
func (t *stageTarget) id() string { return t.s.ID }
type residueTarget struct{ r *domain.Residue }
func (t *residueTarget) id() string { return t.r.ID }

// conflictGroup identifies an ambiguous group: the stage id prefix before ':'.
func conflictGroup(st *State, targetID string) string {
	for i := range st.Stages {
		g := st.Stages[i]
		if g.ID == targetID || strings.HasPrefix(targetID, g.ID) {
			if g.Ambiguous || g.CrossesGap {
				return g.ID
			}
			for _, c := range g.Candidates {
				if c.ID == targetID {
					return g.ID
				}
			}
		}
	}
	return ""
}

func adjustedID(base string, adj map[string]any) string {
	return ingest.ShortID("adj", base, adj)
}
