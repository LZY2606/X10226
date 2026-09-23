// Package domain defines the immutable input strata, typed values and the
// append-only event vocabulary of the thermogravimetric review system.
package domain

import "encoding/json"

// Value is a field exactly as imported. Raw preserves the original textual
// representation; parsing into a numeric value happens only in derived layers.
// A value that cannot be converted to the declared unit stays in place with a
// diagnostic and is never dropped for the sake of plotting.
type Value struct {
	Raw  string  `json:"raw"`
	Num  float64 `json:"num,omitempty"`
	Unit string  `json:"unit,omitempty"`
	// Bad is non-empty when the raw text could not be converted to Unit.
	Bad string `json:"bad,omitempty"`
}

// Diag is a machine-readable diagnostic attached to an input fragment.
type Diag struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

// RawRow is one submitted line from a stratum, preserved verbatim with its
// typed conversion results and any diagnostics. Accepted=false rows are kept
// as evidence but never enter derived computations.
type RawRow struct {
	Stratum string          `json:"stratum"`
	LineNo  int             `json:"line_no"`
	Raw     string          `json:"raw"`
	Fields  map[string]Value `json:"fields,omitempty"`
	Accepted bool           `json:"accepted"`
	Diags   []Diag          `json:"diags,omitempty"`
}

// SectionStatus reports the acceptance state of one stratum inside a bundle.
type SectionStatus struct {
	Stratum    string `json:"stratum"`
	Present    bool   `json:"present"`
	Accepted   int    `json:"accepted"`
	Rejected   int    `json:"rejected"`
	Fatal      string `json:"fatal,omitempty"` // whole-section refusal reason
}

// Bundle is one submitted import request: the raw text per stratum, the
// parent version it was based on, and the resulting parse outcome.
type Bundle struct {
	Kind       string          `json:"kind"` // "initial" | "revision" | "partial"
	ParentEvent string         `json:"parent_event,omitempty"`
	Sections   map[string]string `json:"sections"`
	Statuses   []SectionStatus `json:"statuses"`
	Rows       []RawRow        `json:"rows"`
	// Affected lists the direct-source identities touched by accepted rows.
	Affected []string `json:"affected,omitempty"`
	// HalfFailed marks a bundle whose import succeeded only partially.
	HalfFailed bool `json:"half_failed"`
	Note       string `json:"note,omitempty"`
}

// Params are the tunable analysis parameters. Fingerprint() is part of every
// derived identity so a parameter revision never silently overwrites old math.
type Params struct {
	DriftModel        string  `json:"drift_model"` // "linear" | "none"
	BlankApply        bool    `json:"blank_apply"`
	SmoothWindowS     float64 `json:"smooth_window_s"`
	PeakMinProminence float64 `json:"peak_min_prominence"`
	GasDelayS         float64 `json:"gas_delay_s"` // valve switch -> sample atmosphere
	TempLagS          float64 `json:"temp_lag_s"`  // programmed T -> sample T
	GapMinS           float64 `json:"gap_min_s"`
	OverlapOption     string  `json:"overlap_option"` // "both" | "merged" | "split"
}

// DefaultParams returns the built-in algorithm-1 parameter set.
func DefaultParams() Params {
	return Params{
		DriftModel:        "linear",
		BlankApply:        true,
		SmoothWindowS:     30,
		PeakMinProminence: 1e-6,
		GasDelayS:         5,
		TempLagS:          4,
		GapMinS:           60,
		OverlapOption:     "both",
	}
}

// ParamsFingerprint is a compact deterministic token of the parameter set.
func (p Params) Fingerprint() string {
	b, _ := json.Marshal(p)
	return "pfx-" + shortHash(b)
}

// Event is the unit of the append-only log. Nothing in the log is ever
// overwritten: revisions are expressed as new events with ParentSeq links.
type Event struct {
	Seq       int             `json:"seq"`
	ID        string          `json:"id"`
	At        string          `json:"at"` // virtual-clock timestamp, UTC
	Type      string          `json:"type"`
	ParentSeq int             `json:"parent_seq,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

// Canonical event type vocabulary.
const (
	EvProjectCreated  = "project.created"
	EvBundleImported  = "bundle.imported"
	EvParamsRevised   = "params.revised"
	EvCandidateShown  = "candidate.shown" // materialized machine-readable judgment
	EvDecisionMade    = "decision.made"
	EvConflictResolved = "conflict.resolved"
	EvDecisionUndone  = "decision.undone"
)

// ProjectInfo is the payload of project.created.
type ProjectInfo struct {
	Name      string `json:"name"`
	ClockSeed string `json:"clock_seed"`
	Note      string `json:"note,omitempty"`
}

// Decision is a human adjudication recorded against one or more candidates.
type Decision struct {
	TargetIDs  []string `json:"target_ids"`
	Verdict    string   `json:"verdict"` // accepted | rejected | adjusted
	AdjustedTo *Params  `json:"adjusted_to,omitempty"`
	Reason     string   `json:"reason"`
	ConflictWith string `json:"conflict_with,omitempty"`
}

// ConflictResolution declares which of two contradictory decisions stands.
type ConflictResolution struct {
	WonID  string `json:"won_id"`
	LostID string `json:"lost_id"`
	Reason string `json:"reason"`
}

// Undo reverses one decision without deleting it from history.
type Undo struct {
	DecisionEventID string `json:"decision_event_id"`
	Reason          string `json:"reason"`
}
