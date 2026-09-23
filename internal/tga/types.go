package tga

// AlgoVersion identifies the derivation algorithm. Every derived value
// carries it so results can be replayed and compared across versions.
const AlgoVersion = "tga-algo/1.0.0"

// Layer names for the six stratified raw inputs.
const (
	LayerSamplePoints    = "sample_points"
	LayerDriftCorrection = "drift_correction"
	LayerFurnaceProgram  = "furnace_program"
	LayerGasEvents       = "gas_events"
	LayerBlankCurve      = "blank_curve"
	LayerInitialWeight   = "initial_weight"
)

// LayerOrder is the canonical display/import order of the raw layers.
var LayerOrder = []string{
	LayerSamplePoints,
	LayerDriftCorrection,
	LayerFurnaceProgram,
	LayerGasEvents,
	LayerBlankCurve,
	LayerInitialWeight,
}

// Params are the adjustable derivation parameters. They participate in the
// parameter fingerprint to identify exactly which parameter set produced a
// derived value.
type Params struct {
	GapThresholdS    float64 `json:"gap_threshold_s"`
	SmoothWindow     int     `json:"smooth_window"`
	ProminencePctS   float64 `json:"prominence_pct_per_s"`
	BaselineFrac     float64 `json:"baseline_frac"`
	OverlapFrac      float64 `json:"overlap_frac"`
	AtmosphereDelayS float64 `json:"atmosphere_delay_s"`
	ThermalLagS      float64 `json:"thermal_lag_s"`
}

// DefaultParams returns the factory parameter set.
func DefaultParams() Params {
	return Params{
		GapThresholdS:    5,
		SmoothWindow:     5,
		ProminencePctS:   0.05,
		BaselineFrac:     0.1,
		OverlapFrac:      0.5,
		AtmosphereDelayS: 45,
		ThermalLagS:      20,
	}
}

// RawInput is one immutable version of a raw layer. Revisions are stored as
// new rows pointing at their parent; submitted raw text is never overwritten.
type RawInput struct {
	ID        string `json:"id"`
	Layer     string `json:"layer"`
	Version   int    `json:"version"`
	ParentID  string `json:"parent_id,omitempty"`
	RawText   string `json:"raw_text"`
	EventSeq  int64  `json:"event_seq"`
	CreatedAt int64  `json:"created_at"`
}

// Event types.
const (
	EvImport        = "import"
	EvCompute       = "compute"
	EvParamAdjust   = "param_adjust"
	EvDecision      = "decision"
	EvConflict      = "conflict"
	EvUndo          = "undo"
	EvImportArchive = "import_archive"
)

// Event is the single source of truth. All visible state is derived by
// replaying events in sequence order.
type Event struct {
	Seq       int64  `json:"seq"`
	ID        string `json:"id"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
	CreatedAt int64  `json:"created_at"`
}

// ImportFailure records a layer that failed during a (possibly partial)
// import. Half-failed imports keep the good layers and quarantine the bad
// ones with diagnostics instead of aborting everything.
type ImportFailure struct {
	Layer string `json:"layer"`
	Error string `json:"error"`
}

// ImportPayload is the payload of EvImport. Kind is "baseline", "revision"
// or "partial".
type ImportPayload struct {
	Kind   string          `json:"kind"`
	Layers []RawInput      `json:"layers"`
	Failed []ImportFailure `json:"failed,omitempty"`
}

// DerivedValue is a computed artifact. It always carries the algorithm
// version, a parameter fingerprint and the direct raw sources it was
// derived from, so staleness can be scoped to actual dependencies.
type DerivedValue struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"` // corrected_curve, dtg_curve, stage_set, residue
	AlgoVersion string   `json:"algo_version"`
	Fingerprint string   `json:"fingerprint"`
	ParamFP     string   `json:"param_fp"`
	SourceIDs   []string `json:"source_ids"`
	EventSeq    int64    `json:"event_seq"`
	Payload     string   `json:"payload"` // JSON of the kind-specific payload
}

// Candidate is one stage-decomposition candidate inside a stage_set.
type Candidate struct {
	ID       string `json:"id"`
	SetID    string `json:"set_id"`
	Index    int    `json:"index"`
	Stage    Stage  `json:"stage"`
	EventSeq int64  `json:"event_seq"`
}

// ComputePayload is the payload of EvCompute.
type ComputePayload struct {
	Cause      string         `json:"cause"`
	Params     Params         `json:"params"`
	Derived    []DerivedValue `json:"derived"`
	Candidates []Candidate    `json:"candidates"`
	Judgments  []Judgment     `json:"judgments"`
}

// Judgment is a machine-readable record of a domain judgement made during
// derivation (e.g. atmosphere delay applied, gap not interpolated).
type Judgment struct {
	Code  string         `json:"code"`
	Basis map[string]any `json:"basis"`
}

// ParamAdjustPayload is the payload of EvParamAdjust.
type ParamAdjustPayload struct {
	Params    Params `json:"params"`
	Rationale string `json:"rationale"`
}

// DecisionPayload is the payload of EvDecision. Action is "accept" or
// "reject". Resolves optionally references a conflict event id.
type DecisionPayload struct {
	CandidateID string `json:"candidate_id"`
	Action      string `json:"action"`
	Rationale   string `json:"rationale"`
	Resolves    string `json:"resolves,omitempty"`
}

// ConflictPayload is the payload of EvConflict.
type ConflictPayload struct {
	CandidateID    string `json:"candidate_id"`
	ExistingStatus string `json:"existing_status"`
	IncomingAction string `json:"incoming_action"`
	Note           string `json:"note"`
}

// UndoPayload is the payload of EvUndo.
type UndoPayload struct {
	TargetSeq int64  `json:"target_seq"`
	Reason    string `json:"reason"`
}

// ImportArchivePayload is the payload of EvImportArchive.
type ImportArchivePayload struct {
	SourceSeq int64  `json:"source_seq"`
	Note      string `json:"note"`
}
