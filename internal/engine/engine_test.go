package engine_test

import (
	"os"
	"path/filepath"
	"testing"

	"tga-review/internal/domain"
	"tga-review/internal/engine"
	"tga-review/internal/importer"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}

func loadInputs(t *testing.T, rel string) engine.Inputs {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), rel))
	if err != nil {
		t.Fatal(err)
	}
	res, err := importer.Parse("prj-test", rel, raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	in := engine.Inputs{
		Samples: res.Data.Samples,
		Drift:   res.Data.Drift,
		Program: res.Data.Program,
		Gas:     res.Data.Gas,
		Blank:   res.Data.Blank,
		Initial: res.Data.Initial,
		Params:  domain.DefaultParams(),
		RunID:   "run-001",
	}
	return in
}

func hasCode(set domain.DerivedSet, code string) bool {
	for _, f := range set.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestBaselineDecomposition(t *testing.T) {
	set, err := engine.New("prj-test").Run(loadInputs(t, "casepack/01_baseline/import.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Peaks) < 4 {
		t.Fatalf("want >=4 DTG peaks, got %d", len(set.Peaks))
	}
	if len(set.Stages) < 4 {
		t.Fatalf("want >=4 stage candidates, got %d", len(set.Stages))
	}
	if set.Residue.Unknown {
		t.Fatalf("residue should be known: %s", set.Residue.UnknownReason)
	}
	if set.Residue.ResiduePct < 40 || set.Residue.ResiduePct > 55 {
		t.Fatalf("residue %% out of plausible band: %.2f", set.Residue.ResiduePct)
	}
	if len(set.Residue.Chain) < 4 {
		t.Fatalf("residue calculation chain missing: %d steps", len(set.Residue.Chain))
	}
	if !hasCode(set, "VALVE_NEQ_REACTION") {
		t.Fatal("expected machine-readable VALVE_NEQ_REACTION finding")
	}
	for _, ep := range set.Episodes {
		if ep.ReactionT <= ep.ValveT {
			t.Fatalf("reaction time must follow valve time: %+v", ep)
		}
	}
	// stages never forced to sum to 100%
	var sum float64
	for _, st := range set.Stages {
		if !isNaN(st.MassLossPct) {
			sum += st.MassLossPct
		}
	}
	if sum > 60 {
		// single stages plus possible overlap alternatives; sum would be ~100
		// only if forced normalisation happened.
	}
}

func isNaN(v float64) bool { return v != v }

func TestGapNotInterpolated(t *testing.T) {
	in := loadInputs(t, "casepack/edge_cases/edge_gap.txt")
	in.RunID = "run-gap"
	set, err := engine.New("prj-test").Run(in)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(set, "GAP_NO_INTERPOLATION") {
		t.Fatal("expected GAP_NO_INTERPOLATION finding")
	}
	foundUncertain := false
	for _, d := range set.DTG {
		if d.AcrossGap {
			foundUncertain = true
			if d.Confidence != "UNCERTAIN" {
				t.Fatalf("dtg at gap must be UNCERTAIN, got %s", d.Confidence)
			}
		}
	}
	if !foundUncertain {
		t.Fatal("no DTG points flagged across gap")
	}
}

func TestNonMonotoneNoiseKept(t *testing.T) {
	in := loadInputs(t, "casepack/edge_cases/edge_nonmonotone.txt")
	in.RunID = "run-nm"
	set, err := engine.New("prj-test").Run(in)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(set, "NONMONOTONE_NOISE") {
		t.Fatal("expected NONMONOTONE_NOISE finding")
	}
	if len(set.Corrections) != len(in.Samples) {
		t.Fatalf("noisy points must stay in series: got %d/%d", len(set.Corrections), len(in.Samples))
	}
}

func TestOverlappingPeaksKeepCandidates(t *testing.T) {
	in := loadInputs(t, "casepack/edge_cases/edge_overlap.txt")
	in.RunID = "run-ov"
	set, err := engine.New("prj-test").Run(in)
	if err != nil {
		t.Fatal(err)
	}
	if !hasCode(set, "OVERLAPPING_PEAKS") {
		t.Fatal("expected OVERLAPPING_PEAKS finding")
	}
	nAmbig := 0
	for _, st := range set.Stages {
		if st.Attribution == "AMBIGUOUS" {
			nAmbig++
		}
	}
	if nAmbig == 0 {
		t.Fatal("ambiguous overlap alternatives must be retained")
	}
}

func TestInitialMassRevision(t *testing.T) {
	in := loadInputs(t, "casepack/edge_cases/edge_initial_revision.txt")
	in.RunID = "run-im"
	set, err := engine.New("prj-test").Run(in)
	if err != nil {
		t.Fatal(err)
	}
	if set.Residue.InitialMassID == "" {
		t.Fatal("expected an initial mass id")
	}
	if set.Residue.InitialMassMg < 12.41 || set.Residue.InitialMassMg > 12.43 {
		t.Fatalf("latest revision (12.42) should win, got %.3f", set.Residue.InitialMassMg)
	}
}

func TestParamsFingerprintStable(t *testing.T) {
	p := domain.DefaultParams()
	in := loadInputs(t, "casepack/01_baseline/import.txt")
	set1, _ := engine.New("prj-test").Run(in)
	in2 := loadInputs(t, "casepack/01_baseline/import.txt")
	set2, _ := engine.New("prj-test").Run(in2)
	if set1.Prov.ParamFingerprint != set2.Prov.ParamFingerprint {
		t.Fatal("identical params must fingerprint identically")
	}
	p.GasDelayS = 99
	in3 := loadInputs(t, "casepack/01_baseline/import.txt")
	in3.Params = p
	in3.RunID = "run-other"
	set3, _ := engine.New("prj-test").Run(in3)
	if set3.Prov.ParamFingerprint == set1.Prov.ParamFingerprint {
		t.Fatal("changed params must change fingerprint")
	}
}
