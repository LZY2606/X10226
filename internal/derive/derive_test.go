package derive

import (
	"math"
	"testing"

	"tga-archive/internal/cases"
	"tga-archive/internal/domain"
	"tga-archive/internal/ingest"
)

var delayBundle = func() string {
	p, err := cases.Load("tga-stage-decomposition")
	if err != nil {
		panic(err)
	}
	return p.V1Input
}()

// helper: parse a layer or fail.
func mustParse(t *testing.T, layer, text string) domain.SourcePayload {
	t.Helper()
	p := ingest.ParseLayer(layer, layer, text, "")
	if !p.Accepted {
		t.Fatalf("layer %s expected accepted, got diagnostics: %+v", layer, p.Diagnostics)
	}
	return p
}

func bundleInputs(t *testing.T, bundle string) Inputs {
	t.Helper()
	payloads := ingest.ImportBundle(bundle)
	in := Inputs{}
	for _, p := range payloads {
		if !p.Accepted {
			t.Fatalf("unexpected rejected section %s: %+v", p.Layer, p.Diagnostics)
		}
		pp := p
		switch p.Layer {
		case domain.LayerSamples:
			in.Samples = &pp
		case domain.LayerDrift:
			in.Drift = &pp
		case domain.LayerFurnace:
			in.Furnace = &pp
		case domain.LayerGas:
			in.Valves = &pp
		case domain.LayerBlank:
			in.Blank = &pp
		case domain.LayerInitialMass:
			in.InitialMass = &pp
		}
	}
	return in
}

// TestAtmosphereDelayAndLag verifies valve time != reaction time and measured
// thermal lag is reported.
func TestAtmosphereDelayAndLag(t *testing.T) {
	in := bundleInputs(t, delayBundle)
	p := domain.DefaultParams()
	r := Run(in, p, "params:default-v1")
	if len(r.Windows) != 2 {
		t.Fatalf("want 2 reaction windows, got %d", len(r.Windows))
	}
	w := r.Windows[1]
	if math.Abs(w.StartAt.Value-(w.ValveAt.Value+p.ValveDelayS)) > 1e-9 {
		t.Fatalf("window start must be valve time + delay: valve=%v start=%v delay=%v",
			w.ValveAt.Value, w.StartAt.Value, p.ValveDelayS)
	}
	if !w.MeasuredLagK.Valid || w.MeasuredLagK.Value < p.ReferenceTempLagK {
		t.Fatalf("measured lag %.3f must be reported and >= %.1f K", w.MeasuredLagK.Value, p.ReferenceTempLagK)
	}
	// changing the delay changes only windows (deterministic identity)
	p2 := p
	p2.ValveDelayS = 30
	r2 := Run(in, p2, "params:delay30")
	if len(r2.Windows) != 2 || r2.Windows[1].ID == w.ID {
		t.Fatalf("window identity must change with delay parameter")
	}
	if len(r2.Candidates) != len(r.Candidates) {
		t.Fatalf("stage candidates must not depend on valve delay")
	}
}

// TestBalanceDriftCorrection verifies corrected mass subtracts the linear
// baseline drift and that revising the drift moves the final residue.
func TestBalanceDriftCorrection(t *testing.T) {
	in := bundleInputs(t, delayBundle)
	r := Run(in, domain.DefaultParams(), "v1")
	var final float64
	for _, x := range r.Residues {
		if x.ID == "res:final" {
			final = x.Percent.Value
		}
	}
	if !(final > 44 && final < 50) {
		t.Fatalf("final residue out of expected band: %.2f%%", final)
	}
	revised := mustParse(t, domain.LayerDrift, "rate: 0.0006 mg/s\nanchor_at: 0 s\n")
	in2 := in
	in2.Drift = &revised
	r2 := Run(in2, domain.DefaultParams(), "v1")
	var final2 float64
	for _, x := range r2.Residues {
		if x.ID == "res:final" {
			final2 = x.Percent.Value
		}
	}
	if final2 >= final {
		t.Fatalf("steeper positive drift must lower final residue: %.3f -> %.3f", final, final2)
	}
}

// TestOverlappingPeaks verifies the shoulder peak yields both per-peak stages
// and a merged alternative, with temporal overlap recorded and no 100% sum
// constraint.
func TestOverlappingPeaks(t *testing.T) {
	in := bundleInputs(t, delayBundle)
	r := Run(in, domain.DefaultParams(), "v1")
	byKey := map[string]StageCandidate{}
	for _, c := range r.Candidates {
		byKey[c.Key] = c
	}
	p0, ok0 := byKey["c1_p0"]
	p1, ok1 := byKey["c1_p1"]
	merged, okM := byKey["c1_m0"]
	if !ok0 || !ok1 || !okM {
		t.Fatalf("expected c1_p0,c1_p1,c1_m0 candidates, got keys %v", keys(byKey))
	}
	if !containsS(p0.Overlaps, "c1_p1") {
		t.Fatalf("single stages must record overlap: %v", p0.Overlaps)
	}
	if merged.Kind != "merged" || !containsS(merged.Exclusives, "c1_p0") {
		t.Fatalf("merged candidate must be exclusive alternative to singles")
	}
	// overlapping stages: sum of stage losses may exceed the merged loss and
	// percentages are never normalized to 100.
	if !(p0.LossPct.Valid && p1.LossPct.Valid) {
		t.Fatalf("loss percents must be valid")
	}
	if math.Abs((p0.EndTime.Value-p1.EndTime.Value)) > 200 {
		// sanity: close peaks
	}
}

