// Package domain contains the TGA stage-decomposition domain model.
//
// Raw values keep the representation they were imported with; normalization to
// canonical units happens only in the derivation layer. Every domain quantity
// therefore carries a unit, every series carries a coordinate basis, and
// values that cannot be converted stay at their original location with a
// diagnostic attached instead of being silently dropped.
package domain

import (
	"fmt"
	"strings"
)

// QuantityKind enumerates the physical kinds that have known unit tables.
type QuantityKind string

const (
	KindMass        QuantityKind = "mass"
	KindTemperature QuantityKind = "temperature"
	KindTime        QuantityKind = "time"
	KindRate        QuantityKind = "rate" // dm/dt, percent/time etc.
	KindDimensionless QuantityKind = "dimensionless"
)

// Raw is a value exactly as imported: the textual representation is never
// discarded, even when it fails to parse or convert.
type Raw struct {
	Text string `json:"text"`
	Unit string `json:"unit"` // unit token as written by the operator
}

// Val is a normalized display value. The raw representation lives next to the
// canonical one so the UI can always show where a number came from.
type Val struct {
	Raw      string  `json:"raw"`
	RawUnit  string  `json:"raw_unit"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"` // canonical unit, "" when unconverted
	Kind     QuantityKind `json:"kind,omitempty"`
	ConvertOK bool   `json:"convert_ok"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

func (v Val) String() string {
	if !v.ConvertOK {
		if v.Raw != "" {
			return v.Raw + " " + v.RawUnit
		}
		return "?"
	}
	return fmt.Sprintf("%g %s", v.Value, v.Unit)
}

// canonicalUnits maps each physical kind to the unit used in derived layers.
var canonicalUnits = map[QuantityKind]string{
	KindMass:          "mg",
	KindTemperature:   "degC",
	KindTime:          "s",
	KindRate:          "mg/min",
	KindDimensionless: "",
}

// factor maps a normalized unit token (lowercased, trimmed) to
// (factor-to-canonical, offset) where canonical = value*factor + offset.
func unitTable(kind QuantityKind) map[string][2]float64 {
	switch kind {
	case KindMass:
		return map[string][2]float64{
			"mg": {1, 0}, "milligram": {1, 0}, "milligrams": {1, 0},
			"g": {1000, 0}, "gram": {1000, 0}, "grams": {1000, 0},
			"kg": {1e6, 0},
			"ug": {0.001, 0}, "µg": {0.001, 0},
			"%": {0, 0}, // percent is dimensionless relative mass; kept, no global factor
		}
	case KindTemperature:
		return map[string][2]float64{
			"c": {1, 0}, "°c": {1, 0}, "degc": {1, 0}, "degree_c": {1, 0},
			"k": {1, 0}, "kelvin": {1, 0}, // handled specially (offset)
			"f": {1, 0}, "°f": {1, 0}, "degf": {1, 0},
		}
	case KindTime:
		return map[string][2]float64{
			"s": {1, 0}, "sec": {1, 0}, "second": {1, 0}, "seconds": {1, 0},
			"min": {60, 0}, "minute": {60, 0}, "minutes": {60, 0},
			"ms": {0.001, 0},
			"h": {3600, 0}, "hr": {3600, 0}, "hour": {3600, 0}, "hours": {3600, 0},
		}
	case KindRate:
		return map[string][2]float64{
			"mg/min": {1, 0}, "mg/s": {60, 0}, "g/min": {1000, 0},
			"%/min": {0, 0}, "%/s": {0, 0},
		}
	}
	return map[string][2]float64{"": {1, 0}}
}

func normToken(u string) string {
	return strings.ToLower(strings.TrimSpace(u))
}

// Convert normalizes a textual value of the given kind. Values that cannot be
// parsed or whose unit is unknown are returned unconverted with ConvertOK=false
// and a diagnostic; they are never dropped.
func Convert(text, unit string, kind QuantityKind) Val {
	v := Val{Raw: text, RawUnit: unit, Kind: kind, Unit: canonicalUnits[kind]}
	f, err := parseFloat(text)
	if err != nil {
		v.Diagnostic = "unparseable numeric value"
		return v
	}
	u := normToken(unit)
	// dimensionless pass-through
	if kind == KindDimensionless || (kind == KindMass && u == "%") {
		v.Value = f
		v.Unit = unitOr(unit, "")
		v.ConvertOK = true
		return v
	}
	// temperature offsets
	if kind == KindTemperature {
		switch u {
		case "k", "kelvin":
			v.Value = f - 273.15
			v.ConvertOK = true
			return v
		case "f", "°f", "degf":
			v.Value = (f - 32) * 5 / 9
			v.ConvertOK = true
			return v
		}
	}
	tbl := unitTable(kind)
	row, ok := tbl[u]
	if !ok {
		v.Diagnostic = fmt.Sprintf("unknown %s unit %q", kind, unit)
		return v
	}
	v.Value = f*row[0] + row[1]
	v.ConvertOK = true
	return v
}

func unitOr(u, d string) string {
	if strings.TrimSpace(u) == "" {
		return d
	}
	return strings.TrimSpace(u)
}

// MustConvert converts an already numeric value into a canonical Val.
func MustConvert(f float64, kind QuantityKind) Val {
	return Val{Value: f, Unit: canonicalUnits[kind], Kind: kind, ConvertOK: true,
		Raw: fmt.Sprintf("%g", f), RawUnit: canonicalUnits[kind]}
}
