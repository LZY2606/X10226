// Command gencase writes the domain-specific offline TGA case pack to disk.
// It is deterministic: no clocks, no randomness, no network.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"tga-review/internal/ingest"
)

type assertion struct {
	ID          string `json:"id"`
	Statement   string `json:"statement"`
	EventTypes  []string `json:"event_types"`
	Predicate   string `json:"predicate"`
	Boundary    string `json:"boundary"`
}

func write(dir, name, body string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644)
}

func main() {
	root := "cases/weight_loss_lineage"
	files := map[string]map[string]string{
		filepath.Join(root, "baseline"): {
			"samples.csv": ingest.CaseSamplesV1,
			"drift.csv": ingest.CaseDriftV1,
			"program.csv": ingest.CaseProgramV1,
			"gas.csv": ingest.CaseGasV1,
			"blank.csv": ingest.CaseBlankV1,
			"init_mass.csv": ingest.CaseInitMassV1,
		},
		filepath.Join(root, "revision"): {
			"samples_v2_gap.csv": ingest.CaseSamplesV2Gap,
			"drift_v2.csv": ingest.CaseDriftV2,
			"gas_v2.csv": ingest.CaseGasV2,
			"gas_bad.csv": ingest.CaseGasBad,
			"samples_bad_header.csv": ingest.CaseSamplesBadHeader,
			"samples_bad_rows.csv": ingest.CaseSamplesBadRows,
			"init_mass_v2.csv": ingest.CaseInitMassV2,
		},
	}
	for dir, fs := range files {
		for name, body := range fs {
			if err := write(dir, name, body); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
	}
	assertions := []assertion{
		{ID: "A1", Statement: "气体阀切换时间不等于样品反应时间：反应窗 = 阀时间 + 气氛延迟，样品温度 = 程序温度 - 热滞后", EventTypes: []string{"import.layer_committed", "derived.produced"}, Predicate: "reach_t_min == valve_t_min + atmosphere_delay_min && sample_t_k == program_t_k - thermal_lag_k", Boundary: "atmosphere_delay"},
		{ID: "A2", Statement: "阶段可重叠，且各阶段失重不强制总和为 100%", EventTypes: []string{"derived.produced", "review.decision"}, Predicate: "exists(s1,s2): overlap(s1,s2) && no_normalization_to_100", Boundary: "overlapping_peaks"},
		{ID: "A3", Statement: "数据缺口不跨越插值：DTG 与阶段在缺口处分段，缺测区间不生成点", EventTypes: []string{"derived.produced"}, Predicate: "forall gap g: no_derived_point_inside(g) && segments_split_at(g)", Boundary: "sampling_gap"},
		{ID: "A4", Statement: "天平基线漂移：终点质量扣除插值漂移；漂移源修订只使依赖链下游过期", EventTypes: []string{"import.layer_committed", "derived.produced"}, Predicate: "corrected == raw - drift(t) - blank(t) && stale_downstream_only(layer=drift)", Boundary: "balance_drift"},
		{ID: "A5", Statement: "非单调噪声：质量正反弹被标记 positive_bounce，不改变失量方向结论", EventTypes: []string{"derived.produced"}, Predicate: "dtg[i] > 0 && neighbors_losing => non_monotonic flagged", Boundary: "non_monotonic_noise"},
		{ID: "A6", Statement: "初重修订以新版本+父版本表达，旧版本仍可查看；残留率引用被选初重版本", EventTypes: []string{"import.layer_committed", "derived.produced"}, Predicate: "version.parent_version != '' && residue.init_mass_version == selected", Boundary: "initial_mass_revision"},
		{ID: "A7", Statement: "导入半失败：好层提交、坏层拒绝并留诊断，会话状态 partial；格式错误整层失败 failed/partial", EventTypes: []string{"import.began", "import.layer_committed", "import.concluded"}, Predicate: "session.status == 'partial' && layers_ok ∩ layers_bad == ∅", Boundary: "import_half_failure"},
		{ID: "A8", Statement: "无法唯一决定时保留全部合法候选，拒绝原因/不确定性是一等数据", EventTypes: []string{"derived.produced"}, Predicate: "ambiguous peak => >=2 stage candidates with reject_reason/uncertainty", Boundary: "overlapping_peaks"},
		{ID: "A9", Statement: "过期结果可查看但不能冒充当前结果；current 仅属于最新 batch", EventTypes: []string{"derived.produced"}, Predicate: "node.current iff node.batch == latest_batch", Boundary: "local_invalidation"},
		{ID: "A10", Statement: "回到任意旧事件恢复当时依赖版本与可见结论，不被最新结果覆盖", EventTypes: []string{"*"}, Predicate: "fold(events up to seq) deterministic == state@seq", Boundary: "history_replay"},
	}
	b, _ := json.MarshalIndent(map[string]any{
		"case": "weight_loss_lineage",
		"clock_origin": "2026-01-01T00:00:00Z",
		"seed": 20260101,
		"assertions": assertions,
	}, "", "  ")
	if err := write(filepath.Join(root), "assertions.json", string(b)+"\n"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("case pack written to", root)
}
