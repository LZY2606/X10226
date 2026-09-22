package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tga-review/internal/app"
	"tga-review/internal/clock"
	"tga-review/internal/domain"
	"tga-review/internal/store"
)

func openSvc(t *testing.T) (*app.Service, string, context.Context) {
	t.Helper()
	dir := t.TempDir()
	db := filepath.Join(dir, "tga.db")
	ctx := context.Background()
	st, err := store.Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	vc := clock.NewVirtual(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC))
	return app.New(st, vc), db, ctx
}

const bundle = `# @layer samples
t[s] mass[mg] temp[degC]
0 12.5 30
120 12.05 40
320 8.9 140
560 7.1 255
760 6.0 400
900 6.0 455
# @layer drift
t[s] offset[mg] kind
0 0.01 linear
900 0.03 linear
# @layer program
t_start[s] t_end[s] temp_start[degC] temp_end[degC] label
0 900 30 455 ramp
# @layer gas
t[s] from_gas to_gas flow[sccm]
0 none N2 50
680 N2 air 50
# @layer blank
t[s] mass[mg]
0 0.02
900 0.05
# @layer initial
mass[mg] reason
12.5 "initial"
`

func seedProject(t *testing.T, svc *app.Service, ctx context.Context, text string) string {
	t.Helper()
	pid, err := svc.CreateProject(ctx, "case-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Import(ctx, pid, "case.txt", []byte(text)); err != nil {
		t.Fatal(err)
	}
	return pid
}

