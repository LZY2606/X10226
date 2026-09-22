package project

import (
	"testing"

	"tgareview/internal/domain"
	"tgareview/internal/engine"
)

// Local source change invalidates only dependent derived results; stale ones
// remain viewable and never masquerade as current.
func TestLocalChangeStaleness(t *testing.T) {
	svc, _, clk, ctx := newTestService(t)
	pid, _ := importBaseline(t, svc, ctx)
	art1 := currentArtifact(t, svc, ctx, pid)

	clk.Advance(0)
	// revise a parameter -> new artifact, old retained but stale
	p := engine.DefaultParams()
	p.ThermalLagTauS = 25
	_, _, err := svc.ReviseParams(ctx, pid, p, "lag")
	if err != nil {
		t.Fatal(err)
	}
	st, _ := svc.CurrentState(ctx, pid)
	art2ID := st.CurrentArtifactID()
	if art2ID == art1.ID {
		t.Fatal("artifact id must change with params")
	}
	if !st.IsStale(st.Artifacts[art1.ID]) {
		t.Fatal("first artifact must be stale")
	}
	if st.IsStale(st.Artifacts[art2ID]) {
		t.Fatal("new artifact must be current")
	}
}

// Returning to an old event restores that moment's dependencies and visible
// conclusions; later computation does not overwrite history.
func TestHistoryTimeTravel(t *testing.T) {
	svc, _, _, ctx := newTestService(t)
	pid, base := importBaseline(t, svc, ctx)
	importSeq := base.Sequence

	// accept a candidate on the current artifact
	art := currentArtifact(t, svc, ctx, pid)
	if len(art.Candidates) == 0 {
		t.Fatal("no candidates")
	}
	target := art.Candidates[0].ID
	d, err := svc.Decide(ctx, pid, art.ID, target, "accept", "", "baseline accept")
	if err != nil {
		t.Fatal(err)
	}
	decideSeq := d.Seq

	// revise params after the decision
	p := engine.DefaultParams()
	p.AtmosphereDelayS = 40
	if _, _, err := svc.ReviseParams(ctx, pid, p, "later"); err != nil {
		t.Fatal(err)
	}

	// view at import seq: decision must not exist yet
	past, err := svc.StateAt(ctx, pid, importSeq)
	if err != nil {
		t.Fatal(err)
	}
	if got := past.DecisionFor(target); got != nil {
		t.Fatal("decision must not exist before its event")
	}
	// view at decision seq: decision live; params still default
	atDec, err := svc.StateAt(ctx, pid, decideSeq)
	if err != nil {
		t.Fatal(err)
	}
	if got := atDec.DecisionFor(target); got == nil {
		t.Fatal("decision should be live at its seq")
	}
	if atDec.Params.AtmosphereDelayS != engine.DefaultParams().AtmosphereDelayS {
		t.Fatal("historical view must restore params of that moment")
	}
}

// Conflicting accepts are both kept in history with a conflict edge; undo
// restores prior state and is itself recorded.
func TestConflictAndUndo(t *testing.T) {
	svc, _, _, ctx := newTestService(t)
	pid, _ := importBaseline(t, svc, ctx)
	art := currentArtifact(t, svc, ctx, pid)

	// find a merged candidate that has split alternatives
	var merged, split string
	for _, c := range art.Candidates {
		if c.Option == "merged" && len(c.OverlapsWith) > 0 {
			merged = c.ID
			for _, o := range c.OverlapsWith {
				if cand := findCand(art, o); cand != nil && cand.Option == "split_a" {
					split = o
				}
			}
		}
	}
	if merged == "" || split == "" {
		t.Fatal("baseline must include overlapping merged/split candidates")
	}
	d1, err := svc.Decide(ctx, pid, art.ID, merged, "accept", "", "take merged")
	if err != nil {
		t.Fatal(err)
	}
	d2, err := svc.Decide(ctx, pid, art.ID, split, "accept", "", "take split (conflicts)")
	if err != nil {
		t.Fatal(err)
	}
	if d2.ConflictOf == "" {
		t.Fatal("second accept must record a conflict edge")
	}
	st, _ := svc.CurrentState(ctx, pid)
	if len(st.DecisionLog) < 2 {
		t.Fatal("both decisions retained in history")
	}
	// d2 supersedes d1 for the merged target only for split target; both targets still have live accepts
	if st.DecisionFor(merged) == nil || st.DecisionFor(split) == nil {
		t.Fatal("both conflicting accepts remain live until undone")
	}
	// undo d2 -> still in log but not live
	if _, err := svc.UndoDecision(ctx, pid, d2.ID); err != nil {
		t.Fatal(err)
	}
	st2, _ := svc.CurrentState(ctx, pid)
	if st2.DecisionFor(split) != nil {
		t.Fatal("undone decision must not be live")
	}
	if st2.DecisionFor(merged) == nil {
		t.Fatal("the other decision survives undo")
	}
	var foundUndone bool
	for _, x := range st2.DecisionLog {
		if x.ID == d2.ID && x.Undone {
			foundUndone = true
		}
	}
	if !foundUndone {
		t.Fatal("undone decision kept in history with undone flag")
	}
	_ = d1
}

// Immutable raw layers: a revision chain is appended, never overwritten.
func TestRevisionChainImmutable(t *testing.T) {
	svc, _, _, ctx := newTestService(t)
	pid, _ := importBaseline(t, svc, ctx)
	st, _ := svc.CurrentState(ctx, pid)
	head1 := st.HeadRev[domain.InitialMass]

	m := loadMutation(t, "M6_initial_mass_revision")
	data := mustJSON(m["layer"].(map[string]any)["data"])
	b := oneLayerBundle("M6", "initial_mass", data, head1)
	if _, err := svc.Import(ctx, pid, b); err != nil {
		t.Fatal(err)
	}
	st2, _ := svc.CurrentState(ctx, pid)
	head2 := st2.HeadRev[domain.InitialMass]
	if head2 == head1 {
		t.Fatal("revision must produce a new head id")
	}
	if st2.RevParent[head2] != head1 {
		t.Fatal("new revision must point at its parent revision")
	}
	if st2.RevParent[head1] != "" {
		t.Fatal("first revision must have empty parent")
	}
}

func findCand(art *engine.Artifact, id string) *engine.StageCandidate {
	for i := range art.Candidates {
		if art.Candidates[i].ID == id {
			return &art.Candidates[i]
		}
	}
	return nil
}
