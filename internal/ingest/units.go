package ingest

import (
	"fmt"

	"tga-review/internal/domain"
)

type u = domain.Unit

const (
	UMg      = domain.UnitMilligram
	US       = domain.UnitSecond
	UDegC    = domain.UnitCelsius
	UK       = domain.UnitKelvin
	UKPerMin = domain.UnitPerMinute
)

func normUnit(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '°' {
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r = r + ('a' - 'A')
		}
		out = append(out, r)
	}
	return string(out)
}

// convertUnit normalizes supported aliases to canonical units.
// Unknown units are rejected so the value stays at its original position.
func convertUnit(v float64, from string, to u) (float64, error) {
	from = normUnit(from)
	switch to {
	case UMg:
		switch from {
		case "mg":
			return v, nil
		case "g":
			return v * 1000, nil
		case "ug":
			return v / 1000, nil
		}
	case US:
		switch from {
		case "s", "sec", "secs":
			return v, nil
		case "min", "m":
			return v * 60, nil
		}
	case UDegC:
		switch from {
		case "c", "degc":
			return v, nil
		case "k":
			return v - 273.15, nil
		}
	case UK:
		switch from {
		case "k":
			return v, nil
		case "c", "degc":
			return v + 273.15, nil
		}
	case UKPerMin:
		switch from {
		case "k/min", "c/min":
			return v, nil
		case "k/s":
			return v * 60, nil
		}
	}
	return 0, fmt.Errorf("cannot convert %q to %q", from, to)
}
