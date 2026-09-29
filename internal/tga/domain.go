// Package tga implements a reviewable thermogravimetric analysis (TGA) system:
// stage decomposition, atmosphere valve events, residue evidence chains,
// append-only source revisions, first-class candidates/unknowns and full
// replay of every historical decision.
package tga

import "encoding/json"

// AlgoVersion is bumped whenever the derived-layer algorithms change. Every
// derived value carries it so old conclusions can be reproduced exactly.
const AlgoVersion = "tga-algo-1.0.0"

// StreamKind identifies one of the six layered source materials.
type StreamKind string

const (
	StreamSamplePoints  StreamKind = "sample_points"  // 原始采样点 mass/temp/furnace-time
	StreamBalanceDrift  StreamKind = "balance_drift"  // 天平漂移校正
	StreamFurnaceProg   StreamKind = "furnace_program" // 炉程序
	StreamGasValve      StreamKind = "gas_valve"      // 气体阀事件
	StreamBlankCurve    StreamKind = "blank_curve"    // 空白曲线
	StreamInitialMass   StreamKind = "initial_mass"   // 样品初重分层
	StreamParams        StreamKind = "analysis_params" // 算法参数修订
)

// LayerOrder returns the canonical review order of the six source layers.
func LayerOrder() []StreamKind {
	return []StreamKind{
		StreamSamplePoints,
		StreamBalanceDrift,
		StreamFurnaceProg,
		StreamGasValve,
		StreamBlankCurve,
		StreamInitialMass,
	}
}

func (k StreamKind) Label() string {
	switch k {
	case StreamSamplePoints:
		return "原始采样点"
	case StreamBalanceDrift:
		return "天平漂移校正"
	case StreamFurnaceProg:
		return "炉程序"
	case StreamGasValve:
		return "气体阀事件"
	case StreamBlankCurve:
		return "空白曲线"
	case StreamInitialMass:
		return "样品初重"
	case StreamParams:
		return "分析参数"
	}
	return string(k)
}

// Quantity keeps the original textual representation of a measurement. The
// raw value is never discarded; the canonical numeric value (in the canonical
// unit) lives in the import layer, and an unconvertible value stays in place
// with a diagnostic instead of being dropped for plotting.
type Quantity struct {
	Raw        string  `json:"raw"`
	Value      float64 `json:"value,omitempty"`
	Unit       string  `json:"unit"`                 // canonical unit code
	SourceUnit string  `json:"source_unit,omitempty"` // unit as imported
	Diagnostic string  `json:"diagnostic,omitempty"` // non-empty => value unusable
}

// Valid reports whether the quantity was converted to a canonical value.
func (q Quantity) Valid() bool { return q.Diagnostic == "" }

// Typed imported rows -------------------------------------------------------

// SamplePoint is one raw TGA sample row.
type SamplePoint struct {
	I      int      `json:"i"`
	Time   Quantity `json:"time"` // canonical s
	Temp   Quantity `json:"temp"` // canonical °C
	Mass   Quantity `json:"mass"` // canonical mg
}

// DriftPoint is one balance baseline drift correction point.
type DriftPoint struct {
	I    int      `json:"i"`
	Time Quantity `json:"time"`
	Mass Quantity `json:"mass"` // signed offset, canonical mg
}

// FurnaceSegment is one programmed furnace ramp/hold segment.
type FurnaceSegment struct {
	I         int      `json:"i"`
	T0        Quantity `json:"t0"`
	T1        Quantity `json:"t1"`
	StartTemp Quantity `json:"start_temp"`
	EndTemp   Quantity `json:"end_temp"`
	Rate      Quantity `json:"rate"` // canonical °C/min, 0 => hold
}

// GasEvent is one atmosphere valve switch.
type GasEvent struct {
	I      int      `json:"i"`
	Time   Quantity `json:"time"`
	From   string   `json:"from"`
	To     string   `json:"to"`
	Flow   Quantity `json:"flow"` // canonical mL/min
}

// BlankPoint is one blank/baseline curve point.
type BlankPoint struct {
	I    int      `json:"i"`
	Time Quantity `json:"time"`
	Temp Quantity `json:"temp"`
	Mass Quantity `json:"mass"`
}

// InitialMass is a layered sample initial-mass statement.
type InitialMass struct {
	Basis string   `json:"basis"` // e.g. as-loaded / buoyancy-corrected
	Mass  Quantity `json:"mass"`
	Note  string   `json:"note,omitempty"`
}

// RowDiagnostic records a non-fatal per-row conversion problem.
type RowDiagnostic struct {
	Row     int    `json:"row"`
	Field   string `json:"field"`
	Message string `json:"message"`
	Raw     string `json:"raw,omitempty"`
}

// ParsedBlob is the typed view of one imported source blob. Rows that contain
// unconvertible values are retained; their unusable fields carry diagnostics
// and are also collected in Diagnostics.
type ParsedBlob struct {
	Kind        StreamKind      `json:"kind"`
	Sample      []SamplePoint   `json:"sample,omitempty"`
	Drift       []DriftPoint    `json:"drift,omitempty"`
	Furnace     []FurnaceSegment `json:"furnace,omitempty"`
	Gas         []GasEvent      `json:"gas,omitempty"`
	Blank       []BlankPoint    `json:"blank,omitempty"`
	Initial     *InitialMass    `json:"initial,omitempty"`
	Diagnostics []RowDiagnostic `json:"diagnostics,omitempty"`
}

