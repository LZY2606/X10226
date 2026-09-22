// Package model defines the core entities of the TGA review system:
// layered raw inputs, derived results with provenance, human decisions
// and the machine-readable event log.
package model

import (
	"encoding/json"
	"time"
)

// LayerKind identifies one of the stratified raw input layers.
type LayerKind string

const (
	LayerRawPoints      LayerKind = "raw_points"       // 原始采样点
	LayerDriftCorrect   LayerKind = "drift_correction" // 天平漂移校正
	LayerFurnaceProgram LayerKind = "furnace_program"  // 炉程序
	LayerGasValveEvents LayerKind = "gas_valve_events" // 气体阀事件
	LayerBlankCurve     LayerKind = "blank_curve"      // 空白曲线
	LayerInitialWeight  LayerKind = "initial_weight"   // 样品初重
	LayerAnalysisParams LayerKind = "analysis_params"  // 分析参数（修订走同一版本机制）
)

// LayerKinds lists the input layers in canonical display/import order.
var LayerKinds = []LayerKind{
	LayerRawPoints, LayerDriftCorrect, LayerFurnaceProgram,
	LayerGasValveEvents, LayerBlankCurve, LayerInitialWeight,
	LayerAnalysisParams,
}

// Result kinds produced by the pipeline.
const (
	ResultCorrectedTG = "corrected_tg"
	ResultDTG         = "dtg"
	ResultStageSet    = "stage_candidates"
	ResultResidue     = "residue_rate"
)

// Result statuses. Staleness is also derivable from source versions;
// the stored status is a cache for the current view.
const (
	StatusCurrent = "current"
	StatusStale   = "stale"
	StatusError   = "error"
	StatusUnknown = "unknown"
)

// Decision actions.
const (
	DecideAccept = "accept"
	DecideReject = "reject"
	DecideAdjust = "adjust"
)

// Event types written to the machine-readable log.
const (
	EvLayerSubmitted     = "layer_submitted"
	EvLayerRevised       = "layer_revised"
	EvResultsComputed    = "results_computed"
	EvResultsInvalidated = "results_invalidated"
	EvJudgment           = "judgment" // domain judgment, machine readable
	EvDecision           = "decision_made"
	EvImportPartial      = "import_partial_failure"
	EvImport             = "import_completed"
	EvExport             = "export_created"
)

// Judgment rule identifiers (machine readable payloads of EvJudgment).
const (
	RuleAtmosphereDelay     = "atmosphere_delay_lag"      // 阀切换时间 != 样品反应时间
	RuleStageOverlap        = "stages_may_overlap"        // 阶段可重叠
	RuleSumNotForced        = "sum_not_forced_to_100"     // 不强制总和为 100%
	RuleGapNoInterpolate    = "gap_no_interpolation"      // 数据缺口不跨越插值
	RuleAmbiguousCandidates = "ambiguous_candidates_kept" // 无法唯一决定时保留全部合法候选
)

// Layer is a stratified input container; versions hang off it.
type Layer struct {
	ID         string    `json:"id"`
	Kind       LayerKind `json:"kind"`
	CreatedSeq int64     `json:"created_seq"`
}

// LayerVersion is an immutable version of a layer. Raw representation is
// preserved verbatim in Payload; normalization happens only in derived
// results. Unconvertible values stay in place with a Diagnostic.
type LayerVersion struct {
	ID         string            `json:"id"`
	LayerID    string            `json:"layer_id"`
	Kind       LayerKind         `json:"kind"`
	ParentID   string            `json:"parent_id,omitempty"`
	Seq        int64             `json:"seq"`
	Payload    json.RawMessage   `json:"payload"`
	Units      map[string]string `json:"units,omitempty"`
	Diagnostic string            `json:"diagnostic,omitempty"`
}

// Result is a derived value with algorithm version, parameter fingerprint
// and direct sources. Candidates, rejection reasons, errors and unknown
// states are first-class payload data.
type Result struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	AlgoVersion string          `json:"algo_version"`
	ParamFP     string          `json:"param_fp"`
	Sources     []string        `json:"sources"` // layer version ids
	Seq         int64           `json:"seq"`
	Status      string          `json:"status"`
	Payload     json.RawMessage `json:"payload"`
	Diagnostic  string          `json:"diagnostic,omitempty"`
}

// Decision is a human ruling (accept / reject / adjust) on a candidate or
// result. Conflicting decisions are all retained; ParentID links a
// decision to the one it disputes or supersedes.
type Decision struct {
	ID        string `json:"id"`
	Seq       int64  `json:"seq"`
	TargetID  string `json:"target_id"`
	Action    string `json:"action"`
	Params    string `json:"params,omitempty"`
	Rationale string `json:"rationale,omitempty"`
	ParentID  string `json:"parent_id,omitempty"`
}

// Event is one entry of the append-only, machine-readable log.
type Event struct {
	Seq     int64           `json:"seq"`
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	At      time.Time       `json:"at"`
}
