// Package tga holds the domain model for thermogravimetric review:
// units, immutable source parsing, and the deterministic stage engine.
package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type Unit string

const (
	UnitS      Unit = "s"
	UnitMin    Unit = "min"
	UnitMg     Unit = "mg"
	UnitG      Unit = "g"
	UnitC      Unit = "°C"
	UnitK      Unit = "K"
	UnitPct    Unit = "%"
	UnitPctMin Unit = "%/min"
	UnitMLMin  Unit = "mL/min"
	UnitNone   Unit = ""
)

// Measure is a quantity with its unit and, when imported, the original
// textual representation. Raw layers keep Raw untouched; only the derived
// layer normalizes units.
type Measure struct {
	Value float64 `json:"value"`
	Unit  Unit    `json:"unit"`
	Raw   string  `json:"raw,omitempty"`
}

func (m Measure) String() string {
	if m.Unit == UnitNone {
		return fmt.Sprintf("%g", m.Value)
	}
	return fmt.Sprintf("%g %s", m.Value, m.Unit)
}

// Normalize converts a measure to the canonical derived-layer unit
// (s, mg, °C, %, %/min). The original representation is preserved in Raw.
// Values with unknown units are returned with an error so callers can keep
// them in place with a diagnostic instead of dropping them.
func Normalize(m Measure) (Measure, error) {
	if m.Raw == "" {
		m.Raw = m.String()
	}
	switch m.Unit {
	case UnitS, UnitMg, UnitC, UnitPct, UnitPctMin, UnitMLMin, UnitNone:
		return m, nil
	case UnitMin:
		return Measure{Value: m.Value * 60, Unit: UnitS, Raw: m.Raw}, nil
	case UnitG:
		return Measure{Value: m.Value * 1000, Unit: UnitMg, Raw: m.Raw}, nil
	case UnitK:
		return Measure{Value: m.Value - 273.15, Unit: UnitC, Raw: m.Raw}, nil
	}
	return m, fmt.Errorf("unit %q cannot be normalized", m.Unit)
}

// Hash returns a short deterministic identity for content-addressed ids.
func Hash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ValueID gives every displayed value a stable identity that survives
// export, deletion of the local database, and re-import.
func ValueID(owner, label string) string { return "v" + Hash(owner, label) }

// QVal is a displayable quantity with a stable identity. Stale values stay
// viewable but may not masquerade as current.
type QVal struct {
	VID     string  `json:"vid"`
	Label   string  `json:"label"`
	M       Measure `json:"m"`
	Stale   bool    `json:"stale,omitempty"`
	Unknown bool    `json:"unknown,omitempty"`
	Note    string  `json:"note,omitempty"`
}

func NewQVal(owner, label string, m Measure) QVal {
	return QVal{VID: ValueID(owner, label), Label: label, M: m}
}
