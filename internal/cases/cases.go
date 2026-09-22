// Package cases builds the offline, domain-specific TGA case pack with a
// fixed seed. The generated bundles are committed under /cases so the demo
// and tests never need the network or a developer database; a test asserts
// the committed files are byte-reproducible from this generator.
package cases

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strings"

	"tgaaudit/internal/domain"
)

// Seed is the only RNG seed used by the synthetic material.
const Seed int64 = 20260922

// StageDef is one logistic mass-loss step of the synthetic sample.
type StageDef struct {
	Center float64
	Width  float64
	Loss   float64 // fraction of initial mass
}

// SampleOptions parameterise counter-examples; each changes one source only.
type SampleOptions struct {
	ExtraShoulder bool
	Bumps         []float64 // times at which a positive noise bump is injected
	GapRange      [2]float64
	GapOmit       bool // true = omit rows entirely (time jump), false = empty cells
	BadMassUnit   string
}

// BaselineStages are the three partially overlapping decomposition steps.
func BaselineStages() []StageDef {
	return []StageDef{
		{Center: 200, Width: 11, Loss: 0.030},
		{Center: 250, Width: 9, Loss: 0.020},
		{Center: 520, Width: 20, Loss: 0.050},
	}
}

func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

// TrueFraction is the synthetic underlying remaining mass fraction.
func TrueFraction(t float64, stages []StageDef) float64 {
	f := 1.0
	for _, st := range stages {
		f -= st.Loss * sigmoid((t-st.Center)/st.Width)
	}
	return f
}

// TempRamp maps furnace time to sample temperature (deg C).
func TempRamp(t float64) float64 { return 25 + 0.35*t }

const (
	Mass0         = 10.0 // mg
	DriftMgPerMin = 0.006
	GasSwitchT    = 472.0
)

// SampleCSV renders the samples section CSV text.
func SampleCSV(o SampleOptions) string {
	rng := rand.New(rand.NewSource(Seed))
	stages := BaselineStages()
	if o.ExtraShoulder {
		stages = append(stages, StageDef{Center: 263, Width: 6, Loss: 0.012})
	}
	massUnit := "mg"
	if o.BadMassUnit != "" {
		massUnit = o.BadMassUnit
	}
	var b strings.Builder
	fmt.Fprintf(&b, "t[s],mass[%s],ts[C]\n", massUnit)
	bump := map[float64]float64{}
	for _, bt := range o.Bumps {
		bump[bt] = 1
	}
	for k := 0; k <= 300; k++ {
		t := float64(k * 3)
		if o.GapOmit && t >= o.GapRange[0] && t <= o.GapRange[1] {
			continue
		}
		m := Mass0 * TrueFraction(t, stages)
		// balance drift that the drift section is designed to remove
		m += DriftMgPerMin * (t / 60.0)
		// fixed-seed micro noise (±2 ug)
		m += (rng.Float64()*2 - 1) * 0.002
		if bump[t] > 0 {
			m += 0.008
		}
		if !o.GapOmit && t >= o.GapRange[0] && t <= o.GapRange[1] {
			fmt.Fprintf(&b, "%.1f,,%.2f\n", t, TempRamp(t))
			continue
		}
		fmt.Fprintf(&b, "%.1f,%.4f,%.2f\n", t, m, TempRamp(t))
	}
	return b.String()
}

// FurnaceCSV renders the furnace program.
func FurnaceCSV() string {
	var b strings.Builder
	b.WriteString("t[s],tf[C]\n")
	for t := 0.0; t <= 900; t += 10 {
		fmt.Fprintf(&b, "%.0f,%.2f\n", t, TempRamp(t))
	}
	return b.String()
}

// GasCSV renders valve events; switchT overrides the valve timing for the
// atmosphere-delay counter-example (gas is the only changed source).
func GasCSV(switchT float64) string {
	return fmt.Sprintf("t[s],gas,event\n0,N2,initial_purge\n%.0f,air,valve_open\n", switchT)
}

// BlankCSV renders a dense blank-cruise curve.
func BlankCSV() string {
	var b strings.Builder
	b.WriteString("t[s],mass[mg]\n")
	for t := 0.0; t <= 900; t += 2 {
		v := 0.010*(t/900.0) - 0.005 + 0.0006*math.Sin(t/31.0)
		fmt.Fprintf(&b, "%.0f,%.5f\n", t, v)
	}
	return b.String()
}

// DriftCSV renders piecewise balance drift correction.
func DriftCSV(mgPerMin float64) string {
	return fmt.Sprintf("t0[s],t1[s],slope[mg/min],intercept[mg]\n0,900,%.4f,0\n",
		mgPerMin)
}

// InitialMassCSV renders the declared initial mass layer.
func InitialMassCSV(mg float64, note string) string {
	return fmt.Sprintf("mass[mg],at[s],note\n%.4f,0,%s\n", mg, note)
}

func sectionCSV(text string) json.RawMessage {
	w := map[string]string{"format": "csv", "text": text,
		"source_name": "synthetic-fixed-seed-20260922"}
	b, _ := json.Marshal(w)
	return b
}

