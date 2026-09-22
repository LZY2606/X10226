// Package domain defines the immutable layered data model of the TGA review
// system: raw submissions, derived versions, algorithm parameters, machine
// readable rule events, candidates, decisions and an append-only history.
//
// # Identity discipline
//
// Every object the UI can display carries a stable ID ("stable identity").
// Raw source versions and events are content addressed (sha256 over a
// canonical JSON encoding), so exporting a project archive, deleting the
// local database and importing it again restores identical IDs, ordering and
// provenance edges. Derived nodes are content addressed over their direct
// inputs, algorithm version and parameter fingerprint.
package domain

import "encoding/json"

// SourceKind enumerates the six separately stored input layers required by the
// review workflow.
type SourceKind string

const (
	KindSamples     SourceKind = "samples" // raw mass/temperature/furnace-time sampling points
	KindDrift       SourceKind = "drift"   // balance baseline drift correction
	KindProgram     SourceKind = "furnace_program"
	KindGasEvent    SourceKind = "gas_valve_event"
	KindBlank       SourceKind = "blank_curve"  // empty-pan baseline curve
	KindInitialMass SourceKind = "initial_mass" // sample initial mass (stratified, revisable)
)

// Layer kinds used in the store.
type LayerKind string

const (
	LayerRaw       LayerKind = "raw"
	LayerParams    LayerKind = "params"
	LayerDerived   LayerKind = "derived"
	LayerRuleEvent LayerKind = "rule_event"
	LayerDecision  LayerKind = "decision"
	LayerHistory   LayerKind = "history"
)

// Measurement is a scalar carrying its original textual representation, a
// normalized SI value and the unit/coordinate basis. When a value cannot be
// converted, Normalized is nil and Diagnostics explains why; the original
// representation is never dropped to make plotting possible.
type Measurement struct {
	Raw         string   `json:"raw"`
	Unit        string   `json:"unit"`
	Basis       string   `json:"basis,omitempty"`
	Normalized  *float64 `json:"normalized"`
	Diagnostics string   `json:"diagnostics,omitempty"`
}

// Valid reports whether the measurement normalized successfully.
func (m Measurement) Valid() bool { return m.Normalized != nil && m.Diagnostics == "" }

// SamplePoint is one raw acquisition point. Time is furnace/elapsed time in
// seconds, mass in milligrams and temperature in degrees Celsius after
// normalization; raw strings remain attached.
type SamplePoint struct {
	Index  int         `json:"index"`
	Time   Measurement `json:"time"`
	Mass   Measurement `json:"mass"`
	Temp   Measurement `json:"temp"`
	Origin string      `json:"origin,omitempty"` // row id / source marker from import
}

// DriftPoint is one baseline drift calibration point: mass offset over time.
type DriftPoint struct {
	Index  int         `json:"index"`
	Time   Measurement `json:"time"`
	Offset Measurement `json:"offset"` // signed balance offset, mg
}

// ProgramSegment is one furnace program segment (hold or ramp).
type ProgramSegment struct {
	Index     int         `json:"index"`
	Kind      string      `json:"kind"` // "hold" | "ramp"
	StartTemp Measurement `json:"start_temp"`
	EndTemp   Measurement `json:"end_temp"`
	StartTime Measurement `json:"start_time"`
	EndTime   Measurement `json:"end_time"`
	Rate      Measurement `json:"rate,omitempty"` // ramp rate K/min
}

// GasValveEvent is a logged atmosphere valve transition.
type GasValveEvent struct {
	Index   int         `json:"index"`
	Time    Measurement `json:"time"` // valve switch time (furnace clock)
	FromGas string      `json:"from_gas"`
	ToGas   string      `json:"to_gas"`
	Flow    Measurement `json:"flow,omitempty"`
	Note    string      `json:"note,omitempty"`
}

// BlankPoint is one point of the empty-pan blank curve.
type BlankPoint struct {
	Index int         `json:"index"`
	Time  Measurement `json:"time"`
	Temp  Measurement `json:"temp"`
	Mass  Measurement `json:"mass"`
}

