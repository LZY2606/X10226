package domain

// Proposal status from review decisions.
const (
	StatusPending  = "pending"
	StatusAccepted = "accepted"
	StatusRejected = "rejected"
	StatusAdjusted = "adjusted"
	StatusConflict = "conflicted"
)

// Machine-readable rejection / uncertainty reasons.
const (
	ReasonOverlapUnresolved       = "overlapping_peaks_unresolved"
	ReasonCrossesGap              = "stage_crosses_data_gap"
	ReasonNoiseNonMonotonic       = "non_monotonic_noise_dominant"
	ReasonBelowNoiseFloor         = "loss_below_noise_floor"
	ReasonGasAttributionAmbiguous = "gas_attribution_ambiguous"
	ReasonUnbounded               = "stage_boundary_unbounded"
)

// StageProposal is one legal candidate decomposition of a mass-loss region.
// Stages may overlap and their losses are never forced to sum to 100%.
type StageProposal struct {
	ID               string   `json:"id"`
	RegionID         string   `json:"region_id"`
	Variant          string   `json:"variant"` // single | split_a_b | overlap_split
	Stages           []Stage  `json:"stages"`
	RejectionReasons []string `json:"rejection_reasons,omitempty"`
	Unknowns         []string `json:"unknowns,omitempty"`
	Status           string   `json:"status"`
	Manual           bool     `json:"manual,omitempty"`
	ParentID         string   `json:"parent_id,omitempty"`
	DecisionSeq      int64    `json:"decision_seq,omitempty"`
}

type Stage struct {
	ID              string  `json:"id"`
	StartS          float64 `json:"start_s"`
	EndS            float64 `json:"end_s"`
	PeakS           float64 `json:"peak_s,omitempty"`
	PeakTempC       float64 `json:"peak_temp_c,omitempty"`
	StartTempC      float64 `json:"start_temp_c,omitempty"`
	EndTempC        float64 `json:"end_temp_c,omitempty"`
	LossMg          float64 `json:"loss_mg"`
	LossPercent     float64 `json:"loss_percent"` // vs current initial mass
	GasAtStart      string  `json:"gas_at_start,omitempty"`
	GasAtPeak       string  `json:"gas_at_peak,omitempty"`
	GasEventID      string  `json:"gas_event_id,omitempty"`
	AttributionNote string  `json:"attribution_note,omitempty"`
	CrossesGap      bool    `json:"crosses_gap,omitempty"`
}

type CandidateSet struct {
	Regions []MassLossRegion `json:"regions"`
}

type MassLossRegion struct {
	ID         string          `json:"id"`
	StartS     float64         `json:"start_s"`
	EndS       float64         `json:"end_s"`
	PeakS      float64         `json:"peak_s"`
	PeakTempC  float64         `json:"peak_temp_c"`
	Overlap    bool            `json:"overlap"`
	CrossesGap bool            `json:"crosses_gap"`
	Proposals  []StageProposal `json:"proposals"`
}

// ChainNode is one step in the expandable residual calculation chain.
type ChainNode struct {
	Step         string      `json:"step"`
	Description  string      `json:"description"`
	ValueMg      float64     `json:"value_mg,omitempty"`
	ValuePercent float64     `json:"value_percent,omitempty"`
	Unit         string      `json:"unit,omitempty"`
	SourceID     string      `json:"source_id,omitempty"`
	ArtifactID   string      `json:"artifact_id,omitempty"`
	Children     []ChainNode `json:"children,omitempty"`
}

type Residual struct {
	InitialMassMg   float64     `json:"initial_mass_mg"`
	FinalMassMg     float64     `json:"final_mass_mg"`
	FinalTempC      float64     `json:"final_temp_c"`
	FinalTimeS      float64     `json:"final_time_s"`
	ResidualPercent float64     `json:"residual_percent"`
	Chain           []ChainNode `json:"chain"`
}

type MassBalance struct {
	InitialMassMg      float64 `json:"initial_mass_mg"`
	FinalMassMg        float64 `json:"final_mass_mg"`
	AcceptedLossMg     float64 `json:"accepted_loss_mg"`
	UnaccountedLossMg  float64 `json:"unaccounted_loss_mg"`
	UnaccountedPercent float64 `json:"unaccounted_percent"`
	AcceptedPercent    float64 `json:"accepted_percent"`
	ForcedTo100        bool    `json:"forced_to_100"` // always false; provenance
	Note               string  `json:"note"`
}

// Principle codes written as machine-readable events in the counterexample
// archive.
const (
	PrincipleValveNotReaction  = "valve_time_not_reaction_time"
	PrincipleStagesOverlap     = "stages_may_overlap_not_sum_100"
	PrincipleNoGapInterp       = "no_interpolation_across_gap"
	PrincipleKeepAllCandidates = "keep_all_legal_candidates_when_indeterminate"
)
