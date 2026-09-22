// Package domain defines the immutable domain model for the TGA stage
// decomposition / atmosphere-event review system.
//
// Layering:
//
//	raw text   -> layer records (typed, with original representation kept)
//	parameters -> parameter set versions (fingerprinted)
//	derivations-> derived series / candidates / residues (with provenance)
//	decisions  -> human verdicts recorded as append-only events
//
// Every stored object has a stable identity. Layer and parameter records are
// content addressed; derived records are addressed by (algorithm version,
// parameter id, direct sources) so the same inputs always yield the same id,
// including after export -> delete -> import.
package domain

import "time"

// AlgorithmVersion is the version of the deterministic derivation pipeline.
// Bump on any change to derivation semantics.
const AlgorithmVersion = "tga-derive-1.0.0"

// Unit systems used by TGA imports. Conversion happens only at the typed
// layer; the original textual representation is always retained.
type UnitKind string

const (
	UnitMG     UnitKind = "mg"
	UnitC      UnitKind = "degC"
	UnitK      UnitKind = "K"
	UnitMin    UnitKind = "min"
	UnitS      UnitKind = "s"
	UnitMLMin  UnitKind = "mL/min"
	UnitPercent UnitKind = "%"
	UnitUnknown UnitKind = ""
)

// Measured is a value that keeps both the raw token and the typed
// interpretation. Unconvertible values stay in place: Value is NaN,
// ConvertError is set and a diagnostic is emitted.
type Measured struct {
	Raw          string   `json:"raw"`
	Value        *float64 `json:"value,omitempty"`
	Unit         UnitKind `json:"unit"`
	ConvertError string   `json:"convert_error,omitempty"`
}

// Layer kinds, stored separately ("分层保存").
type LayerKind string

const (
	LayerSeries   LayerKind = "series"   // 原始采样点
	LayerDrift    LayerKind = "drift"    // 天平漂移校正
	LayerFurnace  LayerKind = "furnace"  // 炉程序
	LayerValve    LayerKind = "valve"    // 气体阀事件
	LayerBlank    LayerKind = "blank"    // 空白曲线
	LayerInitialM LayerKind = "initial_mass" // 样品初重
)

// Point is one raw sampling point (or one blank point).
type Point struct {
	ID    string   `json:"id"`
	Index int      `json:"index"`
	T     Measured `json:"t"` // furnace time
	Temp  Measured `json:"temp"`
	Mass  Measured `json:"mass"`
}

// Segment is one furnace program step.
type Segment struct {
	ID       string   `json:"id"`
	Index    int      `json:"index"`
	Mode     string   `json:"mode"` // hold | ramp
	FromTemp *float64 `json:"from_temp_c"`
	ToTemp   *float64 `json:"to_temp_c"`
	RateCmin *float64 `json:"rate_c_per_min,omitempty"`
	StartMin float64  `json:"start_min"`
	EndMin   float64  `json:"end_min"`
}

// ValveEvent is a gas valve switch. The recorded time is the valve switch
// time; the effective reaction-zone time is a derivation that applies the
// atmosphere delay and thermal lag.
type ValveEvent struct {
	ID       string  `json:"id"`
	Index    int     `json:"index"`
	AtMin    float64 `json:"at_min"`
	Gas      string  `json:"gas"`        // N2 | air | O2 | ...
	Action   string  `json:"action"`     // switch | on | off
	FlowMLM  float64 `json:"flow_ml_min,omitempty"`
	RawLine  string  `json:"raw_line,omitempty"`
}

// InitialMass is a sample initial-mass statement. Revisions supersede prior
// ones through parent linkage.
type InitialMass struct {
	ID       string  `json:"id"`
	M        float64 `json:"mg"`
	Revises  string  `json:"revises_id,omitempty"`
	RawLine  string  `json:"raw_line,omitempty"`
}

// DriftModel is a balance baseline drift correction model (linear in furnace
// time) together with the accepted residual diagnostics.
type DriftModel struct {
	ID        string         `json:"id"`
	SlopeMGPM float64        `json:"slope_mg_per_min"`
	OffsetMG  float64        `json:"offset_mg"`
	RMSEMG    float64        `json:"rmse_mg,omitempty"`
	SourceBlank string       `json:"source_blank_id,omitempty"`
	Params    string         `json:"raw_line,omitempty"`
}

// LayerRecord is one immutable submitted input artifact.
type LayerRecord struct {
	ID        string          `json:"id"`
	Kind      LayerKind       `json:"kind"`
	SourceFile string         `json:"source_file"`
	RawText   string          `json:"raw_text"`
	SubmittedAt time.Time     `json:"submitted_at"`
	// Typed payloads, exactly one is populated.
	Points      []Point       `json:"points,omitempty"`
	Segments    []Segment     `json:"segments,omitempty"`
	Valves      []ValveEvent  `json:"valves,omitempty"`
	Drift       *DriftModel   `json:"drift,omitempty"`
	InitialMass *InitialMass  `json:"initial_mass,omitempty"`
	Diagnostics []Diagnostic  `json:"diagnostics,omitempty"`
	// PartiallyAccepted is true when some rows were kept and some rejected.
	PartiallyAccepted bool `json:"partially_accepted,omitempty"`
}

// Severity levels for diagnostics.
type Severity string

const (
	SevError   Severity = "error"   // row / file rejected
	SevWarning Severity = "warning" // kept, but flagged
	SevInfo    Severity = "info"
)

// Diagnostic keeps an unconvertible / malformed value at its original place
// instead of dropping it for plotting.
type Diagnostic struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Line     int      `json:"line,omitempty"`
	Ref      string   `json:"ref,omitempty"` // stable ref, e.g. point id
	Raw      string   `json:"raw,omitempty"`
}

// Params is a versioned derivation parameter set.
type Params struct {
	ID      string         `json:"id"`
	Version string         `json:"version"` // human version e.g. v1/v2
	Parent  string         `json:"parent_id,omitempty"`
	Values  ParamsValues   `json:"values"`
	Note    string         `json:"note,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
}

// ParamsValues carries all tunable parameters plus the fixed seed used by the
// deterministic pipeline.
type ParamsValues struct {
	Seed              int64   `json:"seed"`
	GasDelayMin       float64 `json:"gas_delay_min"`
	TempLagMin        float64 `json:"temp_lag_min"`
	BlankShiftMin     float64 `json:"blank_shift_min"`
	SmoothWindowN     int     `json:"smooth_window_n"`
	ProminenceMGPM    float64 `json:"prominence_mg_per_min"`
	MergeWindowMin    float64 `json:"merge_window_min"`
	MinStageWidthMin  float64 `json:"min_stage_width_min"`
	GapFactor         float64 `json:"gap_factor"`
	NonMonotonicRel   float64 `json:"nonmonotonic_rel"`
	PlateauTailN      int     `json:"plateau_tail_n"`
	ResidueTolPercent float64 `json:"residue_sum_tol_percent"`
}

// DefaultParams returns the v1 parameter set.
func DefaultParams() ParamsValues {
	return ParamsValues{
		Seed:              20260923,
		GasDelayMin:       0.0,
		TempLagMin:        0.0,
		BlankShiftMin:     0.0,
		SmoothWindowN:     5,
		ProminenceMGPM:    0.02,
		MergeWindowMin:    6.0,
		MinStageWidthMin:  2.0,
		GapFactor:         10.0,
		NonMonotonicRel:   0.002,
		PlateauTailN:      10,
		ResidueTolPercent: 0.5,
	}
}
