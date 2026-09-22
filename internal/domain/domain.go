// Package domain holds the event-sourced vocabulary of the TGA review system:
// immutable raw batches, append-only events, derived result nodes and human
// decisions. Nothing in this package performs I/O.
package domain

import "encoding/json"

// Section kinds. A complete baseline import carries one of each.
const (
	KindSamples     = "samples"
	KindDrift       = "drift"
	KindFurnace     = "furnace"
	KindGas         = "gas"
	KindBlank       = "blank"
	KindInitialMass = "initial_mass"
)

// SectionKinds lists every accepted raw section in canonical order.
var SectionKinds = []string{
	KindSamples, KindDrift, KindFurnace, KindGas, KindBlank, KindInitialMass,
}

// Column describes one imported column with its declared unit/basis.
type Column struct {
	Name  string `json:"name"`
	Unit  string `json:"unit,omitempty"`
	Basis string `json:"basis,omitempty"`
}

// Cell preserves the original textual representation. Value is nil when the
// cell could not be converted; Diag then explains why and the cell is kept.
type Cell struct {
	Raw   string   `json:"raw"`
	Value *float64 `json:"value,omitempty"`
	Diag  string   `json:"diag,omitempty"`
}

// RawSection is a single stratified layer of the immutable input baseline.
type RawSection struct {
	Kind       string   `json:"kind"`
	SourceName string   `json:"source_name"`
	Columns    []Column `json:"columns"`
	Rows       [][]Cell `json:"rows"`
	Checksum   string   `json:"checksum"`
}

// Batch is a committed, immutable raw section version.
type Batch struct {
	ID          string     `json:"id"`
	Kind        string     `json:"kind"`
	Version     int        `json:"version"`
	ParentID    string     `json:"parent_id,omitempty"`
	CommittedAt string     `json:"committed_at"`
	Section     RawSection `json:"section"`
	EventID     string     `json:"event_id,omitempty"`
}

// AttemptSection records what happened to one section of an import.
type AttemptSection struct {
	Kind         string   `json:"kind"`
	OK           bool     `json:"ok"`
	RowsAccepted int      `json:"rows_accepted"`
	RowsRejected int      `json:"rows_rejected"`
	Diags        []string `json:"diags,omitempty"`
	CommittedAs  string   `json:"committed_as,omitempty"`
	Fatal        bool     `json:"fatal,omitempty"`
}

// ImportAttempt is the first-class record of an import, including half failures.
type ImportAttempt struct {
	ID             string           `json:"id"`
	At             string           `json:"at"`
	OK             bool             `json:"ok"`
	Partial        bool             `json:"partial"`
	BundleChecksum string           `json:"bundle_checksum,omitempty"`
	Error          string           `json:"error,omitempty"`
	Sections       []AttemptSection `json:"sections"`
}

// Params are the versioned pipeline parameters. Every derived node carries a
// fingerprint of the exact parameter set that produced it.
type Params struct {
	AlgorithmVersion string  `json:"algorithm_version"`
	GapThresholdS    float64 `json:"gap_threshold_s"`
	AtmosphereDelayS float64 `json:"atmosphere_delay_s"`
	LagTauS          float64 `json:"lag_tau_s"`
	TempLagPerKPerS  float64 `json:"temp_lag_per_k_per_s"`
	DtgWindowS       float64 `json:"dtg_window_s"`
	SmoothWindowS    float64 `json:"smooth_window_s"`
	PeakProminence   float64 `json:"peak_prominence"`
	PeakMinSepS      float64 `json:"peak_min_sep_s"`
	MinPeakRate      float64 `json:"min_peak_rate"`
	ResidueTailS     float64 `json:"residue_tail_s"`
	OverlapTolS      float64 `json:"overlap_tol_s"`
}

// CurrentParams is the algorithm version of this binary.
const CurrentParamsVersion = "tga-decomp-v1"

// DefaultParams returns fixed, deterministic parameters.
func DefaultParams() Params {
	return Params{
		AlgorithmVersion: CurrentParamsVersion,
		GapThresholdS:    8,
		AtmosphereDelayS: 45,
		LagTauS:          12,
		TempLagPerKPerS:  0.02,
		DtgWindowS:       6,
		SmoothWindowS:    9,
		PeakProminence:   0.00002,
		PeakMinSepS:      10,
		MinPeakRate:      0.00008,
		ResidueTailS:     25,
		OverlapTolS:      3,
	}
}

