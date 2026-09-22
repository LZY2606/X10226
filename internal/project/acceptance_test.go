package project

import (
	"testing"

	"tgareview/internal/cases"
	"tgareview/internal/domain"
	"tgareview/internal/engine"
)

// loadMutation reads an embedded mutation definition.
func loadMutation(t *testing.T, id string) map[string]any {
	t.Helper()
	raw, err := cases.ReadFile("mutations/" + id + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := jsonUnmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAcceptance_Baseline(t *testing.T) {
	svc, _, _, ctx := newTestService(t)
	pid, res := importBaseline(t, svc, ctx)
	if res.Status != "committed" {
		t.Fatalf("baseline status = %q, want committed", res.Status)
	}
	art := currentArtifact(t, svc, ctx, pid)
	if len(art.Points) == 0 {
		t.Fatal("no points")
	}
	if len(art.Gaps) == 0 {
		t.Fatal("baseline must contain the designed acquisition gap")
	}
	if art.Residue == nil || art.Residue.ResiduePct == nil {
		t.Fatal("residue should be computable")
	}
	// six layers present in basis
	for _, k := range domain.AllLayers() {
		if art.Basis[string(k)] == "" {
			t.Errorf("basis missing layer %s", k)
		}
	}
}

// M1: atmosphere delay moves reaction onset away from valve switch.
func TestM1_AtmosphereDelay(t *testing.T) {
	svc, _, clk, ctx := newTestService(t)
	pid, _ := importBaseline(t, svc, ctx)
	before := currentArtifact(t, svc, ctx, pid)
	clk.Advance(0)

	p := engine.DefaultParams()
	p.AtmosphereDelayS = 30
	rev, _, err := svc.ReviseParams(ctx, pid, p, "M1")
	if err != nil {
		t.Fatal(err)
	}
	if rev == "" {
		t.Fatal("empty params rev")
	}
	after := currentArtifact(t, svc, ctx, pid)
	if after.ID == before.ID {
		t.Fatal("new params must produce a new artifact id")
	}
	var sawOnset bool
	for _, c := range after.Candidates {
		if c.ValveSwitchS != nil && c.ReactionOnsetS != nil {
			if *c.ReactionOnsetS-*c.ValveSwitchS != 30 {
				t.Fatalf("delay = %v, want 30", *c.ReactionOnsetS-*c.ValveSwitchS)
			}
			sawOnset = true
		}
	}
	if !sawOnset {
		t.Fatal("no candidate carried valve/reaction times")
	}
	if r := findRule(after, "R1"); r == nil || r.Status != "applies" {
		t.Fatal("R1 atmosphere-delay rule must apply")
	}
	// old artifact retained and marked stale
	st, _ := svc.CurrentState(ctx, pid)
	old := st.Artifacts[before.ID]
	if old == nil {
		t.Fatal("old artifact must remain viewable")
	}
	if !st.IsStale(old) {
		t.Fatal("old artifact must be stale, not current")
	}
}

// M2: balance baseline drift is a single direct source; only dependent values change.
func TestM2_BaselineDrift(t *testing.T) {
	svc, _, _, ctx := newTestService(t)
	pid, _ := importBaseline(t, svc, ctx)
	before := currentArtifact(t, svc, ctx, pid)
	beforeLast := before.Points[len(before.Points)-1].MCorrMg

	m := loadMutation(t, "M2_baseline_drift")
	data := mustJSON(m["layer"].(map[string]any)["data"])
	b := oneLayerBundle("M2", "drift_correction", data, before.Basis["drift_correction"])
	res, err := svc.Import(ctx, pid, b)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "committed" {
		t.Fatalf("status=%s", res.Status)
	}
	after := currentArtifact(t, svc, ctx, pid)
	if after.Basis["sample_points"] != before.Basis["sample_points"] {
		t.Fatal("sample_points must be untouched when drift source changes")
	}
	if after.Points[len(after.Points)-1].MCorrMg == beforeLast {
		t.Fatal("corrected mass must change with drift rate")
	}
	// raw last mass identical (immutable)
	if after.Points[len(after.Points)-1].MRawMg != before.Points[len(before.Points)-1].MRawMg {
		t.Fatal("raw mass must remain immutable (R7)")
	}
	if r := findRule(after, "R7"); r == nil {
		t.Fatal("R7 drift-on-derived-layer rule missing")
	}
}

// M3: overlapping peaks yield multiple legal candidates, not forced to 100%.
func TestM3_OverlappingPeaks(t *testing.T) {
	svc, _, _, ctx := newTestService(t)
	pid, _ := importBaseline(t, svc, ctx)
	p := engine.DefaultParams()
	p.DtgWindowS = 34
	if _, _, err := svc.ReviseParams(ctx, pid, p, "M3"); err != nil {
		t.Fatal(err)
	}
	art := currentArtifact(t, svc, ctx, pid)
	options := map[string]bool{}
	for _, c := range art.Candidates {
		options[c.Option] = true
	}
	if !options["merged"] || !options["split_a"] || !options["split_b"] {
		t.Fatalf("expected merged/split_a/split_b legal candidates, got %v", options)
	}
	if findRule(art, "R3") == nil || findRule(art, "R6") == nil {
		t.Fatal("R3/R6 rules must be present")
	}
}

// M4: non-monotonic noise is retained and flagged, not deleted.
func TestM4_NonMonotonicNoise(t *testing.T) {
	svc, _, _, ctx := newTestService(t)
	pid, _ := importBaseline(t, svc, ctx)
	art0 := currentArtifact(t, svc, ctx, pid)
	nm0 := countNonMonotonic(art0)

	m := loadMutation(t, "M4_nonmonotonic_noise")
	data := mustJSON(m["layer"].(map[string]any)["data"])
	b := oneLayerBundle("M4", "sample_points", data, art0.Basis["sample_points"])
	if _, err := svc.Import(ctx, pid, b); err != nil {
		t.Fatal(err)
	}
	art := currentArtifact(t, svc, ctx, pid)
	if countNonMonotonic(art) <= nm0 {
		t.Fatalf("injected blip must be flagged (before=%d after=%d)", nm0, countNonMonotonic(art))
	}
	if findRule(art, "R5") == nil {
		t.Fatal("R5 non-monotonic rule must apply")
	}
}

// M5: larger sampling gap => intervals split and no interpolation across.
func TestM5_SamplingGap(t *testing.T) {
	svc, _, _, ctx := newTestService(t)
	pid, _ := importBaseline(t, svc, ctx)
	art0 := currentArtifact(t, svc, ctx, pid)
	m := loadMutation(t, "M5_sampling_gap")
	data := mustJSON(m["layer"].(map[string]any)["data"])
	b := oneLayerBundle("M5", "sample_points", data, art0.Basis["sample_points"])
	if _, err := svc.Import(ctx, pid, b); err != nil {
		t.Fatal(err)
	}
	art := currentArtifact(t, svc, ctx, pid)
	if len(art.Intervals) <= len(art0.Intervals) {
		t.Fatalf("wider gap should split intervals (before=%d after=%d)", len(art0.Intervals), len(art.Intervals))
	}
	if findRule(art, "R4") == nil {
		t.Fatal("R4 no-interpolation-across-gaps rule missing")
	}
	// intervals must not overlap and must be separated by a gap
	for i := 1; i < len(art.Intervals); i++ {
		if art.Intervals[i].FromIndex <= art.Intervals[i-1].ToIndex {
			t.Fatal("intervals overlap")
		}
	}
}

// M6: initial mass revision changes percentages but not corrected mg.
func TestM6_InitialMassRevision(t *testing.T) {
	svc, _, _, ctx := newTestService(t)
	pid, _ := importBaseline(t, svc, ctx)
	before := currentArtifact(t, svc, ctx, pid)
	m := loadMutation(t, "M6_initial_mass_revision")
	data := mustJSON(m["layer"].(map[string]any)["data"])
	b := oneLayerBundle("M6", "initial_mass", data, before.Basis["initial_mass"])
	if _, err := svc.Import(ctx, pid, b); err != nil {
		t.Fatal(err)
	}
	after := currentArtifact(t, svc, ctx, pid)
	if after.Basis["initial_mass"] == before.Basis["initial_mass"] {
		t.Fatal("initial mass revision must create new revision")
	}
	// corrected mass unchanged, percentage changed
	last := len(after.Points) - 1
	if after.Points[last].MCorrMg != before.Points[len(before.Points)-1].MCorrMg {
		t.Fatal("corrected mg must not depend on m0")
	}
	if after.Points[last].Pct == before.Points[len(before.Points)-1].Pct {
		t.Fatal("percentage must change with m0")
	}
}

// M7: half-failed import keeps accepted fragments and rejects the bad layer.
func TestM7_ImportHalfFailure(t *testing.T) {
	svc, _, _, ctx := newTestService(t)
	pid, _ := importBaseline(t, svc, ctx)
	art0 := currentArtifact(t, svc, ctx, pid)
	m := loadMutation(t, "M7_import_half_failure")
	lp := m["layer"].(map[string]any)
	valveData := mustJSON(lp["data"])
	extra := lp["extra_layer"].(map[string]any)
	extraData := mustJSON(extra["data"])
	b := bundleBundle("M7",
		layerIn("gas_valve_events", art0.Basis["gas_valve_events"], valveData),
		layerIn("initial_mass", art0.Basis["initial_mass"], extraData),
	)
	res, err := svc.Import(ctx, pid, b)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "partial" {
		t.Fatalf("status=%q want partial", res.Status)
	}
	st, _ := svc.CurrentState(ctx, pid)
	// rejected valve layer must not replace the valid head
	if st.HeadRev[domain.GasValveEvents] != art0.Basis["gas_valve_events"] {
		t.Fatal("rejected layer must not replace the valid head")
	}
	// accepted legal fragment must commit (initial mass head changes)
	if st.HeadRev[domain.InitialMass] == art0.Basis["initial_mass"] {
		t.Fatal("accepted fragment (initial_mass) must be committed in half-failure")
	}
	if len(res.Diagnostics) == 0 {
		t.Fatal("half-failure must retain diagnostics")
	}
}

func countNonMonotonic(art *engine.Artifact) int {
	n := 0
	for _, p := range art.Points {
		if p.NonMonotonic {
			n++
		}
	}
	return n
}
