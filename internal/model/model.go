// Package model defines the immutable, unit-bearing domain objects of the
// TGA stage-decomposition / atmosphere-event evidence system.
//
// Layering rules enforced across the codebase:
//
//	raw layer  -> RawInput (original representation, never overwritten)
//	parse layer -> parsed rows, each cell keeping its original text
//	derive layer -> runs/candidates with algo version, parameter fingerprint
//	                and direct source references
//
// Every domain quantity travels with a unit and a coordinate basis.
package model

import "encoding/json"

// Input kinds are the six stratified raw layers plus the parameter layer.
const (
	KindPoints  = "points"  // original sampled points (t, T, m)
	KindDrift   = "drift"   // balance drift correction anchors
	KindBlank   = "blank"   // blank curve
	KindFurnace = "furnace" // furnace temperature program
	KindGas     = "gas"     // gas valve events
	KindMass    = "mass"    // sample initial mass
	KindParams  = "params"  // algorithm parameters
)

// AllKinds lists the stratified layers in review order.
func AllKinds() []string {
	return []string{KindPoints, KindDrift, KindBlank, KindFurnace, KindGas, KindMass, KindParams}
}

// Q is a domain quantity: value + unit + coordinate basis.
type Q struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Basis string  `json:"basis"`
}

// Repr keeps the original textual representation next to a canonical value.
type Repr struct {
	Raw      string  `json:"raw"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Parsable bool    `json:"parsable"`
}

// Diagnostic is attached to the exact input location that produced it.
type Diagnostic struct {
	Level   string `json:"level"` // error | warning
	Code    string `json:"code"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	Section string `json:"section,omitempty"`
}

// RawInput is an append-only, immutable revision of one stratified layer.
type RawInput struct {
	ProjectID     string          `json:"project_id"`
	ID            string          `json:"id"` // stable, e.g. inp_points_2
	Kind          string          `json:"kind"`
	Version       int             `json:"version"`
	ParentVersion int             `json:"parent_version"` // 0 = first version
	ContentType   string          `json:"content_type"`   // tga-section/v1
	RawText       string          `json:"raw_text"`       // original bytes, never normalized
	ImportBatch   string          `json:"import_batch"`
	Valid         bool            `json:"valid"` // invalid revisions never become sources
	Seed          int64           `json:"seed,omitempty"`
	Diagnostics   []Diagnostic    `json:"diagnostics"`
	Parsed        json.RawMessage `json:"parsed,omitempty"` // parse layer (typed by kind)
	CreatedAt     string          `json:"created_at"`
}

// SampleRow is one parsed sampled point; each numeric cell keeps raw text.
type SampleRow struct {
	ID   string `json:"id"` // stable within project, e.g. pt_000421
	Line int    `json:"line"`
	T    Repr   `json:"t"`
	Temp Repr   `json:"temp"`
	Mass Repr   `json:"mass"`
}

// DriftAnchor is one balance drift correction anchor.
type DriftAnchor struct {
	ID   string `json:"id"`
	Line int    `json:"line"`
	T    Repr   `json:"t"`
	M    Repr   `json:"m"` // balance offset at t
}

// BlankPoint is one blank-curve sample.
type BlankPoint struct {
	ID   string `json:"id"`
	Line int    `json:"line"`
	T    Repr   `json:"t"`
	Mass Repr   `json:"mass"`
}

// FurnaceSegment is one program segment.
type FurnaceSegment struct {
	ID       string `json:"id"`
	Line     int    `json:"line"`
	Kind     string `json:"kind"`               // ramp | isothermal
	Rate     Repr   `json:"rate,omitempty"`     // C/min
	Target   Repr   `json:"target,omitempty"`   // C
	Duration Repr   `json:"duration,omitempty"` // s (isothermal)
}

// FurnaceProgram is the parsed furnace program (initial temp + segments).
type FurnaceProgram struct {
	Initial  Repr             `json:"initial"`
	Segments []FurnaceSegment `json:"segments"`
}

