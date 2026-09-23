// Package domain defines the immutable TGA review model: raw sources,
// normalized layers, derived nodes, events and the review state machine.
//
// All domain quantities keep their original textual representation at the
// source layer; normalization happens only in derived layers. Values that
// cannot be converted stay in place with diagnostics rather than being
// dropped.
package domain

// Kind values for source layers (stratified persistence).
const (
	KindSamples    = "samples"
	KindDrift      = "drift_correction"
	KindProgram    = "furnace_program"
	KindGasEvents  = "gas_valve_events"
	KindBlank      = "blank_curve"
	KindInitWeight = "initial_mass"
)

// SourceKinds is the canonical import order of the six input strata.
var SourceKinds = []string{
	KindSamples, KindDrift, KindProgram, KindGasEvents, KindBlank, KindInitWeight,
}

// RawRow is one verbatim input row. The header carries per-column units or
// coordinate references. RowNo is 1-based over the provided payload so a
// source can always be audited back to its original representation.
type RawRow struct {
	RowNo int               `json:"row_no"`
	Cells map[string]string `json:"cells"`
}

// Source is an immutable, versioned direct input. Revisions append a new
// source whose ParentID points at the version it replaces.
type Source struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Kind      string          `json:"kind"`
	Version   int             `json:"version"`
	ParentID  string          `json:"parent_id,omitempty"`
	Filename  string          `json:"filename"`
	Header    []string        `json:"header"`
	Units     map[string]string `json:"units"`
	Rows      []RawRow        `json:"rows"`
	BatchID   string          `json:"batch_id,omitempty"`
	Diagnostics []Diagnostic  `json:"diagnostics"`
}

// Severity levels for diagnostics.
const (
	SevInfo    = "info"
	SevWarning = "warning"
	SevError   = "error"
)

// Diagnostic stays attached to the exact original value/location.
type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	RowNo    int    `json:"row_no,omitempty"`
	Column   string `json:"column,omitempty"`
	Value    string `json:"value,omitempty"`
	Message  string `json:"message"`
}

// Point is a normalized coordinate pair. Unit/Reference document the basis.
type Point struct {
	T         float64 `json:"t"`
	V         float64 `json:"v"`
	UnitX     string  `json:"unit_x"`
	UnitY     string  `json:"unit_y"`
	Reference string  `json:"reference,omitempty"`
}

// Gap marks an interval where samples exist on both sides but no
// interpolation is allowed across it.
type Gap struct {
	From float64 `json:"from"`
	To   float64 `json:"to"`
	Unit string  `json:"unit"`
	Reason string `json:"reason"`
}

// SampleLayer is the normalized sample series derived from raw rows.
type SampleLayer struct {
	Mass      []Point      `json:"mass"`
	Temp      []Point      `json:"temperature"`
	Gaps      []Gap        `json:"gaps"`
	NonMono   []NonMonotone `json:"non_monotone"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// NonMonotone records a mass uptick that is treated as noise, never as mass
// gain for stage decomposition.
type NonMonotone struct {
	T          float64 `json:"t"`
	Unit       string  `json:"unit"`
	Observed   float64 `json:"observed_mass"`
	MonotoneV  float64 `json:"monotone_mass"`
	Delta      float64 `json:"delta"`
}

// ProgramStep is one furnace program segment.
type ProgramStep struct {
	Index    int     `json:"index"`
	FromT    float64 `json:"from_t"`
	ToT      float64 `json:"to_t"`
	Rate     float64 `json:"rate"`
	UnitTemp string  `json:"unit_temp"`
	UnitRate string  `json:"unit_rate"`
}

// GasEvent is one valve event in its original time basis.
type GasEvent struct {
	Index   int    `json:"index"`
	AtT     float64 `json:"at_t"`
	FromGas string `json:"from_gas"`
	ToGas   string `json:"to_gas"`
	Unit    string `json:"unit"`
}

// InitWeight records the stratified initial sample mass.
type InitWeight struct {
	Mass        float64 `json:"mass"`
	Unit        string  `json:"unit"`
	OriginalRep string  `json:"original_representation"`
	Revised     bool    `json:"revised"`
	ParentID    string  `json:"parent_id,omitempty"`
}

// DriftPoint is one balance baseline drift correction anchor.
type DriftPoint struct {
	T      float64 `json:"t"`
	Offset float64 `json:"offset"`
	UnitX  string  `json:"unit_x"`
	UnitY  string  `json:"unit_y"`
}

// Parameters control the derived layers. Revisions create a new version and
// only fingerprint-affected nodes become stale.
type Parameters struct {
	Version             int     `json:"version"`
	GasDelaySeconds     float64 `json:"gas_delay_seconds"`
	TempLagSeconds      float64 `json:"temp_lag_seconds"`
	PeakWindowSeconds   float64 `json:"peak_window_seconds"`
	OverlapTolSeconds   float64 `json:"overlap_tol_seconds"`
	GapMaxSeconds       float64 `json:"gap_max_seconds"`
	MassUnit            string  `json:"mass_unit"`
	TimeUnit            string  `json:"time_unit"`
	TempUnit            string  `json:"temp_unit"`
	Fingerprint         string  `json:"fingerprint"`
}

// Node kinds produced by the derivation pipeline.
const (
	NodeNormSample  = "norm.sample"
	NodeNormProgram = "norm.program"
	NodeNormGas     = "norm.gas"
	NodeNormBlank   = "norm.blank"
	NodeNormDrift   = "norm.drift"
	NodeNormInit    = "norm.init_mass"
	NodeCorrected   = "corrected.mass"
	NodeDTG         = "dtg"
	NodeReactWindow = "reaction_windows"
	NodeStages      = "stages"
	NodeResidue     = "residue"
)

// Node is a versioned derived result with direct provenance: algorithm
// version, parameter fingerprint and the exact sources/nodes it used.
type Node struct {
	ID           string         `json:"id"`
	ProjectID    string         `json:"project_id"`
	Kind         string         `json:"kind"`
	AlgorithmVer string         `json:"algorithm_version"`
	ParamFp      string         `json:"param_fingerprint"`
	DirectSources []string      `json:"direct_sources"`
	DirectNodes  []string       `json:"direct_nodes"`
	Payload      map[string]any `json:"payload"`
	Diagnostics  []Diagnostic   `json:"diagnostics"`
}

// Candidate states in the review state machine.
const (
	CandProposed = "proposed"
	CandAccepted = "accepted"
	CandRejected = "rejected"
	CandConflict = "conflict"
	CandAdjusted = "adjusted"
	CandUnknown  = "unknown"
)

// Stage is one decomposition candidate. Stages may overlap and their mass
// losses are never forced to sum to 100%.
type Stage struct {
	ID              string   `json:"id"`
	WindowStart     float64  `json:"window_start"`
	WindowEnd       float64  `json:"window_end"`
	PeakT           float64  `json:"peak_t"`
	StartMass       *float64 `json:"start_mass"`
	EndMass         *float64 `json:"end_mass"`
	MassLoss        *float64 `json:"mass_loss"`
	MassLossPct     *float64 `json:"mass_loss_pct"`
	UnitTime        string   `json:"unit_time"`
	UnitMass        string   `json:"unit_mass"`
	State           string   `json:"state"`
	Ambiguous       bool     `json:"ambiguous"`
	CrossesGap      bool     `json:"crosses_gap"`
	LinkedGasEvent  *int     `json:"linked_gas_event,omitempty"`
	ValveT          *float64 `json:"valve_t,omitempty"`
	ReactionT       *float64 `json:"reaction_t,omitempty"`
	Candidates      []Stage  `json:"candidates,omitempty"`
	RejectReason    string   `json:"reject_reason,omitempty"`
	UnknownReason   string   `json:"unknown_reason,omitempty"`
	SourceNodeID    string   `json:"source_node_id"`
}

// CalcStep is one link in an expandable residue calculation chain.
type CalcStep struct {
	Label  string  `json:"label"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	NodeID string  `json:"node_id"`
	Op     string  `json:"op,omitempty"`
	Detail string  `json:"detail,omitempty"`
}

