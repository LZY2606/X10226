// Package model defines the stable domain types for the TGA review system.
//
// Layering rules enforced across the code base:
//   - raw layer: original textual representation is preserved forever;
//   - derived layer: normalised quantities, series, DTG peaks, stage
//     candidates and residues carry algorithm version, parameter fingerprint
//     and direct source ids.
package model

import (
	"encoding/json"
	"time"
)

// AlgVersion is bumped whenever a derived computation changes behaviour.
const AlgVersion = "tga-stage-alg/1"

// ArchiveVersion identifies the export envelope.
const ArchiveVersion = "tga-review-archive/1"

// FormatVersion is the accepted case/import text format.
const FormatVersion = "tga-casepack/1"

// Clock is the injectable time source (virtual time in tests).
type Clock func() time.Time

// Layer identifiers for raw, append-only source records.
const (
	LayerPoint   = "sample_point"
	LayerDrift   = "balance_drift"
	LayerFurnace = "furnace_program"
	LayerGas     = "gas_valve"
	LayerBlank   = "blank_curve"
	LayerM0      = "initial_mass"
	LayerDelay   = "atmosphere_delay"
)

// Record statuses.
const (
	StatusOK    = "ok"
	StatusError = "error"
)

// Quantity is a domain measurement. Raw keeps the original token; when OK is
// false the value could not be converted and stays at its source location with
// a diagnostic instead of being dropped for charting convenience.
type Quantity struct {
	Value       float64 `json:"value"`
	Unit        string  `json:"unit"`
	Raw         string  `json:"raw"`
	RawUnit     string  `json:"raw_unit"`
	OK          bool    `json:"ok"`
	Reason      string  `json:"reason,omitempty"`
	Coordinates string  `json:"coordinates,omitempty"`
}

// Diagnostic is an attached, machine-readable explanation.
type Diagnostic struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Source  string   `json:"source,omitempty"`
	IDs     []string `json:"ids,omitempty"`
}

// Batch groups everything submitted in one import. Rows never overwrite
// previous rows; re-imported layer/key pairs create new versions.
type Batch struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Label       string    `json:"label"`
	SourceName  string    `json:"source_name"`
	Status      string    `json:"status"` // ok | partial
	OKRows      int       `json:"ok_rows"`
	ErrorRows   int       `json:"error_rows"`
	ImportedAt  time.Time `json:"imported_at"`
	EventSeq    int64     `json:"event_seq"`
}

// RawRecord is an immutable source row.
type RawRecord struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"project_id"`
	BatchID     string       `json:"batch_id"`
	Layer       string       `json:"layer"`
	Key         string       `json:"key"`
	Version     int          `json:"version"`
	Kind        string       `json:"kind"`
	LineNo      int          `json:"line_no"`
	RawLine     string       `json:"raw_line"`
	Repr        map[string]string `json:"repr"`
	Fields      map[string]any    `json:"fields"`
	Status      string       `json:"status"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	EventSeq    int64        `json:"event_seq"`
	CreatedAt   time.Time    `json:"created_at"`
}

// Params is a versioned rule/parameter set. Fingerprint identifies it.
type Params struct {
	ID              string    `json:"id"`
	Version         int       `json:"version"`
	ParentID        string    `json:"parent_id"`
	AlgVersion      string    `json:"alg_version"`
	SmoothWindowS   float64   `json:"smooth_window_s"`
	MinProminence   float64   `json:"min_prominence_pct_per_min"`
	OverlapWindowS  float64   `json:"overlap_window_s"`
	GapFactor       float64   `json:"gap_factor"`
	TempLagTauS     float64   `json:"temp_lag_tau_s"`
	BlankPolicy     string    `json:"blank_policy"`
	Fingerprint     string    `json:"fingerprint"`
	Reason          string    `json:"reason"`
	EventSeq        int64     `json:"event_seq"`
	CreatedAt       time.Time `json:"created_at"`
}

// Run is one immutable derived computation over a source snapshot.
type Run struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	Seq        int       `json:"seq"`
	ParamsID   string    `json:"params_id"`
	AlgVersion string    `json:"alg_version"`
	Fingerprint string   `json:"fingerprint"`
	Sources    []string  `json:"sources"`
	Status     string    `json:"status"` // ok | partial
	Notes      []Diagnostic `json:"diagnostics,omitempty"`
	EventSeq   int64     `json:"event_seq"`
	CreatedAt  time.Time `json:"created_at"`
}

// Derived is an immutable derived object (series, candidate, residue...).
type Derived struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	RunID     string          `json:"run_id"`
	Kind      string          `json:"kind"`
	Key       string          `json:"key"`
	ParentID  string          `json:"parent_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
	EventSeq  int64           `json:"event_seq"`
}

// Event is the append-only history. Payload is kept machine-readable.
type Event struct {
	ID      string         `json:"id"`
	Seq     int64          `json:"seq"`
	Type    string         `json:"type"`
	Actor   string         `json:"actor"`
	At      time.Time      `json:"at"`
	Summary string         `json:"summary"`
	Payload map[string]any `json:"payload"`
}

// Event types.
const (
	EvProjectCreated  = "project_created"
	EvImported        = "imported"
	EvParamsRevised   = "params_revised"
	EvRunCreated      = "run_created"
	EvDecision        = "decision"
	EvConflict        = "conflict_detected"
	EvConflictResolve = "conflict_resolved"
	EvUndo            = "decision_undone"
	EvExported        = "exported"
	EvArchiveImported = "archive_imported"
)