// GasEvent is one valve event; Delay is the atmosphere transport delay for
// the new gas reaching the sample (reaction time = valve time + delay).
type GasEvent struct {
	ID        string `json:"id"`
	Line      int    `json:"line"`
	ValveTime Repr   `json:"valve_time"`
	FromGas   string `json:"from_gas"`
	ToGas     string `json:"to_gas"`
	DelayS    Repr   `json:"delay_s"`
}

// InitialMass is the sample initial mass layer.
type InitialMass struct {
	Value  Repr   `json:"value"`
	Source string `json:"source"`
	Note   string `json:"note"`
}

// Params is the algorithm parameter layer.
type Params struct {
	SmoothingWindow int     `json:"smoothing_window"`
	Prominence      float64 `json:"prominence"`       // %/C minimum DTG peak prominence
	MinSeparationS  float64 `json:"min_separation_s"` // min peak separation
	GapFactor       float64 `json:"gap_factor"`       // dt / median dt threshold for a gap
	DriftSource     string  `json:"drift_source"`     // anchors | blank
	NoiseZSigma     float64 `json:"noise_z_sigma"`    // non-monotonic noise outlier guard
}

// SourceRef points at one direct source object.
type SourceRef struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int    `json:"version"`
	Role    string `json:"role,omitempty"`
}

// Run is an immutable derived computation with full provenance.
type Run struct {
	ProjectID        string          `json:"project_id"`
	ID               string          `json:"id"` // run_n
	AlgoVersion      string          `json:"algo_version"`
	ParamFingerprint string          `json:"param_fingerprint"`
	Sources          []SourceRef     `json:"sources"`
	Current          bool            `json:"current"`
	Diagnostics      []Diagnostic    `json:"diagnostics"`
	Payload          json.RawMessage `json:"payload"`
	CreatedAt        string          `json:"created_at"`
}

// Candidate statuses.
const (
	StatusProposed   = "proposed"
	StatusAccepted   = "accepted"
	StatusRejected   = "rejected"
	StatusSuperseded = "superseded"
	StatusConflict   = "conflict"
	StatusIndeterm   = "indeterminate" // cannot be uniquely decided
)

// Candidate is a stage or residue candidate produced by a run.
type Candidate struct {
	ProjectID string          `json:"project_id"`
	ID        string          `json:"id"`
	RunID     string          `json:"run_id"`
	Type      string          `json:"type"` // stage | residue
	AltGroup  string          `json:"alt_group,omitempty"`
	ParentID  string          `json:"parent_id,omitempty"` // adjusted child
	Status    string          `json:"status"`
	Summary   string          `json:"summary"`
	Detail    json.RawMessage `json:"detail"`
	CreatedAt string          `json:"created_at"`
}

// Event is an append-only history entry (decisions, imports, findings...).
type Event struct {
	ProjectID string          `json:"project_id"`
	Seq       int             `json:"seq"`
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Undone    bool            `json:"undone"`
	Undoes    int             `json:"undoes,omitempty"` // target seq
	Actor     string          `json:"actor"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"created_at"`
}

// Project metadata.
type Project struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Conflict   bool   `json:"conflict"`
	CreatedAt  string `json:"created_at"`
	ArchivedOf string `json:"archived_of,omitempty"` // fork lineage for counter archive
}

// Batch records one import attempt including half-failed sections.
type Batch struct {
	ProjectID string         `json:"project_id"`
	ID        string         `json:"id"`
	Label     string         `json:"label"`
	Sections  []BatchSection `json:"sections"`
	CreatedAt string         `json:"created_at"`
}

// BatchSection records the fate of one section of an import.
type BatchSection struct {
	Kind       string     `json:"kind"`
	InputID    string     `json:"input_id,omitempty"`
	Valid      bool       `json:"valid"`
	Diagnostic Diagnostic `json:"diagnostic,omitempty"`
}
