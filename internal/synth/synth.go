// Package synth generates the deterministic offline case pack.
//
// All numbers come from a fixed-seed LCG, so the domain material is identical
// on every machine and every run. The baseline is a classic polymer TGA run:
// moisture loss -> two overlapping devolatilization stages -> atmosphere
// switches N2 -> air -> char burn-out -> inorganic residue.
package synth

import (
	"fmt"
	"math"
	"strings"

	"tga/internal/model"
)

const Seed uint64 = 20260919

type lcg struct{ s uint64 }

func newRNG(seed uint64) *lcg { return &lcg{s: seed} }

func (r *lcg) float() float64 {
	r.s = r.s*6364136223846793005 + 1442695040888963407
	return float64(r.s>>11) / float64(1<<53)
}

// noise returns centered noise in [-amp, amp).
func (r *lcg) noise(amp float64) float64 { return amp * (2*r.float() - 1) }

// Stage is one logistic-shaped mass-loss process.
type Stage struct {
	T50   float64 // center time (s)
	Width float64 // logistic scale (s)
	Amp   float64 // total loss (mg)
}

func (s Stage) loss(t float64) float64 {
	return s.Amp / (1 + math.Exp(-(t-s.T50)/s.Width))
}

// Spec configures a generated series.
type Spec struct {
	Stages      []Stage
	ResidueMg   float64
	StartMg     float64
	NoiseMg     float64
	NonMonotone bool
}

func baselineSpec() Spec {
	return Spec{
		StartMg:   10.0,
		ResidueMg: 2.0,
		NoiseMg:   0.004,
		Stages: []Stage{
			{T50: 180, Width: 45, Amp: 0.5},  // adsorbed moisture, 5%
			{T50: 620, Width: 60, Amp: 1.5},  // devolatilization I
			{T50: 760, Width: 55, Amp: 2.0},  // devolatilization II (overlaps I)
			{T50: 1560, Width: 50, Amp: 4.0}, // char burn-out under air
		},
	}
}

func cumulativeLoss(spec Spec, t float64) float64 {
	loss := 0.0
	for _, st := range spec.Stages {
		loss += st.loss(t)
	}
	return loss
}

// ProgramTemp is the shared furnace profile.
func ProgramTemp(t float64) float64 {
	switch {
	case t < 120:
		return 25.0
	case t <= 1500:
		return 25 + 10*(t-120)/60.0
	case t <= 1680:
		return 255.0
	case t <= 2280:
		return 255 + 10*(t-1680)/60.0
	default:
		return 355.0
	}
}

func generate(spec Spec, stepSec float64, seed uint64) ([]model.Point, string) {
	rng := newRNG(seed)
	var pts []model.Point
	var b strings.Builder
	fmt.Fprintf(&b, "# generated TGA series, fixed seed=%d\n", seed)
	b.WriteString("unit: time=s\nunit: temp=C\nunit: mass=mg\n")
	for t := 0.0; t <= 2400.0+1e-9; t += stepSec {
		mass := spec.StartMg - cumulativeLoss(spec, t) + rng.noise(spec.NoiseMg)
		if spec.NonMonotone && t >= 400 && t <= 440 {
			mass += 0.06 * math.Sin((t-400)/40*math.Pi) // buoyancy hump
		}
		temp := ProgramTemp(t)
		rawT, rawC, rawM := fmt.Sprintf("%.0f", t), fmt.Sprintf("%.1f", temp), fmt.Sprintf("%.4f", mass)
		pts = append(pts, model.Point{Seq: len(pts) + 1,
			TimeRaw: rawT, TimeSec: t, TimeOK: true,
			TempRaw: rawC, TempC: temp, TempOK: true,
			MassRaw: rawM, MassMg: mass, MassOK: true})
		fmt.Fprintf(&b, "%s,%s,%s\n", rawT, rawC, rawM)
	}
	return pts, b.String()
}

// BaselineSamples generates the baseline mass sample layer.
func BaselineSamples() ([]model.Point, string) {
	return generate(baselineSpec(), 20, Seed)
}

// BaselineBlank generates the linear blank drift curve.
func BaselineBlank() ([]model.Point, string) {
	rng := newRNG(Seed + 1)
	var b strings.Builder
	b.WriteString("# blank run, empty crucible\nunit: time=s\nunit: temp=C\nunit: mass=mg\n")
	var pts []model.Point
	for t := 0.0; t <= 2400.0+1e-9; t += 100 {
		mass := 0.0 - 0.00008*t + rng.noise(0.001)
		temp := ProgramTemp(t)
		pts = append(pts, model.Point{Seq: len(pts) + 1,
			TimeRaw: fmt.Sprintf("%.0f", t), TimeSec: t, TimeOK: true,
			TempRaw: fmt.Sprintf("%.1f", temp), TempC: temp, TempOK: true,
			MassRaw: fmt.Sprintf("%.4f", mass), MassMg: mass, MassOK: true})
		fmt.Fprintf(&b, "%.0f,%.1f,%.4f\n", t, temp, mass)
	}
	return pts, b.String()
}