// InitialMass is the stratified sample initial mass declaration.
type InitialMass struct {
	Value      Measurement `json:"value"`
	Method     string      `json:"method"` // "balance_declared" | "first_sample" | "revised"
	Basis      string      `json:"basis"`  // coordinate basis, e.g. "wet" | "dry"
	Reason     string      `json:"reason,omitempty"`
	Supersedes string      `json:"supersedes,omitempty"` // parent version ID
}

// RawSubmission is one immutable version of one input layer.
type RawSubmission struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Kind      SourceKind      `json:"kind"`
	Version   int             `json:"version"`
	ParentID  string          `json:"parent_id,omitempty"`
	Payload   json.RawMessage `json:"payload"` // typed per Kind; kept verbatim
	Digest    string          `json:"digest"`
	ImportRef string          `json:"import_ref,omitempty"` // batch id of the import that produced it
}

// Params is the revisable algorithm parameter set. Revisions are new versions
// with a parent pointer; nothing is overwritten.
type Params struct {
	ID                 string  `json:"id"`
	ProjectID          string  `json:"project_id"`
	Version            int     `json:"version"`
	ParentID           string  `json:"parent_id,omitempty"`
	AlgorithmVersion   string  `json:"algorithm_version"`
	WindowSize         int     `json:"window_size"`          // DTG smoothing window (points)
	Prominence         float64 `json:"prominence"`           // min |DTG| peak prominence, mg/min
	NoiseThreshold     float64 `json:"noise_threshold"`      // non-monotonic noise threshold, mg
	GapThreshold       float64 `json:"gap_threshold"`        // sampling gap threshold, s
	AtmosDelay         float64 `json:"atmos_delay"`          // gas transport delay, s
	TempLag            float64 `json:"temp_lag"`             // sample-vs-furnace temperature lag, s
	OverlapSplit       bool    `json:"overlap_split"`        // emit overlapping phase candidates
	ForceSum100        bool    `json:"force_sum_100"`        // never set by default; phases need not sum to 100%
	InitialMassVersion string  `json:"initial_mass_version"` // pinned input version
	DriftVersion       string  `json:"drift_version"`
	BlankVersion       string  `json:"blank_version"`
	Reason             string  `json:"reason"`
	Digest             string  `json:"digest"`
}

// Fingerprint is the stable parameter fingerprint embedded in derived IDs.
func (p Params) Fingerprint() string { return p.Digest }

// CandidateStatus marks lifecycle of a phase/residue candidate.
type CandidateStatus string

const (
	CandProposed   CandidateStatus = "proposed"
	CandAccepted   CandidateStatus = "accepted"
	CandRejected   CandidateStatus = "rejected"
	CandConflicted CandidateStatus = "conflicted"
	CandSuperseded CandidateStatus = "superseded"
)

// Gap is a detected sampling gap: interpolation must never span it.
type Gap struct {
	StartIndex int     `json:"start_index"`
	EndIndex   int     `json:"end_index"`
	StartTime  float64 `json:"start_time"`
	EndTime    float64 `json:"end_time"`
	DurationS  float64 `json:"duration_s"`
	Reason     string  `json:"reason"`
}

// Diagnostics attached to points/rows that could not be fully normalized.
type Diagnostic struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
	KeptRaw bool   `json:"kept_raw"`
}

