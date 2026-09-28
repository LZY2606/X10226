package ingest

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"tga-review/internal/domain"
)

type Fragment struct {
	Name string
	Kind string
	Raw  string
}

type ParsedBundle struct {
	Fragments    []Fragment
	Measurements []domain.MeasurementSample
	Drift        []domain.DriftCorrection
	Furnace      []domain.FurnaceSegment
	Valves       []domain.GasValveEvent
	Blank        []domain.BlankPoint
	Diagnostics  []string
}

type ParseError struct {
	Fragment string
	Line     int
	Message  string
}

func (e ParseError) Error() string {
	return fmt.Sprintf("%s line %d: %s", e.Fragment, e.Line, e.Message)
}

func ParseFragments(fragments []Fragment) ParsedBundle {
	out := ParsedBundle{Fragments: append([]Fragment(nil), fragments...)}
	for _, f := range fragments {
		records, err := readCSV(f.Name, f.Raw)
		if err != nil {
			out.Diagnostics = append(out.Diagnostics, err.Error())
			continue
		}
		switch f.Kind {
		case "measurements":
			out.Measurements, out.Diagnostics = parseMeasurements(records, out.Diagnostics)
		case "drift":
			out.Drift, out.Diagnostics = parseDrift(records, out.Diagnostics)
		case "furnace":
			out.Furnace, out.Diagnostics = parseFurnace(records, out.Diagnostics)
		case "valves":
			out.Valves, out.Diagnostics = parseValves(records, out.Diagnostics)
		case "blank":
			out.Blank, out.Diagnostics = parseBlank(records, out.Diagnostics)
		default:
			out.Diagnostics = append(out.Diagnostics, fmt.Sprintf("%s: unknown fragment kind %q", f.Name, f.Kind))
		}
	}
	return out
}

func readCSV(name, raw string) (map[string][]string, error) {
	r := csv.NewReader(strings.NewReader(raw))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, ParseError{Fragment: name, Line: 0, Message: err.Error()}
	}
	if len(rows) < 2 {
		return nil, ParseError{Fragment: name, Line: 1, Message: "CSV requires header and at least one data row"}
	}
	records := make(map[string][]string)
	header := rows[0]
	for _, row := range rows[1:] {
		if isEmpty(row) {
			continue
		}
		if len(row) != len(header) {
			return nil, ParseError{Fragment: name, Line: len(header) + 1, Message: fmt.Sprintf("column count %d, want %d", len(row), len(header))}
		}
		for i, key := range header {
			records[strings.TrimSpace(key)] = append(records[strings.TrimSpace(key)], strings.TrimSpace(row[i]))
		}
	}
	required := map[string]bool{"id": true}
	if !required["id"] {
	}
	if len(records["id"]) == 0 {
		return nil, ParseError{Fragment: name, Line: 1, Message: "missing required id column"}
	}
	return records, nil
}