// BaselineProgram emits the furnace program tagged text.
func BaselineProgram() string {
	return `# furnace program: initial hold, two ramps, final hold
unit: time=s
unit: temp=C
name,t0,t1,temp_start,temp_end
hold25,0,120,25,25
ramp1,120,1500,25,255
hold255,1500,1680,255,255
ramp2,1680,2280,255,355
hold355,2280,2400,355,355
`
}

// BaselineGas emits valve events. Valve time is not reaction time.
func BaselineGas() string {
	return `# gas valve events (valve time is not sample-reaction time)
unit: time=s
time,from,to,note
0,N2,N2,start
1320,N2,air,valve switched to air
`
}

// BaselineM0 is the weighed initial mass.
func BaselineM0() string {
	return `# analytical balance reading
mass=10.0000
unit=mg
method=crucible+sample minus tare
weighed_at=2026-09-19T08:00:00+09:00
note=signed by operator A
`
}

// BaselineParams is parameter set version 1.
func BaselineParams() string {
	return `gas_delay_s=60
thermal_lag_c=8
min_prominence_mg_per_s=0.0005
gap_factor=2.0
drift_model=linear
`
}

// ---- counterexample variants: each changes exactly one direct source ----

// DriftBlank is the revised blank curve (quadratic balance baseline drift).
func DriftBlank() string {
	rng := newRNG(Seed + 2)
	var b strings.Builder
	b.WriteString("# blank curve revision 2: quadratic balance baseline drift\nunit: time=s\nunit: temp=C\nunit: mass=mg\n")
	for t := 0.0; t <= 2400.0+1e-9; t += 100 {
		mass := 0.0 - 0.00005*t - 6e-8*t*t + rng.noise(0.001)
		fmt.Fprintf(&b, "%.0f,%.1f,%.4f\n", t, ProgramTemp(t), mass)
	}
	return b.String()
}

// OverlapSamples tightens stages II and III so the DTG peaks overlap strongly.
func OverlapSamples() ([]model.Point, string) {
	spec := baselineSpec()
	spec.Stages[2].T50 = 690
	spec.Stages[2].Width = 40
	spec.Stages[1].Width = 40
	return generate(spec, 20, Seed+3)
}

// NonMonotoneSamples introduces a buoyancy hump (non-monotone mass segment).
func NonMonotoneSamples() ([]model.Point, string) {
	spec := baselineSpec()
	spec.NonMonotone = true
	return generate(spec, 20, Seed+4)
}

// GapSamples removes points between 900 s and 1080 s (true acquisition gap).
func GapSamples() ([]model.Point, string) {
	pts, _ := BaselineSamples()
	var b strings.Builder
	b.WriteString("# baseline with acquisition gap 900..1080 s (gap must not be bridged)\nunit: time=s\nunit: temp=C\nunit: mass=mg\n")
	var kept []model.Point
	for _, p := range pts {
		if p.TimeSec > 900 && p.TimeSec < 1080 {
			continue
		}
		kept = append(kept, p)
		fmt.Fprintf(&b, "%s,%s,%s\n", p.TimeRaw, p.TempRaw, p.MassRaw)
	}
	return kept, b.String()
}

// PartialSamplesText corrupts exactly two rows (one non-numeric mass cell and
// one token cell), exercising import half-failure: good rows survive, bad rows
// remain in place with diagnostics.
func PartialSamplesText() string {
	_, text := BaselineSamples()
	lines := strings.Split(text, "\n")
	for i, ln := range lines {
		fields := strings.Split(ln, ",")
		if len(fields) != 3 {
			continue
		}
		switch fields[0] {
		case "500":
			fields[2] = "N/A"
			lines[i] = strings.Join(fields, ",")
		case "1100":
			fields[2] = "bogus"
			lines[i] = strings.Join(fields, ",")
		}
	}
	return strings.Join(lines, "\n")
}

// RevisedM0 is the corrected weighing after the tare was rechecked.
func RevisedM0() string {
	return `# initial mass revision: balance tare rechecked
mass=9.9800
unit=mg
method=crucible+sample minus tare (recheck)
weighed_at=2026-09-19T09:10:00+09:00
note=tare cup was damp at first weighing
`
}

// DelayedParams revises only analysis parameters (transport delay + lag).
func DelayedParams() string {
	return `# parameter revision 2: measured transport delay + thermal lag
gas_delay_s=180
thermal_lag_c=12
min_prominence_mg_per_s=0.0003
gap_factor=2.0
drift_model=linear
`
}
