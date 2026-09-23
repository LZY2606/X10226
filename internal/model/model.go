// Package model defines the core domain types for the TGA stage
// decomposition / atmosphere event review system.
package model

// Layer names for imported raw inputs. Raw inputs are immutable once
// submitted; revisions create new versions linked by parent IDs.
const (
	LayerSamplePoints    = "sample_points"
	LayerDriftCorrection = "drift_correction"
	LayerFurnaceProgram  = "furnace_program"
	LayerGasEvents       = "gas_events"
	LayerBlankCurve      = "blank_curve"
	LayerInitialMass     = "initial_mass"
)

// Layers is the canonical ordered list of import layers.
var Layers = []string{
	LayerSamplePoints,
	LayerDriftCorrection,
	LayerFurnaceProgram,
	LayerGasEvents,
	LayerBlankCurve,
	LayerInitialMass,
}

// Import / derived / decision status values.
const (
	StatusOK      = "ok"
	StatusFailed  = "failed"
	StatusPartial = "partial"
	StatusCurrent = "current"
	StatusStale   = "stale"
)

// Derived object kinds.
const (
	DerivedTGCurve    = "tg_curve"
	DerivedAtmosphere = "atmosphere"
	DerivedStageSet   = "stage_set"
	DerivedResidue    = "residue"
)

// Event kinds. Judgment events are machine-readable records of the
// domain rules applied during computation.
const (
	EvProjectCreated     = "project.created"
	EvImportSubmitted    = "import.submitted"
	EvImportPartial      = "import.partial"
	EvLayerRevised       = "layer.revised"
	EvParamsAdjusted     = "params.adjusted"
	EvDerivedInvalidated = "derived.invalidated"
	EvDerivedComputed    = "derived.computed"
	EvCandidateAccepted  = "candidate.accepted"
	EvCandidateRejected  = "candidate.rejected"
	EvCandidateAdjusted  = "candidate.adjusted"
	EvDecisionConflict   = "decision.conflict"
	EvDecisionResolved   = "decision.resolved"
	EvDecisionUndone     = "decision.undone"
	EvScenarioTriggered  = "scenario.triggered"
	EvExported           = "export.created"
	// Machine-readable judgment events.
	EvJudgeAtmosphereDelay = "judgment.atmosphere_delay"
	EvJudgeStageOverlap    = "judgment.stage_overlap_allowed"
	EvJudgeNoForcedSum     = "judgment.no_forced_sum_100"
	EvJudgeGapNoInterp     = "judgment.gap_no_interpolation"
	EvGapDetected          = "tga.gap_detected"
	EvNonMonotonicRegion   = "tga.nonmonotonic_region"
	EvOverlapCandidates    = "tga.overlap_candidates"
)

// AlgoVersion identifies the computation pipeline version stamped on
// every derived value.
const AlgoVersion = "tga.v1"

// Params are the tunable inputs of the decomposition pipeline. The
// fingerprint of a Params value is recorded on every derived object.
type Params struct {
	SmoothWindow     int     `json:"smooth_window"`         // odd, DTG moving-average window in points
	MinProminence    float64 `json:"min_prominence_mg_min"` // min DTG peak prominence, mg/min
	GapThresholdS    float64 `json:"gap_threshold_s"`       // sampling gap threshold, s
	AtmosphereDelayS float64 `json:"atmosphere_delay_s"`    // gas valve -> sample atmosphere delay, s
	TempLagS         float64 `json:"temp_lag_s"`            // furnace temp lag at sample, s
	NoiseToleranceMg float64 `json:"noise_tolerance_mg"`    // non-monotonic noise tolerance, mg
	OverlapFrac      float64 `json:"overlap_frac"`          // interval overlap fraction triggering alternatives
}

// DefaultParams returns the shipped default parameter set.
func DefaultParams() Params {
	return Params{
		SmoothWindow:     5,
		MinProminence:    0.02,
		GapThresholdS:    30,
		AtmosphereDelayS: 25,
		TempLagS:         8,
		NoiseToleranceMg: 0.02,
		OverlapFrac:      0.2,
	}
}

// Event is one append-only entry in the project history.
type Event struct {
	Seq       int64  `json:"seq"`
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	VTime     int64  `json:"vtime"`
	Kind      string `json:"kind"`
	Actor     string `json:"actor"`
	Payload   string `json:"payload"` // JSON
}

// ImportVersion is one immutable version of a raw input layer.
type ImportVersion struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Layer       string `json:"layer"`
	Version     int    `json:"version"`
	ParentID    string `json:"parent_id"`
	BatchID     string `json:"batch_id"`
	Raw         string `json:"raw"`
	Format      string `json:"format"`
	Status      string `json:"status"` // ok | failed
	Diagnostics string `json:"diagnostics"`
	Seq         int64  `json:"seq"`
}

// Batch is one import submission possibly covering several layers.
type Batch struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Status    string `json:"status"` // ok | partial | failed
	Seq       int64  `json:"seq"`
}

// Derived is a computed, provenance-carrying result object.
type Derived struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Kind        string `json:"kind"`
	AlgoVersion string `json:"algo_version"`
	ParamFP     string `json:"param_fp"`
	SourceIDs   string `json:"source_ids"` // JSON array of import version IDs
	Status      string `json:"status"`     // current | stale
	Payload     string `json:"payload"`    // JSON
	CreatedSeq  int64  `json:"created_seq"`
	StaleSeq    int64  `json:"stale_seq"` // 0 = not stale
}

// Decision is a human ruling on a candidate.
type Decision struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	TargetID    string `json:"target_id"` // candidate ID
	Verdict     string `json:"verdict"`   // accept | reject | adjust
	Note        string `json:"note"`
	Actor       string `json:"actor"`
	Seq         int64  `json:"seq"`
	UndoneSeq   int64  `json:"undone_seq"`   // 0 = not undone
	InactiveSeq int64  `json:"inactive_seq"` // 0 = active (set by conflict resolution)
	Conflict    int    `json:"conflict"`
}

// ParamsVersion is one version of the pipeline parameters.
type ParamsVersion struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Params    string `json:"params"` // JSON of Params
	Seq       int64  `json:"seq"`
}

// Project is the top-level container.
type Project struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedSeq int64  `json:"created_seq"`
}
