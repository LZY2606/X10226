// Package model defines the domain types of the TGA review system: layered
// raw inputs, append-only events, and derived values with provenance.
package model

import (
	"time"

	"tga/internal/qty"
)

// LayerKind identifies one of the six layered raw inputs.
type LayerKind string

const (
	LayerSamplePoints   LayerKind = "sample_points"   // 原始采样点
	LayerBalanceDrift   LayerKind = "balance_drift"   // 天平漂移校正
	LayerFurnaceProgram LayerKind = "furnace_program" // 炉程序
	LayerGasEvents      LayerKind = "gas_events"      // 气体阀事件
	LayerBlankCurve     LayerKind = "blank_curve"     // 空白曲线
	LayerInitialMass    LayerKind = "initial_mass"    // 样品初重
)

// LayerKinds lists all layer kinds in canonical order.
var LayerKinds = []LayerKind{
	LayerSamplePoints, LayerBalanceDrift, LayerFurnaceProgram,
	LayerGasEvents, LayerBlankCurve, LayerInitialMass,
}

// LayerNames maps kinds to human readable Chinese labels.
var LayerNames = map[LayerKind]string{
	LayerSamplePoints:   "原始采样点",
	LayerBalanceDrift:   "天平漂移校正",
	LayerFurnaceProgram: "炉程序",
	LayerGasEvents:      "气体阀事件",
	LayerBlankCurve:     "空白曲线",
	LayerInitialMass:    "样品初重",
}

// Diagnostic is attached to a raw row that could not be interpreted. The raw
// value stays in place inside the layer payload.
type Diagnostic struct {
	Row     int    `json:"row"`
	Field   string `json:"field"`
	Message string `json:"message"`
	Raw     string `json:"raw"`
}

// --- Layer payloads (raw representations preserved as submitted) ---

type SamplePoint struct {
	Time qty.Raw `json:"time"`
	Temp qty.Raw `json:"temp"`
	Mass qty.Raw `json:"mass"`
}

type SamplePointsPayload struct {
	Instrument string        `json:"instrument,omitempty"`
	Points     []SamplePoint `json:"points"`
}

type DriftPoint struct {
	Time  qty.Raw `json:"time"`
	Drift qty.Raw `json:"drift"` // mass units
}

type BalanceDriftPayload struct {
	Points []DriftPoint `json:"points"`
}

type FurnaceWaypoint struct {
	Time qty.Raw `json:"time"`
	Temp qty.Raw `json:"temp"`
}

type FurnaceProgramPayload struct {
	Waypoints []FurnaceWaypoint `json:"waypoints"`
}

type GasEvent struct {
	Time qty.Raw `json:"time"` // valve-time basis
	Gas  string  `json:"gas"`
	Flow qty.Raw `json:"flow"`
}

type GasEventsPayload struct {
	Events []GasEvent `json:"events"`
}

type BlankPoint struct {
	Time        qty.Raw `json:"time"`
	MassLossPct qty.Raw `json:"mass_loss_pct"`
}

type BlankCurvePayload struct {
	Points []BlankPoint `json:"points"`
}

type InitialMassPayload struct {
	Mass qty.Raw `json:"mass"`
	Basis string  `json:"basis,omitempty"` // e.g. "weighed-at-25C"
	Note  string  `json:"note,omitempty"`
}

// LayerVersion is an immutable version of one layer. Revisions create a new
// version pointing at its parent; submitted payloads are never overwritten.
type LayerVersion struct {
	ID          string          `json:"id"`
	Kind        LayerKind       `json:"kind"`
	Version     int             `json:"version"`
	ParentID    string          `json:"parent_id,omitempty"`
	EventID     string          `json:"event_id"`
	Payload     []byte          `json:"payload"` // canonical JSON of the kind payload
	Partial     bool            `json:"partial"`
	Diagnostics []Diagnostic    `json:"diagnostics,omitempty"`
}

// --- Events ---

const (
	EvCaseCreated         = "case.created"
	EvLayerImported       = "layer.imported"
	EvLayerRevised        = "layer.revised"
	EvParamsUpdated       = "params.updated"
	EvCandidatesGenerated = "candidates.generated"
	EvJudgmentRecorded    = "judgment.recorded"
	EvDecisionMade        = "decision.made"
	EvDecisionReverted    = "decision.reverted"
	EvCaseImported        = "case.imported"
)

// Event is an append-only record. IDs are stable and survive export/import.
type Event struct {
	Seq     int64     `json:"seq"`
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	Type    string    `json:"type"`
	Payload []byte    `json:"payload"`
}

// Judgment rules recorded as machine-readable events.
const (
	RuleAtmosphereDelay   = "atmosphere-delay"    // 阀切换时间 ≠ 样品反应时间
	RuleOverlapAllowed    = "overlap-allowed"     // 阶段可重叠
	RuleSumNotForced      = "sum-not-forced"      // 不强制总和为 100%
	RuleNoGapInterpolation = "no-gap-interpolation" // 数据缺口不跨越插值
	RuleNonMonotonicNoise = "nonmonotonic-noise"  // 非单调噪声
	RulePartialImport     = "partial-import"      // 导入半失败
)

// Judgment is the machine-readable rationale behind a domain decision.
type Judgment struct {
	Rule     string   `json:"rule"`
	Summary  string   `json:"summary"`
	Detail   string   `json:"detail,omitempty"`
	Affected []string `json:"affected,omitempty"` // candidate / derived ids
}

