// Package domain defines the immutable event log, layered raw inputs,
// unit-bearing quantities and derived review records for the TGA stage
// decomposition / atmosphere-event evidence system.
package domain

import "time"

// EventType enumerates every kind of immutable fact in the event log.
type EventType string

const (
	EvProjectCreated   EventType = "project.created"
	EvBatchImported    EventType = "batch.imported"
	EvSourceRow        EventType = "source.row"
	EvParamsSet        EventType = "params.set"
	EvDecision         EventType = "decision"
	EvConflictResolved EventType = "conflict.resolved"
	EvUndo             EventType = "undo"
	EvNote             EventType = "note"
)

// SourceLayer identifies the six preserved input layers of an import.
type SourceLayer string

const (
	LayerRaw      SourceLayer = "raw_sample"
	LayerDrift    SourceLayer = "balance_drift"
	LayerProgram  SourceLayer = "furnace_program"
	LayerGas      SourceLayer = "gas_valve"
	LayerBlank    SourceLayer = "blank_curve"
	LayerWeight   SourceLayer = "sample_weight"
	LayerParams   SourceLayer = "parameters"
	LayerFragment SourceLayer = "import_fragment"
)

// RowStatus is the judgment state of an imported source row.
type RowStatus string

const (
	RowOK     RowStatus = "accepted"
	RowFailed RowStatus = "rejected"
)

// Event is the unit of the append-only log. Seq is assigned on commit.
// EventID is content-derived and stable across export/re-import.
type Event struct {
	ProjectID string    `json:"project_id"`
	EventID   string    `json:"event_id"`
	Seq       int64     `json:"seq"`
	At        time.Time `json:"at"`
	Actor     string    `json:"actor"`
	Type      EventType `json:"type"`
	ParentID  string    `json:"parent_id"`
	Payload   []byte    `json:"payload"`
}

// ProjectCreatedPayload creates a review project.
type ProjectCreatedPayload struct {
	Name   string `json:"name"`
	Origin string `json:"origin"` // casepack id or "api"
	Seed   int64  `json:"seed"`   // fixed seed recorded for reproducibility
}

// BatchImportedPayload groups rows parsed from one submitted fragment.
// A half-failed import keeps a FAILED batch with accepted rows plus diagnostics.
type BatchImportedPayload struct {
	BatchID      string `json:"batch_id"`
	ParentBatch  string `json:"parent_batch,omitempty"` // revision lineage
	Kind         string `json:"kind"`                   // "import" | "revision"
	Status       string `json:"status"`                 // "ok" | "partial" | "failed"
	RawFragment  string `json:"raw_fragment"`           // verbatim input text
	Format       string `json:"format"`                 // "tga-1"
	AcceptedRows int    `json:"accepted_rows"`
	RejectedRows int    `json:"rejected_rows"`
	Diag         string `json:"diag,omitempty"`
	Origin       string `json:"origin,omitempty"` // casepack scenario id
}

// SourceRowPayload is one preserved line of raw input. Original representation
// (as submitted) is retained; normalization happens only in derived layers.
type SourceRowPayload struct {
	BatchID string      `json:"batch_id"`
	Layer   SourceLayer `json:"layer"`
	RowNo   int         `json:"row_no"`
	Status  RowStatus   `json:"status"`
	Raw     string      `json:"raw"`
	Fields  []Field     `json:"fields"`
	Diag    string      `json:"diag,omitempty"`
}

// Field retains both the original textual representation and a parsed value.
type Field struct {
	Name string `json:"name"`
	Raw  string `json:"raw"`
}

// ParamsSetPayload installs a new parameter revision.
type ParamsSetPayload struct {
	ParentParams string `json:"parent_params,omitempty"`
	Reason       string `json:"reason"`
	Params       Params `json:"params"`
}

// Params are algorithm parameters. All durations are seconds, temperatures °C.
type Params struct {
	AtmosphereDelayS  float64 `json:"atmosphere_delay_s"`
	TemperatureLagC   float64 `json:"temperature_lag_c"`
	DriftRateMgPerMin float64 `json:"drift_rate_mg_per_min"`
	PeakProminenceMgS float64 `json:"peak_prominence_mg_s"`
	PeakMergeWindowS  float64 `json:"peak_merge_window_s"`
	NoiseTolMg        float64 `json:"noise_tol_mg"`
	GapThresholdS     float64 `json:"gap_threshold_s"`
	BlankScale        float64 `json:"blank_scale"`
	AlgorithmVersion  string  `json:"algorithm_version"`
}

// DecisionKind enumerates human verdicts.
type DecisionKind string

const (
	DecAcceptStage   DecisionKind = "accept_stage"
	DecRejectStage   DecisionKind = "reject_stage"
	DecAdjustStage   DecisionKind = "adjust_stage"
	DecAcceptResidue DecisionKind = "accept_residue"
	DecRejectResidue DecisionKind = "reject_residue"
	DecAcceptPeak    DecisionKind = "accept_peak"
)

// DecisionPayload records a human verdict against a derived candidate.
// Adjustments carry overrides; conflicting decisions open a conflict.
type DecisionPayload struct {
	Kind       DecisionKind `json:"kind"`
	TargetKind string       `json:"target_kind"` // "stage" | "residue" | "peak"
	TargetID   string       `json:"target_id"`   // stable candidate id
	SeqRef     int64        `json:"seq_ref"`     // derived view when decision was made
	EventID    string       `json:"event_id"`    // decision event (for precise undo)
	Basis      string       `json:"basis"`
	// Adjustment overrides (seconds), empty unless kind == adjust_stage.
	StartS *float64 `json:"start_s,omitempty"`
	EndS   *float64 `json:"end_s,omitempty"`
	Note   string   `json:"note,omitempty"`
}

// ConflictResolvedPayload keeps all candidates of a conflict and the chosen one.
type ConflictResolvedPayload struct {
	ConflictID string   `json:"conflict_id"`
	ChosenID   string   `json:"chosen_id"`
	LoserIDs   []string `json:"loser_ids"`
	Reason     string   `json:"reason"`
}

// UndoPayload negates a previous event; history is never overwritten.
type UndoPayload struct {
	UndoneEventID string `json:"undone_event_id"`
	Reason        string `json:"reason"`
}

// NotePayload attaches a machine-readable justification to the timeline.
type NotePayload struct {
	Code    string   `json:"code"` // e.g. "valve_time_ne_reaction_time"
	Message string   `json:"message"`
	Refs    []string `json:"refs,omitempty"`
}