// NormalizedSeries is the derived, normalized TG/DTG series.
type NormalizedSeries struct {
	Times       []float64    `json:"times"`    // s, common basis
	MassMG      []float64    `json:"mass_mg"`  // drift/blank corrected
	MassPct     []float64    `json:"mass_pct"` // % of chosen initial mass
	TempC       []float64    `json:"temp_c"`
	DTG         []float64    `json:"dtg"` // mg/min, nil within gaps
	Gaps        []Gap        `json:"gaps"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Unknown     []int        `json:"unknown_indices"` // points left in place, not plottable numerically
	InterpMask  []bool       `json:"interp_mask"`     // true where value spans a gap (always false here)
}

// AtmosReaction is the mapped reaction-time interval of an atmosphere switch:
// valve time != sample reaction time because of transport delay / temp lag.
type AtmosReaction struct {
	ValveID      string  `json:"valve_id"`
	ValveTime    float64 `json:"valve_time"`
	ReactionTime float64 `json:"reaction_time"`
	FromGas      string  `json:"from_gas"`
	ToGas        string  `json:"to_gas"`
	DelayS       float64 `json:"delay_s"`
	UncertaintyS float64 `json:"uncertainty_s"`
}

// Peak is one DTG extremum. Overlapping peaks may share time ranges.
type Peak struct {
	ID         string   `json:"id"`
	Time       float64  `json:"time"`
	TempC      float64  `json:"temp_c"`
	DTG        float64  `json:"dtg"`
	Prominence float64  `json:"prominence"`
	OverlapIDs []string `json:"overlap_ids,omitempty"`
}

// PhaseCandidate is a proposed decomposition phase. Phases may overlap and
// their mass losses are not forced to sum to 100%.
type PhaseCandidate struct {
	ID             string          `json:"id"`
	Label          string          `json:"label"`
	StartTime      float64         `json:"start_time"`
	EndTime        float64         `json:"end_time"`
	StartTempC     float64         `json:"start_temp_c"`
	EndTempC       float64         `json:"end_temp_c"`
	PeakIDs        []string        `json:"peak_ids"`
	MassLossMG     float64         `json:"mass_loss_mg"`
	MassLossPct    float64         `json:"mass_loss_pct"`
	OverlapsWith   []string        `json:"overlaps_with,omitempty"`
	AtmosLinks     []string        `json:"atmos_links,omitempty"`
	Uncertainty    string          `json:"uncertainty,omitempty"` // "none" | "ambiguous_bounds" | ...
	Status         CandidateStatus `json:"status"`
	RejectedReason string          `json:"rejected_reason,omitempty"`
	DecisionIDs    []string        `json:"decision_ids,omitempty"`
	// AlternateBoundIDs enumerate all legal candidates when bounds cannot be
	// decided uniquely.
	AlternateBoundIDs []string `json:"alternate_bound_ids,omitempty"`
}

// CalcChainStep is one auditable calculation step.
type CalcChainStep struct {
	Order     int            `json:"order"`
	Op        string         `json:"op"`
	Inputs    map[string]any `json:"inputs"`
	Outputs   map[string]any `json:"outputs"`
	Unit      string         `json:"unit,omitempty"`
	SourceIDs []string       `json:"source_ids,omitempty"`
	Note      string         `json:"note,omitempty"`
}

// ResidueRate is a final residue result with an expandable calculation chain.
type ResidueRate struct {
	ID            string          `json:"id"`
	ValuePct      float64         `json:"value_pct"`
	WindowEnd     float64         `json:"window_end_time"`
	Status        CandidateStatus `json:"status"`
	ErrorPct      *float64        `json:"error_pct,omitempty"`
	Unknown       bool            `json:"unknown,omitempty"`
	UnknownReason string          `json:"unknown_reason,omitempty"`
	Chain         []CalcChainStep `json:"chain"`
	DecisionIDs   []string        `json:"decision_ids,omitempty"`
	AlternateIDs  []string        `json:"alternate_ids,omitempty"`
}

// DerivedNode is one content-addressed derived computation.
type DerivedNode struct {
	ID               string           `json:"id"`
	ProjectID        string           `json:"project_id"`
	AlgorithmVersion string           `json:"algorithm_version"`
	ParamVersion     string           `json:"param_version"`
	DirectSources    map[string]int   `json:"direct_sources"` // kind -> raw version
	Series           NormalizedSeries `json:"series"`
	Atmos            []AtmosReaction  `json:"atmos"`
	Peaks            []Peak           `json:"peaks"`
	Phases           []PhaseCandidate `json:"phases"`
	Residues         []ResidueRate    `json:"residues"`
	RuleEvents       []RuleEvent      `json:"rule_events"`
	Stale            bool             `json:"stale"`
	StaleReason      string           `json:"stale_reason,omitempty"`
	SupersededBy     string           `json:"superseded_by,omitempty"`
	Digest           string           `json:"digest"`
}

// RuleCode enumerates machine-readable judgments emitted by the domain.
type RuleCode string

const (
	RuleAtmosDelay          RuleCode = "ATMOSPHERE_DELAY_VALVE_NE_REACTION"
	RuleTempLag             RuleCode = "TEMPERATURE_LAG_FURNACE_NE_SAMPLE"
	RulePhasesOverlap       RuleCode = "PHASES_MAY_OVERLAP"
	RuleNoForcedSum         RuleCode = "PHASE_LOSS_NOT_FORCED_TO_100_PERCENT"
	RuleGapNoInterpolation  RuleCode = "GAP_NOT_SPANNED_BY_INTERPOLATION"
	RuleAmbiguousCandidates RuleCode = "AMBIGUOUS_ALL_LEGAL_CANDIDATES_KEPT"
	RuleDriftCorrection     RuleCode = "BALANCE_BASELINE_DRIFT_CORRECTED"
	RuleNoiseNonMonotonic   RuleCode = "NON_MONOTONIC_NOISE_FLAGGED"
	RuleInitialMassPinned   RuleCode = "INITIAL_MASS_VERSION_PINNED"
	RuleUnconvertibleKept   RuleCode = "UNCONVERTIBLE_VALUE_KEPT_IN_PLACE"
	RulePartialImport       RuleCode = "IMPORT_PARTIAL_FAILURE_QUARANTINED"
	RuleOverlapPeaks        RuleCode = "OVERLAPPING_DTG_PEAKS_SPLIT"
)

// RuleEvent is a machine-readable judgment with evidence pointers.
type RuleEvent struct {
	Code       RuleCode       `json:"code"`
	Severity   string         `json:"severity"` // info | warn | error
	Message    string         `json:"message"`
	Evidence   map[string]any `json:"evidence"`
	SubjectIDs []string       `json:"subject_ids"`
	SourceIDs  []string       `json:"source_ids"`
}

// DecisionVerdict enumerates human verdicts.
type DecisionVerdict string

const (
	VerdictAccept   DecisionVerdict = "accept"
	VerdictReject   DecisionVerdict = "reject"
	VerdictAdjust   DecisionVerdict = "adjust"
	VerdictConflict DecisionVerdict = "conflict"
	VerdictUndone   DecisionVerdict = "undone"
)

// Decision is a human action against a candidate/phase/residue.
type Decision struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"project_id"`
	Seq          int             `json:"seq"`
	TargetType   string          `json:"target_type"` // "phase" | "residue" | "candidate"
	TargetID     string          `json:"target_id"`
	Verdict      DecisionVerdict `json:"verdict"`
	Note         string          `json:"note"`
	Adjustments  map[string]any  `json:"adjustments,omitempty"`
	ConflictWith string          `json:"conflict_with,omitempty"`
	DerivedID    string          `json:"derived_id"` // pinned dependency version
	UndoneBy     string          `json:"undone_by,omitempty"`
	ParentEvent  string          `json:"parent_event,omitempty"`
}

