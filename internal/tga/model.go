package tga

import "encoding/json"

// Layer identifies the stratified raw input layers. Raw inputs are committed
// immutably; revisions create a new document with a ParentDocID.
type Layer string

const (
	LayerSamples   Layer = "samples"    // original sampled points (t,T,m)
	LayerDrift     Layer = "drift"      // balance drift / buoyancy correction
	LayerProgram   Layer = "program"    // furnace program segments
	LayerValve     Layer = "valve"      // gas valve events
	LayerBlank     Layer = "blank"      // blank curve
	LayerInitialM  Layer = "initial_mass" // sample initial mass
)

// Layers is the canonical review order used by the UI.
var Layers = []Layer{LayerSamples, LayerDrift, LayerProgram, LayerValve, LayerBlank, LayerInitialM}

func (l Layer) Label() string {
	switch l {
	case LayerSamples:
		return "原始采样点"
	case LayerDrift:
		return "天平漂移校正"
	case LayerProgram:
		return "炉程序"
	case LayerValve:
		return "气体阀事件"
	case LayerBlank:
		return "空白曲线"
	case LayerInitialM:
		return "样品初重"
	}
	return string(l)
}

// Row is one original CSV row: every parsed cell keeps its raw token.
type Row struct {
	Line   int               `json:"line"`
	Cells  map[string]Quantity `json:"cells"`
	Raw    []string          `json:"raw"`
	Header []string          `json:"header"`
}

// Diagnostic is an import/parse problem. Fatal=true means the whole document
// was rejected; otherwise the row (or even cell) is retained in place.
type Diagnostic struct {
	Severity string `json:"severity"` // fatal | warning
	Line     int    `json:"line,omitempty"`
	Column   string `json:"column,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// Document is one immutable, layer-stratified raw input.
type Document struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"project_id"`
	Layer       Layer        `json:"layer"`
	Version     int          `json:"version"`
	ParentDocID string       `json:"parent_doc_id,omitempty"`
	Filename    string       `json:"filename"`
	RawText     string       `json:"raw_text"` // exact bytes as submitted
	Header      []string     `json:"header"`
	Rows        []Row        `json:"rows"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	// Accepted=false marks a half-failed import: rows/diagnostics are retained
	// but the document never feeds derivation.
	Accepted bool `json:"accepted"`
}

// Event is the append-only history. Nothing is ever mutated; a later event
// may supersede an earlier decision or document version.
type Event struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"project_id"`
	Seq          int64           `json:"seq"`
	Type         string          `json:"type"`
	OccurredAt   string          `json:"occurred_at"` // virtual clock, RFC3339
	Actor        string          `json:"actor"`
	Payload      json.RawMessage `json:"payload"`
	SupersedesID string          `json:"supersedes_id,omitempty"`
}

// Event types.
const (
	EvProjectCreated = "project_created"
	EvDocCommitted   = "document_committed"
	EvDocRejected    = "document_rejected"
	EvParamsRevised  = "params_revised"
	EvDecision       = "decision"
)

// Params are the analysis rule parameters. Every revision gets a parent id and
// a content fingerprint; derived results record the exact fingerprint used.
type Params struct {
	ID                string  `json:"id"`
	Version           int     `json:"version"`
	ParentID          string  `json:"parent_id,omitempty"`
	AtmosphereDelayS  float64 `json:"atmosphere_delay_s"`
	ThermalLagS       float64 `json:"thermal_lag_s"`
	MinPeakProminence float64 `json:"min_peak_prominence_mg_min"`
	SmoothWindow      int     `json:"smooth_window"`
	GapNoInterp       bool    `json:"gap_no_interpolation"`
	GapThresholdS     float64 `json:"gap_threshold_s"`
}

// Fingerprint hashes the rule content (id/version excluded).
func (p Params) Fingerprint() string {
	c := struct {
		A, Lg, MP float64
		SW        int
		NoInterp  bool
		Gap       float64
	}{p.AtmosphereDelayS, p.ThermalLagS, p.MinPeakProminence, p.SmoothWindow, p.GapNoInterp, p.GapThresholdS}
	return hashCanonical(c)
}

// Decision is a human verdict on a candidate / value. A later decision may
// supersede it; conflicts and resolutions are all kept as events.
type Decision struct {
	TargetType string `json:"target_type"` // candidate | residual | param_change
	TargetID   string `json:"target_id"`
	Verdict    string `json:"verdict"` // accept | reject | adjust
	Reason     string `json:"reason"`
	Adjustment map[string]float64 `json:"adjustment,omitempty"`
}

// Project metadata.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SampleRef string `json:"sample_ref"`
	CreatedAt string `json:"created_at"`
}
