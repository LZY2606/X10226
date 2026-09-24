// Package tga implements the domain model for thermogravimetric analysis
// (TGA) stage decomposition, atmosphere events and residue evidence chains.
//
// Design rules encoded here:
//   - Raw input is preserved verbatim; normalization happens in the derived
//     layer. Values that cannot be converted stay in place with diagnostics.
//   - Valve switch time != sample reaction time: atmosphere delay and
//     thermal lag shift effective atmosphere segments.
//   - Stages may overlap and mass-loss percentages are NOT forced to sum
//     to 100 %.
//   - Data gaps are never interpolated across.
//   - Candidates, rejection reasons, errors and unknown states are
//     first-class data.
package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// AlgoVersion is stamped on every derived record.
const AlgoVersion = "tga/v1"

// Layer kinds (raw, immutable once submitted; revisions are new versions).
const (
	LayerRawSamples  = "raw_samples"
	LayerDrift       = "drift_correction"
	LayerFurnace     = "furnace_program"
	LayerGasEvents   = "gas_events"
	LayerBlank       = "blank_curve"
	LayerInitialMass = "initial_mass"
)

// LayerKinds lists all importable layers in canonical order.
var LayerKinds = []string{
	LayerRawSamples, LayerDrift, LayerFurnace,
	LayerGasEvents, LayerBlank, LayerInitialMass,
}

// Derived record kinds.
const (
	DerNormSamples = "normalized_samples"
	DerCorrected   = "corrected_signal"
	DerDTG         = "dtg"
	DerAtmosphere  = "atmosphere_timeline"
	DerStages      = "stage_candidates"
	DerResidue     = "residue"
)

// Params holds every tunable of the derived pipeline. A revision of Params
// is a first-class event and only invalidates derived records that consume
// the parameter fingerprint.
type Params struct {
	SmoothingWindow  int     `json:"smoothing_window"`   // moving-average half-window in points
	ProminenceFrac   float64 `json:"prominence_frac"`    // fraction of max rate required for a stage
	AtmosphereDelayS float64 `json:"atmosphere_delay_s"` // gas transport delay
	ThermalLagS      float64 `json:"thermal_lag_s"`      // sample temperature lag behind furnace
	GapFactor        float64 `json:"gap_factor"`         // dt > GapFactor*median(dt) is a sampling gap
	MinWidthS        float64 `json:"min_width_s"`        // peaks narrower than this are noise spikes
	OverlapRatio     float64 `json:"overlap_ratio"`      // valley/peak above this => overlapping split
}

// DefaultParams returns the baseline parameter set.
func DefaultParams() Params {
	return Params{
		SmoothingWindow:  3,
		ProminenceFrac:   0.08,
		AtmosphereDelayS: 45,
		ThermalLagS:      20,
		GapFactor:        4,
		MinWidthS:        20,
		OverlapRatio:     0.5,
	}
}

// Fingerprint returns a stable content hash of any JSON-marshalable value.
func Fingerprint(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// HashID builds a deterministic identity from parts, so exported and
// re-imported records keep the same ids.
func HashID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:%s;", len(p), p)
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}

