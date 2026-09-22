// Command gen produces the committed offline TGA case pack. It uses a fixed
// seed and a deterministic analytic mass-loss model (no network, no RNG state
// leaking into tests).
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const (
	seed           = int64(20260919)
	w0             = 10.000
	residue        = 6.800
	driftRate      = 0.02 // mg/min
	gapA, gapB     = 1400.0, 1640.0
	noiseT, noiseD = 2100.0, 0.005
)

type stage struct{ c, w, amp float64 }

var stages = []stage{
	{400, 55, 0.35},
	{1180, 70, 0.70},
	{1300, 80, 0.50},
	{2600, 130, 1.65},
}

func erf(x float64) float64 {
	// Abramowitz-Stegun 7.1.26, deterministic and adequate for sample data.
	t := 1.0 / (1.0 + 0.3275911*math.Abs(x))
	y := 1.0 - (((((1.061405429*t-1.453152027)*t)+1.421413741)*t-0.284496736)*t+0.254829592)*t*math.Exp(-x*x)
	if x < 0 {
		return -y
	}
	return y
}

func loss(t float64) float64 {
	tot := 0.0
	for _, s := range stages {
		tot += s.amp * 0.5 * (1 + erf((t-s.c)/(s.w*math.Sqrt2)))
	}
	return tot
}

func sampleMass(t float64) float64 { return residue + (w0 - residue) - loss(t) }
func blankMass(t float64) float64  { return 0.10 - 0.00004*t }
func tempAt(t float64) float64 {
	switch {
	case t <= 500:
		return 25 + (150-25)*t/500
	case t <= 1500:
		return 150 + (600-150)*(t-500)/1000
	default:
		v := 600 + (800-600)*(t-1500)/1200
		if v > 800 {
			v = 800
		}
		return v
	}
}

func main() {
	root := filepath.Join(findRepoRoot(), "internal", "casepack", "testdata")
	if err := os.MkdirAll(filepath.Join(root, "scenarios"), 0o755); err != nil {
		panic(err)
	}
	writeFile(filepath.Join(root, "baseline.tga"), baseline())
	for name, body := range map[string]string{
		"atmosphere_delay.tga": scenarioDelay(),
		"drift.tga":            scenarioDrift(),
		"overlap.tga":          scenarioOverlap(),
		"noise.tga":            scenarioNoise(),
		"gap.tga":              scenarioGap(),
		"weight_revision.tga":  scenarioWeight(),
		"half_fail.tga":        scenarioHalfFail(),
	} {
		writeFile(filepath.Join(root, "scenarios", name), body)
	}
	writeFile(filepath.Join(root, "manifest.json"), manifest())
	fmt.Println("case pack generated, seed", seed)
}

func findRepoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

func writeFile(path, body string) {
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		panic(err)
	}
}

func header(title string) string {
	return fmt.Sprintf("# TGA loss-lineage offline case — %s\n# format: tga-1 | fixed seed %d | units explicit\n\n", title, seed)
}

func baseline() string {
	b := header("baseline: raw points, drift, program, valves, blank, initial mass")
	b += "[sample_weight]\nmg=10.000mg\n\n"
	b += "[balance_drift]\nstart=0s rate=0.02mg/min\n\n"
	b += "[blank_curve]\n"
	for t := 0.0; t <= 3600; t += 200 {
		b += fmt.Sprintf("t=%.0fs mass=%.4fmg\n", t, blankMass(t))
	}
	b += "\n[furnace_program]\n"
	b += "start=0s end=500s target=150C ramp=15C/min\n"
	b += "start=500s end=600s target=150C ramp=0C/min\n"
	b += "start=600s end=1500s target=600C ramp=30C/min\n"
	b += "start=1500s end=2700s target=800C ramp=10C/min\n"
	b += "start=2700s end=3600s target=800C ramp=0C/min\n\n"
	b += "[gas_valve]\n"
	b += "at=0s from=ambient to=N2\n"
	b += "at=2400s from=N2 to=O2\n\n"
	b += "[raw_sample]\n"
	for t := 0.0; t <= 3600; t += 20 {
		if t > gapA && t < gapB {
			continue // sampling gap: never back-filled by interpolation
		}
		m := sampleMass(t) + driftRate*t/60 + blankMass(t)
		if t == noiseT {
			m += noiseD // non-monotonic blip within noise tolerance
		}
		b += fmt.Sprintf("t=%.0fs temp=%.1fC mass=%.4fmg\n", t, tempAt(t), m)
	}
	return b
}