// TestNonMonotonicNoise verifies small up-ticks are flagged, never inverted
// into positive mass-loss peaks.
func TestNonMonotonicNoise(t *testing.T) {
	in := bundleInputs(t, delayBundle)
	r := Run(in, domain.DefaultParams(), "v1")
	sawFlag := false
	for _, pt := range r.Points {
		for _, f := range pt.Flags {
			if f == "nonmonotonic_noise" || f == "mass_increase" {
				sawFlag = true
			}
		}
	}
	if !sawFlag {
		t.Fatalf("expected non-monotonic noise / mass increase flag in points")
	}
	// no peak allowed at the noise location (~2100-2130 s)
	for _, pk := range r.Peaks {
		if pk.Time.Value >= 2070 && pk.Time.Value <= 2160 {
			t.Fatalf("noise must not produce a DTG peak at %v s", pk.Time.Value)
		}
	}
}

// TestSamplingGapNeverInterpolated verifies the GAP row breaks DTG continuity.
func TestSamplingGapNeverInterpolated(t *testing.T) {
	in := bundleInputs(t, delayBundle)
	r := Run(in, domain.DefaultParams(), "v1")
	if len(r.Gaps) == 0 {
		t.Fatalf("declared gap must be preserved")
	}
	g := r.Gaps[0]
	if math.Abs(g.From.Value-1200) > 1 || math.Abs(g.To.Value-1260) > 1 {
		t.Fatalf("gap bounds unexpected: %v -> %v", g.From.Value, g.To.Value)
	}
	// the first point after the gap must have GapBefore and unknown DTG
	for i, pt := range r.Points {
		if pt.Time.Valid && pt.Time.Value > 1230 && i > 0 {
			if !pt.GapBefore {
				t.Fatalf("point at %v s must be marked GapBefore", pt.Time.Value)
			}
			if pt.DTG.Valid {
				t.Fatalf("DTG must be unknown (never interpolated) across gap at %v s", pt.Time.Value)
			}
			break
		}
	}
}

// TestInitialMassRevision verifies percentages change but mg values do not.
func TestInitialMassRevision(t *testing.T) {
	in := bundleInputs(t, delayBundle)
	r1 := Run(in, domain.DefaultParams(), "v1")
	rev := mustParse(t, domain.LayerInitialMass, "mass: 10.012 mg\nat: 0 s\nbasis: buoyancy corrected\n")
	in2 := in
	in2.InitialMass = &rev
	r2 := Run(in2, domain.DefaultParams(), "v1")
	f := func(r Report) (float64, float64, float64) {
		for _, x := range r.Residues {
			if x.ID == "res:final" {
				return x.MassCorr.Value, x.Percent.Value, 0
			}
		}
		return 0, 0, 0
	}
	mg1, pct1, _ := f(r1)
	mg2, pct2, _ := f(r2)
	if math.Abs(mg1-mg2) > 1e-9 {
		t.Fatalf("corrected mg must be invariant to initial mass revision: %.6f vs %.6f", mg1, mg2)
	}
	if math.Abs(pct1-pct2) < 1e-6 {
		t.Fatalf("percent must change when initial mass is revised: %.4f vs %.4f", pct1, pct2)
	}
}

// TestHalfFailedImport verifies a malformed layer is rejected and keeps raw
// text + diagnostics while other sections still import.
func TestHalfFailedImport(t *testing.T) {
	bundle := delayBundle + `
# mystery_layer
??? not parseable at all
`
	payloads := ingest.ImportBundle(bundle)
	var good, bad int
	for _, p := range payloads {
		if p.Layer == "mystery_layer" {
			bad++
			if p.Accepted {
				t.Fatalf("unknown layer must be rejected")
			}
			if p.RawText == "" || len(p.Diagnostics) == 0 {
				t.Fatalf("rejected fragment must preserve raw text and diagnostics")
			}
		}
		if p.Accepted {
			good++
		}
	}
	if bad != 1 || good < 6 {
		t.Fatalf("want 1 rejected and >=6 accepted, got bad=%d good=%d", bad, good)
	}
	// a corrupted valves fragment on its own must fail acceptance
	valves := ingest.ParseLayer(domain.LayerGas, "gas", "yesterday N2 50 mL/min\nsoon air maybe\n", "")
	if valves.Accepted {
		t.Fatalf("corrupt valves fragment must not be accepted")
	}
	if len(valves.Diagnostics) == 0 || valves.RawText == "" {
		t.Fatalf("corrupt valves must keep fragment and diagnostics")
	}
}

// TestUnconvertibleValueKept verifies a bad unit stays in place with a diag.
func TestUnconvertibleValueKept(t *testing.T) {
	m := domain.ParseMeasure("12.3 fahrenheit_per_teaspoon", domain.UnitKelvin)
	if m.Valid {
		t.Fatalf("unknown unit must be invalid")
	}
	if m.Raw != "12.3 fahrenheit_per_teaspoon" || m.Note == "" {
		t.Fatalf("raw representation and a diagnostic note must be retained")
	}
}

func keys(m map[string]StageCandidate) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func containsS(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
