// Package domain holds the core immutable domain model for the TGA evidence
// chain: raw layered inputs, analysis parameters, derived results, human
// decisions and the append-only event log.
package domain

import "time"

// Document kinds kept in separate, named layers.
const (
	KindSamples   = "samples"   // raw sampled TG points (time, mass, sample temperature)
	KindDrift     = "drift"     // balance drift correction table / model
	KindFurnace   = "furnace"   // furnace program (setpoint ramps/holds)
	KindGas       = "gas"       // gas valve events
	KindBlank     = "blank"     // blank/baseline curve
	KindInitial   = "initial"   // sample initial mass statement
	KindMeta      = "meta"      // project/sample metadata
	KindUnknown   = "unknown"   // unrecognized section from a half-failed import
	KindParameter = "parameter" // analysis parameters (not a raw source layer)
)

// Source layers that import accepts. Order is the canonical layer ordering.
var SourceKinds = []string{
	KindSamples, KindDrift, KindFurnace, KindGas, KindBlank, KindInitial, KindMeta,
}

// Status marks acceptance of an artifact in a review.
type Status string

const (
	StatusPending   Status = "pending"
	StatusAccepted  Status = "accepted"
	StatusRejected  Status = "rejected"
	StatusSuperseded Status = "superseded"
	StatusConflict  Status = "conflict"
	StatusError     Status = "error"
	StatusUnknown   Status = "unknown"
)

// Q is a quantity carrying its coordinate basis/unit plus the original
// textual representation supplied at import time.
type Q struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Raw   string  `json:"raw,omitempty"`
	// Basis is the coordinate reference, e.g. "furnace_time_s",
	// "sample_temp_C", "mass_mg_actual".
	Basis string `json:"basis,omitempty"`
	// Unconvertible marks values that could not be normalized; the raw text
	// stays in place with a diagnostic instead of being dropped.
	Unconvertible bool   `json:"unconvertible,omitempty"`
	Diagnostic    string `json:"diagnostic,omitempty"`
}

// Issue is a machine-readable diagnostic / judgement record.
type Issue struct {
	Code     string            `json:"code"`
	Severity string            `json:"severity"` // error|warning|info
	Message  string            `json:"message"`
	At       *float64          `json:"at,omitempty"`
	Refs     map[string]string `json:"refs,omitempty"`
}

// Section is one parsed block from an import payload.
type Section struct {
	Kind    string     `json:"kind"`
	Raw     string     `json:"raw"`
	Header  string     `json:"header,omitempty"`
	Records [][]string `json:"records,omitempty"`
	Issues  []Issue    `json:"issues,omitempty"`
}

// RawDoc is an immutable committed input layer (or a parameter document).
type RawDoc struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	Kind       string    `json:"kind"`
	Version    int       `json:"version"`
	ParentID   string    `json:"parent_id,omitempty"`
	Raw        string    `json:"raw"`
	Sections   []Section `json:"sections,omitempty"`
	ImportOK   bool      `json:"import_ok"`
	Issues     []Issue   `json:"issues,omitempty"`
	Author     string    `json:"author,omitempty"`
	ReceivedAt time.Time `json:"received_at"`
}

// SamplePoint is a normalized raw sample reading (kept next to its raw forms).
type SamplePoint struct {
	T    float64 `json:"t"`     // furnace/elapsed time, seconds
	M    Q       `json:"m"`     // balance mass, original unit preserved
	TS   Q       `json:"ts"`    // sample temperature, original unit preserved
	TF   Q       `json:"tf"`    // furnace temperature when supplied
	Note string  `json:"note,omitempty"`
}

// FurnaceStep is one setpoint program step.
type FurnaceStep struct {
	FromT  float64 `json:"from_t"`  // s
	ToT    float64 `json:"to_t"`    // s (0 for holds uses hold duration fields)
	Rate   Q       `json:"rate"`    // K/min
	Target Q       `json:"target"`  // setpoint target temperature
}

// GasEvent is a valve switch with its declared time.
type GasEvent struct {
	T       float64 `json:"t"`
	Gas     string  `json:"gas"`
	Action  string  `json:"action"` // on|off|switch
	Flow    Q       `json:"flow,omitempty"`
	RawLine string  `json:"raw_line,omitempty"`
}

// Parameters control the derivation algorithms.
type Parameters struct {
	ID                string  `json:"id"`
	Version           int     `json:"version"`
	ParentID          string  `json:"parent_id,omitempty"`
	AlgoVersion       string  `json:"algo_version"`
	AtmDelayS         float64 `json:"atm_delay_s"`          // gas transport delay (s)
	TempLagTauS       float64 `json:"temp_lag_tau_s"`       // sample-vs-furnace lag tau (s)
	DriftModel        string  `json:"drift_model"`          // none|linear|blank
	BlankSubtract     bool    `json:"blank_subtract"`
	DTGWindowS        float64 `json:"dtg_window_s"`         // smoothing/derivative window (s)
	PeakPromPct        float64 `json:"peak_prominence_pct"`  // DTG peak prominence (%/min of mass)
	MergeGapS         float64 `json:"merge_gap_s"`          // peaks closer than this may merge
	OverlapSplit      string  `json:"overlap_split"`        // auto|manual|none
	GapInterp         bool    `json:"gap_interpolate"`      // must stay false across gaps
	NoiseMonotonic    bool    `json:"noise_enforce_mono"`   // false => keep non-monotonic points
	Fingerprint       string  `json:"fingerprint"`
}
