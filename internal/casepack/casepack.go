// Package casepack builds the offline, domain-specific dossier
// "失重谱系：TGA 阶段分解、气氛切换与残留物证据链".
//
// All material is generated deterministically (fixed seed, virtual time), so
// the same files and machine-readable expectations are reproducible without
// network access.
package casepack

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
)

// Baseline scenario (time in seconds, temperature in C, mass in mg):
//
//	furnace: 30 C hold 2 min; ramp 10 C/min to 150 C; hold 4 min (moisture);
//	         ramp 10 C/min to 350 C; hold 6 min (main devolatilization);
//	         ramp 10 C/min to 600 C; hold 6 min; gas valve N2->O2 at 2700 s
//	         with 45 s atmosphere delay and 6 C temperature lag (oxidation);
//	         ramp to 700 C; hold 3 min.
//	initial mass 10.50 mg; linear balance drift 0.012 mg/h; blank curve small.
//	three mass-loss peaks: moisture ~80 C, devolatilization ~300 C,
//	oxidation ~560 C; peaks 2 and 3 partially overlap in the ramp/hold joint.
const (
	InitialMassMg = 10.50
	DriftRateMgH  = 0.012
	AtmosDelayS   = 45.0
	TempLagC      = 6.0
	ValveAtS      = 2700.0
	SampleStepS   = 2.0
)

// Perturbation identifiers (the seven acceptance scenarios).
const (
	PAtmosDelay     = "atmosphere_delay"
	PBaselineDrift  = "balance_baseline_drift"
	POverlapping    = "overlapping_peaks"
	PNonMonotonic   = "non_monotonic_noise"
	PSamplingGap    = "sampling_gap"
	PInitialRevise  = "initial_mass_revision"
	PImportHalfFail = "import_half_failure"
)

func AllPerturbations() []string {
	return []string{
		PAtmosDelay, PBaselineDrift, POverlappingPeaksAlias, PNonMonotonic,
		PSamplingGap, PInitialRevise, PImportHalfFail,
	}
}

const POverlappingPeaksAlias = "overlapping_peaks"

type Sample struct {
	T float64
	C float64
	M float64
}

type Furnace struct {
	From, To, T0, T1 float64
	Hold             bool
}

func FurnaceProgram() []Furnace {
	return []Furnace{
		{0, 120, 30, 30, true},
		{120, 840, 30, 150, false},  // 10 C/min
		{840, 1080, 150, 150, true}, // moisture hold
		{1080, 2280, 150, 350, false},
		{2280, 2640, 350, 350, true}, // devol. hold
		{2640, 4140, 350, 600, false},
		{4140, 4500, 600, 600, true}, // oxidation hold (valve at 2700)
		{4500, 5100, 600, 700, false},
		{5100, 5280, 700, 700, true},
	}
}

func tempAt(prog []Furnace, t float64) float64 {
	for _, st := range prog {
		if t >= st.From && t <= st.To {
			if st.Hold {
				return st.T0
			}
			frac := (t - st.From) / (st.To - st.From)
			return st.T0 + frac*(st.T1-st.T0)
		}
	}
	last := prog[len(prog)-1]
	return last.T1
}

// gaussianPeak returns cumulative mass lost (mg) contributed by a peak whose
// DTG is a negative gaussian. Total loss L mg, center at seconds tc, width w.
func peakLoss(t, tc, w, L float64) float64 {
	// cumulative fraction = 0.5*(1+erf((t-tc)/(sqrt2 w)))
	z := (t - tc) / (math.Sqrt2 * w)
	return 0.5 * L * (1 + math.Erf(z))
}

// trueMass is the ideal corrected sample mass at time t.
func trueMass(t float64, overlap float64) float64 {
	m := InitialMassMg
	m -= peakLoss(t, 660, 55, 0.42)           // moisture near 80 C
	m -= peakLoss(t, 2350, 120, 2.18)         // devolatilization 1 ~300 C
	m -= peakLoss(t, 2520, 110, 0.95+overlap) // shoulder / overlapping peak
	m -= peakLoss(t, 4230, 150, 1.85)         // oxidation after N2->O2
	return m
}