func isEmpty(row []string) bool {
	for _, v := range row {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

func parseMeasurements(records map[string][]string, diags []string) ([]domain.MeasurementSample, []string) {
	n := len(records["id"])
	out := make([]domain.MeasurementSample, 0, n)
	for i := 0; i < n; i++ {
		m := domain.MeasurementSample{ID: at(records, "id", i), SegmentID: at(records, "segment_id", i)}
		m.Time = parseUnitValue(at(records, "time", i), "min", "furnace_time", "time")
		m.Temperature = parseUnitValue(at(records, "temperature", i), "°C", "sample_temperature", "temperature")
		m.Mass = parseUnitValue(at(records, "mass", i), "mg", "balance_mass", "mass")
		if rawGap := at(records, "gap_after_min", i); rawGap != "" {
			v := parseUnitValue(rawGap, "min", "sampling_gap", "gap")
			if v.Convertible {
				m.GapAfterMin = &v.Value
			} else {
				diags = append(diags, fmt.Sprintf("measurement %s: unconvertible sampling gap %q kept raw", m.ID, rawGap))
			}
		}
		if !m.Time.Convertible || !m.Temperature.Convertible || !m.Mass.Convertible {
			diags = append(diags, fmt.Sprintf("measurement %s: at least one value is unconvertible and was not used for derived plots", m.ID))
		}
		out = append(out, m)
	}
	return out, diags
}

func parseDrift(records map[string][]string, diags []string) ([]domain.DriftCorrection, []string) {
	for i := range records["id"] {
		d := domain.DriftCorrection{ID: at(records, "id", i), Method: at(records, "method", i)}
		d.Start = parseUnitValue(at(records, "start", i), "min", "furnace_time", "time")
		d.ReferenceTime = parseUnitValue(at(records, "reference_time", i), "min", "furnace_time", "time")
		d.Slope = parseUnitValue(at(records, "slope", i), "mg/min", "balance_drift_rate", "mass_rate")
		d.BaselineMass = parseUnitValue(at(records, "baseline_mass", i), "mg", "balance_baseline_mass", "mass")
		diags = appendInvalid(diags, "drift", d.ID, d.Start, d.ReferenceTime, d.Slope, d.BaselineMass)
		diags = append(diags, parsed(len(diags), d.ID)...)
		_ = d
	}
	return nil, diags
}

func parseFurnace(records map[string][]string, diags []string) ([]domain.FurnaceSegment, []string) {
	out := make([]domain.FurnaceSegment, 0, len(records["id"]))
	for i := range records["id"] {
		f := domain.FurnaceSegment{ID: at(records, "id", i), ProgramID: at(records, "program_id", i)}
		f.Start = parseUnitValue(at(records, "start", i), "min", "furnace_time", "time")
		f.End = parseUnitValue(at(records, "end", i), "min", "furnace_time", "time")
		f.Setpoint = parseUnitValue(at(records, "setpoint", i), "°C", "furnace_setpoint", "temperature")
		f.Rate = parseUnitValue(at(records, "rate", i), "°C/min", "furnace_ramp_rate", "temperature_rate")
		diags = appendInvalid(diags, "furnace", f.ID, f.Start, f.End, f.Setpoint, f.Rate)
		out = append(out, f)
	}
	return out, diags
}

func parseValves(records map[string][]string, diags []string) ([]domain.GasValveEvent, []string) {
	out := make([]domain.GasValveEvent, 0, len(records["id"]))
	for i := range records["id"] {
		v := domain.GasValveEvent{ID: at(records, "id", i), FromGas: at(records, "from_gas", i), ToGas: at(records, "to_gas", i)}
		v.ValveTime = parseUnitValue(at(records, "valve_time", i), "min", "valve_clock", "time")
		v.ObservedDelay = parseUnitValue(at(records, "observed_delay", i), "min", "gas_transport_delay", "time")
		v.TemperatureLag = parseUnitValue(at(records, "temperature_lag", i), "°C", "sample_to_program_lag", "temperature")
		v.ReactionTime = parseUnitValue(at(records, "reaction_time", i), "min", "sample_reaction_time", "time")
		diags = appendInvalid(diags, "valve", v.ID, v.ValveTime, v.ObservedDelay, v.TemperatureLag, v.ReactionTime)
		out = append(out, v)
	}
	return out, diags
}

func parseBlank(records map[string][]string, diags []string) ([]domain.BlankPoint, []string) {
	out := make([]domain.BlankPoint, 0, len(records["id"]))
	for i := range records["id"] {
		b := domain.BlankPoint{ID: at(records, "id", i)}
		b.Time = parseUnitValue(at(records, "time", i), "min", "furnace_time", "time")
		b.Mass = parseUnitValue(at(records, "mass", i), "mg", "blank_balance_mass", "mass")
		diags = appendInvalid(diags, "blank", b.ID, b.Time, b.Mass)
		out = append(out, b)
	}
	return out, diags
}

func at(records map[string][]string, key string, i int) string {
	vals := records[key]
	if i >= len(vals) {
		return ""
	}
	return vals[i]
}

func parsed(_ int, _ string) []string { return nil }

func appendInvalid(diags []string, kind, id string, vals ...domain.Value) []string {
	for _, v := range vals {
		if !v.Convertible {
			diags = append(diags, fmt.Sprintf("%s %s: unconvertible value %q kept in place: %s", kind, id, v.Raw, v.Diagnostic))
		}
	}
	return diags
}

func parseUnitValue(raw, defaultUnit, basis, kind string) domain.Value {
	raw = strings.TrimSpace(raw)
	v := domain.Value{Raw: raw, Unit: defaultUnit, Basis: basis}
	if raw == "" {
		v.Convertible = false
		v.Diagnostic = "empty value"
		return v
	}
	numPart, unitPart := splitQuantity(raw)
	num, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		v.Convertible = false
		v.Diagnostic = fmt.Sprintf("invalid %s number", kind)
		v.Unit = unitOr(unitPart, defaultUnit)
		return v
	}
	v.Value = num
	unit := unitOr(unitPart, defaultUnit)
	converted, normUnit, ok := normalize(num, unit, kind)
	if !ok {
		v.Convertible = false
		v.Unit = unit
		v.Diagnostic = fmt.Sprintf("unsupported unit %q for %s", unit, kind)
		return v
	}
	v.Value = converted
	v.Unit = normUnit
	v.Convertible = true
	return v
}

func splitQuantity(raw string) (string, string) {
	for i, r := range raw {
		if (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '+' || r == 'e' || r == 'E' {
			continue
		}
		return strings.TrimSpace(raw[:i]), strings.TrimSpace(raw[i:])
	}
	return raw, ""
}

func unitOr(unit, fallback string) string {
	if unit == "" {
		return fallback
	}
	return unit
}

func normalize(value float64, unit, kind string) (float64, string, bool) {
	switch kind {
	case "time":
		switch strings.ToLower(unit) {
		case "min", "minute", "minutes":
			return value, "min", true
		case "s", "sec", "second", "seconds":
			return value / 60, "min", true
		case "h", "hr", "hour", "hours":
			return value * 60, "min", true
		}
	case "temperature":
		switch strings.ToLower(unit) {
		case "c", "°c", "degc":
			return value, "°C", true
		case "k":
			return value - 273.15, "°C", true
		}
	case "mass":
		switch strings.ToLower(unit) {
		case "mg":
			return value, "mg", true
		case "g":
			return value * 1000, "mg", true
		case "ug", "µg":
			return value / 1000, "mg", true
		}
	case "mass_rate":
		switch strings.ToLower(unit) {
		case "mg/min":
			return value, "mg/min", true
		case "mg/s":
			return value * 60, "mg/min", true
		}
	case "temperature_rate":
		switch strings.ToLower(unit) {
		case "°c/min", "c/min":
			return value, "°C/min", true
		case "k/min":
			return value, "°C/min", true
		}
	case "gap":
		switch strings.ToLower(unit) {
		case "min":
			return value, "min", true
		case "s":
			return value / 60, "min", true
		}
	}
	return value, unit, false
}

func EnsureReader(_ io.Reader) {}
