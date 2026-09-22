// Package domain holds the TGA (thermogravimetric) review domain model:
// immutable layered source inputs, derived analyses, stage decomposition,
// atmosphere evidence and review decisions.
//
// Conventions:
//   - Times are furnace/program times in seconds ("furnace time").
//   - Masses are balance readings in milligrams unless a layer states another
//     unit; temperatures are degrees Celsius.
//   - Original textual representations are retained on import; normalisation
//     happens only on the derived layer. Values that cannot be converted stay
//     in place with a Diagnostic instead of being dropped.
package domain

// Layer names for the six immutable source layers.
const (
	LayerSamplePoints = "sample_points"
	LayerDriftCorr    = "drift_correction"
	LayerFurnaceProg  = "furnace_program"
	LayerGasValves    = "gas_valves"
	LayerBlankCurve   = "blank_curve"
	LayerInitialMass  = "initial_mass"
)

// Layers is the canonical import order of the six source layers.
var Layers = []string{
	LayerSamplePoints,
	LayerDriftCorr,
	LayerFurnaceProg,
	LayerGasValves,
	LayerBlankCurve,
	LayerInitialMass,
}

// Severity levels for diagnostics.
const (
	SevError   = "error"
	SevWarning = "warning"
	SevInfo    = "info"
)

// Diagnostic records an import or computation problem without discarding the
// offending source representation.
type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Layer    string `json:"layer,omitempty"`
	Index    int    `json:"index,omitempty"`
	Field    string `json:"field,omitempty"`
}

