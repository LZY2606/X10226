// Package tga implements the domain model for thermogravimetric
// analysis (TGA): layered raw inputs, unit normalization, drift and
// blank correction, DTG computation, atmosphere-switch delay handling,
// overlapping stage candidates and residue calculation.
package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Layer names for the stratified raw inputs.
const (
	LayerSamplePoints = "sample_points"
	LayerDrift        = "drift"
	LayerFurnace      = "furnace"
	LayerGasEvents    = "gas_events"
	LayerBlank        = "blank"
	LayerInitialMass  = "initial_mass"
)

// Layers lists every known raw layer in canonical order.
var Layers = []string{
	LayerSamplePoints,
	LayerDrift,
	LayerFurnace,
	LayerGasEvents,
	LayerBlank,
	LayerInitialMass,
}

// Diagnostic is a first-class finding attached to a layer or a derived
// value. Values that cannot be converted stay in their raw position and
// get a Diagnostic instead of being silently dropped.
type Diagnostic struct {
	Layer    string `json:"layer"`
	Location string `json:"location"`
	Message  string `json:"message"`
	Raw      string `json:"raw,omitempty"`
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("[%s %s] %s", d.Layer, d.Location, d.Message)
}

// Point is a normalized sample point: time in seconds, mass in mg,
// furnace temperature in Celsius.
type Point struct {
	T     float64 `json:"t"`
	Mass  float64 `json:"mass"`
	TempC float64 `json:"temp_c"`
}

// Curve is a derived series with an explicit unit / coordinate basis.
type Curve struct {
	XUnit  string    `json:"x_unit"`
	YUnit  string    `json:"y_unit"`
	Label  string    `json:"label"`
	Points []XYPoint `json:"points"`
}

// XYPoint is one (x, y) pair of a derived curve.
type XYPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// FurnSeg is one furnace-program segment.
type FurnSeg struct {
	T0          float64 `json:"t0"`
	T1          float64 `json:"t1"`
	Kind        string  `json:"kind"` // "ramp" | "hold"
	T0C         float64 `json:"t0_c,omitempty"`
	RateCPerMin float64 `json:"rate_c_per_min,omitempty"`
	HoldC       float64 `json:"hold_c,omitempty"`
}

// GasEvent is a gas-valve switch as recorded at the valve.
type GasEvent struct {
	T            float64 `json:"t"`
	From         string  `json:"from"`
	To           string  `json:"to"`
	FlowMlPerMin float64 `json:"flow_ml_per_min"`
}

// AtmoInterval is a derived atmosphere interval at the sample. The
// effective start differs from the valve time by the transport delay
// and temperature lag; this is the machine-readable form of
// "valve switch time != sample reaction time".
type AtmoInterval struct {
	Gas        string  `json:"gas"`
	ValveT     float64 `json:"valve_t"`
	EffectiveT float64 `json:"effective_t"`
	DelayS     float64 `json:"delay_s"`
	TempLagS   float64 `json:"temp_lag_s"`
	Note       string  `json:"note"`
}

// StageCandidate is one decomposition-stage hypothesis. Candidates may
// overlap and their mass losses are not forced to sum to 100%.
type StageCandidate struct {
	Key        string  `json:"key"`
	Segment    int     `json:"segment"`
	T0         float64 `json:"t0"`
	T1         float64 `json:"t1"`
	Temp0C     float64 `json:"temp0_c"`
	Temp1C     float64 `json:"temp1_c"`
	MassLossMg float64 `json:"mass_loss_mg"`
	PeakT      float64 `json:"peak_t"`
	PeakDTGMgS float64 `json:"peak_dtg_mg_per_s"`
	Atmosphere string  `json:"atmosphere"`
	Ambiguous  bool    `json:"ambiguous,omitempty"`
	Note       string  `json:"note,omitempty"`
}

// Residue is the final-residue result with its full basis.
type Residue struct {
	FinalMassMg      float64 `json:"final_mass_mg"`
	InitialMassMg    float64 `json:"initial_mass_mg"`
	InitialMassBasis string  `json:"initial_mass_basis"`
	Pct              float64 `json:"pct"`
	StageSumPct      float64 `json:"stage_sum_pct"`
	Note             string  `json:"note"`
}

// ParamSet carries every algorithm parameter; its fingerprint is stored
// on each derived value so a parameter change only expires dependents.
type ParamSet struct {
	Algo              string  `json:"algo"`
	GapThresholdS     float64 `json:"gap_threshold_s"`
	AtmosphereDelayS  float64 `json:"atmosphere_delay_s"`
	TempLagS          float64 `json:"temp_lag_s"`
	PeakProminenceMgS float64 `json:"peak_prominence_mg_per_s"`
	EdgeFraction      float64 `json:"edge_fraction"`
	MinStageMassMg    float64 `json:"min_stage_mass_mg"`
	NonMonotonicTolMg float64 `json:"nonmonotonic_tol_mg"`
}

// DefaultParams returns the shipped parameter set.
func DefaultParams() ParamSet {
	return ParamSet{
		Algo:              "tga-v1",
		GapThresholdS:     20,
		AtmosphereDelayS:  45,
		TempLagS:          20,
		PeakProminenceMgS: 0.0008,
		EdgeFraction:      0.15,
		MinStageMassMg:    0.05,
		NonMonotonicTolMg: 0.005,
	}
}

// Fingerprint returns a stable content hash of the parameter set.
func (p ParamSet) Fingerprint() string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// SourceRef pins one direct source of a derived value.
type SourceRef struct {
	Layer   string `json:"layer"`
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// HashID builds a stable, content-addressed identity.
func HashID(prefix string, parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		b, err := json.Marshal(p)
		if err != nil {
			fmt.Fprintf(h, "%v", p)
			continue
		}
		h.Write(b)
		h.Write([]byte{0})
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}
