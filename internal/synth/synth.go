// Package synth deterministically synthesizes domain TGA materials: raw
// sample points, balance drift, furnace program, gas valve events, blank
// curve and stratified initial mass. It powers both the offline case pack
// and the fixed-seed automated tests; it never touches the network.
package synth

import (
	"fmt"
	"math"
	"strings"
)

// Options selects counterexample features for a scenario.
type Options struct {
	Name       string
	Noise      bool    // inject non-monotonic mass upticks
	Gap        bool    // inject a sampling gap (no rows, no interpolation)
	Overlap    bool    // add a second overlapping DTG peak
	BadRow     bool    // malformed numeric cell -> half failure candidate
	DriftShift bool    // changed balance baseline drift (local source change)
	InitMass   float64 // revised stratified initial mass (0 => baseline 10.0 mg)
	Step       int     // sampling period in seconds
}

// Loss models one Gaussian-shaped mass-loss event.
type Loss struct {
	Center float64
	Sigma  float64
	Total  float64
}

func gaussIntCumulative(t, center, sigma float64) float64 {
	return 0.5 * (1 + math.Erf((t-center)/(sigma*math.Sqrt2)))
}

// massAt evaluates the sample mass model at time t under options.
func massAt(t float64, opt Options) float64 {
	losses := []Loss{
		{Center: 240, Sigma: 70, Total: 0.5}, // moisture
		{Center: 750, Sigma: 90, Total: 5.5}, // main devolatilization
		{Center: 1250, Sigma: 80, Total: 2.0}, // oxidation/residue burn-off
	}
	if opt.Overlap {
		losses = []Loss{
			{Center: 240, Sigma: 70, Total: 0.5},
			{Center: 740, Sigma: 80, Total: 3.4},
			{Center: 805, Sigma: 55, Total: 2.1}, // overlaps previous (< 60 s apart)
			{Center: 1250, Sigma: 80, Total: 2.0},
		}
	}
	initMass := 10.0
	if opt.InitMass > 0 {
		initMass = opt.InitMass
	}
	m := initMass
	for _, l := range losses {
		m -= l.Total * gaussIntCumulative(t, l.Center, l.Sigma)
	}
	if opt.Noise {
		// sparse positive spikes: non-monotonic noise, never true mass gain
		for _, nt := range []float64{510, 1050} {
			if float64(t) == nt {
				m += 0.18
			}
		}
	}
	return m
}

// tempAt is the furnace program temperature in C.
func tempAt(t float64) float64 {
	switch {
	case t < 300:
		return 25 + (300-25)*t/300
	case t < 1200:
		return 300 + (700-300)*(t-300)/900
	default:
		return 700 + (800-700)*(t-1200)/600
	}
}

// Files returns the six stratified CSV payloads for a scenario.
func Files(opt Options) map[string]string {
	step := opt.Step
	if step <= 0 {
		step = 15
	}
	var sb strings.Builder
	sb.WriteString("time_s,mass_mg,temp_C\n")
	for t := 0; t <= 1800; t += step {
		if opt.Gap && t >= 700 && t <= 820 {
			continue // missing rows: explicit sampling gap
		}
		m := massAt(float64(t), opt)
		if opt.BadRow && t == 900 {
			sb.WriteString(fmt.Sprintf("%d,not_a_number,%.2f\n", t, tempAt(float64(t))))
			continue
		}
		sb.WriteString(fmt.Sprintf("%d,%.4f,%.2f\n", t, m, tempAt(float64(t))))
	}
	out := map[string]string{}
	out["samples.csv"] = sb.String()
	// balance baseline drift correction anchors
	driftSlope := 0.00008 // mg/s over 1800 s -> ~0.144 mg
	if opt.DriftShift {
		driftSlope = 0.00016 // revised baseline: local dependency only
	}
	var db strings.Builder
	db.WriteString("time_s,offset_mg\n")
	for _, t := range []int{0, 300, 600, 900, 1200, 1500, 1800} {
		db.WriteString(fmt.Sprintf("%d,%.4f\n", t, driftSlope*float64(t)))
	}
	out["drift.csv"] = db.String()
	// furnace program
	out["program.csv"] = "from_s,to_s,rate_Cpmin\n0,300,55.0\n300,1200,26.7\n1200,1800,10.0\n"
	// gas valve events: switch at 900 s; with delay+lag reaction ~ 975 s
	out["gas.csv"] = "at_s,from_gas,to_gas\n0,,N2\n900,N2,air\n"
	// blank curve (instrument baseline, near zero)
	var bb strings.Builder
	bb.WriteString("time_s,mass_mg\n")
	for t := 0; t <= 1800; t += 300 {
		bb.WriteString(fmt.Sprintf("%d,%.4f\n", t, 0.01*float64(t)/1800))
	}
	out["blank.csv"] = bb.String()
	// stratified initial sample mass
	initMass := 10.0
	if opt.InitMass > 0 {
		initMass = opt.InitMass
	}
	out["init_mass.csv"] = fmt.Sprintf("mass_mg\n%.4f\n", initMass)
	return out
}

// GasSwitchS exposes the domain timing anchors for assertions.
const GasSwitchS = 900
