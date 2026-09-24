// Package tga implements the domain model and computations for
// thermogravimetric analysis (TGA) stage decomposition and atmosphere events.
package tga

// SamplePoint is one raw sampling point from the balance/furnace.
type SamplePoint struct {
	T       float64 `json:"t_s"`
	TempC   float64 `json:"temp_c"`
	MassMg  float64 `json:"mass_mg"`
}

// DriftPoint is one balance-drift correction anchor: drift_mg is added to
// the balance reading over time and must be subtracted.
type DriftPoint struct {
	T       float64 `json:"t_s"`
	DriftMg float64 `json:"drift_mg"`
}

// FurnacePoint is one furnace-program anchor.
type FurnacePoint struct {
	T          float64 `json:"t_s"`
	SetpointC  float64 `json:"setpoint_c"`
	RateCPerMin float64 `json:"rate_c_per_min"`
}

// GasEvent is one gas-valve switch event (raw valve time, not sample time).
type GasEvent struct {
	T         float64 `json:"t_s"`
	Valve     string  `json:"valve"`
	Gas       string  `json:"gas"`
	FlowMlMin float64 `json:"flow_ml_min"`
}

// BlankPoint is the blank (empty crucible) curve vs temperature.
type BlankPoint struct {
	TempC   float64 `json:"temp_c"`
	BlankMg float64 `json:"blank_mg"`
}

// InitialMass is the sample initial mass record.
type InitialMass struct {
	MassMg float64 `json:"mass_mg"`
	Unit   string  `json:"unit"`
	Note   string  `json:"note,omitempty"`
}

// Diagnostic marks a raw value that could not be converted; the raw text
// stays in place and is never silently dropped.
type Diagnostic struct {
	Line   int    `json:"line"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

// Params holds every algorithm parameter; its canonical JSON is hashed into
// the parameter fingerprint attached to each derived value.
type Params struct {
	GapThresholdS float64 `json:"gap_threshold_s"`
	NoiseTolMg    float64 `json:"noise_tol_mg"`
	SmoothWindow  int     `json:"smooth_window"`
	MinPeakProm   float64 `json:"min_peak_prom_mg_perC"`
	OverlapRatio  float64 `json:"overlap_ratio"`
	TransportLagS float64 `json:"transport_lag_s"`
	ThermalLagS   float64 `json:"thermal_lag_s"`
}

// DefaultParams returns the standard parameter set.
func DefaultParams() Params {
	return Params{
		GapThresholdS: 30,
		NoiseTolMg:    0.05,
		SmoothWindow:  7,
		MinPeakProm:   0.004,
		OverlapRatio:  0.5,
		TransportLagS: 45,
		ThermalLagS:   20,
	}
}

// Point is a corrected/normalized curve point.
type Point struct {
	T      float64 `json:"t_s"`
	TempC  float64 `json:"temp_c"`
	MassMg float64 `json:"mass_mg"`
}

// Gap is a sampling gap; interpolation must not cross it.
type Gap struct {
	StartT float64 `json:"start_t_s"`
	EndT   float64 `json:"end_t_s"`
	Policy string  `json:"policy"` // always "no_interpolation_across_gap"
}

// NoiseFlag marks a non-monotonic mass increase beyond tolerance.
type NoiseFlag struct {
	T        float64 `json:"t_s"`
	TempC    float64 `json:"temp_c"`
	Increase float64 `json:"increase_mg"`
}

// DTGPoint is the derivative curve (mg per degC, negative for mass loss).
type DTGPoint struct {
	T     float64 `json:"t_s"`
	TempC float64 `json:"temp_c"`
	DmDT  float64 `json:"dmg_per_c"`
}

// Peak is a DTG loss peak (on -dm/dT).
type Peak struct {
	T        float64 `json:"t_s"`
	TempC    float64 `json:"temp_c"`
	Height   float64 `json:"height_mg_per_c"`
	Prom     float64 `json:"prominence_mg_per_c"`
	Segment  int     `json:"segment"`
}

// StageCandidate is one possible decomposition stage. Stages may overlap
// and are never forced to sum to 100%.
type StageCandidate struct {
	ID        string   `json:"id"`
	GroupID   string   `json:"group_id"` // mutually exclusive candidates share a group
	Segment   int      `json:"segment"`
	StartT    float64  `json:"start_t_s"`
	EndT      float64  `json:"end_t_s"`
	StartTemp float64  `json:"start_temp_c"`
	EndTemp   float64  `json:"end_temp_c"`
	PeakTemp  *float64 `json:"peak_temp_c,omitempty"`
	LossMg    *float64 `json:"loss_mg,omitempty"`   // nil => unknown (e.g. crosses a gap)
	LossPct   *float64 `json:"loss_pct,omitempty"`  // nil => unknown
	Basis     string   `json:"basis"`               // machine-readable reason
	Exclusive bool     `json:"exclusive"`
}

// AtmosSegment is one atmosphere interval with valve time vs effective
// sample-reaction time (transport + thermal lag applied).
type AtmosSegment struct {
	Gas        string  `json:"gas"`
	Valve      string  `json:"valve"`
	ValveT     float64 `json:"valve_t_s"`
	EffectiveT float64 `json:"effective_t_s"`
	EndT       float64 `json:"end_t_s"` // effective end (next segment's effective time)
	FlowMlMin  float64 `json:"flow_ml_min"`
}

// ChainStep is one step of a residue calculation chain.
type ChainStep struct {
	Step      string   `json:"step"`
	Algo      string   `json:"algo"`
	ValueMg   *float64 `json:"value_mg,omitempty"`
	ValuePct  *float64 `json:"value_pct,omitempty"`
	Params    string   `json:"params_fp"`
	SourceIDs []string `json:"source_ids"`
	Note      string   `json:"note,omitempty"`
}

// Residue is the final residue rate with its full calculation chain.
type Residue struct {
	FinalMg  *float64    `json:"final_mg,omitempty"`
	FinalPct *float64    `json:"final_pct,omitempty"` // nil => unknown (e.g. run ends in a gap)
	Chain    []ChainStep `json:"chain"`
}
