// Package domain defines the immutable event model and replay state for the
// thermogravimetric analysis (TGA) review system.
//
// All quantities carry an explicit unit/coordinate basis. Original
// representations of imported values are preserved in the source layer; only
// derived layers normalize units. Values that cannot be converted stay at the
// source location with a diagnostic.
package domain

import "encoding/json"

// AlgorithmVersion identifies the version of the derivation pipeline. Bump
// whenever analyzer semantics change so that stale derivations are visible.
const AlgorithmVersion = "tga-derive-1"

// Event type constants. Events are append-only: original inputs can never be
// overwritten; revisions are expressed as new versions linked to a parent.
const (
	EvProjectCreated     = "project.created"
	EvSourceCommitted    = "source.committed"
	EvParamsRevised      = "params.revised"
	EvAnalysisRun        = "analysis.run"
	EvDecisionRecorded   = "decision.recorded"
	EvAdjustmentRecorded = "adjustment.recorded"
	EvConflictNoted      = "conflict.noted"
	EvConflictResolved   = "conflict.resolved"
	EvUndoRecorded       = "undo.recorded"
)

// Finding codes are machine-readable judgments emitted by the derivation
// pipeline and by the human review process.
const (
	FindGasDelay            = "gas_delay:valve_time_equals_not_reaction_time"
	FindThermalLag          = "thermal_lag:sample_temperature_trails_program"
	FindOverlappingStages   = "stages:overlap_allowed_not_summing_to_100"
	FindGapNoInterp         = "gap:no_cross_gap_interpolation"
	FindAmbiguousCandidates = "ambiguous:multiple_legal_candidates_kept"
	FindBaselineDrift       = "baseline:drift_corrected_from_blank"
	FindNonMonotonic        = "noise:non_monotonic_points_flagged"
	FindUnconvertible       = "import:unconvertible_value_retained"
	FindPartialImport       = "import:partial_failure_committed_with_diagnostics"
	FindInitialRevised      = "initial_mass:revision_new_version_with_parent"
	FindHumanConflict       = "decision:conflicting_human_decisions"
)

// Quantity is a value with its unit/coordinate basis. The raw representation
// is preserved so importing "10,2 mg" keeps "10,2 mg" while Value/Unit carry
// the normalized interpretation (and ParseError explains any failure).
type Quantity struct {
	Raw        string  `json:"raw"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	ParseError string  `json:"parse_error,omitempty"`
}

// RawCell is an imported token kept at its original coordinate.
type RawCell struct {
	Section   string `json:"section"`
	Row       int    `json:"row"`
	Column    string `json:"column"`
	Raw       string `json:"raw"`
	Line      int    `json:"line"`
	Diagnosis string `json:"diagnosis,omitempty"`
}

// Diagnostic records a per-row/section import problem without dropping data.
type Diagnostic struct {
	Severity string `json:"severity"` // "error" | "warning"
	Section  string `json:"section"`
	Line     int    `json:"line,omitempty"`
	Row      int    `json:"row,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// BatchRow is one structured row of an imported section.
type BatchRow struct {
	Row      int                 `json:"row"`
	Line     int                 `json:"line"`
	Fields   map[string]Quantity `json:"fields"`
	Tokens   map[string]string   `json:"tokens,omitempty"`
	Trailing []string            `json:"trailing,omitempty"`
	Bad      bool                `json:"bad,omitempty"`
}

// Section is one parsed section of a source file.
type Section struct {
	Kind        string            `json:"kind"`
	Headers     []string          `json:"headers"`
	Rows        []BatchRow        `json:"rows"`
	Diagnostics []Diagnostic      `json:"diagnostics"`
	RawText     string            `json:"raw_text"`
	LineStart   int               `json:"line_start"`
	LineEnd     int               `json:"line_end"`
	Extra       map[string]string `json:"extra,omitempty"`
}

// SourceRef points at an immutable input object.
type SourceRef struct {
	EventSeq int    `json:"event_seq"`
	BatchID  string `json:"batch_id"`
	Section  string `json:"section,omitempty"`
	Row      int    `json:"row,omitempty"`
	Column   string `json:"column,omitempty"`
}

// CalcStep is one step of an expandable calculation chain. Inputs/Outputs are
// already rendered in their unit basis so the chain reads back without code.
type CalcStep struct {
	Op      string            `json:"op"`
	Inputs  map[string]string `json:"inputs,omitempty"`
	Output  string            `json:"output"`
	Sources []SourceRef       `json:"sources,omitempty"`
	Detail  string            `json:"detail,omitempty"`
}

// Event is the unit of the append-only log.
type Event struct {
	Seq        int             `json:"seq"`
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	OccurredAt string          `json:"occurred_at"` // virtual clock, UTC RFC3339
	Actor      string          `json:"actor"`
	ParentSeq  int             `json:"parent_seq,omitempty"`
	Payload    json.RawMessage `json:"payload"`
}
