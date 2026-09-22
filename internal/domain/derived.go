package domain

// Params is one revision of the analysis parameters. Parameter revisions are
// versioned entities exactly like raw submissions.
type Params struct {
	AtmosphereDelayS   float64 `json:"atmosphere_delay_s"`   // valve -> sample transport delay
	TemperatureLagS    float64 `json:"temperature_lag_s"`    // furnace -> sample temperature lag
	DriftOrder         int     `json:"drift_order"`          // 0 constant, 1 linear
	SmoothingWindow    int     `json:"smoothing_window"`     // DTG smoothing points (0 = none)
	ProminenceFactor   float64 `json:"prominence_factor"`    // min peak prominence / max(-dm/dt)
	MinStageWidthS     float64 `json:"min_stage_width_s"`    // reject narrower candidates
	OverlapMergeTolS   float64 `json:"overlap_merge_tol_s"`  // tolerance when checking overlap
	ParentFingerprint  string  `json:"parent_fingerprint,omitempty"`
	Reason             string  `json:"reason,omitempty"`
}

// DefaultParams is the immutable algorithm baseline (version 1).
func DefaultParams() Params {
	return Params{
		AtmosphereDelayS: 12.0,
		TemperatureLagS:  8.0,
		DriftOrder:       1,
		SmoothingWindow:  3,
		ProminenceFactor: 0.08,
		MinStageWidthS:   30,
		OverlapMergeTolS: 5,
	}
}

// CorrectedPoint is a sampling point after balance drift / blank correction.
// Corrected mass is missing across sampling gaps; it is never interpolated.
type CorrectedPoint struct {
	Index       int      `json:"index"`
	TimeS       *float64 `json:"time_s"`
	TempC       *float64 `json:"temp_c"`
	MassMg      *float64 `json:"mass_mg_corrected"`
	RawMassMg   *float64 `json:"raw_mass_mg"`
	BiasMg      *float64 `json:"bias_mg"`
	AcrossGap   bool     `json:"across_gap"`
	NonMonotone bool     `json:"non_monotone,omitempty"`
}

type CorrectedMass struct {
	Points []CorrectedPoint `json:"points"`
}

// FurnaceTimeline resolves the programmed temperature as a function of time.
type FurnaceTimeline struct {
	Segments []TimelineSegment `json:"segments"`
}

type TimelineSegment struct {
	Index         int     `json:"index"`
	Kind          string  `json:"kind"`
	T0S, T1S      float64 `json:"t0_s,t1_s"`
	Temp0C, Temp1C float64 `json:"temp0_c,temp1_c"`
}

// GasTimelineEntry is a valve switch plus the estimated reaction-time window
// of the atmosphere actually arriving at the sample. Because of atmosphere
// delay and temperature lag the valve time never equals reaction time.
type GasTimelineEntry struct {
	Index        int     `json:"index"`
	ValveTimeS   float64 `json:"valve_time_s"`
	ReactStartS  float64 `json:"reaction_start_s"`
	ReactEndS    float64 `json:"reaction_end_s"`
	FromAt       string  `json:"from_atmosphere"`
	ToAt         string  `json:"to_atmosphere"`
	Basis        string  `json:"basis"` // "params.atmosphere_delay_s + uncertainty"
}

type GasTimeline struct {
	Entries []GasTimelineEntry `json:"entries"`
}

// TGPoint is one normalized TG/DTG point. DTG is nil across gaps, at missing
// mass and at gap-adjacent derivatives (never bridged by interpolation).
type TGPoint struct {
	Index       int      `json:"index"`
	TimeS       *float64 `json:"time_s"`
	TempC       *float64 `json:"temp_c"`
	MassMg      *float64 `json:"mass_mg"`
	MassPct     *float64 `json:"mass_pct_of_initial"`
	DTG         *float64 `json:"dtg_mg_per_s"`
	AcrossGap   bool     `json:"across_gap,omitempty"`
	NonMonotone bool     `json:"non_monotone,omitempty"`
}

type TGSeries struct {
	Points []TGPoint `json:"points"`
	Gaps   [][2]float64 `json:"gaps,omitempty"` // time intervals never interpolated
}

// Peak is one detected DTG peak. Overlapping peaks stay distinct.
type Peak struct {
	ID          string   `json:"id"`
	TimeS       float64  `json:"time_s"`
	TempC       *float64 `json:"temp_c"`
	DTG         float64  `json:"dtg_mg_per_s"`
	Prominence  float64  `json:"prominence"`
	OverlapsWith []string `json:"overlaps_with,omitempty"`
}

// StageCandidate is one legal decomposition stage. Candidates are first-class
// data: rejected and uncertain candidates are retained with their reasons.
type StageCandidate struct {
	ID             string   `json:"id"`
	StartS, EndS   float64  `json:"start_s,end_s"`
	StartTempC     *float64 `json:"start_temp_c"`
	EndTempC       *float64 `json:"end_temp_c"`
	PeakID         string   `json:"peak_id,omitempty"`
	MassLossMg     *float64 `json:"mass_loss_mg"`
	MassLossPct    *float64 `json:"mass_loss_pct"`
	Atmosphere     string   `json:"atmosphere,omitempty"`
	Overlaps       []string `json:"overlaps,omitempty"`
	Unique         bool     `json:"unique"`
	Accepted       bool     `json:"accepted"` // default acceptance within the set
	RejectionCodes []string `json:"rejection_codes,omitempty"`
	Note           string   `json:"note,omitempty"`
}

// StageSet is the full candidate set for one parameter/source combination.
type StageSet struct {
	Peaks      []Peak           `json:"peaks"`
	Candidates []StageCandidate `json:"candidates"`
	// SumPct is reported for inspection only; overlapping stages are legal so
	// the sum is not constrained to 100. Nil when not computable.
	SumPct *float64 `json:"sum_pct"`
	// UniqueSolution is false when more than one legal decomposition exists.
	UniqueSolution bool `json:"unique_solution"`
	Diagnostics    []Diagnostic `json:"diagnostics,omitempty"`
}

// ChainLink is one step in an expandable residual-rate calculation chain.
type ChainLink struct {
	Step        string  `json:"step"`
	Detail      string  `json:"detail"`
	Value       *float64 `json:"value,omitempty"`
	Unit        string  `json:"unit,omitempty"`
	SourceID    string  `json:"source_id,omitempty"`
	SourceKind  Kind    `json:"source_kind,omitempty"`
}

// Residual is the final residue fraction with its full provenance chain.
type Residual struct {
	AtS      *float64    `json:"at_s"`
	AtTempC  *float64    `json:"at_temp_c"`
	RatePct  *float64    `json:"rate_pct"` // nil => unknown/uncomputable
	Unknown  bool        `json:"unknown,omitempty"`
	Reasons  []string    `json:"reasons,omitempty"`
	Chain    []ChainLink `json:"chain"`
}