// Version status for a submitted source version.
type VersionStatus string

const (
	VersionOK      VersionStatus = "ok"
	VersionPartial VersionStatus = "partial" // rows accepted, some diagnostics
	VersionRejected VersionStatus = "rejected" // fatal format error, nothing stored
)

// SourceVersion is an immutable submission for one stream. Submissions are
// never overwritten: each new version links its parent.
type SourceVersion struct {
	ID        string          `json:"id"`
	Project   string          `json:"project"`
	Kind      StreamKind      `json:"kind"`
	ParentID  string          `json:"parent_id,omitempty"`
	Seq       int             `json:"seq"`
	MediaType string          `json:"media_type"`
	Raw       string          `json:"raw"`
	SHA256    string          `json:"sha256"`
	Status    VersionStatus   `json:"status"`
	Parsed    *ParsedBlob     `json:"parsed,omitempty"`
	Era       int64           `json:"era"` // event that created this version
	ImportRef string          `json:"import_ref,omitempty"` // case/import step provenance
}

// Params is the parameter set carried by every derived result via its
// fingerprint. Revisions are versioned like any other source.
type Params struct {
	DelayReactionMinSec float64 `json:"delay_reaction_min_s"` // valve -> earliest observed reaction
	DelayReactionMaxSec float64 `json:"delay_reaction_max_s"` // valve -> latest observed reaction
	TempLagMinC         float64 `json:"temp_lag_min_c"`
	TempLagMaxC         float64 `json:"temp_lag_max_c"`
	NoiseThresholdPct   float64 `json:"noise_threshold_pct"`  // |upward ratio jump| classified as noise
	ProminencePctPerMin float64 `json:"prominence_pct_per_min"` // DTG peak minimum prominence
	MinPeakDistanceSec  float64 `json:"min_peak_distance_s"`
	GapFactor           float64 `json:"gap_factor"` // dt > factor*median dt => sampling gap
	Note                string  `json:"note,omitempty"`
}

// DefaultParams returns the v1 baseline analysis parameters.
func DefaultParams() Params {
	return Params{
		DelayReactionMinSec: 5,
		DelayReactionMaxSec: 25,
		TempLagMinC:         2,
		TempLagMaxC:         12,
		NoiseThresholdPct:   0.05,
		ProminencePctPerMin: 0.05,
		MinPeakDistanceSec:  12,
		GapFactor:           3,
		Note:                "v1 baseline parameters",
	}
}

func (p Params) Fingerprint() string {
	b, _ := json.Marshal(p)
	return "pfp-" + shortHash(b)
}

// Decision kinds ------------------------------------------------------------

type DecisionKind string

const (
	DecAcceptCand  DecisionKind = "accept_candidate"
	DecRejectCand  DecisionKind = "reject_candidate"
	DecAdjustParam DecisionKind = "adjust_parameter"
	DecResolve     DecisionKind = "resolve_conflict"
	DecUndo        DecisionKind = "undo"
)

type CandidateVerdict string

const (
	VerdictProposed CandidateVerdict = "proposed"
	VerdictAccepted CandidateVerdict = "accepted"
	VerdictRejected CandidateVerdict = "rejected"
	VerdictConflict CandidateVerdict = "conflict"
	VerdictUnknown  CandidateVerdict = "unknown" // cannot uniquely decide
)

// Decision is an append-only human action recorded in the project history.
type Decision struct {
	ID          string          `json:"id"`
	Kind        DecisionKind    `json:"kind"`
	Era         int64           `json:"era"`
	TargetKey   string          `json:"target_key,omitempty"` // e.g. candidate stable id
	Payload     json.RawMessage `json:"payload,omitempty"`
	Reason      string          `json:"reason,omitempty"`
	UndoesEra   int64           `json:"undoes_era,omitempty"`
	ParentEvent string          `json:"parent_event"`
	ClockTime   string          `json:"clock_time"`
}

// ChainStep is one step of an expandable residue (or any value) calculation
// chain. Every input reference points back to a stable display identity.
type ChainStep struct {
	Order  int            `json:"order"`
	RuleID string         `json:"rule_id"`
	Label  string         `json:"label"`
	Formula string        `json:"formula,omitempty"`
	Inputs []ChainInput   `json:"inputs,omitempty"`
	Output ChainValue     `json:"output"`
	Note   string         `json:"note,omitempty"`
}

// ChainInput references a value by its stable display identity.
type ChainInput struct {
	Ref   string     `json:"ref"`
	Label string     `json:"label"`
	Value ChainValue `json:"value"`
}

// ChainValue is a displayed number with unit/coordinate basis and status.
type ChainValue struct {
	ID         string  `json:"id"`
	Value      *float64 `json:"value"` // nil => unknown/uncomputable
	Unit       string  `json:"unit,omitempty"`
	Basis      string  `json:"basis,omitempty"`
	Status     string  `json:"status"` // ok | unknown | stale | rejected
	Diagnostic string  `json:"diagnostic,omitempty"`
}