// Bundle assembles a domain bundle from named sections.
func Bundle(id, label string, sections map[string]string) *domain.Bundle {
	b := &domain.Bundle{BundleID: id, Label: label,
		Sections: map[string]json.RawMessage{}}
	for kind, text := range sections {
		b.Sections[kind] = sectionCSV(text)
	}
	return b
}

// Baseline builds the complete six-layer baseline import.
func Baseline() *domain.Bundle {
	return Bundle("baseline-v1", "Baseline TGA run: three overlapping stages, N2 to air switch",
		map[string]string{
			domain.KindSamples:     SampleCSV(SampleOptions{}),
			domain.KindFurnace:     FurnaceCSV(),
			domain.KindGas:         GasCSV(GasSwitchT),
			domain.KindBlank:       BlankCSV(),
			domain.KindDrift:       DriftCSV(DriftMgPerMin),
			domain.KindInitialMass: InitialMassCSV(Mass0, "balance_readout_42_tare_B7"),
		})
}

// Scenario identifies a counter-example.
type Scenario struct {
	ID          string
	Title       string
	Description string
	Changed     string
	Build       func() *domain.Bundle
}

// Scenarios returns the seven required boundary situations. Every
// counter-example changes exactly one direct source relative to the baseline.
func Scenarios() []Scenario {
	return []Scenario{
		{
			ID: "atmosphere_delay", Title: "Atmosphere transport delay",
			Description: "Valve switch moved earlier; only the gas source changes, so only reaction-window evidence may be affected.",
			Changed:     "gas",
			Build: func() *domain.Bundle {
				return Bundle("ce-atmosphere-delay", "Counter: gas valve switched at 400 s",
					map[string]string{domain.KindGas: GasCSV(400)})
			},
		},
		{
			ID: "balance_drift", Title: "Balance baseline drift revision",
			Description: "Drift slope revised fivefold; only corrected mass and its dependants may be affected.",
			Changed:     "drift",
			Build: func() *domain.Bundle {
				return Bundle("ce-balance-drift", "Counter: drift slope 0.030 mg/min",
					map[string]string{domain.KindDrift: DriftCSV(0.030)})
			},
		},
		{
			ID: "overlapping_peaks", Title: "Overlapping / unresolved peaks",
			Description: "An added shoulder admits two legal decompositions; both hypotheses are retained.",
			Changed:     "samples",
			Build: func() *domain.Bundle {
				return Bundle("ce-overlapping-peaks", "Counter: extra shoulder near 263 s",
					map[string]string{domain.KindSamples: SampleCSV(SampleOptions{ExtraShoulder: true})})
			},
		},
		{
			ID: "nonmonotonic_noise", Title: "Non-monotonic noise",
			Description: "Positive mass excursions are flagged as noise evidence; points are kept.",
			Changed:     "samples",
			Build: func() *domain.Bundle {
				return Bundle("ce-nonmonotonic", "Counter: positive bumps at 330 s and 642 s",
					map[string]string{domain.KindSamples: SampleCSV(SampleOptions{Bumps: []float64{330, 642}})})
			},
		},
		{
			ID: "sampling_gap", Title: "Sampling gap (omitted rows)",
			Description: "Rows 300-342 s are missing: the gap is never interpolated; affected stages and residue become unknown.",
			Changed:     "samples",
			Build: func() *domain.Bundle {
				return Bundle("ce-sampling-gap", "Counter: missing rows 300-342 s",
					map[string]string{domain.KindSamples: SampleCSV(SampleOptions{
						GapRange: [2]float64{300, 342}, GapOmit: true})})
			},
		},
		{
			ID: "initial_mass_revision", Title: "Initial mass revision",
			Description: "Declared initial mass revised after re-tare; only mass-derived results change.",
			Changed:     "initial_mass",
			Build: func() *domain.Bundle {
				return Bundle("ce-initial-mass", "Counter: initial mass 9.987 mg after re-tare",
					map[string]string{domain.KindInitialMass: InitialMassCSV(9.987,
						"revised_after_crucible_retare_B8")})
			},
		},
		{
			ID: "import_half_failure", Title: "Import half failure",
			Description: "Samples declare an unsupported unit so that section is rejected while other sections commit; the attempt is recorded as partial.",
			Changed:     "mixed",
			Build: func() *domain.Bundle {
				sections := map[string]string{
					domain.KindFurnace:     FurnaceCSV(),
					domain.KindGas:         GasCSV(GasSwitchT),
					domain.KindBlank:       BlankCSV(),
					domain.KindDrift:       DriftCSV(DriftMgPerMin),
					domain.KindInitialMass: InitialMassCSV(Mass0, "balance_readout_42_tare_B7"),
					domain.KindSamples:     SampleCSV(SampleOptions{BadMassUnit: "stones"}),
				}
				return Bundle("ce-import-half-fail", "Counter: samples section has unsupported unit", sections)
			},
		},
	}
}

// Marshal renders a bundle deterministically.
func Marshal(b *domain.Bundle) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(b); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// FileName is the committed on-disk file name for a bundle.
func FileName(id string) string { return id + ".json" }