// --- Params ---

// Params are the reviewable analysis parameters. Each update is an event.
type Params struct {
	Version          int     `json:"version"`
	AtmosphereDelayS float64 `json:"atmosphere_delay_s"` // 气氛延迟
	TemperatureLagS  float64 `json:"temperature_lag_s"`  // 温度滞后
	GapThresholdS    float64 `json:"gap_threshold_s"`    // 采样缺口阈值
	SmoothWindow     int     `json:"smooth_window"`      // DTG 平滑窗口（点）
	MinStageMassPct  float64 `json:"min_stage_mass_pct"` // 最小阶段失重
	PeakThreshold    float64 `json:"peak_threshold"`     // 峰阈值（相对最大失重速率）
}

// DefaultParams returns the initial parameter set.
func DefaultParams() Params {
	return Params{
		Version:          1,
		AtmosphereDelayS: 20,
		TemperatureLagS:  10,
		GapThresholdS:    45,
		SmoothWindow:     5,
		MinStageMassPct:  1.0,
		PeakThreshold:    0.15,
	}
}

// --- Derived values ---

// AlgoVersion identifies the derivation algorithm version.
const AlgoVersion = "tga-derive/v1"

const (
	DerivedNormalizedCurve = "normalized_curve"
	DerivedDTG             = "dtg"
	DerivedStageCandidates = "stage_candidates"
	DerivedResidue         = "residue"
	DerivedAtmosphere      = "atmosphere"
)

// DerivedValue carries algorithm version, parameter fingerprint and direct
// sources so any change can expire exactly the dependent results.
type DerivedValue struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	AlgoVersion string   `json:"algo_version"`
	Fingerprint string   `json:"fingerprint"`
	SourceRefs  []string `json:"source_refs"` // layer version ids + params version
	EventID     string   `json:"event_id"`
	Payload     []byte   `json:"payload"`
}

// NormPoint is one point of the normalized TG curve.
type NormPoint struct {
	TS      float64 `json:"t_s"`
	TempC   float64 `json:"temp_c"`
	MassPct float64 `json:"mass_pct"`
	Segment int     `json:"segment"`
}

// Gap is a sampling gap; no interpolation ever crosses it.
type Gap struct {
	FromS float64 `json:"from_s"`
	ToS   float64 `json:"to_s"`
}

type NormalizedCurve struct {
	Points []NormPoint `json:"points"`
	Gaps   []Gap       `json:"gaps,omitempty"`
}

type DTGPoint struct {
	TS          float64 `json:"t_s"`
	TempC       float64 `json:"temp_c"`
	PctPerMin   float64 `json:"pct_per_min"`
	Segment     int     `json:"segment"`
}

type DTGCurve struct {
	Points []DTGPoint `json:"points"`
}

// StageCandidate is a first-class candidate; rejected candidates keep their
// reject reason instead of disappearing.
type StageCandidate struct {
	ID           string   `json:"id"`
	StartS       float64  `json:"start_s"`
	EndS         float64  `json:"end_s"`
	StartTempC   float64  `json:"start_temp_c"`
	EndTempC     float64  `json:"end_temp_c"`
	PeakTempC    float64  `json:"peak_temp_c"`
	MassLossPct  float64  `json:"mass_loss_pct"`
	ResiduePct   float64  `json:"residue_pct"` // mass% at stage end
	OverlapsWith []string `json:"overlaps_with,omitempty"`
	AlgoStatus   string   `json:"algo_status"` // "candidate" | "rejected"
	RejectReason string   `json:"reject_reason,omitempty"`
}

type StageCandidateSet struct {
	Candidates []StageCandidate `json:"candidates"`
}

// ChainStep is one step of a residue calculation chain.
type ChainStep struct {
	Label  string  `json:"label"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Source string  `json:"source"` // layer version id or "derived"
}

type Residue struct {
	MassPct float64     `json:"mass_pct"`
	Chain   []ChainStep `json:"chain"`
}

// AtmInterval assigns atmosphere to sample-time using delay + lag.
type AtmInterval struct {
	Gas       string  `json:"gas"`
	ValveS    float64 `json:"valve_s"`     // valve-time basis
	EffectiveS float64 `json:"effective_s"` // sample-time basis
	FlowMlMin float64 `json:"flow_ml_min"`
}

type AtmosphereAssignment struct {
	DelayS   float64      `json:"delay_s"`
	LagS     float64      `json:"lag_s"`
	Intervals []AtmInterval `json:"intervals"`
}

// --- Decisions ---

const (
	DecisionAccept = "accept"
	DecisionReject = "reject"
	DecisionAdjust = "adjust"
)

type DecisionPayload struct {
	CandidateID   string  `json:"candidate_id"`
	Action        string  `json:"action"` // accept | reject | adjust
	Reason        string  `json:"reason"`
	By            string  `json:"by"`
	AdjustParam   string  `json:"adjust_param,omitempty"`
	AdjustValue   float64 `json:"adjust_value,omitempty"`
	ConflictsWith string  `json:"conflicts_with,omitempty"` // event id of conflicting decision
}

type RevertPayload struct {
	TargetEventID string `json:"target_event_id"`
	Reason        string `json:"reason"`
	By            string `json:"by"`
}
