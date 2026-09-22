// Package model defines the core domain types for the TGA review system.
// Every quantity carries a unit or a coordinate basis; raw imported
// representations are preserved and normalization happens only in the
// derived layer.
package model

import (
	"fmt"
	"sort"
	"strings"
)

// Qty is a measured or derived quantity. Value is expressed in Unit.
// Unit is never empty for domain quantities; values that could not be
// converted stay in their original unit and are flagged via Diag.
type Qty struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

func (q Qty) String() string { return fmt.Sprintf("%.6g %s", q.Value, q.Unit) }

// Convert returns the quantity expressed in target unit. ok=false when the
// conversion is unknown; callers must keep the original value in place and
// attach a diagnostic instead of dropping it.
func (q Qty) Convert(target string) (Qty, bool) {
	if q.Unit == target {
		return q, true
	}
	f, ok := unitFactor(q.Unit)
	if !ok {
		return Qty{}, false
	}
	t, ok := unitFactor(target)
	if !ok {
		return Qty{}, false
	}
	if f.kind != t.kind {
		return Qty{}, false
	}
	return Qty{Value: q.Value * f.toBase / t.toBase, Unit: target}, true
}

type unitDef struct {
	toBase float64
	kind   string
}

func unitFactor(u string) (unitDef, bool) {
	switch u {
	case "mg":
		return unitDef{1, "mass"}, true
	case "g":
		return unitDef{1000, "mass"}, true
	case "ug":
		return unitDef{0.001, "mass"}, true
	case "s":
		return unitDef{1, "time"}, true
	case "min":
		return unitDef{60, "time"}, true
	case "C":
		return unitDef{1, "temp"}, true
	case "K":
		return unitDef{1, "tempK"}, true
	case "%":
		return unitDef{1, "frac"}, true
	case "mg/min":
		return unitDef{1, "rate"}, true
	case "mg/s":
		return unitDef{1.0 / 60.0, "rate"}, true
	case "C/min":
		return unitDef{1, "ramprate"}, true
	case "K/min":
		return unitDef{1, "ramprate"}, true
	case "mL/min":
		return unitDef{1, "flow"}, true
	case "sccm":
		return unitDef{1, "flow"}, true
	}
	return unitDef{}, false
}

// Point is one raw TGA sample. T is the time coordinate (seconds after
// normalization), Temp the sample temperature, Mass the balance reading.
type Point struct {
	T    Qty `json:"t"`
	Temp Qty `json:"temp"`
	Mass Qty `json:"mass"`
}

// Gap marks an interval of missing samples. Derived values must never be
// interpolated across a gap.
type Gap struct {
	StartS float64 `json:"start_s"`
	EndS   float64 `json:"end_s"`
	Reason string  `json:"reason"`
}

// FurnaceSeg is one programmed furnace segment.
type FurnaceSeg struct {
	Kind    string  `json:"kind"` // ramp | hold
	TStartS float64 `json:"t_start_s"`
	RateCPM float64 `json:"rate_c_per_min,omitempty"`
	FromC   float64 `json:"from_c,omitempty"`
	ToC     float64 `json:"to_c,omitempty"`
	HoldMin float64 `json:"hold_min,omitempty"`
	AtC     float64 `json:"at_c,omitempty"`
}

// ValveEvent is a gas valve switch as recorded by the instrument. The
// recorded switch time is NOT the sample reaction time: atmosphere delay
// and thermal lag shift the response.
type ValveEvent struct {
	T       Qty    `json:"t"`
	FromGas string `json:"from_gas"`
	ToGas   string `json:"to_gas"`
	Flow    Qty    `json:"flow"`
}

// BlankPoint is one point of the blank (buoyancy/drift) correction curve.
type BlankPoint struct {
	T  Qty `json:"t"`
	Mg Qty `json:"mg"`
}

// Layer kinds, committed as separate versioned source documents.
const (
	LayerPoints      = "points"       // raw sample points
	LayerDrift       = "drift"        // balance drift correction
	LayerFurnace     = "furnace"      // furnace program
	LayerValves      = "valves"       // gas valve events
	LayerBlank       = "blank"        // blank curve
	LayerInitialMass = "initial_mass" // sample initial mass
)

// LayerOrder fixes the canonical display/dependency order of layers.
var LayerOrder = []string{LayerPoints, LayerDrift, LayerFurnace, LayerValves, LayerBlank, LayerInitialMass}

// Layer is one committed version of a source document. Raw input is never
// overwritten: a revision commits a new Layer with ParentID set.
type Layer struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"`
	Version    int               `json:"version"`
	ParentID   string            `json:"parent_id,omitempty"`
	Submission string            `json:"submission"` // import batch that committed it
	Meta       map[string]string `json:"meta,omitempty"`

	// Raw text is preserved verbatim.
	RawText string `json:"raw_text"`

	// Parsed payload (only the fields relevant to Kind are set).
	Points      []Point      `json:"points,omitempty"`
	DriftMgPerM float64      `json:"drift_mg_per_min,omitempty"`
	Segments    []FurnaceSeg `json:"segments,omitempty"`
	Valves      []ValveEvent `json:"valves,omitempty"`
	Blank       []BlankPoint `json:"blank,omitempty"`
	InitialMass Qty          `json:"initial_mass,omitempty"`

	// Diagnostics for values that could not be converted; the raw cell
	// text stays in place and the row is excluded from derived input.
	Diags []Diag `json:"diags,omitempty"`
}

// Diag is a parse/normalization diagnostic attached to a layer.
type Diag struct {
	Line   int    `json:"line"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

// InGap reports whether time t (seconds) falls inside any gap.
func InGap(gaps []Gap, t float64) bool {
	for _, g := range gaps {
		if t >= g.StartS && t <= g.EndS {
			return true
		}
	}
	return false
}

// CrossesGap reports whether the interval [a,b] (seconds) intersects a gap.
func CrossesGap(gaps []Gap, a, b float64) bool {
	for _, g := range gaps {
		if a <= g.EndS && b >= g.StartS {
			return true
		}
	}
	return false
}

// Fingerprint is a deterministic fingerprint of a parameter set.
func Fingerprint(params map[string]float64) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%.10g;", k, params[k])
	}
	return b.String()
}
