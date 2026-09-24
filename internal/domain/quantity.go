package domain

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Unit identifiers are SI-aligned local conventions used by the thermal team.
const (
	UnitMassMG      = "mg"
	UnitMassG       = "g"
	UnitTempC       = "degC"
	UnitTempK       = "K"
	UnitTimeMin     = "min"
	UnitTimeS       = "s"
	UnitPercent     = "%"
	UnitRateMass    = "mg/min"
	UnitRatePercent = "%/min"
	UnitFlow        = "mL/min"
	UnitNone        = ""
)

// Coordinate bases make explicit which timeline / temperature axis a value sits on.
const (
	BasisFurnaceProgram = "furnace_program" // commanded furnace time/temperature
	BasisFurnaceMeas    = "furnace_measured"
	BasisSampleEst      = "sample_estimated" // sample zone, after thermal lag
	BasisValve          = "valve"            // gas valve switch time
	BasisSampleReaction = "sample_reaction"  // reaction time at the sample, after atmosphere delay
)

// Q is a domain quantity. The original textual representation is always retained;
// Value is only populated when the representation converts to the declared unit.
// Values that cannot be converted stay in place and carry diagnostics instead of
// being dropped for the sake of plotting.
type Q struct {
	Value       *float64 `json:"value,omitempty"`
	Raw         string   `json:"raw"`
	Unit        string   `json:"unit"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

func qptr(v float64) *float64 { return &v }

func qDiag(format string, args ...any) []string {
	return []string{fmt.Sprintf(format, args...)}
}

// QFrom builds a quantity from an already typed float, recording its raw form.
func QFrom(v float64, unit string) Q {
	return Q{Value: qptr(v), Raw: formatFloat(v), Unit: unit}
}

// ParseQ accepts a string or a JSON number (delivered as float64/string by the
// importer) and never discards the original representation.
func ParseQ(x any, unit string) Q {
	switch t := x.(type) {
	case nil:
		return Q{Raw: "", Unit: unit, Diagnostics: qDiag("missing value")}
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return Q{Raw: fmt.Sprintf("%v", t), Unit: unit, Diagnostics: qDiag("non-finite number")}
		}
		return Q{Value: qptr(t), Raw: formatFloat(t), Unit: unit}
	case string:
		s := strings.TrimSpace(t)
		q := Q{Raw: t, Unit: unit}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			q.Diagnostics = qDiag("cannot parse %q as number for unit %s", s, unit)
			return q
		}
		if math.IsNaN(v) || math.IsInf(v, 0) {
			q.Diagnostics = qDiag("non-finite number")
			return q
		}
		q.Value = &v
		return q
	default:
		return Q{Raw: fmt.Sprintf("%v", t), Unit: unit, Diagnostics: qDiag("unsupported numeric type %T", t)}
	}
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// ConvertibleTo reports whether the quantity can be expressed in dst on a
// supported mass/temperature/time axis.
func (q Q) ConvertibleTo(dst string) bool {
	if q.Value == nil {
		return false
	}
	return q.Unit == dst || conversion(q.Unit, dst) != nil
}

func conversion(src, dst string) func(float64) float64 {
	if src == dst {
		return func(v float64) float64 { return v }
	}
	type pair struct{ a, b string }
	table := map[pair]func(float64) float64{
		{UnitMassG, UnitMassMG}:  func(v float64) float64 { return v * 1000 },
		{UnitMassMG, UnitMassG}:  func(v float64) float64 { return v / 1000 },
		{UnitTempK, UnitTempC}:   func(v float64) float64 { return v - 273.15 },
		{UnitTempC, UnitTempK}:   func(v float64) float64 { return v + 273.15 },
		{UnitTimeS, UnitTimeMin}: func(v float64) float64 { return v / 60 },
		{UnitTimeMin, UnitTimeS}: func(v float64) float64 { return v * 60 },
	}
	return table[pair{src, dst}]
}

// In returns the value expressed in dst. On failure the quantity stays in its
// original location with a diagnostic and ok=false is returned.
func (q Q) In(dst string) (Q, bool) {
	if q.Value == nil {
		out := q
		out.Unit = dst
		out.Diagnostics = append(append([]string{}, q.Diagnostics...), fmt.Sprintf("cannot convert: no numeric value (target %s)", dst))
		return out, false
	}
	if q.Unit == dst {
		return q, true
	}
	f := conversion(q.Unit, dst)
	if f == nil {
		out := q
		out.Diagnostics = append(append([]string{}, q.Diagnostics...), fmt.Sprintf("no conversion %s -> %s", q.Unit, dst))
		return out, false
	}
	return Q{Value: qptr(f(*q.Value)), Raw: q.Raw, Unit: dst}, true
}

// MustValue assumes convertibility to unit and returns the canonical value.
func (q Q) MustValue(unit string) float64 {
	cv, ok := q.In(unit)
	if !ok || cv.Value == nil {
		return math.NaN()
	}
	return *cv.Value
}

func (q Q) String() string {
	if q.Value == nil {
		return fmt.Sprintf("%s?(unit %s)", q.Raw, q.Unit)
	}
	return fmt.Sprintf("%s %s", formatFloat(*q.Value), q.Unit)
}
