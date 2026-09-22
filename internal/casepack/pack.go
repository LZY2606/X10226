package casepack

import (
	"embed"
	"encoding/json"
)

//go:embed tga-stage-atmosphere-evidence-case.json
var caseJSON []byte

//go:embed README.md
var readme []byte

var _ embed.FS

func copiedRawWith420Mass(mass float64) map[string]any {
	content := initialImport()["sources"].([]map[string]any)[0]["content"].(map[string]any)
	rows := content["rows"].([]map[string]any)
	copiedRows := make([]map[string]any, len(rows))
	for i, row := range rows {
		copiedRows[i] = map[string]any{"time": row["time"], "temperature": row["temperature"], "mass": row["mass"]}
	}
	copiedRows[7]["mass"] = q(mass, "mg", "局部修订天平原始读数")
	return map[string]any{"coordinate_basis": content["coordinate_basis"], "rows": copiedRows}
}

type Pack struct {
	Schema            string         `json:"schema"`
	Title             string         `json:"title"`
	Description       string         `json:"description"`
	Seed              int64          `json:"seed"`
	InitialImport     map[string]any `json:"initial_import"`
	Replay            ReplayPack     `json:"replay"`
	MachineAssertions map[string]any `json:"machine_assertions"`
}

type ReplayPack struct {
	InitialMassRevision map[string]any   `json:"initial_mass_revision"`
	DriftRevision       map[string]any   `json:"balance_drift_revision"`
	LocalRawRevision    map[string]any   `json:"local_raw_revision"`
	HalfFailedImport    map[string]any   `json:"import_half_failure"`
	ParameterAdjustment map[string]any   `json:"parameter_adjustment"`
	Decisions           []map[string]any `json:"decisions"`
}

func Builtin() Pack {
	var pack Pack
	_ = json.Unmarshal(caseJSON, &pack)
	return pack
}

func BuiltinRaw() []byte { return caseJSON }
func Readme() []byte     { return readme }

func init() {
	// Keep helper payloads aligned with the checked-in narrative pack.
	_ = json.Valid(caseJSON)
}

func ExportPackForGenerator() Pack {
	return Pack{
		Schema:        "tga-domain-casepack/v1",
		Title:         "失重谱系：TGA 阶段分解、气氛切换与残留物证据链",
		Description:   "面向热分析团队的离线案例：分层基线、天平漂移、炉程序、气体阀、空白曲线、初重、重叠峰、非单调噪声、采样缺口、初重修订、导入半失败和冲突人工决定。",
		Seed:          20260923,
		InitialImport: initialImport(),
		Replay: ReplayPack{
			InitialMassRevision: initialMassRevision(),
			DriftRevision:       driftRevision(),
			LocalRawRevision:    localRawRevision(),
			HalfFailedImport:    halfFailedImport(),
			ParameterAdjustment: parameterAdjustment(),
			Decisions: []map[string]any{
				{"step": "accept_broad_shoulder_first", "subject_id": "stage:s02:broad", "verdict": "accepted", "rationale": "重叠峰宽窗口先作为合理解释保留", "actor": "reviewer-a"},
				{"step": "reject_broad_shoulder_conflict", "subject_id": "stage:s02:broad", "verdict": "rejected", "rationale": "复核后决定改采窄窗口；系统保留冲突双方", "actor": "reviewer-b"},
				{"step": "undo_conflicting_rejection", "decision_id": "__decision_id_from_previous_step__", "verdict": "undo", "rationale": "撤销冲突解决以回到上一可见结论", "actor": "reviewer-b"},
			},
		},
		MachineAssertions: map[string]any{
			"required_situations":           []string{"atmosphere_delay", "balance_baseline_drift", "overlapping_peaks", "nonmonotonic_noise", "sampling_gap", "initial_mass_revision", "import_half_failure"},
			"rules":                         []string{"valve_time_is_not_reaction_time", "stages_may_overlap", "stage_losses_not_forced_to_100_percent", "gaps_do_not_interpolate", "gap_difference_not_a_stage", "nonmonotonic_mass_retained"},
			"stable_identity_export_import": "event, source, node and decision IDs are preserved with sequence order",
			"local_expiration":              "revising one raw point expires only chains containing that point",
		},
	}
}
