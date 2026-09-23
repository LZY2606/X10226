// Package cases ships the offline TGA domain case pack: synthetic mass,
// temperature, furnace program, valve events, blank curve and stratified
// initial mass, plus the seven counterexample scenarios.
package cases

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"

	"tga-review/internal/app"
	"tga-review/internal/domain"
)

//go:embed data/*
var FS embed.FS

func MustRead(name string) string {
	b, err := FS.ReadFile("data/" + name)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// Baseline imports the six stratified layers that constitute the saved
// baseline: raw sample points, balance drift calibration, furnace program,
// gas valve events, blank curve and sample initial mass.
func Baseline(ctx context.Context, svc *app.Service, projectID string) (*app.ImportResult, error) {
	return svc.Import(ctx, projectID, []app.ImportFragment{
		{Layer: domain.LayerSamples, Format: "json", Raw: MustRead("samples.json")},
		{Layer: domain.LayerDrift, Format: "csv", Raw: MustRead("drift.csv")},
		{Layer: domain.LayerFurnace, Format: "csv", Raw: MustRead("furnace.csv")},
		{Layer: domain.LayerValves, Format: "csv", Raw: MustRead("valves.csv")},
		{Layer: domain.LayerBlank, Format: "csv", Raw: MustRead("blank.csv")},
		{Layer: domain.LayerInitMass, Format: "csv", Raw: MustRead("initial_mass.csv")},
	})
}

// Manifest describes the synthetic experiment.
type Manifest struct {
	Seed        int             `json:"seed"`
	HorizonS    float64         `json:"horizon_s"`
	Components  map[string]float64 `json:"components_mg"`
	ValveO2     float64         `json:"valve_o2_t_s"`
	DelayO2     float64         `json:"atmo_delay_o2_s"`
	GapS        []float64       `json:"gap_s"`
	SpikeTS     []int           `json:"spike_t_s"`
}

func ReadManifest() Manifest {
	var m Manifest
	_ = json.Unmarshal([]byte(MustRead("manifest.json")), &m)
	return m
}

// ScenarioSpec is a named, executable counterexample.
type ScenarioSpec struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Desc  string `json:"desc"`
	Run   func(context.Context, *app.Service, string) (app.ScenarioOutcome, error)
}

// Specs returns all seven required counterexample scenarios.
func Specs() []ScenarioSpec {
	return []ScenarioSpec{
		{"atmo_delay", "气氛延迟：阀切换时间不等于反应时间", "O2 transport delay changes attribution window", ScenarioAtmoDelay},
		{"balance_drift", "天平基线漂移修订", "drift calibration revision changes corrected mass", ScenarioDriftRevision},
		{"overlap_peaks", "重叠峰：保留全部合法候选", "merged and split proposals coexist until adjudication", ScenarioOverlap},
		{"nonmonotonic", "非单调噪声被隔离而不被抹平", "spike points are quarantined, not interpolated", ScenarioNonmonotonic},
		{"sample_gap", "采样缺口：不跨越插值", "gap splits regions and bounds trapezoid area", ScenarioGap},
		{"initial_mass_revision", "初重修订：仅质量比下游过期", "m0 revision changes normalized DTG and residue only", ScenarioInitMassRevision},
		{"half_import", "导入半失败：提交片段与拒绝片段并存", "rejected fragments remain visible; committed ones still flow", ScenarioHalfImport},
	}
}

func adopted(src string, before, after map[string]any, scope []string, note string) app.ScenarioOutcome {
	return app.ScenarioOutcome{Status: "adopted", ChangedSource: src, Before: before, After: after, Scope: scope, Note: note}
}

func rejected(src string, diags []domain.Diag, note string) app.ScenarioOutcome {
	return app.ScenarioOutcome{Status: "rejected", ChangedSource: src,
		After: map[string]any{"diagnostics": diags}, Note: note}
}

// RunSpec executes a named scenario and records it in the counterexample
// archive (both adopted and rejected outcomes are retained).
func RunSpec(ctx context.Context, svc *app.Service, projectID, key string) (*domain.ArchiveEntry, error) {
	for _, sp := range Specs() {
		if sp.Key == key {
			return svc.RunScenario(ctx, projectID, app.Scenario{
				Key: sp.Key, Title: sp.Title, Run: sp.Run,
			})
		}
	}
	return nil, fmt.Errorf("unknown scenario %q", key)
}
