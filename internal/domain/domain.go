// Package domain holds the core, storage-independent model of the TGA review
// system: immutable source layers, versioned parameters, an append-only event
// log, derived results with provenance, decisions and stage/residue candidates.
package domain

// Layer identifies one independently preserved source stratum.
type Layer string

const (
	LayerSample   Layer = "sample_points" // raw sampled mass/temperature vs furnace time
	LayerDrift    Layer = "drift_correction" // balance drift correction
	LayerProgram  Layer = "furnace_program"
	LayerGas      Layer = "gas_valve_events"
	LayerBlank    Layer = "blank_curve"
	LayerInitial  Layer = "initial_mass"
	LayerParams   Layer = "algorithm_params"
)

// SourceLayers is the order used in import, review and export.
var SourceLayers = []Layer{LayerSample, LayerDrift, LayerProgram, LayerGas, LayerBlank, LayerInitial, LayerParams}

// EventType enumerates every machine-readable event.
type EventType string

const (
	EvtImportStarted   EventType = "import.started"
	EvtLayerAccepted   EventType = "import.layer_accepted"
	EvtLayerRejected   EventType = "import.layer_rejected" // half-failed import
	EvtImportFinished  EventType = "import.finished"
	EvtParamsRevised   EventType = "params.revised"
	EvtComputed        EventType = "derived.computed"
	EvtImpact          EventType = "derived.impact"
	EvtRuleAtmosLag    EventType = "rule.atmosphere_delay"
	EvtRuleTempLag     EventType = "rule.temperature_lag"
	EvtRuleOverlap     EventType = "rule.stage_overlap"
	EvtRuleNoSum       EventType = "rule.residual_not_sum100"
	EvtRuleGap         EventType = "rule.gap_not_interpolated"
	EvtRuleAmbiguity   EventType = "rule.ambiguous_candidates"
	EvtRuleDrift       EventType = "rule.baseline_drift"
	EvtRuleNonMonotonic EventType = "rule.non_monotonic_noise"
	EvtRuleInitialRev  EventType = "rule.initial_mass_revised"
	EvtCandidateProposed EventType = "candidate.proposed"
	EvtDecisionAccept  EventType = "decision.accept"
	EvtDecisionReject  EventType = "decision.reject"
	EvtDecisionAdjust  EventType = "decision.adjust"
	EvtConflictResolved EventType = "decision.conflict_resolved"
	EvtUndo            EventType = "history.undo"
	EvtCheckout        EventType = "history.checkout"
)

// Event is an append-only, immutable history record.
type Event struct {
	Seq       int64     `json:"seq"`
	At        string    `json:"at"` // virtual timestamp (RFC3339)
	Type      EventType `json:"type"`
	Layer     string    `json:"layer,omitempty"`
	VersionID string    `json:"version_id,omitempty"`
	Subject   string    `json:"subject,omitempty"` // affected stable object id
	Payload   []byte    `json:"payload,omitempty"` // machine-readable JSON
	Undone    bool      `json:"undone,omitempty"`
}

// RawRow is one preserved source cell row in a layer.
type RawRow struct {
	ID       string `json:"id"`
	Layer    Layer  `json:"layer"`
	Version  string `json:"version"`
	Ordinal  int    `json:"ordinal"`
	Ref      string `json:"ref,omitempty"` // row key, e.g. furnace time raw text
	Col1     string `json:"col1"`
	Col2     string `json:"col2"`
	Col3     string `json:"col3,omitempty"`
	Col4     string `json:"col4,omitempty"`
	Col5     string `json:"col5,omitempty"`
	Status   string `json:"status"` // ok, unknown, invalid...
	Diag     string `json:"diag,omitempty"`
}

// LayerVersion captures the parent-version chain. Raw input is never
// overwritten; revisions are new versions pointing at a parent.
type LayerVersion struct {
	Layer     Layer  `json:"layer"`
	VersionID string `json:"version_id"`
	ParentID  string `json:"parent_id,omitempty"`
	Seq       int64  `json:"seq"`
	CommitAt  string `json:"commit_at"`
	RevNote   string `json:"rev_note,omitempty"`
}

// Params is the versioned algorithm parameter set.
type Params struct {
	AtmosDelayS      float64 `json:"atmos_delay_s"`       // valve -> sample atmosphere delay
	TempLagS         float64 `json:"temp_lag_s"`          // program -> sample temperature lag
	DriftMode        string  `json:"drift_mode"`          // blank | linear | none
	SmoothingWindowN int     `json:"smoothing_window_n"`  // DTG smoothing
	MinProminence    float64 `json:"min_prominence_mg_per_s"`
	OverlapTolS      float64 `json:"overlap_tol_s"`
	NoiseTolMg       float64 `json:"noise_tol_mg"`
}

// AlgoVersion identifies the calculation code.
const AlgoVersion = "tga-decomp-v1"

// DefaultParams returns the baseline parameter set.
func DefaultParams() Params {
	return Params{
		AtmosDelayS:      20,
		TempLagS:         8,
		DriftMode:        "blank",
		SmoothingWindowN: 5,
		MinProminence:    0.0004,
		OverlapTolS:      12,
		NoiseTolMg:       0.02,
	}
}

// Provenance ties every derived value to its direct sources, algorithm and
// parameter fingerprint.
type Provenance struct {
	AlgoVersion string   `json:"algo_version"`
	ParamHash   string   `json:"param_fingerprint"`
	DirectFrom  []string `json:"direct_from"` // stable ids
	Introduced  int64    `json:"introduced_seq"`
}

// CandidateStatus is first-class state for a stage/residue candidate.
type CandidateStatus string

const (
	CandPending   CandidateStatus = "pending"
	CandAccepted  CandidateStatus = "accepted"
	CandRejected  CandidateStatus = "rejected"
	CandAlternative CandidateStatus = "alternative" // legal alternative, never unique
	CandConflict  CandidateStatus = "conflict"
)

// StageCandidate is a proposed decomposition stage around a DTG peak.
type StageCandidate struct {
	ID         string  `json:"id"`
	PeakS      float64 `json:"peak_s"`
	PeakTempC  float64 `json:"peak_temp_c"`
	StartS     float64 `json:"start_s"`
	EndS       float64 `json:"end_s"`
	OverlapsWith []string `json:"overlaps_with,omitempty"`
	MassLossMg float64 `json:"mass_loss_mg"`
	Prominence float64 `json:"prominence_mg_per_s"`
	AtmosLabel string  `json:"atmos_label"`
	RejectReason string `json:"reject_reason,omitempty"`
	Status     CandidateStatus `json:"status"`
	GroupID    string  `json:"group_id,omitempty"` // ambiguity group
}

// Residue is a final-residue-rate result with an expandable calculation chain.
type Residue struct {
	ID        string  `json:"id"`
	AtS       float64 `json:"at_s"`
	Percent   float64 `json:"percent"`
	FinalMg   float64 `json:"final_mg"`
	InitialMg float64 `json:"initial_mg"`
	Chain     []ChainStep `json:"chain"`
}

// ChainStep is one hop in the provenance calculation chain.
type ChainStep struct {
	Label string   `json:"label"`
	Value string   `json:"value"`
	Unit  string   `json:"unit,omitempty"`
	From  []string `json:"from,omitempty"`
	Note  string   `json:"note,omitempty"`
}
