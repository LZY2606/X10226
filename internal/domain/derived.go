package domain

// DerivedNode is any computed result. It is content-addressed from its direct
// sources, algorithm version and parameter fingerprint. A node is immutable;
// when inputs change a new node is produced and the old one becomes stale.
type DerivedNode struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"project_id"`
	Kind        DerivedKind  `json:"kind"`
	BatchID     string       `json:"batch_id"`
	Fingerprint Fingerprint  `json:"fingerprint"`
	// Direct provenance: version IDs, parameter set IDs and other derived nodes.
	SourceVersionIDs []string `json:"source_version_ids"`
	ParamIDs         []string `json:"param_ids"`
	InputNodeIDs     []string `json:"input_node_ids"`
	ProducedEventID  string   `json:"produced_event_id"`
	// Staleness is folded state, not stored on the node itself; the projection
	// marks the newest batch per lineage as current and earlier ones stale.
	Payload any `json:"payload"`
}

type DerivedKind string

const (
	DKNormalizedSeries DerivedKind = "normalized_series"
	DKCorrectedSeries  DerivedKind = "corrected_series"
	DKBlankSubtracted  DerivedKind = "blank_subtracted"
	DKDTGSeries        DerivedKind = "dtg_series"
	DKGapMap           DerivedKind = "gap_map"
	DKReactionWindow   DerivedKind = "reaction_window"
	DKPeak             DerivedKind = "dtg_peak"
	DKStage            DerivedKind = "stage"
	DKResidue          DerivedKind = "residue"
	DKInitMassChoice   DerivedKind = "init_mass_choice"
)

// CanonicalPoint is a derived, unit-normalized point. Values are NaN-safe:
// gaps are represented on GapMap, never by invented interpolated points.
type CanonicalPoint struct {
	Row   int     `json:"row"`
	TMin  float64 `json:"t_min"`
	TK    float64 `json:"t_k"`
	MassMg float64 `json:"mass_mg"`
	Present bool   `json:"present"` // false marks a missing sample (gap endpoint)
}

type SeriesPayload struct {
	Name   string           `json:"name"`
	Points []CanonicalPoint `json:"points"`
}

// Gap describes a missing acquisition interval. Interpolation is forbidden
// across gaps: derivatives and stages split at them.
type Gap struct {
	StartRow int     `json:"start_row"`
	EndRow   int     `json:"end_row"`
	T0Min    float64 `json:"t0_min"`
	T1Min    float64 `json:"t1_min"`
	Reason   string  `json:"reason"` // explicit_gap | time_anomaly | unparsable
}

type GapMapPayload struct {
	Gaps []Gap `json:"gaps"`
}

// DTGPoint keeps local monotonicity diagnostics as first-class data.
type DTGPoint struct {
	Row        int     `json:"row"`
	TMin       float64 `json:"t_min"`
	TK         float64 `json:"t_k"`
	DTGMgMin   float64 `json:"dtg_mg_min"`
	SegmentID  string  `json:"segment_id"` // bounded by gaps
	NonMonotonic bool  `json:"non_monotonic"`
	NoiseFlag  string  `json:"noise_flag,omitempty"` // positive_bounce | isolated_spike ...
}

type DTGPayload struct {
	Points []DTGPoint `json:"points"`
	Window int        `json:"window"` // smoothing window actually applied (0 = none)
}

// ReactionWindow expresses that a valve switch at t_v reaches the sample at
// t_v + delay, and the sample temperature lags the program by a separate
// thermal lag. Valve time therefore never equals reaction time.
type ReactionWindow struct {
	GasEventRow int     `json:"gas_event_row"`
	Gas         string  `json:"gas"`
	ValveTMin   float64 `json:"valve_t_min"`
	DelayMin    float64 `json:"delay_min"`
	ReachTMin   float64 `json:"reach_t_min"`
	ProgramTK   float64 `json:"program_t_k"` // program-set temperature at reach time
	ThermalLagK float64 `json:"thermal_lag_k"`
	SampleTK    float64 `json:"sample_t_k"`
}

type ReactionWindowsPayload struct {
	Windows []ReactionWindow `json:"windows"`
}

// Peak is a DTG extremum inside one gap-bounded segment.
type Peak struct {
	ID           string  `json:"id"`
	Row          int     `json:"row"`
	SegmentID    string  `json:"segment_id"`
	TMin         float64 `json:"t_min"`
	TK           float64 `json:"t_k"`
	DTGMgMin     float64 `json:"dtg_mg_min"`
	ProminenceMg float64 `json:"prominence_mg"`
	AmbiguousWith []string `json:"ambiguous_with,omitempty"` // overlapping peak IDs
}

type PeaksPayload struct {
	Peaks []Peak `json:"peaks"`
}

// Stage is a proposed decomposition interval. Stages may overlap (overlapping
// peaks / convolved processes) and their mass losses are never normalized to
// sum to 100%.
type Stage struct {
	ID            string   `json:"id"`
	StartRow      int      `json:"start_row"`
	EndRow        int      `json:"end_row"`
	StartTMin     float64  `json:"start_t_min"`
	EndTMin       float64  `json:"end_t_min"`
	StartTK       float64  `json:"start_t_k"`
	EndTK         float64  `json:"end_t_k"`
	PeakID        string   `json:"peak_id,omitempty"`
	GasContext    string   `json:"gas_context,omitempty"`
	MassLossMg    float64  `json:"mass_loss_mg"`
	MassLossPct   float64  `json:"mass_loss_pct"` // relative to chosen initial mass
	Overlaps      []string `json:"overlaps,omitempty"`
	// Candidate membership: when boundaries cannot be uniquely decided all legal
	// candidates are kept and carry their rejection reason / uncertainty.
	Status        StageStatus `json:"status"`
	RejectReason  string      `json:"reject_reason,omitempty"`
	UncertaintyPct float64   `json:"uncertainty_pct,omitempty"`
}

type StageStatus string

const (
	StageCandidate StageStatus = "candidate"
	StageAccepted  StageStatus = "accepted"
	StageRejected  StageStatus = "rejected"
	StageStale     StageStatus = "stale"
)

type StagesPayload struct {
	Stages []Stage `json:"stages"`
}

// CalcChainStep is one hop of an expandable residue calculation chain.
type CalcChainStep struct {
	Order   int      `json:"order"`
	Label   string   `json:"label"`
	NodeID  string   `json:"node_id,omitempty"`
	Value   string   `json:"value,omitempty"` // display value with unit
	Source  string   `json:"source,omitempty"`
	Unknown bool     `json:"unknown,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// Residue is the final (or at-a-boundary) residual mass fraction. Unknown state