// SourceRef links a derived node to one direct source.
type SourceRef struct {
	Kind string `json:"kind"` // batch | params | node | decision
	ID   string `json:"id"`
	Role string `json:"role,omitempty"`
}

// Node is an immutable derived result. Read-time projections decide whether a
// node is current, superseded or unknown; nodes themselves never change.
type Node struct {
	ID               string          `json:"id"`
	Kind             string          `json:"kind"`
	AlgorithmVersion string          `json:"algorithm_version"`
	ParamFingerprint string          `json:"param_fingerprint"`
	Sources          []SourceRef     `json:"sources"`
	EventSeq         int             `json:"event_seq"`
	Data             json.RawMessage `json:"data"`
}

// Node kinds.
const (
	NodeCorrected = "corrected_series"
	NodeDtg       = "dtg_series"
	NodeGap       = "data_gap"
	NodeFlag      = "quality_flag"
	NodePeak      = "peak_candidate"
	NodeStage     = "stage_candidate"
	NodeReaction  = "reaction_window"
	NodeResidue   = "residue"
)

// Verdict strings.
const (
	VerdictAccepted   = "accepted"
	VerdictRejected   = "rejected"
	VerdictAdjusted   = "adjusted"
	VerdictConflicted = "conflicted"
)

// Candidate status strings.
const (
	StatusRetained   = "retained"
	StatusRejected   = "rejected"
	StatusUnknown    = "unknown"
	StatusSuperseded = "superseded"
)

// Decision is a human ruling over a node or a stage/peak key.
type Decision struct {
	ID         string             `json:"id"`
	EventSeq   int                `json:"event_seq"`
	At         string             `json:"at"`
	By         string             `json:"by"`
	TargetKind string             `json:"target_kind"`
	TargetID   string             `json:"target_id"`
	Verdict    string             `json:"verdict"`
	Rationale  string             `json:"rationale,omitempty"`
	Adjustment map[string]float64 `json:"adjustment,omitempty"`
	Supersedes string             `json:"supersedes,omitempty"`
	ConflictID string             `json:"conflict_id,omitempty"`
}

// Conflict marks two mutually exclusive decisions and its resolution.
type Conflict struct {
	ID         string `json:"id"`
	At         string `json:"at"`
	DecisionA  string `json:"decision_a"`
	DecisionB  string `json:"decision_b"`
	TargetID   string `json:"target_id"`
	ResolvedBy string `json:"resolved_by,omitempty"`
	Winner     string `json:"winner,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// Event is the unit of the append-only log.
type Event struct {
	ID        string          `json:"id"`
	Seq       int             `json:"seq"`
	ProjectID string          `json:"project_id"`
	At        string          `json:"at"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	PrevHash  string          `json:"prev_hash"`
	Hash      string          `json:"hash"`
}

// Event types.
const (
	EvProjectCreated  = "project_created"
	EvImportAttempted = "import_attempted"
	EvCommitted       = "committed"
	EvParamRevised    = "param_revised"
	EvDerived         = "derived"
	EvDecision        = "decision"
	EvConflict        = "conflict"
	EvResolved        = "resolved"
	EvUndid           = "undid"
)

// UndoRecord describes a logical undo (the undone event stays in the log).
type UndoRecord struct {
	ID        string `json:"id"`
	At        string `json:"at"`
	EventSeq  int    `json:"event_seq"`
	Kind      string `json:"kind"`
	Reason    string `json:"reason,omitempty"`
	UndoneSeq int    `json:"undone_seq"`
}

// RuleEvent is a machine-readable statement of judgement rationale
// (atmosphere delay, no interpolation across gaps, overlapping stages, ...).
type RuleEvent struct {
	Code     string   `json:"code"`
	Subject  string   `json:"subject"`
	Refs     []string `json:"refs,omitempty"`
	Message  string   `json:"message"`
	Severity string   `json:"severity"` // info | warn | rule
}
