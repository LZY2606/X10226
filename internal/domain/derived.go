package domain

// AlgorithmVersion is bumped when domain computation semantics change.
const AlgorithmVersion = "tga-alg-1.0.0"

// DerivationKind enumerates the dependency-layered derived results.
type DerivationKind string

const (
	DerivSeries   DerivationKind = "series"    // normalized + drift-corrected mass series
	DerivDTG      DerivationKind = "dtg"       // derivative trace
	DerivStages   DerivationKind = "stages"    // stage/peak candidates
	DerivResidue  DerivationKind = "residue"   // residue & loss results with calc chains
)

// Diagnostic is an attached note about a value or region (unconverted units,
// gaps, non-monotonic noise, ambiguity...). Diagnostics are first-class data.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Severity string `json:"severity"` // info | warning | error
	StartS  float64 `json:"start_s,omitempty"`
	EndS    float64 `json:"end_s,omitempty"`
}

// Gap marks a sampling gap. interpolation=false means the gap was never bridged.
type Gap struct {
	AfterIndex int     `json:"after_index"`
	BeforeIndex int    `json:"before_index"`
	FromS      float64 `json:"from_s"`
	ToS        float64 `json:"to_s"`
	NSkipped   int     `json:"n_skipped"`
	Interpolated bool  `json:"interpolated"`
}

// SeriesPoint is a normalized series point (canonical units).
type SeriesPoint struct {
	Index    int     `json:"index"`
	TimeS    float64 `json:"time_s"`
	FurnTimeS float64 `json:"furnace_time_s"`
	RawMassMg *float64 `json:"raw_mass_mg,omitempty"`
	MassMg   *float64 `json:"mass_mg,omitempty"` // nil inside non-bridged gaps
	TempC    *float64 `json:"temp_c,omitempty"`
	Present  bool     `json:"present"`
	NonMonotonic bool  `json:"non_monotonic"`
}

// SeriesResult is the normalized, drift-corrected measurement series.
type SeriesResult struct {
	Points     []SeriesPoint `json:"points"`
	Gaps       []Gap         `json:"gaps"`
	DriftApplied string       `json:"drift_applied"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Unconverted []UnconvertedCell `json:"unconverted"`
}

// UnconvertedCell keeps a value that could not be normalized at its original
// location (it must not be discarded just to draw a chart).
type UnconvertedCell struct {
	Index  int    `json:"index"`
	Column string `json:"column"`
	Raw    Raw    `json:"raw"`
	Reason string `json:"reason"`
}

// DTGPoint is one derivative point.
type DTGPoint struct {
	Index       int     `json:"index"`
	TimeS       float64 `json:"time_s"`
	DTGmgPerMin float64 `json:"dtg_mg_per_min"`
	DTGpctPerMin float64 `json:"dtg_pct_per_min,omitempty"`
	InGap       bool    `json:"in_gap"`
}

// DTGResult is the derivative trace plus detected extrema.
type DTGResult struct {
	Points []DTGPoint `json:"points"`
	Peaks  []Peak     `json:"peaks"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Peak is a DTG extremum.
type Peak struct {
	Index       int     `json:"index"`
	TimeS       float64 `json:"time_s"`
	TempC       float64 `json:"temp_c"`
	ValueMgPerMin float64 `json:"value_mg_per_min"`
	Prominence  float64 `json:"prominence_mg_per_min"`
}

// Stage is a proposed decomposition stage. Stages may overlap and their losses
// are never forced to sum to 100%.
type Stage struct {
	ID            string  `json:"id"`
	Label         string  `json:"label"`
	Number        int     `json:"number,omitempty"`
	StartS        float64 `json:"start_s"`
	EndS          float64 `json:"end_s"`
	PeakS         float64 `json:"peak_s,omitempty"`
	StartTempC    float64 `json:"start_temp_c"`
	EndTempC      float64 `json:"end_temp_c"`
	StartMassMg   float64 `json:"start_mass_mg"`
	EndMassMg     float64 `json:"end_mass_mg"`
	LossMg        float64 `json:"loss_mg"`
	LossPct       float64 `json:"loss_pct"`
	PeakMgPerMin  float64 `json:"peak_mg_per_min"`
	TempAtPeakC    float64 `json:"temp_at_peak_c,omitempty"`
	// Evidence links the stage to raw/derived evidence and gas context.
	PeakIndices   []int   `json:"peak_indices"`
	ValveTimeS    *float64 `json:"valve_time_s,omitempty"`
	EffectiveGasS *float64 `json:"effective_gas_time_s,omitempty"`
	GasAtStart    string  `json:"gas_at_start"`
	GasAtEnd      string  `json:"gas_at_end"`
	Overlaps      []string `json:"overlaps,omitempty"`
	// Ambiguous marks stages that cannot be uniquely decided; Alternatives
	// carries every legal candidate so none is collapsed to one number.
	Ambiguous     bool     `json:"ambiguous"`
	RejectReason  string   `json:"reject_reason,omitempty"`
	Alternatives  []StageAlternative `json:"alternatives,omitempty"`
}

// StageAlternative is one legal decomposition choice for an ambiguous region.
type StageAlternative struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	StartS   float64 `json:"start_s"`
	EndS     float64 `json:"end_s"`
	LossPct  float64 `json:"loss_pct"`
	Reason   string  `json:"reason"`
}