// is a first-class outcome.
type Residue struct {
	ID              string          `json:"id"`
	AtTMin          float64         `json:"at_t_min"`
	AtTK            float64         `json:"at_t_k"`
	ResidualMassMg  float64         `json:"residual_mass_mg"`
	InitialMassMg   float64         `json:"initial_mass_mg"`
	ResidualPct     float64         `json:"residual_pct"`
	Unknown         bool            `json:"unknown"`
	UnknownReason   string          `json:"unknown_reason,omitempty"`
	ErrorMg         float64         `json:"error_mg,omitempty"`
	Chain           []CalcChainStep `json:"chain"`
	StageIDs        []string        `json:"stage_ids,omitempty"`
	InitMassVersion string          `json:"init_mass_version"`
}

type ResiduesPayload struct {
	Residues []Residue `json:"residues"`
}

// InitMassChoice records which weighing record underpins percentages and why.
type InitMassChoice struct {
	VersionID string  `json:"version_id"`
	Row       int     `json:"row"`
	MassMg    float64 `json:"mass_mg"`
	Selected  bool    `json:"selected"`
	Reason    string  `json:"reason"`
}

type InitMassChoicesPayload struct {
	Choices []InitMassChoice `json:"choices"`
}

// CorrectedPoint carries per-point corrections with provenance-friendly detail.
type CorrectedPoint struct {
	Row        int     `json:"row"`
	TMin       float64 `json:"t_min"`
	TK         float64 `json:"t_k"`
	RawMassMg  float64 `json:"raw_mass_mg"`
	DriftMg    float64 `json:"drift_mg"`
	BlankMg    float64 `json:"blank_mg"`
	CorrectedMg float64 `json:"corrected_mg"`
	BlankApplied bool  `json:"blank_applied"`
	Note       string  `json:"note,omitempty"`
}

type CorrectedPayload struct {
	Points []CorrectedPoint `json:"points"`
}
