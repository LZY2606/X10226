package domain

// SourceType identifies one of the six stratified, independently versioned
// input layers.
type SourceType string

const (
	SourceSamples    SourceType = "samples"     // raw sampled points (mass/temp/furnace time)
	SourceDrift      SourceType = "drift"        // balance drift correction definition
	SourceProgram    SourceType = "furnace"      // furnace temperature program
	SourceGas        SourceType = "gas"          // gas valve events
	SourceBlank      SourceType = "blank"        // blank (baseline) curve
	SourceInitMass   SourceType = "initial_mass" // sample initial mass
	SourceParams     SourceType = "params"       // algorithm parameter set (a configuration input)
)

// Origin tells whether a source came from an explicit import or was assumed by
// the system (assumed sources are still versioned and traceable).
type Origin string

const (
	OriginImported Origin = "imported"
	OriginAssumed  Origin = "assumed"
)

// CoordinateBasis fixes what a time/temperature axis is measured against.
// Atmosphere delays mean valve time and sample-reaction time differ, so axes
// never silently mix.
type CoordinateBasis struct {
	TimeOrigin string `json:"time_origin"` // "experiment_start" | "furnace_start" | "valve_clock"
	TimeUnit   string `json:"time_unit"`   // canonical: "s"
	TempBasis  string `json:"temp_basis"`  // "sample_measured" | "furnace_program"
}

// SamplePoint is one raw acquisition row. Raw text is retained per channel.
type SamplePoint struct {
	Index int  `json:"index"`
	Time  Val  `json:"time"`
	FurnaceTime Val `json:"furnace_time"`
	Mass  Val  `json:"mass"`
	Temp  Val  `json:"temp"` // measured sample temperature
	// Present=false marks a sampling gap: the slot exists but no reading does.
	Present bool `json:"present"`
	Raw     map[string]string `json:"raw,omitempty"` // original cell text by column
}

// DriftCorrection describes balance baseline drift. Kind: "linear" uses
// mg(t)=Slope*t+Intercept; "blank_curve" points at a blank source version.
type DriftCorrection struct {
	Kind      string  `json:"kind"` // "linear" | "blank_curve" | "none"
	Slope     float64 `json:"slope_mg_per_s,omitempty"`
	Intercept float64 `json:"intercept_mg,omitempty"`
	BlankID   string  `json:"blank_id,omitempty"`
	Raw       map[string]string `json:"raw,omitempty"`
}

// ProgramStep is one furnace program segment (all times canonical seconds).
type ProgramStep struct {
	Index         int     `json:"index"`
	FromTime      float64 `json:"from_s"`
	ToTime        float64 `json:"to_s"`
	StartTempC    float64 `json:"start_c"`
	TargetTempC   float64 `json:"target_c"`
	Kind          string  `json:"kind"` // "ramp" | "hold"
	RateCperMin   float64 `json:"rate_c_per_min,omitempty"`
}

// GasEvent is one valve switch recorded on the valve clock.
type GasEvent struct {
	Index       int     `json:"index"`
	ValveTimeS  float64 `json:"valve_time_s"`
	FromGas     string  `json:"from_gas"`
	ToGas       string  `json:"to_gas"`
	FlowMLperMin float64 `json:"flow_ml_per_min,omitempty"`
	Raw         map[string]string `json:"raw,omitempty"`
}

// BlankPoint is one point of a blank (empty-pan) run.
type BlankPoint struct {
	Index int     `json:"index"`
	TimeS float64 `json:"time_s"`
	MassMg float64 `json:"mass_mg"`
	TempC  float64 `json:"temp_c"`
}

// InitialMass is the separately recorded sample starting mass.
type InitialMass struct {
	Mass       Val    `json:"mass"`
	Basis      string `json:"basis"` // "as_loaded" | "dried" | "corrected"
	Note       string `json:"note,omitempty"`
}

// Params is the algorithm/parameter fingerprint inputs.
type Params struct {
	SmoothingWindowN int     `json:"smoothing_window_n"` // points each side, 0 disables
	MinProminence    float64 `json:"min_prominence_mg_per_min"`
	MinStageLossPct  float64 `json:"min_stage_loss_pct"`
	GasDelayS        float64 `json:"gas_delay_s"`         // valve -> sample reaction lag
	TempLagC         float64 `json:"temp_lag_c"`          // furnace -> sample temperature lag
	GapMaxBridgeN    int     `json:"gap_max_bridge_n"`    // gaps wider than this are never interpolated across
	NoiseMonotonicWindowN int `json:"noise_monotonic_window_n"`
	AlgVersion       string  `json:"alg_version"`
}

// DefaultParams returns the versioned baseline algorithm parameters.
func DefaultParams() Params {
	return Params{
		SmoothingWindowN:      2,
		MinProminence:         0.05,
		MinStageLossPct:       1.0,
		GasDelayS:             0,
		TempLagC:              0,
		GapMaxBridgeN:         1,
		NoiseMonotonicWindowN: 3,
		AlgVersion:            AlgorithmVersion,
	}
}

// Source is one immutable version of an input layer. New imports create a new
// Source with ParentID set; an existing Source is never overwritten.
type Source struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"project_id"`
	Type      SourceType `json:"type"`
	Version   int        `json:"version"`
	ParentID  string     `json:"parent_id,omitempty"`
	Origin    Origin     `json:"origin"`
	Basis     CoordinateBasis `json:"basis,omitempty"`
	Label     string     `json:"label"`
	CreatedEventSeq int64 `json:"created_event_seq"`

	Samples   []SamplePoint   `json:"samples,omitempty"`
	Drift     *DriftCorrection `json:"drift,omitempty"`
	Program   []ProgramStep   `json:"program,omitempty"`
	Gas       []GasEvent      `json:"gas,omitempty"`
	Blank     []BlankPoint    `json:"blank,omitempty"`
	InitMass  *InitialMass    `json:"initial_mass,omitempty"`
	Params    *Params         `json:"params,omitempty"`
}
