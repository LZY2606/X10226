package domain

// Q is a domain quantity that keeps BOTH the original textual representation
// (as submitted by the operator) and the normalized SI-ish value. When a value
// cannot be converted, Value stays NaN-ish and a diagnostic is attached in
// place; the raw value is never dropped just to make the chart render.
type Q struct {
	Raw     string  `json:"raw"`              // original representation, preserved
	Unit    string  `json:"unit"`             // declared unit/coordinate basis
	Value   float64 `json:"value"`            // normalized value; NaN if unconvertible
	Valid   bool    `json:"valid"`            // conversion succeeded
	Note    string  `json:"note,omitempty"`   // diagnostic when !Valid
}

func Quantity(raw, unit string, value float64, valid bool, note string) Q {
	return Q{Raw: raw, Unit: unit, Value: value, Valid: valid, Note: note}
}

// Unit bases used across the system. All derived math uses these normalized
// bases; every emitted number carries its unit.
const (
	UnitSecond   = "s"
	UnitCelsius  = "°C"   // furnace program labels are kept in °C
	UnitKelvin   = "K"
	UnitMg       = "mg"
	UnitGram     = "g"
	UnitPercent  = "%"
	UnitMgPerMin = "mg/min"
	UnitPercentPerC = "%/°C"
)
