// Package sources defines the six stratified, immutable source layers of a TGA
// project. Submissions are append-only: a revision creates a new version with a
// parent pointer instead of overwriting the original.
package sources

import "tgasys/internal/domain/units"

type Layer string

const (
	LayerRaw     Layer = "raw_points"      // 原始采样点
	LayerDrift   Layer = "balance_drift"   // 天平漂移校正
	LayerFurnace Layer = "furnace_program" // 炉程序
	LayerGas     Layer = "gas_valve"       // 气体阀事件
	LayerBlank   Layer = "blank_curve"     // 空白曲线
	LayerInitial Layer = "initial_mass"    // 样品初重
)

// AllLayers lists layers in canonical import/ordering order.
var AllLayers = []Layer{LayerRaw, LayerDrift, LayerFurnace, LayerGas, LayerBlank, LayerInitial}

func (l Layer) Label() string {
	switch l {
	case LayerRaw:
		return "原始采样点"
	case LayerDrift:
		return "天平漂移校正"
	case LayerFurnace:
		return "炉程序"
	case LayerGas:
		return "气体阀事件"
	case LayerBlank:
		return "空白曲线"
	case LayerInitial:
		return "样品初重"
	}
	return string(l)
}

// Diag is a non-fatal parse/normalization diagnostic. A value that cannot be
// converted stays at its original position and carries one of these.
type Diag struct {
	Scope    string `json:"scope"` // e.g. "row:7", "field:mass", "header"
	Severity string `json:"severity"`
	Message  string `json:"message"`
	RawLine  string `json:"raw_line,omitempty"`
}

// Submission is an immutable record of one imported source blob.
type Submission struct {
	ID         string  `json:"id"`
	Project    string  `json:"project"`
	Layer      Layer   `json:"layer"`
	Version    int     `json:"version"`
	ParentID   string  `json:"parent_id,omitempty"`
	ReceivedAt string  `json:"received_at"` // virtual clock, RFC3339Nano UTC
	SourceFile string  `json:"source_file"`
	RawText    string  `json:"raw_text"`
	Sha        string  `json:"sha"`
	Accepted   bool    `json:"accepted"` // false => half-failed, kept for inspection
	Diags      []Diag  `json:"diags,omitempty"`
	Ordinal    int     `json:"ordinal"` // import ordinal, preserves order
}

// SamplePoint is one raw acquisition row. Time/mass/temp keep original tokens.
type SamplePoint struct {
	Index    int            `json:"index"`
	RowNo    int            `json:"row_no"`
	Time     units.Quantity `json:"time"`
	Mass     units.Quantity `json:"mass"`
	Temp     units.Quantity `json:"temp"`
	TimeRaw  string         `json:"time_raw"`
	MassRaw  string         `json:"mass_raw"`
	TempRaw  string         `json:"temp_raw"`
	Usable   bool           `json:"usable"`
	GapAfter bool           `json:"gap_after,omitempty"` // declared sampling gap after this point
	Diags    []Diag         `json:"diags,omitempty"`
}

// RawSeries is the original sampling layer.
type RawSeries struct {
	SubmissionID string        `json:"submission_id"`
	Instrument   string        `json:"instrument,omitempty"`
	TimeBasis    string        `json:"time_basis"` // e.g. "furnace_clock:seconds"
	Points       []SamplePoint `json:"points"`
	Gaps         []Gap         `json:"gaps,omitempty"`
	Diags        []Diag        `json:"diags,omitempty"`
}

// Gap is a declared acquisition hole. Derived curves must NOT interpolate
// across it.
type Gap struct {
	FromIndex int    `json:"from_index"` // last usable point before hole
	ToIndex   int    `json:"to_index"`   // first usable point after hole
	FromTimeS float64 `json:"from_time_s"`
	ToTimeS   float64 `json:"to_time_s"`
	Reason    string `json:"reason,omitempty"`
}

// DriftModel is the balance baseline-drift correction (often derived from a
// blank run). Piecewise linear anchors in (time_s, drift_mg).
type DriftModel struct {
	SubmissionID string     `json:"submission_id"`
	Method       string     `json:"method"` // "blank_linear_anchors_v1"
	Anchors      []XY       `json:"anchors"`
	SlopeMgPerS  float64    `json:"slope_mg_per_s,omitempty"`
	Diags        []Diag     `json:"diags,omitempty"`
}

type XY struct {
	T float64 `json:"t_s"`
	V float64 `json:"v_mg"`
}

// FurnaceSegment is one ramp or hold of the furnace program.
type FurnaceSegment struct {
	Index    int            `json:"index"`
	Kind     string         `json:"kind"` // "ramp" | "hold"
	StartT   units.Quantity `json:"start_time"`
	EndT     units.Quantity `json:"end_time"`
	StartTemp units.Quantity `json:"start_temp"`
	EndTemp  units.Quantity `json:"end_temp"`
	Rate     units.Quantity `json:"rate"`
}

type FurnaceProgram struct {
	SubmissionID string          `json:"submission_id"`
	Segments     []FurnaceSegment `json:"segments"`
	Diags        []Diag          `json:"diags,omitempty"`
}

// GasValve is one gas valve switching event at the furnace inlet.
type GasValve struct {
	Index  int            `json:"index"`
	Time   units.Quantity `json:"time"`
	FromGas string        `json:"from_gas"`
	ToGas   string        `json:"to_gas"`
	Note   string         `json:"note,omitempty"`
}

type GasTimeline struct {
	SubmissionID string     `json:"submission_id"`
	Valves       []GasValve `json:"valves"`
	Diags        []Diag     `json:"diags,omitempty"`
}

// BlankPoint and BlankCurve record the blank (empty pan) baseline run.
type BlankPoint struct {
	Index int            `json:"index"`
	Time  units.Quantity `json:"time"`
	Mass  units.Quantity `json:"mass"`
	Temp  units.Quantity `json:"temp"`
}

type BlankCurve struct {
	SubmissionID string      `json:"submission_id"`
	PanID        string      `json:"pan_id,omitempty"`
	Points       []BlankPoint `json:"points"`
	Diags        []Diag      `json:"diags,omitempty"`
}

// InitialMass records the starting sample mass with its weighing basis.
type InitialMass struct {
	SubmissionID string         `json:"submission_id"`
	Mass         units.Quantity `json:"mass"`
	Basis        string         `json:"basis"` // "tare_settled" | "auto_zero" | "revised"
	WeighedAt    string         `json:"weighed_at"`
	Note         string         `json:"note,omitempty"`
	Diags        []Diag         `json:"diags,omitempty"`
}
