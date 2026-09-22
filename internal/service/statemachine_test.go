package service_test

import (
	"testing"

	"tgareview/internal/domain"
	"tgareview/internal/service"
)

// TestDecisionsConflictUndo verifies accept/reject opens a conflict, the
// resolution and undo are both recorded in history, and time travel restores
// the visible conclusions of old events.
func TestDecisionsConflictUndo(t *testing.T) {
	h := newHarness(t)
	ctx := ctx()
	pid := h.baselineProject(ctx)

	head, err := h.svc.HeadView(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	var stageID string
	for _, s := range head.View.Stages {
		if s.Rejected == nil {
			stageID = s.ID
		}
	}
	if stageID == "" {
		t.Fatal("no stage candidate to adjudicate")
	}

	if _, _, err := h.svc.Decide(ctx, pid, service.DecideRequest{
		Kind: domain.DecAcceptStage, Target: "stage", TargetID: stageID,
		Basis: "DTG+gas", Note: "accept moisture loss", Actor: "reviewer-a",
	}); err != nil {
		t.Fatal(err)
	}
	_, cid, err := h.svc.Decide(ctx, pid, service.DecideRequest{
		Kind: domain.DecRejectStage, Target: "stage", TargetID: stageID,
		Basis: "disagree", Note: "baseline artifact", Actor: "reviewer-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cid == "" {
		t.Fatal("contradictory accept/reject must open a conflict")
	}

	resolveSeq, err := h.svc.ResolveConflict(ctx, pid, cid, stageID, "keep", "lead")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := h.svc.HeadView(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range resolved.Conflicts {
		if c.ID == cid && c.Open {
			t.Fatal("conflict should be resolved")
		}
	}

	// Time travel to before resolution: conflict must read as open again and
	// conclusions reflect that moment, not the newest computation.
	old, err := h.svc.ViewAt(ctx, pid, resolveSeq-1)
	if err != nil {
		t.Fatal(err)
	}
	foundOpen := false
	for _, c := range old.Conflicts {
		if c.ID == cid && c.Open {
			foundOpen = true
		}
	}
	if !foundOpen {
		t.Fatal("time travel did not restore the previously open conflict")
	}

	// Undo the resolution; history still contains every event.
	events, err := h.svc.RawEvents(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	var resolveEvent string
	for _, ev := range events {
		if ev.Type == domain.EvConflictResolved {
			resolveEvent = ev.EventID
		}
	}
	if _, err := h.svc.UndoEvent(ctx, pid, resolveEvent, "reopen", "lead"); err != nil {
		t.Fatal(err)
	}
	afterUndo, err := h.svc.HeadView(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range afterUndo.Conflicts {
		if c.ID == cid && !c.Open {
			t.Fatal("undo of resolution must reopen the conflict")
		}
	}
}

// TestAdjustmentCreatesVersionedCandidate verifies manual parameter adjustment
// yields a new candidate linked to the original (history is preserved).
func TestAdjustmentCreatesVersionedCandidate(t *testing.T) {
	h := newHarness(t)
	ctx := ctx()
	pid := h.baselineProject(ctx)
	head, _ := h.svc.HeadView(ctx, pid)
	var st domain.StageCandidate
	for _, s := range head.View.Stages {
		if s.Rejected == nil {
			st = s
			break
		}
	}
	start, end := st.StartS+10, st.EndS-10
	if _, _, err := h.svc.Decide(ctx, pid, service.DecideRequest{
		Kind: domain.DecAdjustStage, Target: "stage", TargetID: st.ID,
		StartS: &start, EndS: &end, Note: "tighten", Actor: "reviewer",
	}); err != nil {
		t.Fatal(err)
	}
	v, err := h.svc.HeadView(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	manual := 0
	for _, s := range v.View.Stages {
		if s.Origin == "manual" && s.Adjusted {
			manual++
			linked := false
			for _, src := range s.Prov.DirectSources {
				if src.Kind == "decision" {
					linked = true
				}
			}
			if !linked {
				t.Fatal("adjusted candidate missing decision provenance")
			}
		}
	}
	if manual == 0 {
		t.Fatal("adjustment did not create a manual candidate")
	}
}

// TestLocalInvalidation changes only drift after accepting a stage: the old
// candidate remains viewable as stale (never current), while a fresh candidate
// is computed. Only drift-dependent values move.
func TestLocalInvalidation(t *testing.T) {
	h := newHarness(t)
	ctx := ctx()
	pid := h.baselineProject(ctx)
	before, _ := h.svc.HeadView(ctx, pid)
	var acceptedID string
	for _, s := range before.View.Stages {
		if s.Rejected == nil {
			acceptedID = s.ID
			break
		}
	}
	if _, _, err := h.svc.Decide(ctx, pid, service.DecideRequest{
		Kind: domain.DecAcceptStage, Target: "stage", TargetID: acceptedID,
		Note: "baseline verdict", Actor: "reviewer",
	}); err != nil {
		t.Fatal(err)
	}
	r0 := before.View.Residues[len(before.View.Residues)-1].Pct.Value

	frag := "# drift revision\n[balance_drift]\nstart=0s rate=0.05mg/min\n"
	if _, err := h.svc.ImportFragment(ctx, pid, frag, "revision", "", "drift", "analyst"); err != nil {
		t.Fatal(err)
	}
	after, _ := h.svc.HeadView(ctx, pid)
	var stale *domain.StageCandidate
	for i := range after.View.StaleStages {
		if after.View.StaleStages[i].ID == acceptedID {
			stale = &after.View.StaleStages[i]
		}
	}
	if stale == nil {
		t.Fatal("accepted stage must remain viewable as stale after local source change")
	}
	if stale.Status != domain.StatusStale || stale.Verdict != "accepted" {
		t.Fatalf("stale stage status=%s verdict=%s", stale.Status, stale.Verdict)
	}
	for _, s := range after.View.Stages {
		if s.Status == domain.StatusStale {
			t.Fatal("a stale candidate must not be presented among current stages")
		}
	}
	r1 := after.View.Residues[len(after.View.Residues)-1].Pct.Value
	if r0 == r1 {
		t.Fatal("final residue percent should change with drift correction")
	}
}

// TestResidueChain ensures every displayed residue expands to sources.
func TestResidueChain(t *testing.T) {
	h := newHarness(t)
	ctx := ctx()
	pid := h.baselineProject(ctx)
	v, _ := h.svc.HeadView(ctx, pid)
	if len(v.View.Residues) == 0 {
		t.Fatal("no residues")
	}
	unknown, computable := 0, 0
	for _, r := range v.View.Residues {
		if len(r.Chain) < 2 {
			t.Fatalf("residue %s chain too short", r.ID)
		}
		if r.Pct.Bad {
			unknown++ // e.g. furnace landmark inside the sampling gap
			if r.Status != domain.StatusUnknown {
				t.Fatalf("unknown residue %s must carry unknown status", r.ID)
			}
		} else {
			computable++
		}
		for _, n := range r.Chain {
			if n.ID == "" || n.SourceID == "" {
				t.Fatal("chain node lacks stable identity")
			}
		}
	}
	if unknown == 0 {
		t.Fatal("baseline gap landmark should yield a first-class unknown residue")
	}
	if computable == 0 {
		t.Fatal("baseline must still have computable residues (final sample)")
	}
}
