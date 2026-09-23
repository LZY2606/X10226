// Package domain defines the immutable core model of the thermogravimetric
// review system: raw layered inputs, derived runs, stage candidates, residue
// evidence chains and the append-only event history.
//
// Every domain quantity keeps its original representation plus a normalized
// value in SI/base units; values that cannot be converted stay in place and
// carry diagnostics instead of being dropped.
package domain

import "time"

// Layers (strata) in which submitted input is kept separately.
const (
	LayerSample      = "sample_points" // raw mass/temperature/furnace-time sampling
	LayerDrift       = "drift_corr"    // balance drift correction
	LayerProgram     = "furnace_program"
	LayerGas         = "gas_valve"    // gas valve events
	LayerBlank       = "blank_curve"  // blank/baseline curve
	LayerInitialMass = "initial_mass" // sample initial mass strata
)

// AllLayers lists the import strata in their canonical order.
func AllLayers() []string {
	return []string{LayerSample, LayerDrift, LayerProgram, LayerGas, LayerBlank, LayerInitialMass}
}

// Frame names: which coordinate a value is anchored to.
const (
	FrameFurnaceTime = "furnace_time_s"
	FrameSampleTime  = "sample_reaction_time_s"
	FrameTemperature = "temperature_C"
)

// Quantity is a measurement that preserves how it was written.
type Quantity struct {
	Raw        string  `json:"raw"`
	Unit       string  `json:"unit"`
	Value      float64 `json:"value"`
	Converted  bool    `json:"converted"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

// Point is one raw sample observation. Unknown/unconvertible fields stay on
// the point with diagnostics instead of being dropped or invented.
type Point struct {
	Index        int      `json:"index"`
	TimeRaw      string   `json:"time_raw"`
	TimeUnit     string   `json:"time_unit"`
	TimeS        *float64 `json:"time_s,omitempty"`
	MassRaw      string   `json:"mass_raw"`
	MassUnit     string   `json:"mass_unit"`
	MassG        *float64 `json:"mass_g,omitempty"`
	TempRaw      string   `json:"temp_raw"`
	TempUnit     string   `json:"temp_unit"`
	TempC        *float64 `json:"temp_c,omitempty"`
	Diagnostics  []string `json:"diagnostics,omitempty"`
	NonMonotonic bool     `json:"non_monotonic,omitempty"`
	Noise        bool     `json:"noise,omitempty"`
}

// BlankPoint is one point of the blank (empty-pan) baseline curve.
type BlankPoint struct {
	Index       int      `json:"index"`
	TimeRaw     string   `json:"time_raw"`
	TimeUnit    string   `json:"time_unit"`
	TimeS       *float64 `json:"time_s,omitempty"`
	MassRaw     string   `json:"mass_raw"`
	MassUnit    string   `json:"mass_unit"`
	MassG       *float64 `json:"mass_g,omitempty"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

// DriftCorrection describes the balance baseline drift model.
type DriftCorrection struct {
	Kind          string   `json:"kind"` // linear | none
	SlopeRaw      string   `json:"slope_raw"`
	SlopeUnit     string   `json:"slope_unit"`
	SlopeGPS      *float64 `json:"slope_g_per_s,omitempty"`
	InterceptRaw  string   `json:"intercept_raw"`
	InterceptUnit string   `json:"intercept_unit"`
	InterceptG    *float64 `json:"intercept_g,omitempty"`
	Diagnostics   []string `json:"diagnostics,omitempty"`
}

// ProgramStep is one furnace program segment.
type ProgramStep struct {
	Index         int      `json:"index"`
	FromTimeRaw   string   `json:"from_time_raw"`
	FromTimeS     *float64 `json:"from_time_s,omitempty"`
	ToTimeRaw     string   `json:"to_time_raw"`
	ToTimeS       *float64 `json:"to_time_s,omitempty"`
	TargetTempRaw string   `json:"target_temp_raw"`
	TargetTempC   *float64 `json:"target_temp_c,omitempty"`
	RateRaw       string   `json:"rate_raw"`
	RateUnit      string   `json:"rate_unit"`
	RateCPerMin   *float64 `json:"rate_c_per_min,omitempty"`
	Diagnostics   []string `json:"diagnostics,omitempty"`
}

// GasEvent is a gas valve switch. Valve time is explicitly NOT the sample
// reaction time when atmosphere delay/temperature lag is present.
type GasEvent struct {
	Index       int      `json:"index"`
	TimeRaw     string   `json:"time_raw"`
	TimeUnit    string   `json:"time_unit"`
	TimeS       *float64 `json:"time_s,omitempty"`
	FromGas     string   `json:"from_gas"`
	ToGas       string   `json:"to_gas"`
	DelayRaw    string   `json:"delay_raw"`
	DelayUnit   string   `json:"delay_unit"`
	DelayS      *float64 `json:"delay_s,omitempty"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

// InitialMass is a stratum-saved statement of the starting sample mass.
type InitialMass struct {
	Revision    int      `json:"revision"`
	MassRaw     string   `json:"mass_raw"`
	MassUnit    string   `json:"mass_unit"`
	MassG       *float64 `json:"mass_g,omitempty"`
	Basis       string   `json:"basis"` // tared | as_loaded | blank_subtracted
	Diagnostics []string `json:"diagnostics,omitempty"`
}

// Source is one immutable raw input record of one stratum.
type Source struct {
	ID          string   `json:"id"`
	ProjectID   string   `json:"project_id"`
	Layer       string   `json:"layer"`
	Revision    int      `json:"revision"`
	ParentID    string   `json:"parent_id,omitempty"`
	Original    string   `json:"original"`    // verbatim submitted text
	Format      string   `json:"format"`      // csv | json
	ParseState  string   `json:"parse_state"` // parsed | partial | failed
	Diagnostics []string `json:"diagnostics,omitempty"`
	SubmittedAt string   `json:"submitted_at"`
	Origin      string   `json:"origin"`
	Payload     []byte   `json:"payload"` // normalized-as-possible layer payload, JSON
}

// InputBatch records a single import transaction, including half-failure.
type InputBatch struct {
	ID             string   `json:"id"`
	ProjectID      string   `json:"project_id"`
	AcceptedIDs    []string `json:"accepted_ids"`
	RejectedLayers []string `json:"rejected_layers"`
	PartialLayers  []string `json:"partial_layers"`
	State          string   `json:"state"` // complete | partial
	Diagnostics    []string `json:"diagnostics,omitempty"`
	SubmittedAt    string   `json:"submitted_at"`
}

// Parameter fingerprints and algorithm versions live on every run.
const AlgorithmVersion = "tga-decomp-v1"

// Params bundles every tunable; its JSON fingerprint identifies revisions.
type Params struct {
	SmoothingWindow     int     `json:"smoothing_window"`
	DtgWindow           int     `json:"dtg_window"`
	PeakProminenceGPerS float64 `json:"peak_prominence_g_per_s"`
	StageMinLossG       float64 `json:"stage_min_loss_g"`
	AtmosphereDelayS    float64 `json:"atmosphere_delay_s"`
	TempLagC            float64 `json:"temp_lag_c"`
	OverlapAllowed      bool    `json:"overlap_allowed"`
	GapMaxMissing       int     `json:"gap_max_missing"`
	BlankSubtract       bool    `json:"blank_subtract"`
	DriftCorrect        bool    `json:"drift_correct"`
}

// DefaultParams returns the frozen v1 parameter set.
func DefaultParams() Params {
	return Params{
		SmoothingWindow:     3,
		DtgWindow:           2,
		PeakProminenceGPerS: 0.0005,
		StageMinLossG:       0.05,
		AtmosphereDelayS:    30,
		TempLagC:            4,
		OverlapAllowed:      true,
		GapMaxMissing:       1,
		BlankSubtract:       true,
		DriftCorrect:        true,
	}
}

// DataGap is a sampling hole; it is never bridged by interpolation.
type DataGap struct {
	ID           string  `json:"id"`
	AfterIndex   int     `json:"after_index"`
	BeforeIndex  int     `json:"before_index"`
	AfterTimeS   float64 `json:"after_time_s"`
	BeforeTimeS  float64 `json:"before_time_s"`
	MissingCount int     `json:"missing_count"`
	NoInterp     bool    `json:"no_interpolation"`
}

// StageCandidate is a first-class, possibly ambiguous decomposition result.
type StageCandidate struct {
	ID           string `json:"id"`
	RunID        string `json:"run_id"`
	VariantGroup string `json:"variant_group"` // shared across legal alternatives
	Rank         int    `json:"rank"`
	Label        string `json:"label"`
	// Bounds are offered in each frame; uncertainty is explicit.
	StartTimeS    *float64 `json:"start_time_s,omitempty"`
	EndTimeS      *float64 `json:"end_time_s,omitempty"`
	PeakTimeS     *float64 `json:"peak_time_s,omitempty"`
	StartTempC    *float64 `json:"start_temp_c,omitempty"`
	EndTempC      *float64 `json:"end_temp_c,omitempty"`
	PeakTempC     *float64 `json:"peak_temp_c,omitempty"`
	StartFrame    string   `json:"start_frame"`
	EndFrame      string   `json:"end_frame"`
	MassLossG     *float64 `json:"mass_loss_g,omitempty"`
	MassLossPct   *float64 `json:"mass_loss_pct,omitempty"`
	Overlaps      bool     `json:"overlaps"`
	LinkedGasID   string   `json:"linked_gas_event_id,omitempty"`
	Attribution   string   `json:"attribution,omitempty"` // valve | reaction | ambiguous
	Status        string   `json:"status"`                // proposed | accepted | rejected | superseded
	Unknowns      []string `json:"unknowns,omitempty"`
	RejectReasons []string `json:"reject_reasons,omitempty"`
}

// CalcStep is one link in a residue/value calculation chain.
type CalcStep struct {
	Order     int      `json:"order"`
	Op        string   `json:"op"`
	Detail    string   `json:"detail"`
	Inputs    []string `json:"inputs"`
	ValueG    *float64 `json:"value_g,omitempty"`
	ValuePct  *float64 `json:"value_pct,omitempty"`
	Unit      string   `json:"unit,omitempty"`
	SourceIDs []string `json:"source_ids,omitempty"`
}

// ResidueValue is any displayed residue with expandable provenance.
type ResidueValue struct {
	ID      string     `json:"id"`
	Label   string     `json:"label"`
	Frame   string     `json:"frame"`
	AtTimeS *float64   `json:"at_time_s,omitempty"`
	AtTempC *float64   `json:"at_temp_c,omitempty"`
	MassG   *float64   `json:"mass_g,omitempty"`
	Percent *float64   `json:"percent,omitempty"`
	Status  string     `json:"status"` // current | stale | unknown
	Unknown bool       `json:"unknown,omitempty"`
	Chain   []CalcStep `json:"chain"`
}

// CurvePoint drives the synchronized SVG views.
type CurvePoint struct {
	Index        int      `json:"index"`
	TimeS        *float64 `json:"time_s,omitempty"`
	TempC        *float64 `json:"temp_c,omitempty"`
	MassRawG     *float64 `json:"mass_raw_g,omitempty"`
	MassCorrG    *float64 `json:"mass_corr_g,omitempty"`
	DtgGPerS     *float64 `json:"dtg_g_per_s,omitempty"`
	GapAfter     bool     `json:"gap_after,omitempty"`
	NonMonotonic bool     `json:"non_monotonic,omitempty"`
	Noise        bool     `json:"noise,omitempty"`
}

// RunResult is the full payload of a deterministic analysis run.
type RunResult struct {
	Params                Params           `json:"params"`
	Curves                []CurvePoint     `json:"curves"`
	Gaps                  []DataGap        `json:"gaps"`
	Candidates            []StageCandidate `json:"candidates"`
	Residues              []ResidueValue   `json:"residues"`
	Diagnostics           []string         `json:"diagnostics"`
	EffectiveInitialMassG *float64         `json:"effective_initial_mass_g,omitempty"`
}

// Run is an immutable, content-addressed derived result.
type Run struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"project_id"`
	AlgVersion        string    `json:"alg_version"`
	ParamsFingerprint string    `json:"params_fingerprint"`
	Params            Params    `json:"params"`
	DirectSources     []string  `json:"direct_sources"`
	Status            string    `json:"status"` // current | stale | invalid
	Reason            string    `json:"reason,omitempty"`
	Result            RunResult `json:"result"`
	CreatedAt         string    `json:"created_at"`
}

// Event kinds in the append-only hash-chained history.
const (
	EvProjectCreated   = "project_created"
	EvSourceSubmitted  = "source_submitted"
	EvRunProduced      = "run_produced"
	EvRunStaled        = "run_staled"
	EvDecision         = "decision"
	EvConflictRaised   = "conflict_raised"
	EvConflictResolved = "conflict_resolved"
	EvUndo             = "undo"
	EvParamRevision    = "param_revision"
	EvJudgment         = "judgment"
	EvCounterexample   = "counterexample"
)

// Decision actions.
const (
	DecisionAccept   = "accept"
	DecisionReject   = "reject"
	DecisionAdjust   = "adjust"
	DecisionConflict = "conflict"
)

// Event is one immutable history entry. Seq and ID are deterministic so a
// re-import reproduces identical identities and ordering.
type Event struct {
	Seq       int64  `json:"seq"`
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Kind      string `json:"kind"`
	Actor     string `json:"actor"`
	VClock    int64  `json:"vclock"`
	ParentID  string `json:"parent_id"`
	Payload   []byte `json:"payload"`
	Basis     []byte `json:"basis,omitempty"` // machine-readable judgment evidence
	CreatedAt string `json:"created_at"`
	UndoneBy  string `json:"undone_by,omitempty"`
}

// CounterexampleSummary records one deliberately triggered boundary case.
type CounterexampleSummary struct {
	ID                string   `json:"id"`
	Scenario          string   `json:"scenario"`
	ProjectID         string   `json:"project_id"`
	BaselineRunID     string   `json:"baseline_run_id"`
	ChangedLayer      string   `json:"changed_layer"`
	ChangedSourceID   string   `json:"changed_source_id,omitempty"`
	AffectedRunIDs    []string `json:"affected_run_ids"`
	UnaffectedRunIDs  []string `json:"unaffected_run_ids"`
	AcceptedResultIDs []string `json:"accepted_result_ids"`
	RejectedResultIDs []string `json:"rejected_result_ids"`
	ExpectationBasis  []byte   `json:"expectation_basis"`
	Diagnostics       []string `json:"diagnostics"`
	CreatedAt         string   `json:"created_at"`
}

// Project bundles everything reviewed together.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	CaseRef   string `json:"case_ref,omitempty"`
}

// Now is indirected so tests run on virtual time.
type Clock func() time.Time