// Residue is the final residue result with a full calculation chain.
type Residue struct {
	ID           string     `json:"id"`
	ValuePct     float64    `json:"value_pct"`
	FinalMass    float64    `json:"final_mass"`
	InitialMass  float64    `json:"initial_mass"`
	Unit         string     `json:"unit"`
	State        string     `json:"state"`
	Chain        []CalcStep `json:"chain"`
	RejectReason string     `json:"reject_reason,omitempty"`
	SourceNodeID string     `json:"source_node_id"`
}

// Event kinds in the append-only history.
const (
	EvProjectCreated = "project.created"
	EvSourceImported = "source.imported"
	EvParamsRevised  = "params.revised"
	EvDerived        = "derived.computed"
	EvNodesStale     = "derived.stale"
	EvCandAccepted   = "candidate.accepted"
	EvCandRejected   = "candidate.rejected"
	EvCandAdjusted   = "candidate.adjusted"
	EvConflict       = "decision.conflict"
	EvConflictResolved = "decision.conflict_resolved"
	EvUndo           = "decision.undone"
	EvCaseJudgement  = "case.judgement"
)

// Event is an append-only history record. Snap holds the project snapshot at
// that point so any old event restores exact dependencies and conclusions.
type Event struct {
	Seq       int64          `json:"seq"`
	ProjectID string         `json:"project_id"`
	Kind      string         `json:"kind"`
	At        string         `json:"at"`
	Actor     string         `json:"actor"`
	Payload   map[string]any `json:"payload"`
	Snap      *Snapshot      `json:"snap,omitempty"`
}

// Snapshot is the replayable state at an event. Nodes embedded here are the
// exact derived versions visible then; revisiting an old event never shows
// newer recomputation results.
type Snapshot struct {
	Seq            int64                  `json:"seq"`
	Sources        []Source               `json:"sources"`
	Parameters     Parameters             `json:"parameters"`
	Nodes          []Node                 `json:"nodes"`
	StageOrder     []string               `json:"stage_order"`
	ResidueID      string                 `json:"residue_id"`
	CurrentNodeIDs []string               `json:"current_node_ids,omitempty"`
	Decisions      map[string]Decision    `json:"decisions,omitempty"`
	Flags          map[string]string      `json:"flags,omitempty"`
}

// Decision is a human verdict attached to a candidate id. Conflicts are
// first-class and resolution/undo append further events.
type Decision struct {
	TargetID      string         `json:"target_id"`
	State         string         `json:"state"`
	Actor         string         `json:"actor"`
	At            string         `json:"at"`
	Reason        string         `json:"reason,omitempty"`
	Adjustment    map[string]any `json:"adjustment,omitempty"`
	SupersedesID  string         `json:"supersedes_id,omitempty"`
	ConflictWith  string         `json:"conflict_with,omitempty"`
	GroupID       string         `json:"group_id,omitempty"`
	EventSeq      int64          `json:"event_seq"`
}

// Project is a review container.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	Seed      int64  `json:"seed"`
}

// StaleRecord marks a derived node superseded by newer inputs; it stays
// viewable but can never act as a current result.
type StaleRecord struct {
	NodeID   string `json:"node_id"`
	Reason   string `json:"reason"`
	SinceSeq int64  `json:"since_seq"`
}