// HistoryEvent is the append-only, hash-chained audit record.
type HistoryEvent struct {
	Seq       int64           `json:"seq"`
	ProjectID string          `json:"project_id"`
	Type      string          `json:"type"` // project_created|raw_committed|params_committed|derived|decision|undo|conflict|import_partial|export|project_archived
	Payload   json.RawMessage `json:"payload"`
	RefIDs    []string        `json:"ref_ids"`
	VirtualTS string          `json:"virtual_ts"`
	PrevHash  string          `json:"prev_hash"`
	Hash      string          `json:"hash"`
}

// Project is a review case.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedTS string `json:"created_ts"`
	Archived  bool   `json:"archived"`
	HeadSeq   int64  `json:"head_seq"`
}

// ImportReport describes an import batch, including partial failures.
type ImportReport struct {
	ImportRef string          `json:"import_ref"`
	Accepted  map[string]int  `json:"accepted"` // kind -> count
	Failed    []ImportFailure `json:"failed"`
	Partial   bool            `json:"partial"`
	ProjectID string          `json:"project_id,omitempty"`
}

// ImportFailure keeps the raw fragment, status and affected objects for a
// rejected layer/fragment of an import.
type ImportFailure struct {
	Kind        SourceKind `json:"kind"`
	Fragment    string     `json:"fragment"`
	Code        string     `json:"code"`
	Message     string     `json:"message"`
	AffectedIDs []string   `json:"affected_ids"`
	Status      string     `json:"status"` // "rejected" | "quarantined"
}