// Diagnostic is attached to anything that could not be fully processed.
// The offending value is never dropped: it stays in place with this note.
type Diagnostic struct {
	Where  string `json:"where"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

// Judgment is a machine-readable domain decision emitted during derivation.
type Judgment struct {
	Kind   string         `json:"kind"` // e.g. "atmosphere_delay", "gap_no_interpolation"
	Fields map[string]any `json:"fields"`
}

// Point is one normalized sample. Raw keeps the original representation.
type Point struct {
	T       float64 `json:"t_s"`
	Temp    float64 `json:"temp_c"`
	Mass    float64 `json:"mass_mg"`
	MassOK  bool    `json:"mass_ok"`
	RawMass string  `json:"raw_mass,omitempty"`
}

// Gap is a sampling interval that must not be interpolated across.
type Gap struct {
	StartT float64 `json:"start_t_s"`
	EndT   float64 `json:"end_t_s"`
}

// ---------- unit conversion ----------

var timeUnits = map[string]float64{"s": 1, "sec": 1, "min": 60, "h": 3600}
var massUnits = map[string]float64{"mg": 1, "g": 1000, "ug": 0.001, "µg": 0.001}

// ConvertTemp converts to °C; ok=false when the unit is unknown.
func ConvertTemp(v float64, unit string) (float64, bool) {
	switch unit {
	case "C", "°C", "degC", "celsius":
		return v, true
	case "K", "kelvin":
		return v - 273.15, true
	}
	return v, false
}

// ConvertMass converts to mg; ok=false when the unit is unknown.
func ConvertMass(v float64, unit string) (float64, bool) {
	f, ok := massUnits[unit]
	return v * f, ok
}

// ConvertTime converts to seconds; ok=false when the unit is unknown.
func ConvertTime(v float64, unit string) (float64, bool) {
	f, ok := timeUnits[unit]
	return v * f, ok
}

// ---------- derived payload types ----------

// NormSamples is the payload of a normalized_samples derived record.
type NormSamples struct {
	Points []Point      `json:"points"`
	Gaps   []Gap        `json:"gaps"`
	Diags  []Diagnostic `json:"diagnostics,omitempty"`
}

// Corrected is the payload of a corrected_signal derived record.
type Corrected struct {
	T            []float64    `json:"t_s"`
	Temp         []float64    `json:"temp_c"`
	Mass         []float64    `json:"mass_mg"` // drift- and blank-corrected
	Gaps         []Gap        `json:"gaps"`
	BlankApplied bool         `json:"blank_applied"`
	DriftApplied bool         `json:"drift_applied"`
	Diags        []Diagnostic `json:"diagnostics,omitempty"`
}

// DTG is the payload of a dtg derived record. Rate is -dm/dt in mg/min.
// Points adjacent to a gap carry NaN-free one-sided or missing values:
// indices listed in GapEdges have no valid derivative.
type DTG struct {
	T        []float64 `json:"t_s"`
	Temp     []float64 `json:"temp_c"`
	Rate     []float64 `json:"rate_mg_per_min"`
	Gaps     []Gap     `json:"gaps"`
	GapEdges []int     `json:"gap_edges"`
}

// AtmSegment is one effective atmosphere interval at the sample.
type AtmSegment struct {
	Gas           string  `json:"gas"`
	ValveT        float64 `json:"valve_t_s"`     // when the valve switched
	EffectiveT    float64 `json:"effective_t_s"` // when the sample actually sees it
	EndEffectiveT float64 `json:"end_effective_t_s"`
	FlowMLMin     float64 `json:"flow_ml_min"`
}

// Atmosphere is the payload of an atmosphere_timeline derived record.
type Atmosphere struct {
	Segments  []AtmSegment `json:"segments"`
	DelayS    float64      `json:"delay_s"`
	LagS      float64      `json:"lag_s"`
	Judgments []Judgment   `json:"judgments"`
}

// Candidate is one stage-decomposition hypothesis. Status is a projection
// over human decisions; the auto-generated RejectReason keeps refused
// hypotheses as first-class data.
type Candidate struct {
	ID          string  `json:"id"`
	StartT      float64 `json:"start_t_s"`
	PeakT       float64 `json:"peak_t_s"`
	EndT        float64 `json:"end_t_s"`
	PeakTemp    float64 `json:"peak_temp_c"`
	PeakRate    float64 `json:"peak_rate_mg_per_min"`
	MassLossMg  float64 `json:"mass_loss_mg"`
	MassLossPct float64 `json:"mass_loss_pct"`
	Atmosphere  string  `json:"atmosphere"`
	Overlaps    bool    `json:"overlaps"`
	AutoReject  string  `json:"auto_reject,omitempty"` // reason when auto-refused
}

// Stages is the payload of a stage_candidates derived record.
type Stages struct {
	Candidates     []Candidate `json:"candidates"`
	SumPct         float64     `json:"sum_pct"`
	SumEnforced    bool        `json:"sum_enforced"` // always false: stages may overlap
	OverlapAllowed bool        `json:"overlap_allowed"`
	Judgments      []Judgment  `json:"judgments"`
}

// Residue is the payload of a residue derived record.
type Residue struct {
	ValuePct    float64      `json:"value_pct"`
	Unit        string       `json:"unit"`  // always "%"
	Basis       string       `json:"basis"` // e.g. "as-received"
	FinalMass   float64      `json:"final_mass_mg"`
	InitialMass float64      `json:"initial_mass_mg"`
	Unknown     bool         `json:"unknown"`
	Diags       []Diagnostic `json:"diagnostics,omitempty"`
}

// SortedKeys is a small helper for deterministic maps in tests.
func SortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
