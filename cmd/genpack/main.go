// Command genpack regenerates the deterministic offline case pack under
// internal/casepack/data. It uses no randomness and no wall clock, so the
// generated files are byte-stable. Run: go run ./cmd/genpack
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const outRoot = "internal/casepack/data"

// massModel returns the true sample mass (mg) at temperature tC.
func massModel(tC float64) float64 {
	const m0 = 12.483
	m := m0
	m -= 1.0 / (1 + math.Exp(-(tC-225)/12)) // stage A, overlapping
	m -= 1.5 / (1 + math.Exp(-(tC-262)/14)) // stage B, overlapping
	m -= 3.1 / (1 + math.Exp(-(tC-480)/25)) // stage C
	return m
}

// blankModel is the blank (buoyancy) curve in mg as a function of temp.
func blankModel(tC float64) float64 { return 0.03 - 0.00008*(tC-30) }

// noiseBump adds deterministic non-monotonic bumps at fixed times.
func noiseBump(t float64) float64 {
	bumps := []float64{12.5, 55, 78}
	v := 0.0
	for _, c := range bumps {
		d := (t - c) / 0.5
		v += 0.01 * math.Exp(-d*d)
	}
	return v
}

func main() {
	must(os.MkdirAll(filepath.Join(outRoot, "baseline"), 0o755))
	must(os.MkdirAll(filepath.Join(outRoot, "revisions"), 0o755))
	must(os.MkdirAll(filepath.Join(outRoot, "partial"), 0o755))

	// sample_points.csv: time_s, temp_C (measured, lagging), mass_mg
	// ramp 30 -> 600 C over 100 min, dt 0.5 min, gap in (40,45) min.
	sp := "time_s,temp_C,mass_mg\n"
	for k := 0; k <= 200; k++ {
		t := float64(k) * 0.5
		if t > 40 && t < 45 {
			continue // sampling gap: points simply missing
		}
		temp := 30 + 5.7*(t-3*(1-math.Exp(-t/3))) // measured temp lags setpoint
		mass := massModel(temp) + blankModel(temp) + 0.0015*t + noiseBump(t)
		sp += fmt.Sprintf("%.0f,%.3f,%.4f\n", t*60, temp, mass)
	}
	write("baseline/sample_points.csv", sp)

	// drift_correction.csv
	write("baseline/drift_correction.csv", "param,value,unit\ndrift_rate,0.0015,mg/min\n")

	// furnace_program.csv: setpoint ramp (leads the measured temperature)
	fp := "time_s,setpoint_C\n"
	for k := 0; k <= 20; k++ {
		t := float64(k) * 5
		fp += fmt.Sprintf("%.0f,%.1f\n", t*60, 30+5.7*t)
	}
	write("baseline/furnace_program.csv", fp)

	// gas_valve_events.csv: N2 -> air at 10 min, air -> N2 at 70 min
	write("baseline/gas_valve_events.csv",
		"time_s,valve,gas,flow_mL_min\n600,V2,air,80\n4200,V2,N2,80\n")

	// blank_curve.csv
	bc := "temp_C,mass_mg\n"
	for tC := 30.0; tC <= 600; tC += 30 {
		bc += fmt.Sprintf("%.0f,%.5f\n", tC, blankModel(tC))
	}
	write("baseline/blank_curve.csv", bc)

	// initial_mass.csv
	write("baseline/initial_mass.csv", "value,unit\n12.483,mg\n")

	// revisions
	write("revisions/initial_mass_v2.csv", "value,unit\n12.461,mg\n")
	write("revisions/drift_correction_v2.csv", "param,value,unit\ndrift_rate,0.0019,mg/min\n")

	// partial bundle: five valid files + one broken gas_valve_events.csv
	for _, f := range []string{"sample_points.csv", "drift_correction.csv", "furnace_program.csv", "blank_curve.csv", "initial_mass.csv"} {
		data, err := os.ReadFile(filepath.Join(outRoot, "baseline", f))
		must(err)
		must(os.WriteFile(filepath.Join(outRoot, "partial", f), data, 0o644))
	}
	write("partial/gas_valve_events.csv",
		"time_s,valve,gas,flow_mL_min\n600,V2,air\n4200,V2,N2,80\n")

	fmt.Println("casepack data regenerated under", outRoot)
}

func write(rel, content string) {
	must(os.WriteFile(filepath.Join(outRoot, rel), []byte(content), 0o644))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
