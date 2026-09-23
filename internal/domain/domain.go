// Package domain defines the immutable core types of the TGA review system.
//
// Conventions:
//   - raw layers keep the original textual representation (RawValue.Text);
//   - only the derived layer carries normalized SI-ish working units;
//   - values that cannot be converted stay in place with a diagnostic and are
//     never dropped just to make plotting possible.
package domain

// Layer identifiers. Raw submissions are stored in these six separate layers.
const (
	LayerSamples = "samples" // 原始采样点
	LayerDrift   = "drift"   // 天平漂移校正
	LayerFurnace = "furnace" // 炉程序
	LayerValve   = "valve"   // 气体阀事件
	LayerBlank   = "blank"   // 空白曲线
	LayerInitial = "initial" // 样品初重
)

// Layers is the canonical order used everywhere in the UI and exports.
var Layers = []string{
	LayerSamples, LayerDrift, LayerFurnace,
	LayerValve, LayerBlank, LayerInitial,
}

// LayerTitle maps layers to their Chinese narrative names.
var LayerTitle = map[string]string{
	LayerSamples: "原始采样点",
	LayerDrift:   "天平漂移校正",
	LayerFurnace: "炉程序",
	LayerValve:   "气体阀事件",
	LayerBlank:   "空白曲线",
	LayerInitial: "样品初重",
}

// Import statuses.
const (
	ImportComplete = "complete"
	ImportPartial  = "partial" // 导入半失败：部分行可用，部分行带诊断
	ImportFailed   = "failed"  // 该层没有任何可用行
)

// Verdict / candidate statuses.
const (
	VerdictAccepted = "accepted"
	VerdictRejected = "rejected"

	StatusProposed   = "proposed"
	StatusAccepted   = "accepted"
	StatusRejected   = "rejected"
	StatusAdjusted   = "adjusted"
	StatusSuperseded = "superseded"
	StatusStale      = "stale"
	StatusCurrent    = "current"
)

// RawValue is a field exactly as typed by the operator, plus an optional
// normalized parse. Value == nil means the number could not be converted;
// the original text and diagnostic are preserved in place.
type RawValue struct {
	Text       string   `json:"text"`
	Value      *float64 `json:"value,omitempty"`
	Unit       string   `json:"unit,omitempty"`
	OK         bool     `json:"ok"`
	Diagnostic string   `json:"diagnostic,omitempty"`
}

// Diagnostic is attached to a layer or to an individual raw line.
type Diagnostic struct {
	Line     int    `json:"line,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // error | warning
}

// RawEntry is one line in one raw layer. Bad lines are kept with Kind=="error".
type RawEntry struct {
	ID         string     `json:"id"`
	Line       int        `json:"line"`
	Kind       string     `json:"kind"` // sample|gap|drift|ramp|hold|valve|blank|initial|error
	Time       *RawValue  `json:"time,omitempty"`
	Mass       *RawValue  `json:"mass,omitempty"`
	Temp       *RawValue  `json:"temp,omitempty"`
	ToTemp     *RawValue  `json:"to_temp,omitempty"`
	Rate       *RawValue  `json:"rate,omitempty"`
	Gas        string     `json:"gas,omitempty"`
	Name       string     `json:"name,omitempty"`
	Text       string     `json:"text,omitempty"`
	Diagnostic string     `json:"diagnostic,omitempty"`
}

// RawLayer is one parsed section belonging to one of the six layers.
type RawLayer struct {
	Layer       string       `json:"layer"`
	RawSection  string       `json:"raw_section"`
	Entries     []RawEntry   `json:"entries"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Status      string       `json:"status"`
}

// Bundle is the result of parsing one submission (possibly all six layers).
type Bundle struct {
	FormatVersion string              `json:"format_version"`
	Layers        map[string]*RawLayer `json:"layers"`
	Diagnostics   []Diagnostic        `json:"diagnostics"`
}

// Quantity is a normalized derived value. Unit and coordinate basis travel
// with every number. Unknown is used when the value cannot be computed.
type Quantity struct {
	Value   *float64 `json:"value,omitempty"`
	Unit    string   `json:"unit,omitempty"`
	Basis   string   `json:"basis,omitempty"`
	RawText string   `json:"raw_text,omitempty"`
	Unknown string   `json:"unknown,omitempty"` // reason when Value == nil
}

// Params controls the derivation. A revision produces a new param version.
type Params struct {
	ValveLagSec         float64 `json:"valve_lag_sec"`
	TempLagThresholdC   float64 `json:"temp_lag_threshold_c"`
	PeakMinMgPerMin     float64 `json:"peak_min_mg_per_min"`
	PeakProminence      float64 `json:"peak_prominence_mg_per_min"`
	MergeWindowMin      float64 `json:"merge_window_min"`
	NoiseWindowPoints   int     `json:"noise_window_points"`
	NoiseMaxSignFlips   int     `json:"noise_max_sign_flips"`
	BlankModel          string  `json:"blank_model"`
	DriftModel          string  `json:"drift_model"`
}

// DefaultParams is algorithm version tga-derive-v1's baseline parameter set.
func DefaultParams() Params {
	return Params{
		ValveLagSec:       45,
		TempLagThresholdC: 8,
		PeakMinMgPerMin:   0.05,
		PeakProminence:    0.03,
		MergeWindowMin:    2.0,
		NoiseWindowPoints: 7,
		NoiseMaxSignFlips: 4,
		BlankModel:        "linear",
		DriftModel:        "piecewise_linear",
	}
}
