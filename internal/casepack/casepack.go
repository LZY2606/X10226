// Package casepack embeds the offline TGA case pack "失重谱系：TGA 阶段分解、
// 气氛切换与残留物证据链". It is domain material (six stratified layers +
// domain manifest + targeted mutations), never a generic spreadsheet.
package casepack

import (
	"embed"
	"fmt"
)

//go:embed all:data
var data embed.FS

func Read(name string) ([]byte, error) {
	return data.ReadFile("data/" + name)
}

func Must(name string) []byte {
	b, err := Read(name)
	if err != nil {
		panic(fmt.Sprintf("casepack %s: %v", name, err))
	}
	return b
}

type ScenarioSpec struct {
	ID     string
	Kind   string
	File   string
	Note   string
	Expect []string
	Direct string
}

type Manifest struct {
	Title       string         `json:"title"`
	Baseline    []string       `json:"baseline_files"`
	ReplayOrder []string       `json:"replay_order"`
	Scenarios   []ScenarioSpec `json:"scenarios"`
}

func BuiltinManifest() Manifest {
	return Manifest{
		Title: "失重谱系：TGA 阶段分解、气氛切换与残留物证据链",
		Baseline: []string{
			"samples.csv", "drift.csv", "blank.csv",
			"furnace.csv", "valves.csv", "initial.csv",
		},
		ReplayOrder: []string{"baseline", "param_revision", "local_change", "conflict"},
		Scenarios: []ScenarioSpec{
			{ID: "atmosphere_delay", Kind: "param", Direct: "params.atmosphere",
				Note:   "gas front reaches the sample after the valve switches",
				Expect: []string{"stages", "valve_effective", "program_sample_t", "residuals", "mass_fraction"}},
			{ID: "drift_baseline", Kind: "param", Direct: "params.drift",
				Note:   "balance baseline drift offset changes corrected mass",
				Expect: []string{"corrected_tg", "dtg", "stages", "residuals", "mass_fraction"}},
			{ID: "overlapping_peaks", Kind: "param", Direct: "params.peak",
				Note:   "lower prominence exposes overlapping DTG shoulders",
				Expect: []string{"stages"}},
			{ID: "nonmonotonic_noise", Kind: "param", Direct: "params.noise",
				Note:   "stricter tolerance flags noise; data retained",
				Expect: []string{"stages", "residuals"}},
			{ID: "sampling_gap", Kind: "param", Direct: "params.gap",
				Note:   "smaller max step opens a sampling gap; no interpolation",
				Expect: []string{"corrected_tg", "dtg", "stages", "residuals", "mass_fraction"}},
			{ID: "initial_mass_revision", Kind: "import", File: "initial_v2.csv",
				Direct: "initial", Note: "revised initial mass; only percent-bearing results change",
				Expect: []string{"stages", "residuals", "mass_fraction"}},
			{ID: "partial_import", Kind: "import", File: "samples_partial_bundle.txt",
				Direct: "samples", Note: "one file bad, one layer partial: half failure contained",
				Expect: []string{"corrected_tg", "dtg", "stages", "residuals", "mass_fraction"}},
		},
	}
}
