package domain

import "fmt"

// Quantity is a domain value carrying its unit / coordinate basis.
// Raw layers keep the original textual representation; normalization to the
// canonical basis (s, °C, mg) happens only in the derived layer.
type Quantity struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Canonical bases used by the derived layer.
const (
	UnitTime = "s"
	UnitTemp = "C"
	UnitMass = "mg"
	UnitFlow = "ml/min"
)

// ToSeconds converts a time quantity to seconds.
func ToSeconds(v float64, unit string) (float64, error) {
	switch unit {
	case "s", "sec", "second", "seconds":
		return v, nil
	case "min", "minute", "minutes":
		return v * 60, nil
	case "h", "hr", "hour", "hours":
		return v * 3600, nil
	}
	return 0, fmt.Errorf("unknown time unit %q", unit)
}

// ToCelsius converts a temperature quantity to °C.
func ToCelsius(v float64, unit string) (float64, error) {
	switch unit {
	case "C", "degC", "celsius":
		return v, nil
	case "K", "kelvin":
		return v - 273.15, nil
	case "F", "fahrenheit":
		return (v - 32) * 5 / 9, nil
	}
	return 0, fmt.Errorf("unknown temperature unit %q", unit)
}

// ToMilligrams converts a mass quantity to mg.
func ToMilligrams(v float64, unit string) (float64, error) {
	switch unit {
	case "mg":
		return v, nil
	case "g":
		return v * 1000, nil
	case "ug", "µg":
		return v / 1000, nil
	case "kg":
		return v * 1e6, nil
	}
	return 0, fmt.Errorf("unknown mass unit %q", unit)
}
