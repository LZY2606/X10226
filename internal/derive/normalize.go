// Package derive turns immutable raw sources into normalized derived values.
// Raw payloads are never mutated: unconvertible values stay in place with a
// diagnostic, and only the derived layer performs normalization.
package derive

import (
	"fmt"
	"strconv"
	"strings"

	"tgareview/internal/domain"
)

// ConvertError marks a value that cannot be normalized.
type ConvertError struct{ Reason string }

func (e *ConvertError) Error() string { return e.Reason }

func parseNum(raw string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0, &ConvertError{Reason: fmt.Sprintf("not a number: %q", raw)}
	}
	return v, nil
}

// ToSeconds normalizes time quantities.
func ToSeconds(q domain.Quantity) (float64, error) {
	v, err := parseNum(q.Raw)
	if err != nil {
		return 0, err
	}
	switch strings.ToLower(strings.TrimSpace(q.Unit)) {
	case "s", "sec", "secs", "second", "seconds":
		return v, nil
	case "min", "mins", "minute", "minutes":
		return v * 60, nil
	case "h", "hr", "hour", "hours":
		return v * 3600, nil
	}
	return 0, &ConvertError{Reason: fmt.Sprintf("unknown time unit %q", q.Unit)}
}

// ToCelsius normalizes temperature quantities.
func ToCelsius(q domain.Quantity) (float64, error) {
	v, err := parseNum(q.Raw)
	if err != nil {
		return 0, err
	}
	switch strings.ToLower(strings.TrimSpace(q.Unit)) {
	case "c", "°c", "degc", "celsius":
		return v, nil
	case "k", "kelvin":
		return v - 273.15, nil
	}
	return 0, &ConvertError{Reason: fmt.Sprintf("unknown temperature unit %q", q.Unit)}
}

// ToMg normalizes mass quantities.
func ToMg(q domain.Quantity) (float64, error) {
	v, err := parseNum(q.Raw)
	if err != nil {
		return 0, err
	}
	switch strings.ToLower(strings.TrimSpace(q.Unit)) {
	case "mg":
		return v, nil
	case "g":
		return v * 1000, nil
	case "ug", "µg":
		return v / 1000, nil
	}
	return 0, &ConvertError{Reason: fmt.Sprintf("unknown mass unit %q", q.Unit)}
}

// ToMlPerMin normalizes gas flow quantities.
func ToMlPerMin(q domain.Quantity) (float64, error) {
	v, err := parseNum(q.Raw)
	if err != nil {
		return 0, err
	}
	switch strings.ToLower(strings.TrimSpace(q.Unit)) {
	case "ml/min", "sccm":
		return v, nil
	case "l/min":
		return v * 1000, nil
	}
	return 0, &ConvertError{Reason: fmt.Sprintf("unknown flow unit %q", q.Unit)}
}

// ToMgPerMin normalizes drift rate quantities.
func ToMgPerMin(q domain.Quantity) (float64, error) {
	v, err := parseNum(q.Raw)
	if err != nil {
		return 0, err
	}
	switch strings.ToLower(strings.TrimSpace(q.Unit)) {
	case "mg/min":
		return v, nil
	case "mg/s":
		return v * 60, nil
	case "ug/min":
		return v / 1000, nil
	}
	return 0, &ConvertError{Reason: fmt.Sprintf("unknown drift-rate unit %q", q.Unit)}
}