func scenarioDelay() string {
	b := header("counter-case: atmosphere delay revision (single source)")
	b += "# Only the delay parameter changes; valve time stays identical.\n"
	b += "[params]\natmosphere_delay_s=120 temperature_lag_c=12\n"
	return b
}

func scenarioDrift() string {
	b := header("counter-case: balance baseline drift revision (single source)")
	b += "[balance_drift]\nstart=0s rate=0.05mg/min\n"
	return b
}

func scenarioOverlap() string {
	b := header("counter-case: overlapping peaks kept separate (merge window widened)")
	b += "[params]\npeak_merge_window_s=240 peak_prominence_mg_s=0.0002\n"
	return b
}

func scenarioNoise() string {
	b := header("counter-case: non-monotonic noise, local source append")
	b += "[raw_sample]\n"
	t := 2120.0
	m := sampleMass(t) + driftRate*t/60 + blankMass(t) + 0.008
	b += fmt.Sprintf("t=%.0fs temp=%.1fC mass=%.4fmg\n", t, tempAt(t), m)
	return b
}

func scenarioGap() string {
	b := header("counter-case: fill part of the gap with two source points")
	b += "[raw_sample]\n"
	for _, t := range []float64{1500, 1580} {
		m := sampleMass(t) + driftRate*t/60 + blankMass(t)
		b += fmt.Sprintf("t=%.0fs temp=%.1fC mass=%.4fmg\n", t, tempAt(t), m)
	}
	return b
}

func scenarioWeight() string {
	b := header("counter-case: initial mass revision (parent-version relation)")
	b += "[sample_weight]\nmg=10.050mg\n"
	return b
}

func scenarioHalfFail() string {
	b := header("counter-case: import half-failure (accepted + rejected rows kept)")
	b += "[raw_sample]\n"
	t := 3000.0
	m := sampleMass(t) + driftRate*t/60 + blankMass(t)
	b += fmt.Sprintf("t=%.0fs temp=%.1fC mass=%.4fmg\n", t, tempAt(t), m)
	b += "t=3020s temp=800C mass=notanumber\n" // unconvertible value
	b += "t=3040s mass=6.1mg\n"                // missing field
	b += "[unknown_section]\nfoo=bar\n"        // unknown layer
	b += "t=3060s temp=800C mass=6.0mg\n"      // outside section -> rejected
	return b
}

func manifest() string {
	return `{
  "format": "tga-casepack-1",
  "title": "TGA stage decomposition, atmosphere switching and residue evidence chain",
  "seed": 20260919,
  "baseline": "baseline.tga",
  "scenarios": [
    {"id": "atmosphere_delay", "file": "scenarios/atmosphere_delay.tga", "expect": "gas_boundary_shifts"},
    {"id": "drift", "file": "scenarios/drift.tga", "expect": "drift_revision_local_invalidation"},
    {"id": "overlap", "file": "scenarios/overlap.tga", "expect": "overlapping_peaks_separate"},
    {"id": "noise", "file": "scenarios/noise.tga", "expect": "nonmonotonic_flag"},
    {"id": "gap", "file": "scenarios/gap.tga", "expect": "gap_never_interpolated"},
    {"id": "weight_revision", "file": "scenarios/weight_revision.tga", "expect": "initial_mass_supersedes"},
    {"id": "half_fail", "file": "scenarios/half_fail.tga", "expect": "partial_import_preserved"}
  ],
  "narrative": {
    "id": "loss_lineage_master",
    "steps": [
      {"op": "import", "file": "baseline.tga"},
      {"op": "add_note", "code": "valve_time_ne_reaction_time", "message": "valve switch is not sample reaction time; delay and temperature lag apply"},
      {"op": "set_params", "atmosphere_delay_s": 60, "reason": "revision: measured transfer line delay"},
      {"op": "import", "file": "scenarios/drift.tga", "kind": "revision"},
      {"op": "decide", "kind": "accept_stage", "which": "first_stage"},
      {"op": "decide", "kind": "reject_stage", "which": "first_stage"},
      {"op": "resolve_conflict", "choose": "accept"},
      {"op": "undo", "target": "conflict_resolution"}
    ]
  }
}
`
}
