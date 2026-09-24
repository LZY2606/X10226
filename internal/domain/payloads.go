package domain

// ProjectCreatedPayload creates a review project.
type ProjectCreatedPayload struct {
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
}

// SourceCommittedPayload stores one immutable import batch. Original inputs
// are never overwritten: a batch may be a parent-linked revision.
type SourceCommittedPayload struct {
	BatchID     string       `json:"batch_id"`
	Role        string       `json:"role"` // base | initial_revision | blank_patch | partial
	ParentBatch string       `json:"parent_batch,omitempty"`
	FileName    string       `json:"file_name"`
	MediaType   string       `json:"media_type"`
	Sections    []Section    `json:"sections"`
	RawCells    []RawCell    `json:"raw_cells"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Partial     bool         `json:"partial"`
}

// ParamsRevisedPayload records a named parameter version.
type ParamsRevisedPayload struct {
	Version string `json:"version"`
	Parent  string `json:"parent,omitempty"`
	Params  Params `json:"params"`
	Reason  string `json:"reason,omitempty"`
}

// Params are the review-controllable analysis parameters.
type Params struct {
	// GasDelayS is the transport delay between valve switch and arrival at
	// the sample. Valve time therefore does not equal reaction time.
	GasDelayS float64 `json:"gas_delay_s"`
	// ThermalTauS models sample-temperature lag.
	ThermalTauS float64 `json:"thermal_tau_s"`
	// Drift correction mode: "blank" subtracts interpolated blank curve;
	// "linear" fits a slope to the blank program; "none" disables it.
	DriftMode string `json:"drift_mode"`
	// MinPeakProminencePct is the minimum DTG prominence, in %/s.
	MinPeakProminencePct float64 `json:"min_peak_prominence_pct_per_s"`
	// GapMaxStepS: a time gap larger than this splits the series and is
	// never bridged by interpolation.
	GapMaxStepS float64 `json:"gap_max_step_s"`
	// NoiseMonotonicEpsPct flags decreases larger than this (percent of
	// initial mass) as non-monotonic noise instead of real mass loss.
	NoiseMonotonicEpsPct float64 `json:"noise_monotonic_eps_pct"`
}

// DefaultParams returns the v0 baseline parameters.
func DefaultParams() Params {
	return Params{
		GasDelayS:            30,
		ThermalTauS:          12,
		DriftMode:            "linear",
		MinPeakProminencePct: 0.01,
		GapMaxStepS:          25,
		NoiseMonotonicEpsPct: 0.25,
	}
}

// Point is one normalized sampling point (derived layer).
type Point struct {
	ID              string   `json:"id"`
	FurnaceTimeS    float64  `json:"furnace_time_s"`
	ProgramTempC    *float64 `json:"program_temp_c,omitempty"`
	SampleTempC     *float64 `json:"sample_temp_c,omitempty"`
	RawMassMg       *float64 `json:"raw_mass_mg,omitempty"`
	CorrectedMassMg *float64 `json:"corrected_mass_mg,omitempty"`
	DTGPctPerS      *float64 `json:"dtg_pct_per_s,omitempty"`
	Gas             string   `json:"gas,omitempty"`
	GasLabel        string   `json:"gas_label,omitempty"`
	Segment         int      `json:"segment"`
	NonMonotonic    bool     `json:"non_monotonic,omitempty"`
	GapAfter        bool     `json:"gap_after,omitempty"`
	Notes           []string `json:"notes,omitempty"`
}

// ProgramSegment is a furnace program ramp/hold segment.
type ProgramSegment struct {
	ID     string  `json:"id"`
	Kind   string  `json:"kind"` // ramp | hold
	StartS float64 `json:"start_s"`
	EndS   float64 `json:"end_s"`
	StartT float64 `json:"start_temp_c"`
	EndT   float64 `json:"end_temp_c"`
}

// GasWindow is an effective atmosphere interval at the sample. Valve times
// (ValveStartS/ValveEndS) are shifted by gas delay to reaction-relative
// times (StartS/EndS).
type GasWindow struct {
	ID          string  `json:"id"`
	Gas         string  `json:"gas"`
	Label       string  `json:"label"`
	ValveStartS float64 `json:"valve_start_s"`
	ValveEndS   float64 `json:"valve_end_s"`
	StartS      float64 `json:"start_s"`
	EndS        float64 `json:"end_s"`
}

// Peak is a DTG extremum.
type Peak struct {
	ID             string  `json:"id"`
	Index          int     `json:"index"`
	TimeS          float64 `json:"time_s"`
	SampleTempC    float64 `json:"sample_temp_c"`
	DTGPctPerS     float64 `json:"dtg_pct_per_s"`
	Prominence     float64 `json:"prominence_pct_per_s"`
	Segment        int     `json:"segment"`
	BelowThreshold bool    `json:"below_threshold,omitempty"`
	GroupID        string  `json:"group_id,omitempty"`
}

// Stage is a derived mass-loss interval.
type Stage struct {
	ID           string   `json:"id"`
	Index        int      `json:"index"`
	StartS       float64  `json:"start_s"`
	EndS         float64  `json:"end_s"`
	StartTempC   float64  `json:"start_temp_c"`
	EndTempC     float64  `json:"end_temp_c"`
	MassLossMg   float64  `json:"mass_loss_mg"`
	LossPct      float64  `json:"loss_pct"`
	PeakIDs      []string `json:"peak_ids"`
	GasID        string   `json:"gas_id,omitempty"`
	OverlapsWith []string `json:"overlaps_with,omitempty"`
	Segment      int      `json:"segment"`
}

// Candidate is a legal stage-decomposition proposal. Multiple candidates may
// coexist; rejected ones carry an explicit reason.
type Candidate struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`   // merged | separated | single | human
	Status         string   `json:"status"` // legal | rejected | unresolved
	StageIDs       []string `json:"stage_ids"`
	PeakIDs        []string `json:"peak_ids"`
	RejectReason   string   `json:"reject_reason,omitempty"`
	UnresolvedNote string   `json:"unresolved_note,omitempty"`
	Origin         string   `json:"origin"` // algorithm | human
}

