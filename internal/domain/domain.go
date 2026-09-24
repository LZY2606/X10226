// Package domain defines the immutable event model and value objects for the
// thermogravimetric review system.
//
// The whole project state is an append-only log of events. Raw inputs are
// never overwritten: a correction is expressed as a new version that
// supersedes its parent version. Every derived value carries the algorithm
// version, a parameter fingerprint and the direct sources it was computed
// from, so any past event sequence can be replayed step by step.
package domain

import "time"

// Layers are the independently versioned direct-source strata.
type Layer string

const (
	LayerRawPoints   Layer = "raw_points"
	LayerBlank       Layer = "blank_curve"
	LayerDrift       Layer = "drift_correction"
	LayerFurnace     Layer = "furnace_program"
	LayerGas         Layer = "gas_valve_events"
	LayerInitialMass Layer = "initial_mass"
	LayerParameters  Layer = "parameters"
)

// Event types recorded in the append-only project log.
const (
	EvProjectCreated   = "project_created"
	EvImportOpened     = "import_opened"
	EvRawRecordAdded   = "raw_record_added"
	EvImportClosed     = "import_closed"
	EvLayerRevised     = "layer_revised"
	EvDerivedComputed  = "derived_computed"
	EvCandidateStaged  = "candidate_staged"
	EvDecisionMade     = "decision_made"
	EvConflictNoted    = "conflict_noted"
	EvConflictResolved = "conflict_resolved"
	EvReviewStageMoved = "review_stage_moved"
	EvJudgmentRule     = "judgment_rule_asserted"
)

// Review stages form the core state machine.
const (
	StageImporting      = "importing"
	StageInputCheck     = "input_check"
	StageCandidateReview = "candidate_review"
	StageFinalReview    = "final_review"
	StageAccepted       = "accepted"
)

// Event is the immutable envelope persisted in SQLite and exported verbatim.
type Event struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Seq       int64          `json:"seq"`
	Type      string         `json:"type"`
	AtVirtual time.Time      `json:"at_virtual"`
	ParentSeq int64          `json:"parent_seq"`
	Payload   map[string]any `json:"payload"`
}

// RawValue preserves the original textual representation and unit supplied by
// the lab instrument. Normalisation happens only in the derived layer; values
// that cannot be converted stay in place with a diagnostic instead of being
// dropped to please the chart.
type RawValue struct {
	Text     string  `json:"text"`
	Unit     string  `json:"unit"`
	Value    float64 `json:"value"`
	Converted bool   `json:"converted"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

// RawPoint is one instrument sample. t is furnace time (s), T temperature
// (degC), m mass (mg after SI normalisation when convertible).
type RawPoint struct {
	Index int      `json:"index"`
	T     RawValue `json:"t"`
	Temp  RawValue `json:"temp"`
	M     RawValue `json:"m"`
}

// FurnaceSegment is one programmed furnace ramp/hold.
type FurnaceSegment struct {
	ID         string  `json:"id"`
	Kind       string  `json:"kind"` // ramp | hold
	StartS     float64 `json:"start_s"`
	EndS       float64 `json:"end_s"`
	StartC     float64 `json:"start_c"`
	EndC       float64 `json:"end_c"`
}

// ValveEvent marks a gas valve switch (furnace-side time).
type ValveEvent struct {
	ID         string  `json:"id"`
	AtS        float64 `json:"at_s"`
	FromGas    string  `json:"from_gas"`
	ToGas      string  `json:"to_gas"`
}

// RawRecord is one file/section inside an import batch.
type RawRecord struct {
	ID     string `json:"id"`
	Layer  Layer  `json:"layer"`
	Name   string `json:"name"`
	Format string `json:"format"`
	Body   string `json:"body"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
}

// Gap is a detected sampling gap that must never be interpolated across.
type Gap struct {
	StartS float64 `json:"start_s"`
	EndS   float64 `json:"end_s"`
	Reason string  `json:"reason"`
}

// ChainLink is one step in an expandable residue / stage calculation chain.
type ChainLink struct {
	Order       int            `json:"order"`
	Kind        string         `json:"kind"`
	Description string         `json:"description"`
	Inputs      map[string]any `json:"inputs,omitempty"`
	Value       string         `json:"value,omitempty"`
	Unit        string         `json:"unit,omitempty"`
	SourceIDs   []string       `json:"source_ids,omitempty"`
	Unknown     bool           `json:"unknown,omitempty"`
	Note        string         `json:"note,omitempty"`
}
