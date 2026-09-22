package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Dimension classifies a physical quantity. Canonical units are fixed for each
// dimension so the derived layer works only on normalized numbers.
type Dimension string

const (
	DimNone   Dimension = ""
	DimMass   Dimension = "mass"   // canonical: mg
	DimTemp   Dimension = "temp"   // canonical: degC
	DimTime   Dimension = "time"   // canonical: s
	DimRate   Dimension = "dmdt"   // canonical: mg/min
	DimPct    Dimension = "pct"    // canonical: %
	DimGas    Dimension = "gas"    // token identifier, no conversion
	DimNumber Dimension = "number" // dimensionless numeric
)

// CanonicalUnit returns the normalized unit token of a dimension.
func (d Dimension) CanonicalUnit() string {
	switch d {
	case DimMass:
		return "mg"
	case DimTemp:
		return "C"
	case DimTime:
		return "s"
	case DimRate:
		return "mg/min"
	case DimPct:
		return "%"
	default:
		return ""
	}
}

type unitDef struct {
	dim Dimension
	mul float64 // canonical = raw*mul + add
	add float64
}

// UnitTable maps every accepted representation to its conversion. Imports keep
// the original representation; only the derived layer normalizes.
var UnitTable = map[string]unitDef{
	// mass -> mg
	"mg":  {DimMass, 1, 0},
	"g":   {DimMass, 1000, 0},
	"ug":  {DimMass, 0.001, 0},
	"µg":  {DimMass, 0.001, 0},
	// temperature -> degC
	"C":   {DimTemp, 1, 0},
	"degC": {DimTemp, 1, 0},
	"°C":  {DimTemp, 1, 0},
	"K":   {DimTemp, 1, -273.15},
	// time -> s
	"s":   {DimTime, 1, 0},
	"sec": {DimTime, 1, 0},
	"min": {DimTime, 60, 0},
	"h":   {DimTime, 3600, 0},
	// dm/dt -> mg/min
	"mg/min": {DimRate, 1, 0},
	"mg/s":   {DimRate, 60, 0},
	"%/min":  {DimRate, 1, 0}, // percent rates are left as declared
	"%/s":    {DimRate, 60, 0},
	// percent
	"%":   {DimPct, 1, 0},
	"pct": {DimPct, 1, 0},
}

// Cell preserves one imported value exactly as received, alongside its
// normalized form. A value that cannot be converted stays in place with a
// diagnostic instead of being dropped to suit plotting.
type Cell struct {
	Raw       string   `json:"raw"`
	Unit      string   `json:"unit,omitempty"`
	Dim       Dimension `json:"dim,omitempty"`
	CanonUnit string   `json:"canon_unit,omitempty"`
	Value     *float64 `json:"value,omitempty"` // numeric value in declared unit
	Canon     *float64 `json:"canon,omitempty"` // normalized value
	Missing   bool     `json:"missing,omitempty"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

// MissingTokens are accepted as explicit "no measurement" markers. They do not
// interpolate across surrounding points.
var MissingTokens = map[string]bool{"": true, "na": true, "n/a": true, "nan": true, "-": true, "null": true}

// ParseCell parses one field. defaultUnit applies when the header carries the
// unit instead of the cell. expect constrains the dimension; DimNone accepts
// anything (gas tokens, free notes).
func ParseCell(raw, defaultUnit string, expect Dimension) Cell {
	c := Cell{Raw: raw, Dim: expect}
	tok := strings.TrimSpace(raw)
	if MissingTokens[strings.ToLower(tok)] {
		c.Missing = true
		return c
	}

	numTok, unitTok := splitNumberUnit(tok)
	if unitTok == "" {
		unitTok = defaultUnit
	}
	c.Unit = unitTok

	if expect == DimGas {
		return c
	}
	if expect == DimNone {
		return c
	}

	v, err := strconv.ParseFloat(numTok, 64)
	if err != nil {
		c.Diagnostic = fmt.Sprintf("unparseable %s value %q (kept in place)", expect, tok)
		return c
	}
	c.Value = &v

	def, ok := UnitTable[strings.TrimSpace(unitTok)]
	if !ok {
		c.Diagnostic = fmt.Sprintf("unknown unit %q (original value retained)", unitTok)
		return c
	}
	if expect != DimNumber && def.dim != expect {
		c.Diagnostic = fmt.Sprintf("unit %q is %s, expected %s", unitTok, def.dim, expect)
		return c
	}
	cv := v*def.mul + def.add
	c.Canon = &cv
	c.CanonUnit = expect.CanonicalUnit()
	return c
}

func splitNumberUnit(s string) (num, unit string) {
	s = strings.TrimSpace(s)
	for i, r := range s {
		if r >= '0' && r <= '9' || r == '.' || r == '-' || r == '+' || r == 'e' || r == 'E' {
			continue
		}
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i:])
	}
	return s, ""
}

// CanonOrNil returns the normalized numeric value or nil when missing/invalid.
func (c Cell) CanonOrNil() *float64 { return c.Canon }

// Fmt renders the value with its declared unit for traceability.
func (c Cell) Fmt() string {
	switch {
	case c.Missing:
		return "N/A"
	case c.Diagnostic != "":
		return c.Raw + " !" + c.Diagnostic
	case c.Canon != nil:
		return fmt.Sprintf("%g %s", *c.Canon, c.CanonUnit)
	default:
		return c.Raw
	}
}
