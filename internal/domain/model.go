// Package domain contains the core vocabulary of the thermogravimetric
// analysis (TGA) stage-decomposition and atmosphere-event archive.
//
// Every physical quantity carries a unit or coordinate basis. Raw imports
// preserve their original textual representation; normalization happens only
// in the derived layer. Values that cannot be converted stay in place with a
// diagnostic instead of being silently dropped "to make the plot work".
package domain

// AlgorithmVersion is bumped whenever the derived-calculation code changes in
// a way that affects numerical results.
const AlgorithmVersion = "tga-derive-1"

// Kinds / statuses shared across the system.
const (
	KindRawSample   = "raw_sample"
	KindDrift       = "balance_drift"
	KindProgram     = "furnace_program"
	KindGasEvent    = "gas_valve_event"
	KindBlank       = "blank_curve"
	KindInitialMass = "initial_mass"
)

const (
	StatusOK      = "ok"
	StatusUnknown = "unknown"
	StatusRejected = "rejected"
	StatusStale   = "stale"
	StatusCurrent = "current"
)

// Source kinds stored verbatim.
var SourceKinds = []string{
	KindRawSample, KindDrift, KindProgram, KindGasEvent, KindBlank, KindInitialMass,
}

// Q is a physical quantity: normalized numeric value plus the unit/coordinate
// basis it is expressed in, and the original representation from the import.
type Q struct {
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Basis    string  `json:"basis,omitempty"`
	Original string  `json:"original,omitempty"`
}

// Diagnostic explains why a value could not be normalized or a result could not
// be uniquely decided.
type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // error | warning | note
	Message  string `json:"message"`
	Ref      string `json:"ref,omitempty"`
}

// RawPoint is one sampled point exactly as imported.
type RawPoint struct {
	Line     int      `json:"line"`
	Time     string   `json:"time_raw"`
	Temp     string   `json:"temp_raw"`
	Mass     string   `json:"mass_raw"`
	TimeQ    Q        `json:"time"`
	TempQ    Q        `json:"temp"`
	MassQ    Q        `json:"mass"`
	Accepted bool     `json:"accepted"`
	Diags    []Diagnostic `json:"diags,omitempty"`
}

// ProgramStep is one furnace-program step (time basis: furnace seconds).
type ProgramStep struct {
	Line       int    `json:"line"`
	FromT      Q      `json:"from_t"`
	ToT        Q      `json:"to_t"`
	StartAt    Q      `json:"start_at"`
	EndAt      Q      `json:"end_at"`
	RateKPerMin Q     `json:"rate_K_per_min,omitempty"`
	Original   string `json:"original"`
}

// GasEvent is one gas-valve switch as recorded by the instrument (valve time),
// before any atmosphere-delay/temperature-lag correction.
type GasEvent struct {
	Line     int    `json:"line"`
	ValveAt  Q      `json:"valve_at"`
	FromGas  string `json:"from_gas"`
	ToGas    string `json:"to_gas"`
	Note     string `json:"note,omitempty"`
	Original string `json:"original"`
}

// DriftPoint / BlankPoint are raw auxiliary-curve points.
type DriftPoint struct {
	Line  int `json:"line"`
	Time  Q   `json:"time"`
	Mass  Q   `json:"mass"`
	Original string `json:"original"`
}
type BlankPoint struct {
	Line  int `json:"line"`
	Time  Q   `json:"time"`
	Mass  Q   `json:"mass"`
	Original string `json:"original"`
}

// InitialMass is the stratified sample initial mass record (operator weighing),
// kept as its own source layer.
type InitialMass struct {
	Mass       Q      `json:"mass"`
	TaredAt    Q      `json:"tared_at,omitempty"`
	Note       string `json:"note,omitempty"`
	Original   string `json:"original"`
}

// Params are the tunable analysis rules. A revision creates a new version with
// a parent relationship; old versions are never overwritten.
type Params struct {
	SmoothWindowS        float64 `json:"smooth_window_s"`
	PeakMinPromPct       float64 `json:"peak_min_prom_pct"`
	PeakMergeWithinS    float64 `json:"peak_merge_within_s"`
	GapMinMissingS       float64 `json:"gap_min_missing_s"`
	AtmosDelayS         float64 `json:"atmos_delay_s"`
	TempLagS            float64 `json:"temp_lag_s"`
	DriftInterpLimitS   float64 `json:"drift_interp_limit_s"`
	BlankInterpLimitS   float64 `json:"blank_interp_limit_s"`
	NonMonoAbsEpsilonMg  float64 `json:"non_mono_abs_epsilon_mg"`
	OverlapAllowed      bool    `json:"overlap_allowed"`
	StageSumForced100   bool    `json:"stage_sum_forced_100"`
}

// DefaultParams returns the v1 baseline rule set.
func DefaultParams() Params {
	return Params{
		SmoothWindowS:       20,
		PeakMinPromPct:      0.05,
		PeakMergeWithinS:    12,
		GapMinMissingS:      30,
		AtmosDelayS:        0,
		TempLagS:           0,
		DriftInterpLimitS:  120,
		BlankInterpLimitS:  120,
		NonMonoAbsEpsilonMg: 0.05,
		OverlapAllowed:     true,
		StageSumForced100:  false,
	}
}

// Fingerprint is a canonical-content hash of the parameter values.
func (p Params) Fingerprint() string { return ShortHash(p) }