// Residue is a residual-rate result with its full calculation chain.
type Residue struct {
	ID            string     `json:"id"`
	AtTimeS       float64    `json:"at_time_s"`
	ResidualPct   *float64   `json:"residual_pct"`
	Unknown       bool       `json:"unknown,omitempty"`
	UnknownReason string     `json:"unknown_reason,omitempty"`
	Chain         []CalcStep `json:"chain"`
	DependsOn     []string   `json:"depends_on"`
}

// Finding is a machine-readable domain judgment (see Find* constants).
type Finding struct {
	Code     string                 `json:"code"`
	Severity string                 `json:"severity"` // info | warning | error
	Message  string                 `json:"message"`
	Evidence map[string]interface{} `json:"evidence,omitempty"`
}

// SeriesBundle groups derived series; IDs are content-addressed.
type SeriesBundle struct {
	Points         []Point          `json:"points"`
	Segments       [][]int          `json:"segments"`
	GasWindows     []GasWindow      `json:"gas_windows"`
	FurnaceProgram []ProgramSegment `json:"furnace_program"`
}

// AnalysisRunPayload is the full derived state of one algorithm run.
type AnalysisRunPayload struct {
	RunID             string       `json:"run_id"`
	AlgorithmVersion  string       `json:"algorithm_version"`
	ParamVersion      string       `json:"param_version"`
	ParamFingerprint  string       `json:"param_fingerprint"`
	SourceBatches     []string     `json:"source_batches"`
	InitialMass       Quantity     `json:"initial_mass"`
	InitialMassSource SourceRef    `json:"initial_mass_source"`
	Series            SeriesBundle `json:"series"`
	Peaks             []Peak       `json:"peaks"`
	Stages            []Stage      `json:"stages"`
	Candidates        []Candidate  `json:"candidates"`
	Residues          []Residue    `json:"residues"`
	Findings          []Finding    `json:"findings"`
	Diagnostics       []Diagnostic `json:"diagnostics"`
	Unconvertibles    []RawCell    `json:"unconvertibles"`
	TotalLossPct      float64      `json:"total_loss_pct"`
	StagesSumTo100    bool         `json:"stages_sum_to_100"`
}

// DecisionRecordedPayload records accept/reject/adjust on a candidate.
type DecisionRecordedPayload struct {
	DecisionID  string `json:"decision_id"`
	RunID       string `json:"run_id"`
	CandidateID string `json:"candidate_id"`
	Action      string `json:"action"` // accept | reject | adjust
	Note        string `json:"note,omitempty"`
	ConflictID  string `json:"conflict_id,omitempty"`
}

// AdjustmentRecordedPayload records a reviewer-specified stage boundary.
type AdjustmentRecordedPayload struct {
	AdjustmentID      string  `json:"adjustment_id"`
	RunID             string  `json:"run_id"`
	ParentCandidateID string  `json:"parent_candidate_id,omitempty"`
	StartS            float64 `json:"start_s"`
	EndS              float64 `json:"end_s"`
	Reason            string  `json:"reason,omitempty"`
	HumanCandidateID  string  `json:"human_candidate_id"`
}

// ConflictNotedPayload records contradictory human decisions.
type ConflictNotedPayload struct {
	ConflictID  string   `json:"conflict_id"`
	CandidateID string   `json:"candidate_id"`
	DecisionIDs []string `json:"decision_ids"`
	Summary     string   `json:"summary"`
}

// ConflictResolvedPayload records the winning decision after a conflict.
type ConflictResolvedPayload struct {
	ConflictID       string `json:"conflict_id"`
	WinnerDecisionID string `json:"winner_decision_id"`
	Resolution       string `json:"resolution"`
}

// UndoRecordedPayload expresses undo as a new event, never a deletion.
type UndoRecordedPayload struct {
	UndoneSeq int    `json:"undone_seq"`
	Reason    string `json:"reason"`
}
