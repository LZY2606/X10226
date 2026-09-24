// Package tga implements a thermogravimetric analysis (TGA) review system:
// layered raw imports, an event-sourced store, a derivation engine for
// TG/DTG/atmosphere/stage-decomposition, and an HTTP+SVG review UI.
package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Layer names for stratified raw inputs. Raw inputs are immutable once
// submitted; corrections arrive as new versions linked by Parent.
const (
	LayerRawPoints   = "raw_points"        // mass / temperature / furnace-time samples
	LayerDrift       = "drift_correction"  // balance baseline drift model
	LayerFurnaceProg = "furnace_program"   // programmed temperature segments
	LayerGasEvents   = "gas_events"        // gas valve switching events
	LayerBlankCurve  = "blank_curve"       // empty-pan blank run
	LayerInitialMass = "initial_mass"      // sample initial mass
)

var LayerOrder = []string{
	LayerRawPoints, LayerDrift, LayerFurnaceProg, LayerGasEvents, LayerBlankCurve, LayerInitialMass,
}

// Derived kinds.
const (
	DerivedNormalized = "normalized_curve"
	DerivedDTG        = "dtg_curve"
	DerivedAtmosphere = "atmosphere_timeline"
	DerivedStage      = "stage_candidate"
	DerivedResidue    = "residue"
)

// Derived status.
const (
	StatusCurrent = "current"
	StatusStale   = "stale" // viewable, but must not masquerade as current
)

// Event kinds.
const (
	EvImport          = "import"
	EvLayerRevision   = "layer_revision"
	EvDerive          = "derive"
	EvParamChange     = "param_change"
	EvDecision        = "decision"
	EvDecisionUndo    = "decision_undo"
	EvDecisionConflict = "decision_conflict"
	EvRuleFinding     = "rule_finding"
	EvArchiveImport   = "archive_import"
)

// Rule identifiers for machine-readable judgement events.
const (
	RuleAtmosphereDelay   = "atmosphere_delay"    // valve switch time != sample reaction time
	RuleNoGapInterpolation = "no_gap_interpolation" // sampling gaps are never interpolated across
	RuleOverlapAllowed    = "stages_may_overlap"  // stages overlap; sum is not forced to 100%
	RuleNonMonotonic      = "nonmonotonic_noise"  // non-monotonic mass noise kept in place
)

// Qty is a domain quantity that always carries a unit and, where relevant, a
// coordinate basis. Raw preserves the original textual representation exactly
// as submitted; normalization happens only in the derived layer.
type Qty struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Basis string  `json:"basis,omitempty"`
	Raw   string  `json:"raw,omitempty"`
}

// Diagnostic is attached to inputs or derived values that could not be fully
// normalized. The original value stays in place; nothing is dropped to make
// plotting easier.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Where   string `json:"where,omitempty"`
}

// ---- Layer payloads (raw, as imported) ----

// RawPoint is one sample from the thermobalance.
type RawPoint struct {
	Time Qty `json:"time"` // furnace clock
	Mass Qty `json:"mass"`
	Temp Qty `json:"temp"` // sample thermocouple
}

type RawPointsPayload struct {
	Points []RawPoint `json:"points"`
}

type DriftPayload struct {
	Model     string `json:"model"` // "linear"
	Intercept Qty    `json:"intercept"`
	Slope     Qty    `json:"slope"` // mass per minute
}

type FurnaceSegment struct {
	Start   Qty    `json:"start"`          // time
	Kind    string `json:"kind"`           // "ramp" | "hold"
	Rate    Qty    `json:"rate,omitempty"` // C/min for ramp
	Target  Qty    `json:"target"`         // C reached at end of segment
	EndTime Qty    `json:"end_time"`
}

type FurnaceProgramPayload struct {
	Segments []FurnaceSegment `json:"segments"`
}

type GasEvent struct {
	Time     Qty    `json:"time"` // valve switch time (furnace clock)
	Gas      string `json:"gas"`
	FlowRate Qty    `json:"flow_rate"`
}

type GasEventsPayload struct {
	Events []GasEvent `json:"events"`
}

type BlankPoint struct {
	Time       Qty `json:"time"`
	BlankMass  Qty `json:"blank_mass"` // apparent mass artifact of empty pan
}

type BlankCurvePayload struct {
	Points []BlankPoint `json:"points"`
}

type InitialMassPayload struct {
	Mass Qty `json:"mass"`
}

// ---- Params ----

// Params are the reviewer-adjustable derivation parameters. Any change
// produces a new parameter fingerprint and therefore new derived identities.
type Params struct {
	TransportDelayS float64 `json:"transport_delay_s"` // gas transport delay valve->sample
	TempLagS        float64 `json:"temp_lag_s"`        // thermal lag of sample vs furnace
	GapThresholdS   float64 `json:"gap_threshold_s"`   // sampling gap: never interpolate across
	SmoothWindow    int     `json:"smooth_window"`     // DTG moving-average window (odd)
	PeakPromPctMin  float64 `json:"peak_prominence_pct_min"`
	NoiseFloorPctMin float64 `json:"noise_floor_pct_min"`
}

