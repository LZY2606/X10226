// Package core implements the domain model for thermogravimetric analysis
// (TGA) stage decomposition, atmosphere events and residue evidence chains.
//
// Design rules honored throughout:
//   - Raw inputs are immutable once submitted; revisions are new versions
//     linked to their parent version.
//   - Every domain quantity carries a unit and, where relevant, a coordinate
//     basis (e.g. furnace clock vs. sample-effective time).
//   - The import layer preserves the original textual representation; only
//     the derived layer normalizes. Values that cannot be converted stay in
//     place with a diagnostic attached.
//   - Candidates, rejection reasons, errors and unknown states are
//     first-class data, never silently dropped.
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// LayerKind identifies one of the six stratified input layers.
type LayerKind string

const (
	LayerRawPoints       LayerKind = "raw_points"
	LayerDriftCorrection LayerKind = "drift_correction"
	LayerFurnaceProgram  LayerKind = "furnace_program"
	LayerGasEvents       LayerKind = "gas_events"
	LayerBlankCurve      LayerKind = "blank_curve"
	LayerInitialMass     LayerKind = "initial_mass"
)

// LayerOrder is the canonical display/processing order of layers.
var LayerOrder = []LayerKind{
	LayerRawPoints, LayerDriftCorrection, LayerFurnaceProgram,
	LayerGasEvents, LayerBlankCurve, LayerInitialMass,
}

// Import status values.
const (
	StatusCommitted       = "committed"
	StatusPartialFailure  = "partial_failure"
)

// Derived value lifecycle states.
const (
	DerivedCurrent  = "current"
	DerivedStale    = "stale"
	DerivedRejected = "rejected"
	DerivedUnknown  = "unknown"
)

// Diagnostic is attached to anything that could not be fully processed.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Ref     string `json:"ref,omitempty"`
}

// Quantity is a domain value with unit and coordinate basis.
type Quantity struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Basis string  `json:"basis,omitempty"`
}

// RawValue keeps the original textual representation next to the parsed
// quantity. If parsing/conversion fails, Parsed is nil and Diagnostic set.
type RawValue struct {
	Raw        string      `json:"raw"`
	Parsed     *Quantity   `json:"parsed,omitempty"`
	Diagnostic *Diagnostic `json:"diagnostic,omitempty"`
}

// Input is one immutable submitted layer version.
type Input struct {
	ID          string       `json:"id"`
	Kind        LayerKind    `json:"kind"`
	Version     int          `json:"version"`
	ParentID    string       `json:"parent_id,omitempty"`
	SubmittedAt time.Time    `json:"submitted_at"`
	Raw         string       `json:"raw"`
	Status      string       `json:"status"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	Payload     any          `json:"payload,omitempty"`
}

// Derived is an immutable derived value with full provenance.
type Derived struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	AlgoVersion string   `json:"algo_version"`
	ParamFP     string   `json:"param_fp"`
	SourceIDs   []string `json:"source_ids"`
	Value       any      `json:"value"`
	CreatedSeq  int64    `json:"created_seq"`
}

// Event is one entry in the append-only history.
type Event struct {
	Seq     int64     `json:"seq"`
	ID      string    `json:"id"`
	Type    string    `json:"type"`
	At      time.Time `json:"at"`
	Payload any       `json:"payload,omitempty"`
}

// Event types.
const (
	EvImportCommit         = "import.commit"
	EvImportPartialFailure = "import.partial_failure"
	EvDeriveRun            = "derive.run"
	EvStaleMark            = "stale.mark"
	EvAtmosphereEffective  = "atmosphere.effective"
	EvCandidatePropose     = "candidate.propose"
	EvDecisionAccept       = "decision.accept"
	EvDecisionReject       = "decision.reject"
	EvDecisionAdjust       = "decision.adjust"
	EvConflict             = "conflict.record"
	EvConflictResolve      = "conflict.resolve"
	EvUndo                 = "undo"
	EvExport               = "export"
	EvImportRestore        = "import_restore"
	EvCasepackLoad         = "casepack.load"
)

// HashID builds a stable, content-addressed identifier.
func HashID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}

// Fingerprint produces a stable fingerprint of any JSON-marshalable value.
func Fingerprint(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "fp_unmarshalable"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}
