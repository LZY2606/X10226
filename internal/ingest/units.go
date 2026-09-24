package ingest

import (
	"regexp"
	"strconv"
	"strings"

	"tgareview/internal/model"
)

var qtyRe = regexp.MustCompile(`^\s*([+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)\s*([^\s]+)?\s*$`)

// ParseQuantity converts a token into a normalised quantity. kind is one of
// temp, time, mass, rate. The raw representation is always retained. Values
// that cannot be converted come back with OK=false and a diagnostic reason.
func ParseQuantity(token, kind string) model.Quantity {
	q := model.Quantity{Raw: token, RawUnit: "", OK: false}
	m := qtyRe.FindStringSubmatch(token)
	if m == nil {
		q.Reason = "unparseable_" + kind
		return q
	}
	num, rawUnit := m[1], strings.TrimSpace(m[2])
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		q.Reason = "unparseable_number"
		return q
	}
	q.RawUnit = rawUnit
	conv, unit, ok := convert(kind, v, rawUnit)
	if !ok {
		q.Value = v
		q.Reason = "unknown_unit"
		return q
	}
	q.Value, q.Unit, q.OK = conv, unit, true
	return q
}

func convert(kind string, v float64, u string) (float64, string, bool) {
	switch kind {
	case "temp":
		switch strings.ToLower(u) {
		case "", "c", "°c", "degc":
			return v, "C", true
		case "k":
			return v - 273.15, "C", true
		case "f":
			return (v - 32) * 5 / 9, "C", true
		}
	case "time":
		switch strings.ToLower(u) {
		case "", "s", "sec", "secs", "second", "seconds":
			return v, "s", true
		case "min", "mins", "minute", "minutes":
			return v * 60, "s", true
		case "h", "hr", "hour", "hours":
			return v * 3600, "s", true
		}
	case "mass":
		switch strings.ToLower(u) {
		case "", "mg":
			return v, "mg", true
		case "ug", "µg":
			return v / 1000, "mg", true
		case "g":
			return v * 1000, "mg", true
		case "kg":
			return v * 1e6, "mg", true
		}
	case "rate":
		// TG rates normalise to percent per minute.
		switch strings.ToLower(u) {
		case "", "%/min", "%min-1", "pct/min":
			return v, "%/min", true
		case "%/s", "pct/s":
			return v * 60, "%/min", true
		case "k/min", "c/min", "°c/min":
			return v, "C/min", true
		case "ml/min", "cm3/min", "sccm":
			return v, "mL/min", true
		}
	}
	return 0, "", false
}
