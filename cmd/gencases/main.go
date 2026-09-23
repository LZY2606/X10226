// Command gencases regenerates the committed offline TGA domain case pack.
// Deterministic: fixed seed, fixed virtual timestamps. Do not use wall clock.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
)

type sampleRow struct {
	TS     float64 `json:"t_s"`
	MassMG string  `json:"mass_mg"`
	TempC  string  `json:"temp_c,omitempty"`
}

func main() {
	dir := filepath.Join("internal", "cases", "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(err)
	}
	rng := rand.New(rand.NewSource(20260919))

	// ---- furnace program (0..3300 s) ----
	furnace := `from_c,to_c,rate_k_per_min,duration_s,gas,kind
30,150,20,360,N2,ramp
150,150,0,420,N2,hold
150,500,10,2100,N2,ramp
500,500,0,420,O2,hold
`
	// 30->150 in 360s (20/min), hold 420 (to 780), 150->500 in 2100s (10/min, to 2880), hold 420 (to 3300)
	// ---- valve events (valve clock != sample reaction clock) ----
	valves := `t_s,gas
0,N2
2880,O2
`

	// ---- blank curve: tiny, nearly flat with own drift ----
	var blank string = "t_s,mass_mg\n"
	for t := 0.0; t <= 3300; t += 120 {
		bm := 0.10 + 0.00002*t
		bm += rng.NormFloat64() * 0.0005
		blank += fmt.Sprintf("%.0f,%.5f\n", t, bm)
	}

	// ---- drift calibration (balance baseline drift) ----
	drift := `t_s,mass_mg
0,-0.002
3300,0.196
`
	// slope = 0.198/3300 = 6.0e-5 mg/s linear bias

	// ---- sample series ----
	// components (mg): start 10.0
	// moisture: 0.20 peak at 360s (~80C)
	// decomp A: 1.20 peak at 1080s (setpoint 180C)
	// decomp B: 1.00 peak at 1440s (setpoint 240C) -- overlaps A
	// oxidation: 1.80 peak at effective-arrival ~3000s (valve 2880 + 45s delay)
	trueMass := func(t float64) float64 {
		m := 10.0
		m -= 0.20 * stepLoss(t, 300, 430)
		m -= 1.20 * gaussLoss(t, 1080, 75)
		m -= 1.00 * gaussLoss(t, 1440, 90)
		// oxidation reaction after gas arrives at sample (delay 45s)
		m -= 1.80 * stepLoss(t, 2960, 3120)
		return m
	}
	gap := func(t float64) bool {
		// acquisition gap 1700..1820 s (60s missing at 2s spacing)
		return t >= 1700 && t <= 1820
	}
	var samples []sampleRow
	for t := 0.0; t <= 3300; t += 2 {
		if gap(t) {
			continue
		}
		if t > 3300 {
			break
		}
		m := trueMass(t)
		// balance drift + blank-like bias + measurement noise
		bias := -0.002 + 6.0e-5*t
		m += bias + 0.10 + rng.NormFloat64()*0.003
		// deliberate non-monotonic spikes (instrument bounce, not chemistry)
		if int(t) == 1240 || int(t) == 2100 {
			m += 0.06
		}
		// one unconvertible temperature token preserved in place
		temp := fmt.Sprintf("%.2f", sampleTemp(t))
		if int(t) == 600 {
			temp = "N/A"
		}
		samples = append(samples, sampleRow{TS: t, MassMG: fmt.Sprintf("%.5f", m), TempC: temp})
	}
	initialMass := "mass_mg,note\n10.000,weighed before ramp v1\n"

	writeFile(filepath.Join(dir, "furnace.csv"), furnace)
	writeFile(filepath.Join(dir, "valves.csv"), valves)
	writeFile(filepath.Join(dir, "blank.csv"), blank)
	writeFile(filepath.Join(dir, "drift.csv"), drift)
	writeFile(filepath.Join(dir, "initial_mass.csv"), initialMass)
	b, err := json.MarshalIndent(map[string]any{"points": samples}, "", "  ")
	if err != nil {
		fail(err)
	}
	writeFile(filepath.Join(dir, "samples.json"), string(b)+"\n")

	// parameter revision scenario: delay 45s -> 90s
	writeFile(filepath.Join(dir, "scenario_params_delay90.json"),
		`{"name":"atmo_delay_90","rationale":"revisited transfer line length","params":{
"atmo_delay_s":{"O2":90,"N2":0},"temp_lag_s":30,"dtg_smooth_window":5,"dtg_threshold_percent_min":0.6,"overlap_split":true}}
`)
	// initial mass revision v2
	writeFile(filepath.Join(dir, "initial_mass_v2.csv"), "mass_mg,note\n10.050,re-weighed from sealed pan log v2\n")
	// half-failed import: one fully rejected fragment + one committed no-op
	writeFile(filepath.Join(dir, "halffail_bad_valves.csv"), "t_s,gas\nnot-a-time,O2\n")
	writeFile(filepath.Join(dir, "halffail_bad_furnace.csv"), "from_c,to_c,rate\nbanana,500,10\n")
	writeFile(filepath.Join(dir, "halffail_ok_initmass.csv"), "mass_mg,note\n10.000,no-op re-submission during half-fail probe\n")
	// drift revision v2 (local dependency change)
	writeFile(filepath.Join(dir, "drift_v2.csv"), "t_s,mass_mg\n0,0.004\n3300,0.100\n")
	// noise-heavy samples fragment used to show non-monotonic quarantine
	writeFile(filepath.Join(dir, "noise_note.txt"), "nonmonotonic spikes embedded at t=1240s and t=2100s; gap 1700-1820s\n")

	manifest := map[string]any{
		"seed": 20260919, "horizon_s": 3300, "sample_period_s": 2,
		"components_mg": map[string]float64{"moisture": 0.20, "decomp_a": 1.20, "decomp_b": 1.00, "oxidation": 1.80, "residue": 5.80},
		"valve_o2_t_s": 2880, "atmo_delay_o2_s": 45, "temp_lag_s": 30,
		"gap_s": []float64{1700, 1820}, "spike_t_s": []int{1240, 2100},
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	writeFile(filepath.Join(dir, "manifest.json"), string(mb)+"\n")
	fmt.Println("case pack generated in", dir)
}

func writeFile(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// gaussLoss approximates a TGA loss peak as a Gaussian cumulative transition
// scaled so its integral matches the target component mass.
func gaussLoss(t, mu, sigma float64) float64 {
	// cumulative fraction of a Gaussian center mu with width sigma
	return 0.5 * (1 + math.Erf((t-mu)/(sigma*math.Sqrt2)))
}

// stepLoss is a smooth linear loss ramp across [a,b].
func stepLoss(t, a, b float64) float64 {
	if t <= a {
		return 0
	}
	if t >= b {
		return 1
	}
	return (t - a) / (b - a)
}

// sampleTemp returns the sample thermocouple reading following the program
// with first-order lag approximated at 2s resolution.
func sampleTemp(t float64) float64 {
	setpoint := func(t float64) float64 {
		switch {
		case t <= 360:
			return 30 + (150-30)*t/360
		case t <= 780:
			return 150
		case t <= 2880:
			return 150 + (500-150)*(t-780)/2100
		default:
			return 500
		}
	}
	const lag = 30.0
	eff := setpoint(0)
	dt := 2.0
	for tt := 0.0; tt <= t; tt += dt {
		sp := setpoint(tt)
		eff += (1 - math.Exp(-dt/lag)) * (sp - eff)
	}
	return eff
}