// StagesResult bundles all candidates including rejected ones.
type StagesResult struct {
	Stages       []Stage      `json:"stages"`
	Diagnostics  []Diagnostic `json:"diagnostics"`
	GasTimeline  []GasWindow  `json:"gas_timeline"` // effective (delay-applied) windows
	ProgramCurve []ProgramPoint `json:"program_curve"`
}

// GasWindow is an effective atmosphere window on the sample clock.
type GasWindow struct {
	FromS float64 `json:"from_s"`
	ToS   float64 `json:"to_s"`
	Gas   string  `json:"gas"`
	ValveFromS float64 `json:"valve_from_s"`
}

// ProgramPoint is one point of the furnace program envelope.
type ProgramPoint struct {
	TimeS float64 `json:"time_s"`
	ProgramTempC float64 `json:"program_temp_c"`
	ExpectedSampleC float64 `json:"expected_sample_c"`
}

// ChainStep is one step in an expandable residue calculation chain.
type ChainStep struct {
	Order   int    `json:"order"`
	Op      string `json:"op"`
	Detail  string `json:"detail"`
	Inputs  []string `json:"inputs,omitempty"` // stable ids of direct sources
	Value   string `json:"value,omitempty"`
	Unit    string `json:"unit,omitempty"`
}

// ResidueResult is a final residue / mass-loss result with full provenance.
type ResidueResult struct {
	ID          string      `json:"id"`
	Kind        string      `json:"kind"` // "final_residue" | "stage_loss"
	StageID     string      `json:"stage_id,omitempty"`
	Value       Val         `json:"value"`
	Status      string      `json:"status"` // ok | unknown | rejected
	Unknown     bool        `json:"unknown"`
	Reason      string      `json:"reason,omitempty"`
	Chain       []ChainStep `json:"chain"`
	DirectSources []string  `json:"direct_sources"`
}

// ResidueResultSet holds every residue result, including rejected/unknown.
type ResidueResultSet struct {
	Results     []ResidueResult `json:"results"`
	Diagnostics []Diagnostic    `json:"diagnostics"`
}

// Derivation is one versioned, content-addressed derived result. When the
// direct sources change the old derivation is kept but marked Superseded.
type Derivation struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Kind      DerivationKind `json:"kind"`
	Version   int            `json:"version"`
	AlgVersion string        `json:"alg_version"`
	ParamFingerprint string  `json:"param_fingerprint"`
	DirectSources []string   `json:"direct_sources"`
	CreatedEventSeq int64    `json:"created_event_seq"`
	Superseded bool          `json:"superseded"`
	Unknown   bool           `json:"unknown"`
	Series    *SeriesResult  `json:"series,omitempty"`
	DTG       *DTGResult     `json:"dtg,omitempty"`
	Stages    *StagesResult  `json:"stages,omitempty"`
	Residue   *ResidueResultSet `json:"residue,omitempty"`
}