// Generate creates baseline samples for one perturbation. Only one direct
// source is changed in each variant; everything else is inherited.
func Generate(perturbation string, seed int64) (samples []Sample, overlap float64, skip func(i int, s Sample) bool, extra map[string]string) {
	rng := rand.New(rand.NewSource(seed))
	prog := FurnaceProgram()
	overlap = 0.10
	if perturbation == POverlappingPeaksAlias {
		overlap = 0.55 // deepen the shoulder so two legal splits survive
	}
	gapWindow := func(i int, t float64) bool { return false }
	_ = gapWindow
	n := int(5280/SampleStepS) + 1
	noise := func(t float64) float64 {
		// symmetric micro noise; still mostly monotonic at -10 C/min ramps
		return (rng.Float64() - 0.5) * 0.006
	}
	if perturbation == PNonMonotonic {
		noise = func(t float64) float64 {
			// 1.5 mg scale jitter on a 6 min window around the devol peak:
			// non-monotonic, larger than the weak moisture loss
			if t > 2260 && t < 2620 {
				return (rng.Float64() - 0.5) * 0.30
			}
			return (rng.Float64() - 0.5) * 0.006
		}
	}
	gapStart, gapEnd := -1, -1
	if perturbation == PSamplingGap {
		// 7-minute sampling gap across the devol. shoulder region
		gapStart, gapEnd = 2440, 2860
	}
	for i := 0; i < n; i++ {
		t := float64(i) * SampleStepS
		if perturbation == PSamplingGap && int(t) >= gapStart && int(t) <= gapEnd {
			continue
		}
		c := tempAt(prog, t)
		mObs := trueMass(t, overlap)
		// balance baseline drift adds to observed reading; blank subtracts
		mObs += DriftRateMgH * (t / 3600.0)
		mObs -= blankAt(t)
		if perturbation == PBaselineDrift {
			mObs += 0.045 * (t / 3600.0) // unmodeled extra linear drift
		}
		mObs += noise(t)
		samples = append(samples, Sample{T: t, C: c, M: mObs})
	}
	extra = map[string]string{}
	if perturbation == PAtmosDelay {
		// same raw data; the single changed source is the params layer
		extra["delay_s"] = "120"
	}
	if perturbation == PInitialRevise {
		extra["initial_mg"] = "10.36"
	}
	if perturbation == PImportHalfFail {
		// data file intentionally broken: missing furnace layer + a bad unit
		extra["broken"] = "true"
	}
	_ = sort.Strings
	return samples, overlap, nil, extra
}

func blankAt(t float64) float64 {
	// small buoyancy-like curve peaking mid-run, returns to ~0
	return 0.03 * math.Exp(-math.Pow((t-2600)/1800, 2))
}

// RenderTGA renders the domain .tga text format for one variant.
func RenderTGA(perturbation string, samples []Sample, extra map[string]string, broken bool) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# 失重谱系离线案例包 — TGA 阶段分解、气氛切换与残留物证据链")
	fmt.Fprintf(&b, "# variant: %s (fixed seed, original representation preserved)\n", perturbation)
	initMg := InitialMassMg
	if v, ok := extra["initial_mg"]; ok {
		fmt.Fprintf(&b, "initial_mass %s mg  # revision: calibrated pan mass (parent version retained)\n", v)
		initMg = parseFloat(v)
	} else {
		fmt.Fprintln(&b, "initial_mass 10.50 mg")
	}
	delay := AtmosDelayS
	if v, ok := extra["delay_s"]; ok {
		delay = parseFloat(v)
	}
	fmt.Fprintf(&b, "params delay %.0f s lag %.0f C\n", delay, TempLagC)
	fmt.Fprintf(&b, "drift linear %.3f mg/h\n", DriftRateMgH)
	prog := FurnaceProgram()
	for _, st := range prog {
		if st.Hold {
			fmt.Fprintf(&b, "furnace hold %.0f s %.0f s %.0f C\n", st.From, st.To, st.T0)
		} else {
			fmt.Fprintf(&b, "furnace ramp %.0f s %.0f s %.0f %.0f C\n", st.From, st.To, st.T0, st.T1)
		}
	}
	fmt.Fprintf(&b, "gas switch %.0f s N2 O2 50 mL/min  # valve time; reaction at +%.0fs\n", ValveAtS, delay)
	// blank curve points
	for _, t := range []float64{0, 1200, 2600, 4000, 5280} {
		fmt.Fprintf(&b, "blank %.0f s %.4f mg\n", t, blankAt(t))
	}
	for i, sm := range samples {
		if broken && i%37 == 0 && i > 0 {
			// a deliberately unconvertible mass token (kept with a diagnostic)
			fmt.Fprintf(&b, "point %.0f s %.2f C NaN_mass mg\n", sm.T, sm.C)
			continue
		}
		fmt.Fprintf(&b, "point %.1f s %.2f C %.4f mg\n", sm.T, sm.C, sm.M)
	}
	if broken {
		// no furnace clause emitted in broken variant? They are already present;
		// to force half failure we append a structurally invalid directive and
		// the caller must also omit the furnace layer — handled by RenderBroken.
	}
	_ = initMg
	return b.String()
}

// RenderBroken renders a genuinely half-failing submission: furnace layer is
// absent and one directive is structurally invalid.
func RenderBroken(perturbation string, samples []Sample) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# import half-failure variant: furnace program missing + bad directive")
	fmt.Fprintln(&b, "initial_mass 10.50 mg")
	fmt.Fprintf(&b, "params delay %.0f s lag %.0f C\n", AtmosDelayS, TempLagC)
	fmt.Fprintf(&b, "drift linear %.3f mg/h\n", DriftRateMgH)
	fmt.Fprintf(&b, "gas switch %.0f s N2 O2 50 mL/min\n", ValveAtS)
	fmt.Fprintln(&b, "furnace bogus 100 s 200 s 30 50 C")
	for _, sm := range samples {
		fmt.Fprintf(&b, "point %.1f s %.2f C %.4f mg\n", sm.T, sm.C, sm.M)
	}
	return b.String()
}

func parseFloat(s string) float64 {
	var v float64
	fmt.Sscanf(s, "%g", &v)
	return v
}