func TestImportRunDecideFlow(t *testing.T) {
	svc, _, ctx := openSvc(t)
	pid := seedProject(t, svc, ctx, bundle)
	set, err := svc.RunPipeline(ctx, pid, "reviewer-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Stages) == 0 {
		t.Fatal("expected stage candidates")
	}
	first := set.Stages[0].ID
	decID, err := svc.Decide(ctx, pid, "a", domain.DecisionPayload{
		TargetType: "stage", TargetID: first, Action: domain.DecisionAccept, Reason: "clean peak",
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := svc.Store.Replay(ctx, pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, sg := range st.CurrentRun.Stages {
		if sg.ID == first {
			got = sg.Status
		}
	}
	if got != "ACCEPTED" {
		t.Fatalf("want ACCEPTED got %s", got)
	}
	if _, err := svc.Undo(ctx, pid, "a", decID, "changed mind"); err != nil {
		t.Fatal(err)
	}
	st2, _ := svc.Store.Replay(ctx, pid, 0)
	for _, sg := range st2.CurrentRun.Stages {
		if sg.ID == first && sg.Status == "ACCEPTED" {
			t.Fatal("undo must revert the decision")
		}
	}
}

func TestConflictAndResolutionRecorded(t *testing.T) {
	svc, _, ctx := openSvc(t)
	pid := seedProject(t, svc, ctx, bundle)
	set, _ := svc.RunPipeline(ctx, pid, "a")
	target := set.Stages[0].ID
	d1, err := svc.Decide(ctx, pid, "a", domain.DecisionPayload{TargetType: "stage", TargetID: target, Action: domain.DecisionAccept})
	if err != nil {
		t.Fatal(err)
	}
	d2, err := svc.Decide(ctx, pid, "b", domain.DecisionPayload{TargetType: "stage", TargetID: target, Action: domain.DecisionReject, Reason: "overlap"})
	if err != nil {
		t.Fatal(err)
	}
	resID, err := svc.ResolveConflict(ctx, pid, "lead", target, d1, d2, "first", "a's evidence wins")
	if err != nil {
		t.Fatal(err)
	}
	st, _ := svc.Store.Replay(ctx, pid, 0)
	found := false
	for _, ev := range st.Events {
		if ev.ID == resID && ev.Type == domain.EvtConflict {
			found = true
		}
	}
	if !found {
		t.Fatal("conflict resolution event missing from ledger")
	}
}

func TestParamsRevisionMakesPriorRunStale(t *testing.T) {
	svc, _, ctx := openSvc(t)
	pid := seedProject(t, svc, ctx, bundle)
	first, _ := svc.RunPipeline(ctx, pid, "a")
	if _, err := svc.ReviseParams(ctx, pid, "a", "longer measured delay", domain.Params{
		GasDelayS: 45, TempLagS: 15, BlankSubtraction: true, DriftCorrection: true,
		SmoothingWindow: 5, PeakProminenceMgMin: 0.02, GapThresholdS: 60, OverlapWindowS: 40,
	}); err != nil {
		t.Fatal(err)
	}
	second, _ := svc.RunPipeline(ctx, pid, "a")
	st, _ := svc.Store.Replay(ctx, pid, 0)
	isStale := false
	for _, id := range st.StaleRuns {
		if id == first.RunID {
			isStale = true
		}
	}
	if !isStale {
		t.Fatal("first run must be marked stale after params revision")
	}
	if st.CurrentRun.RunID != second.RunID {
		t.Fatal("latest run must be current")
	}
	// old run still viewable
	var old *domain.DerivedSet
	for i := range st.Runs {
		if st.Runs[i].RunID == first.RunID {
			old = &st.Runs[i]
		}
	}
	if old == nil {
		t.Fatal("stale run must remain viewable")
	}
}

func TestRawImportImmutable(t *testing.T) {
	svc, _, ctx := openSvc(t)
	pid := seedProject(t, svc, ctx, bundle)
	// undoing a raw batch is forbidden
	st, _ := svc.Store.Replay(ctx, pid, 0)
	var batchEvt string
	for _, ev := range st.Events {
		if ev.Type == domain.EvtBatchImported {
			batchEvt = ev.ID
		}
	}
	if _, err := svc.Undo(ctx, pid, "a", batchEvt, "no"); err == nil {
		t.Fatal("raw imports must be immutable (undo rejected)")
	}
}

func TestExportDeleteImportPreservesIdentity(t *testing.T) {
	svc, dbPath, ctx := openSvc(t)
	pid := seedProject(t, svc, ctx, bundle)
	set, _ := svc.RunPipeline(ctx, pid, "a")
	stageID := set.Stages[0].ID
	resID := set.Residue.ID
	sampleID := set.Corrections[0].SampleID
	_, _ = svc.Decide(ctx, pid, "a", domain.DecisionPayload{
		TargetType: "stage", TargetID: stageID, Action: domain.DecisionAccept,
	})
	bundle, err := svc.Export(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	raw := mustMarshal(t, bundle)

	// delete the database
	if err := svc.Store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		_ = os.Remove(f)
	}
	st2, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	svc2 := app.New(st2, clock.NewVirtual(time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)))
	pid2, err := svc2.ImportBundle(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if pid2 != pid {
		t.Fatalf("project id changed: %s != %s", pid2, pid)
	}
	state, err := svc2.Store.Replay(ctx, pid2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentRun.Residue.ID != resID {
		t.Fatalf("residue id changed: %s != %s", state.CurrentRun.Residue.ID, resID)
	}
	var foundStage bool
	for _, sg := range state.CurrentRun.Stages {
		if sg.ID == stageID && sg.Status == "ACCEPTED" {
			foundStage = true
		}
	}
	if !foundStage {
		t.Fatal("stage id or decision not restored")
	}
	layers, _ := svc2.Store.Layers(ctx, pid2)
	if len(layers.Samples) == 0 || layers.Samples[0].ID != sampleID {
		t.Fatalf("raw sample identity/order not preserved: %+v", layers.Samples[:1])
	}
}

func TestAsOfReplayRestoresOldConclusion(t *testing.T) {
	svc, _, ctx := openSvc(t)
	pid := seedProject(t, svc, ctx, bundle)
	run1, _ := svc.RunPipeline(ctx, pid, "a")
	_, _ = svc.ReviseParams(ctx, pid, "a", "v2", domain.Params{
		GasDelayS: 60, TempLagS: 30, BlankSubtraction: true, DriftCorrection: true,
		SmoothingWindow: 7, PeakProminenceMgMin: 0.02, GapThresholdS: 60, OverlapWindowS: 60,
	})
	run2, _ := svc.RunPipeline(ctx, pid, "a")
	if run1.RunID == run2.RunID {
		t.Fatal("runs need distinct ids")
	}
	// replay at run1's sequence: current run must be run1, not recomputed
	events, _ := svc.Store.Events(ctx, pid)
	var run1Seq int64
	for _, ev := range events {
		if ev.Type == domain.EvtRunProduced {
			run1Seq = ev.Seq
			break
		}
	}
	old, err := svc.Store.Replay(ctx, pid, run1Seq)
	if err != nil {
		t.Fatal(err)
	}
	if old.CurrentRun == nil || old.CurrentRun.RunID != run1.RunID {
		t.Fatalf("as-of replay must restore run %s, got %v", run1.RunID, old.CurrentRun)
	}
	if old.Params.GasDelayS != 20 {
		t.Fatal("as-of replay must restore the params version seen then")
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := jsonMarshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
