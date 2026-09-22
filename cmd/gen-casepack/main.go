// Command gen-casepack regenerates the offline domain dossier under
// testdata/casepack with a fixed seed. It performs no network access.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"tga-review/internal/casepack"
)

type expectation struct {
	Perturbation  string   `json:"perturbation"`
	ChangedSource string   `json:"changed_direct_source"`
	OnlyAffects   []string `json:"only_affects_families"`
	Principles    []string `json:"principles"`
	MustSee       []string `json:"must_see"` // machine-readable outcome codes
}

func main() {
	dir := flag.String("out", "testdata/casepack", "output directory")
	flag.Parse()
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	type variantFile struct {
		Perturbation string `json:"perturbation"`
		File         string `json:"file"`
	}
	var files []variantFile

	// baseline
	samples, _, _, _ := casepack.Generate("baseline", 20260919)
	write(filepath.Join(*dir, "baseline.tga"), casepack.RenderTGA("baseline", samples, nil, false))
	files = append(files, variantFile{"baseline", "baseline.tga"})

	expectations := []expectation{
		{
			Perturbation: casepack.PAtmosDelay, ChangedSource: "params.atmos_delay_s",
			OnlyAffects: []string{"gas-timeline", "candidate-set", "residual"},
			Principles:  []string{"valve_time_not_reaction_time"},
			MustSee:     []string{"gas_attribution_ambiguous", "reaction_after_valve"},
		},
		{
			Perturbation: casepack.PBaselineDrift, ChangedSource: "observed_points(extra_linear_drift)",
			OnlyAffects: []string{"corrected-series", "dtg-series", "candidate-set", "residual", "mass-balance"},
			Principles:  []string{"drift_correction_is_its_own_layer"},
			MustSee:     []string{"residual_shifts", "raw_points_unchanged_absent_drift_layer"},
		},
		{
			Perturbation: casepack.POverlappingPeaks, ChangedSource: "observed_points(shoulder_depth)",
			OnlyAffects: []string{"candidate-set", "mass-balance"},
			Principles:  []string{"stages_may_overlap_not_sum_100", "keep_all_legal_candidates_when_indeterminate"},
			MustSee:     []string{"variant_single", "variant_split_a_b", "variant_overlap_split"},
		},
		{
			Perturbation: casepack.PNonMonotonic, ChangedSource: "observed_points(noise_amplitude)",
			OnlyAffects: []string{"dtg-series", "candidate-set"},
			Principles:  []string{"uncertainty_is_first_class"},
			MustSee:     []string{"loss_below_noise_floor_or_unknown_kept"},
		},
		{
			Perturbation: casepack.PSamplingGap, ChangedSource: "observed_points(removed_window)",
			OnlyAffects: []string{"segments", "dtg-series", "candidate-set"},
			Principles:  []string{"no_interpolation_across_gap", "keep_all_legal_candidates_when_indeterminate"},
			MustSee:     []string{"stage_crosses_data_gap", "segments_split", "no_bridged_points"},
		},
		{
			Perturbation: casepack.PInitialRevise, ChangedSource: "initial_mass",
			OnlyAffects: []string{"residual", "mass-balance", "candidate-set(loss_percent_only)"},
			Principles:  []string{"revisions_are_versioned_with_parent"},
			MustSee:     []string{"raw_points_identical", "residual_percent_changes"},
		},
		{
			Perturbation: casepack.PImportHalfFail, ChangedSource: "submission(missing_furnace+bad_directive)",
			OnlyAffects: []string{"(none — quarantined, no derived artifacts)"},
			Principles:  []string{"unconvertible_values_stay_in_place"},
			MustSee:     []string{"quarantined", "diagnostics_present", "no_new_candidate_set"},
		},
	}
	for i, e := range expectations {
		samples, _, _, extra := casepack.Generate(e.Perturbation, int64(1000+i))
		name := e.Perturbation
		if e.Perturbation == casepack.PImportHalfFail {
			fn := name + ".tga"
			write(filepath.Join(*dir, fn), casepack.RenderBroken(name, samples))
			files = append(files, variantFile{name, fn})
			continue
		}
		fn := name + ".tga"
		write(filepath.Join(*dir, fn), casepack.RenderTGA(name, samples, extra, false))
		files = append(files, variantFile{name, fn})
	}

	manifest := map[string]any{
		"title": "失重谱系：TGA 阶段分解、气氛切换与残留物证据链",
		"seed":  20260919,
		"principles": map[string]string{
			"valve_time_not_reaction_time":                 "气氛延迟和温度滞后使阀切换时间不等于样品反应时间",
			"stages_may_overlap_not_sum_100":               "阶段可重叠且不强制总和为百分之百",
			"no_interpolation_across_gap":                  "数据缺口不跨越插值",
			"keep_all_legal_candidates_when_indeterminate": "无法唯一决定时保留全部合法候选",
		},
		"variants":     files,
		"expectations": expectations,
	}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	write(filepath.Join(*dir, "manifest.json"), string(mb))
	fmt.Println("casepack written to", *dir)
}

func write(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