// Value is a physical quantity. Raw is the exact imported representation;
// when Unit cannot be converted into the normalised target, Valid is false and
// Error carries the diagnostic while Raw remains in place.
type Value struct {
	Raw   string  `json:"raw"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Valid bool    `json:"valid"`
	Error string  `json:"error,omitempty"`
}

// CoordinateBasis documents the reference frame of a series.
type CoordinateBasis struct {
	TimeOrigin string `json:"time_origin"` // e.g. "furnace_program_start"
	TimeUnit   string `json:"time_unit"`   // "s"
	TempUnit   string `json:"temp_unit"`   // "degC"
	MassUnit   string `json:"mass_unit"`   // "mg"
}

// ---- Source layers ---------------------------------------------------------

// SamplePoint is one raw balance sample. A missing reading is encoded by
// Present=false (a sampling gap), not by interpolating neighbours.
type SamplePoint struct {
	Index   int    `json:"index"`
	TimeRaw string `json:"time_raw"`
	MassRaw string `json:"mass_raw"`
	TempRaw string `json:"temp_raw,omitempty"`
	Time    Value  `json:"time"`
	Mass    Value  `json:"mass"`
	Temp    Value  `json:"temp"`
	Present bool   `json:"present"`
}

// SamplePointsLayer is the raw trace.
type SamplePointsLayer struct {
	Basis  CoordinateBasis `json:"basis"`
	Points []SamplePoint   `json:"points"`
}

// DriftPoint anchors the blank/balance baseline drift model.
type DriftPoint struct {
	Index   int    `json:"index"`
	TimeRaw string `json:"time_raw"`
	MassRaw string `json:"mass_raw"`
	Time    Value  `json:"time"`
	Mass    Value  `json:"mass"`
}

// DriftCorrectionLayer declares the balance baseline-drift model, fit to the
// blank curve: corrected(t) = measured(t) - (slope*t + intercept).
type DriftCorrectionLayer struct {
	Model          string       `json:"model"` // "linear"
	SlopeRaw       string       `json:"slope_raw"`
	InterceptRaw   string       `json:"intercept_raw"`
	Slope          Value        `json:"slope"`     // mg/s
	Intercept      Value        `json:"intercept"` // mg
	FitAnchors     []DriftPoint `json:"fit_anchors,omitempty"`
	FitRMSE        Value        `json:"fit_rmse,omitempty"`
	AppliedToBlank string       `json:"applied_to_blank,omitempty"`
}

// FurnaceSegment is one program segment. RampRate degrees C/min; 0 means hold.
type FurnaceSegment struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	StartTRaw    string `json:"start_t_raw"`
	EndTRaw      string `json:"end_t_raw"`
	StartTempRaw string `json:"start_temp_raw"`
	EndTempRaw   string `json:"end_temp_raw"`
	StartT       Value  `json:"start_t"`
	EndT         Value  `json:"end_t"`
	StartTemp    Value  `json:"start_temp"`
	EndTemp      Value  `json:"end_temp"`
}

// FurnaceProgramLayer is the commanded furnace program.
type FurnaceProgramLayer struct {
	Segments []FurnaceSegment `json:"segments"`
}

// GasEvent is one gas valve switch in furnace time.
type GasEvent struct {
	Index   int    `json:"index"`
	TimeRaw string `json:"time_raw"`
	Action  string `json:"action"` // open|close|switch
	Gas     string `json:"gas"`    // N2, air, O2 ...
	Time    Value  `json:"time"`
}

// GasValvesLayer is the valve timeline.
type GasValvesLayer struct {
	Events []GasEvent `json:"events"`
}

// BlankCurveLayer is the empty-pan run used to evidence drift correction.
type BlankCurveLayer struct {
	Basis  CoordinateBasis `json:"basis"`
	Points []DriftPoint    `json:"points"`
}

// InitialMassLayer records the weighed sample initial mass. Revisions create a
// new version with ParentVersion; the original submission is never overwritten.
type InitialMassLayer struct {
	MassRaw       string `json:"mass_raw"`
	Mass          Value  `json:"mass"`
	Method        string `json:"method,omitempty"` // e.g. "external_balance"
	ParentVersion string `json:"parent_version,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

// LayerRef points at one committed source-layer version.
type LayerRef struct {
	Layer   string `json:"layer"`
	Version string `json:"version"`
}

// Params are analysis parameters (atmosphere delay, thermal lag, peak ...).
// Revisions are versioned and fingerprinted.
type Params struct {
	// AtmosphereDelayS maps valve time to the time gas composition change is
	// estimated to reach the sample: t_sample = t_valve + delay.
	AtmosphereDelayS float64 `json:"atmosphere_delay_s"`
	// ThermalLagDegC shifts sample temperature relative to furnace/programmed
	// temperature: T_sample = T_furnace - lag.
	ThermalLagDegC float64 `json:"thermal_lag_deg_c"`
	// DTGWindowS is the half-window of the robust central DTG smoother.
	DTGWindowS float64 `json:"dtg_window_s"`
	// MinProminenceMgPerS is the minimum |DTG| for a peak candidate.
	MinProminenceMgPerS float64 `json:"min_prominence_mg_per_s"`
	// MinSeparationS rejects duplicate maxima closer than this.
	MinSeparationS float64 `json:"min_separation_s"`
	// GapMaxS: gaps wider than this are never bridged by interpolation.
	GapMaxS float64 `json:"gap_max_s"`
	// SmoothPasses is the number of central smoothing passes for DTG.
	SmoothPasses int `json:"smooth_passes"`
	// BlankDriftToleranceMg warns when blank fit residual exceeds it.
	BlankDriftToleranceMg float64 `json:"blank_drift_tolerance_mg"`
	// NonMonotonicThresholdMg flags jitter that is not a real mass gain.
	NonMonotonicThresholdMg float64 `json:"nonmonotonic_threshold_mg"`
}

// DefaultParams returns the algorithm v1 default parameters.
func DefaultParams() Params {
	return Params{
		AtmosphereDelayS:        18,
		ThermalLagDegC:          9,
		DTGWindowS:              30,
		MinProminenceMgPerS:     0.0004,
		MinSeparationS:          45,
		GapMaxS:                 20,
		SmoothPasses:            2,
		BlankDriftToleranceMg:   0.12,
		NonMonotonicThresholdMg: 0.05,
	}
}

// ---- Derived artifacts ------------------------------------------------------

// Gap is a detected sampling gap. Span is the missing time interval between the
// last present point before and the first present point after.
type Gap struct {
	ID          string  `json:"id"`
	StartT      float64 `json:"start_t"`
	EndT        float64 `json:"end_t"`
	WidthS      float64 `json:"width_s"`
	Bridged     bool    `json:"bridged"`
	Reason      string  `json:"reason"`
	BeforeIndex int     `json:"before_index"`
	AfterIndex  int     `json:"after_index"`
}

// NoiseFlag marks a non-monotonic jitter that exceeds the noise threshold.
type NoiseFlag struct {
	ID      string  `json:"id"`
	Index   int     `json:"index"`
	Time    float64 `json:"time"`
	DeltaMg float64 `json:"delta_mg"`
	Message string  `json:"message"`
}

// CorrectedTrace is the mass trace after baseline-drift correction, with
// gaps preserved (no interpolation across them).
type CorrectedTrace struct {
	Times        []float64   `json:"times"`
	RawMg        []float64   `json:"raw_mg"`
	CorrectedMg  []float64   `json:"corrected_mg"`
	TempC        []float64   `json:"temp_c"`
	Present      []bool      `json:"present"`
	Gaps         []Gap       `json:"gaps"`
	NoiseFlags   []NoiseFlag `json:"noise_flags"`
	DriftApplied bool        `json:"drift_applied"`
}

// DTGPoint is one derivative point. DTG is mg/s (negative during loss).
type DTGPoint struct {
	Time      float64 `json:"time"`
	TempC     float64 `json:"temp_c"`
	DTGMgPerS float64 `json:"dtg_mg_per_s"`
	AcrossGap bool    `json:"across_gap"`
}

// Peak is a DTG minimum (a mass-loss peak).
type Peak struct {
	ID           string   `json:"id"`
	Time         float64  `json:"time"`
	TempC        float64  `json:"temp_c"`
	DTGMgPerS    float64  `json:"dtg_mg_per_s"`
	Prominence   float64  `json:"prominence"`
	OverlapsWith []string `json:"overlaps_with,omitempty"`
}

// AtmosphereReach maps a valve event to the estimated time/temperature at
// which the gas change actually reaches the sample.
type AtmosphereReach struct {
	ValveIndex int     `json:"valve_index"`
	ValveTime  float64 `json:"valve_time"`
	ReachTime  float64 `json:"reach_time"`
	ReachTempC float64 `json:"reach_temp_c"`
	Action     string  `json:"action"`
	Gas        string  `json:"gas"`
}

// StageCandidate is one possible decomposition stage. Stages may overlap and
// their percentage losses are relative to the stage entry mass; they are NOT
// normalised to sum to 100%.
type StageCandidate struct {
	ID             string        `json:"id"`
	Label          string        `json:"label"`
	StartT         float64       `json:"start_t"`
	EndT           float64       `json:"end_t"`
	StartTempC     float64       `json:"start_temp_c"`
	EndTempC       float64       `json:"end_temp_c"`
	StartMg        float64       `json:"start_mg"`
	EndMg          float64       `json:"end_mg"`
	LossMg         float64       `json:"loss_mg"`
	LossPercent    float64       `json:"loss_percent"` // relative to stage entry mass
	PeakIDs        []string      `json:"peak_ids"`
	OverlapsStages []string      `json:"overlaps_stages,omitempty"`
	GasAtStart     string        `json:"gas_at_start"`
	GasAtEnd       string        `json:"gas_at_end"`
	BoundaryGap    bool          `json:"boundary_gap"`
	Ambiguous      bool          `json:"ambiguous"`
	Alternatives   []Alternative `json:"alternatives,omitempty"`
	RejectReason   string        `json:"reject_reason,omitempty"`
}

// Alternative is a legal but non-unique boundary kept when a stage cannot be
// decided uniquely.
type Alternative struct {
	Boundary string  `json:"boundary"` // "start" | "end"
	Time     float64 `json:"time"`
	TempC    float64 `json:"temp_c"`
	Reason   string  `json:"reason"`
}

// CalcChainStep is one auditable step of a residual-percent calculation.
type CalcChainStep struct {
	Order   int     `json:"order"`
	Op      string  `json:"op"`
	Detail  string  `json:"detail"`
	ValueMg float64 `json:"value_mg,omitempty"`
	Time    float64 `json:"time,omitempty"`
	Source  string  `json:"source"` // stable artifact/field identity
}

// Residue is the final residual fraction, with a fully expandable calc chain.
type Residue struct {
	ID            string          `json:"id"`
	Label         string          `json:"label"`
	FinalT        float64         `json:"final_t"`
	FinalTempC    float64         `json:"final_temp_c"`
	FinalMg       float64         `json:"final_mg"`
	InitialMg     float64         `json:"initial_mg"`
	Percent       float64         `json:"percent"`
	Status        string          `json:"status"` // ok | unknown | gap_bounded
	UnknownReason string          `json:"unknown_reason,omitempty"`
	Chain         []CalcChainStep `json:"chain"`
}

// Judgement is a machine-readable conclusion emitted by analysis. It encodes
// the domain rules (atmosphere delay, non-summing stages, no interpolation
// across gaps, preserved candidates) so reviewers and tests can read them.
type Judgement struct {
	Code     string         `json:"code"`
	Severity string         `json:"severity"`
	Message  string         `json:"message"`
	Subject  string         `json:"subject,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

// AnalysisBundle is one published derived version. It is immutable; a new
// bundle supersedes older ones for affected subjects but old ones remain
// viewable as history.
type AnalysisBundle struct {
	Version          string            `json:"version"`
	Algorithm        string            `json:"algorithm"`
	ParamVersion     string            `json:"param_version"`
	ParamFingerprint string            `json:"param_fingerprint"`
	SourceLayers     []LayerRef        `json:"source_layers"`
	Trace            CorrectedTrace    `json:"trace"`
	DTG              []DTGPoint        `json:"dtg"`
	Peaks            []Peak            `json:"peaks"`
	Atmosphere       []AtmosphereReach `json:"atmosphere"`
	Stages           []StageCandidate  `json:"stages"`
	Residues         []Residue         `json:"residues"`
	Judgements       []Judgement       `json:"judgements"`
	Diagnostics      []Diagnostic      `json:"diagnostics"`
}

// ---- Review decisions -------------------------------------------------------

// Decision kinds.
const (
	DecisionAccept   = "accept"
	DecisionReject   = "reject"
	DecisionAdjust   = "adjust"
	DecisionConflict = "conflict"
	DecisionUndo     = "undo"
	DecisionRollback = "rollback"
)

// Target kinds for review decisions.
const (
	TargetStage   = "stage"
	TargetPeak    = "peak"
	TargetResidue = "residue"
	TargetBundle  = "bundle"
)

// Decision is one human review act. Decisions are immutable events; undo and
// conflict resolution add new decisions rather than deleting history.
type Decision struct {
	Kind          string `json:"kind"`
	TargetKind    string `json:"target_kind"`
	TargetID      string `json:"target_id"`
	BundleVersion string `json:"bundle_version"`
	Comment       string `json:"comment,omitempty"`
	Reviewer      string `json:"reviewer,omitempty"`
	// Adjust fields for kind=adjust.
	ParamName string  `json:"param_name,omitempty"`
	OldValue  float64 `json:"old_value,omitempty"`
	NewValue  float64 `json:"new_value,omitempty"`
	// For undo/rollback.
	UndoneEventSeq int64 `json:"undone_event_seq,omitempty"`
	RollbackSeq    int64 `json:"rollback_seq,omitempty"`
	// Conflict bookkeeping.
	ConflictsWith string `json:"conflicts_with,omitempty"`
	ResolvedBy    string `json:"resolved_by,omitempty"`
}