func DefaultParams() Params {
	return Params{
		TransportDelayS: 45, TempLagS: 15, GapThresholdS: 45,
		SmoothWindow: 5, PeakPromPctMin: 0.35, NoiseFloorPctMin: 0.05,
	}
}

func (p Params) Fingerprint() string { return shortHash(canonicalJSON(p)) }

// ---- Canonical (normalized) series used by the derived layer ----

// Point is a canonical sample: seconds, milligrams, degrees Celsius.
type Point struct {
	T   float64 `json:"t_s"`
	M   float64 `json:"m_mg"`
	TempC float64 `json:"temp_c"`
}

type NormalizedPayload struct {
	Points     []Point      `json:"points"`     // blank- and drift-corrected
	MassPct    []float64    `json:"mass_pct"`   // corrected / m0 * 100
	Gaps       [][2]float64 `json:"gaps"`       // sampling gaps (no interpolation across)
	NonMono    [][2]int     `json:"nonmonotonic_regions"`
	M0Mg       float64      `json:"m0_mg"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type DTGPayload struct {
	Segments   [][]DTGPoint `json:"segments"` // split at sampling gaps
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type DTGPoint struct {
	T       float64 `json:"t_s"`
	TempC   float64 `json:"temp_c"`
	DmDt    float64 `json:"dm_dt_pct_min"`  // smoothed, %/min
	DmDTemp float64 `json:"dm_dtemp_pct_c"` // smoothed, %/°C
}

type AtmosphereEvent struct {
	ValveTimeS     float64 `json:"valve_time_s"`
	EffectiveTimeS float64 `json:"effective_time_s"` // valve time + transport delay + temp lag
	Gas            string  `json:"gas"`
	FlowMlMin      float64 `json:"flow_ml_min"`
}

type AtmospherePayload struct {
	Events []AtmosphereEvent `json:"events"`
}

type StageCandidate struct {
	Label        string   `json:"label"`
	OnsetS       float64  `json:"onset_s"`
	PeakS        float64  `json:"peak_s"`
	EndsetS      float64  `json:"endset_s"`
	OnsetC       float64  `json:"onset_c"`
	PeakC        float64  `json:"peak_c"`
	EndsetC      float64  `json:"endset_c"`
	MassLossPct  float64  `json:"mass_loss_pct"`
	PeakRatePctMin float64 `json:"peak_rate_pct_min"`
	Uncertain    bool     `json:"uncertain"` // e.g. window contains a sampling gap
	Notes        []string `json:"notes,omitempty"`
}

type ResiduePayload struct {
	FinalMassPct float64 `json:"final_mass_pct"`
	FinalMassMg  float64 `json:"final_mass_mg"`
	AtS          float64 `json:"at_s"`
	AtC          float64 `json:"at_c"`
	Chain        []string `json:"chain"` // derived + input ids forming the calculation chain
}

// ---- helpers ----

func canonicalJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func shortHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// contentID builds a stable identity from kind + content. Identical content
// always yields the same id, which keeps identity stable across export/import.
func contentID(kind string, parts ...any) string {
	h := sha256.New()
	h.Write([]byte(kind))
	for _, p := range parts {
		h.Write([]byte{0})
		h.Write(canonicalJSON(p))
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}

// unit conversion to canonical units (s, mg, C). Returns ok=false when the
// unit is unknown: the value must stay in place with a diagnostic.
func convertQty(q Qty, to string) (float64, bool) {
	v := q.Value
	switch strings.ToLower(strings.TrimSpace(q.Unit)) {
	case "s", "sec", "seconds":
		if to == "s" {
			return v, true
		}
	case "min", "minutes":
		if to == "s" {
			return v * 60, true
		}
	case "mg":
		if to == "mg" {
			return v, true
		}
	case "g":
		if to == "mg" {
			return v * 1000, true
		}
	case "c", "°c", "degc":
		if to == "C" {
			return v, true
		}
	case "k":
		if to == "C" {
			return v - 273.15, true
		}
	case "c/min", "k/min":
		if to == "C/min" {
			return v, true
		}
	case "ml/min":
		if to == "mL/min" {
			return v, true
		}
	case "mg/min":
		if to == "mg/min" {
			return v, true
		}
	}
	return 0, false
}

// parseQty accepts either a JSON number (unitless canonical) or an object.
func parseQty(raw json.RawMessage, defaultUnit string) (Qty, error) {
	var num float64
	if err := json.Unmarshal(raw, &num); err == nil {
		return Qty{Value: num, Unit: defaultUnit, Raw: strings.TrimSpace(string(raw))}, nil
	}
	var q Qty
	if err := json.Unmarshal(raw, &q); err != nil {
		return Qty{}, fmt.Errorf("invalid quantity: %s", truncate(string(raw), 60))
	}
	if q.Unit == "" {
		q.Unit = defaultUnit
	}
	if q.Raw == "" {
		q.Raw = strings.TrimSpace(string(raw))
	}
	return q, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func almostEq(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func fmtFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
