package importpkg

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"tga-evidence/internal/domain"
)

type ParseResult struct {
	Batch domain.Batch
}

func ParseText(text string) ParseResult {
	lines := splitPhysicalLines(text)
	if len(lines) == 0 {
		return ParseResult{Batch: domain.Batch{RawText: text, Diagnostics: []string{"empty import"}}}
	}
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	rows, err := r.ReadAll()
	if err != nil {
		return parseLongForm(lines, text, "CSV parse error: "+err.Error())
	}
	if len(rows) >= 1 && looksLongHeader(rows[0]) {
		return parseRows(rows, text)
	}
	return parseLongForm(lines, text, "missing required header layer,time")
}

func parseRows(rows [][]string, text string) ParseResult {
	header := map[string]int{}
	for i, h := range rows[0] {
		header[strings.TrimSpace(strings.ToLower(h))] = i
	}
	var recs []domain.Record
	var diags []string
	accepted, rejected := 0, 0
	for ri := 1; ri < len(rows); ri++ {
		rawLine := strings.Join(rows[ri], ",")
		if strings.TrimSpace(rawLine) == "" {
			continue
		}
		rec, err := recordFromRow(header, rows[ri], ri+1, rawLine)
		if err != nil {
			rejected++
			rec.Valid = false
			rec.Diagnostic = err.Error()
			diags = append(diags, fmt.Sprintf("line %d: %s", ri+1, err.Error()))
		} else {
			accepted++
		}
		recs = append(recs, rec)
	}
	return ParseResult{Batch: domain.Batch{Records: recs, AcceptedCount: accepted, RejectedCount: rejected, RawText: text, Diagnostics: diags}}
}

func looksLongHeader(row []string) bool {
	for _, cell := range row {
		if strings.EqualFold(strings.TrimSpace(cell), "layer") {
			return true
		}
	}
	return false
}

func recordFromRow(header map[string]int, row []string, line int, rawLine string) (domain.Record, error) {
	get := func(names ...string) string {
		for _, n := range names {
			if idx, ok := header[n]; ok && idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
		}
		return ""
	}
	layer := get("layer", "kind")
	if layer == "" {
		return domain.Record{RawText: rawLine}, fmt.Errorf("layer is required")
	}
	var problems []string
	tRaw := get("time", "furnace_time", "t")
	tUnit := orDefault(get("time_unit", "time_uom"), "s")
	tq := quantity(tRaw, tUnit, "furnace time")
	if tq.Diagnostic != "" {
		problems = append(problems, tq.Diagnostic)
	}
	tempRaw := get("temperature", "temp", "sample_temperature")
	temp := optionalQuantity(tempRaw, get("temperature_unit", "temp_unit"), "degC", "sample temperature")
	if temp.Diagnostic != "" {
		problems = append(problems, temp.Diagnostic)
	}
	mass := optionalQuantity(get("mass", "sample_mass", "m"), get("mass_unit"), "mg", "sample mass")
	if mass.Diagnostic != "" {
		problems = append(problems, mass.Diagnostic)
	}
	initial := optionalQuantity(get("initial_mass", "m0"), get("initial_mass_unit"), "mg", "initial mass")
	if initial.Diagnostic != "" {
		problems = append(problems, initial.Diagnostic)
	}
	drift := optionalQuantity(get("drift", "balance_drift"), get("drift_unit"), "mg", "balance drift")
	if drift.Diagnostic != "" {
		problems = append(problems, drift.Diagnostic)
	}
	target := optionalQuantity(get("target_temperature"), get("target_temperature_unit"), "degC", "target temperature")
	ramp := optionalQuantity(get("ramp_rate"), get("ramp_rate_unit"), "degC/min", "ramp rate")
	rec := domain.Record{
		ID: stableRecordID(line, rawLine), Layer: layer, Time: tq, Temperature: temp, Mass: mass,
		InitialMass: initial, Drift: drift, Gas: get("gas", "atmosphere"), Segment: get("segment", "program_segment"),
		TargetTemp: target, RampRate: ramp, Note: get("note", "comment"), RawText: rawLine,
		Supersedes: get("supersedes"), Extras: map[string]string{},
	}
	for _, name := range []string{"id", "source", "operator", "batch"} {
		if v := get(name); v != "" {
			rec.Extras[name] = v
		}
	}
	if len(problems) > 0 {
		return rec, fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	rec.Valid = true
	return rec, nil
}

func quantity(raw, unit, label string) domain.Quantity {
	raw = strings.TrimSpace(raw)
	unit = strings.TrimSpace(unit)
	if raw == "" {
		return domain.Quantity{Raw: raw, Unit: unit, Diagnostic: label + " is required"}
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return domain.Quantity{Raw: raw, Unit: unit, Diagnostic: "cannot convert " + label + " \"" + raw + "\" to number"}
	}
	return domain.Quantity{Raw: raw, Value: v, Unit: normalizeUnit(unit)}
}

func optionalQuantity(raw, unit, defaultUnit, label string) domain.Quantity {
	if strings.TrimSpace(raw) == "" {
		return domain.Quantity{Raw: "", Unit: normalizeUnit(defaultUnit)}
	}
	return quantity(raw, orDefault(unit, defaultUnit), label)
}

func normalizeUnit(unit string) string {
	u := strings.TrimSpace(strings.ToLower(unit))
	switch u {
	case "c", "celsius", "°c", "deg c", "degree_c":
		return "degC"
	case "s", "sec", "seconds", "second":
		return "s"
	case "m", "min", "minute", "minutes":
		return "min"
	case "mg":
		return "mg"
	case "%", "percent":
		return "%"
	default:
		return unit
	}
}

func orDefault(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}

func stableRecordID(line int, raw string) string {
	return "rec:" + domain.StableHash(strconv.Itoa(line)+"|"+raw)
}

func splitPhysicalLines(text string) []string {
	var out []string
	s := bufio.NewScanner(strings.NewReader(text))
	for s.Scan() {
		out = append(out, s.Text())
	}
	return out
}

func parseLongForm(lines []string, text, fatal string) ParseResult {
	var recs []domain.Record
	var diags []string
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		parts := strings.Fields(trim)
		rec := domain.Record{ID: stableRecordID(i+1, line), RawText: line, Valid: false, Diagnostic: "not a long-form TGA row"}
		_ = parts
		recs = append(recs, rec)
		diags = append(diags, fmt.Sprintf("line %d: %s", i+1, fatal))
	}
	if len(recs) == 0 {
		diags = append(diags, fatal)
	}
	return ParseResult{Batch: domain.Batch{Records: recs, RejectedCount: len(recs), RawText: text, Fragment: text, Diagnostics: diags}}
}

func ParseWithClock(text string, _ time.Time) ParseResult { return ParseText(text) }

var _ io.Reader = strings.Reader{}
