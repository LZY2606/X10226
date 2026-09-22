// Package ingest parses the six stratified input layers. Two rules are
// non-negotiable:
//
//  1. every submitted cell is retained verbatim in Record.Raw; parsing never
//     overwrites the original representation;
//  2. a value that cannot be converted to the declared unit stays in place
//     with a diagnostic — rows are dropped from derivation, never from the
//     archive.
//
// Two envelopes are accepted: a single CSV file for one layer, or a
// reproducible text bundle ("== file: name ==" separators) that carries
// several files, e.g. a case pack. Bundles let the tests exercise
// file-level half failures while single files exercise row-level ones.
package ingest

import (
	"bufio"
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"tga-lineage/internal/identity"
	"tga-lineage/internal/model"
)

type FileResult struct {
	Name       string
	Kind       string // resolved layer kind, "" if unknown
	OK         bool
	Layer      *model.LayerVersion // populated when Kind resolved (may be partial)
	Diagnostic *model.Diagnostic   // set when OK == false (whole file rejected)
	Raw        []byte
}

type BundleResult struct {
	Files      []FileResult
	SHA        string
	HasBadFile bool
}

// known unit synonyms, normalized into canonical unit symbols. Conversion is
// only applied at the derived layer; the raw text is always preserved.
var unitSynonyms = map[string]string{
	"mg": "mg", "milligram": "mg", "milligrams": "mg",
	"g": "g", "gram": "g", "grams": "g",
	"ug": "ug", "µg": "ug",
	"c": "degC", "°c": "degC", "degc": "degC", "k": "K", "f": "degF",
	"min": "min", "minute": "min", "minutes": "min",
	"s": "s", "sec": "s", "second": "s",
	"%": "%", "percent": "%", "pct": "%",
	"n2": "N2", "nitrogen": "N2", "ar": "Ar", "argon": "Ar",
	"air": "Air", "o2": "O2", "oxygen": "O2", "co2": "CO2",
}

var fileKindHints = map[string]string{
	"samples": model.LayerSamples, "sample": model.LayerSamples, "tg": model.LayerSamples,
	"drift": model.LayerDrift, "blank": model.LayerBlank,
	"furnace": model.LayerFurnace, "program": model.LayerFurnace,
	"valve": model.LayerValves, "valves": model.LayerValves, "gas": model.LayerValves,
	"initial": model.LayerInitial, "m0": model.LayerInitial, "mass0": model.LayerInitial,
}

var marker = regexp.MustCompile(`^==\s*file:\s*(.+?)\s*==\s*$`)

// SplitBundle divides a bundle into named files. A plain CSV passes through
// as a single unnamed file (name assigned by the caller).
func SplitBundle(data []byte) [][2]string {
	var files [][2]string
	var cur strings.Builder
	name := ""
	started := false
	flush := func() {
		if started || cur.Len() > 0 {
			files = append(files, [2]string{name, cur.String()})
		}
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		if m := marker.FindStringSubmatch(line); m != nil {
			flush()
			name, cur, started = strings.TrimSpace(m[1]), strings.Builder{}, true
			continue
		}
		if started || strings.TrimSpace(line) != "" {
			started = true
			cur.WriteString(line)
			cur.WriteByte('\n')
		}
	}
	flush()
	return files
}

func resolveKind(name string, header []string) string {
	base := strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(name), ".csv"), ".txt"))
	if k, ok := fileKindHints[base]; ok {
		return k
	}
	for token := range strings.SplitSeq(base, "_") {
		if k, ok := fileKindHints[token]; ok {
			return k
		}
	}
	// structural fallback from columns
	joined := strings.ToLower(strings.Join(header, ","))
	switch {
	case strings.Contains(joined, "valve") || strings.Contains(joined, "gas_event"):
		return model.LayerValves
	case strings.Contains(joined, "blank_mg"):
		return model.LayerBlank
	case strings.Contains(joined, "drift"):
		return model.LayerDrift
	case strings.Contains(joined, "setpoint"):
		return model.LayerFurnace
	case strings.Contains(joined, "initial_mass"):
		return model.LayerInitial
	case strings.Contains(joined, "mass_mg") && strings.Contains(joined, "temp_c"):
		return model.LayerSamples
	}
	return ""
}

// canonicalField strips a trailing [unit] annotation from a header.
func splitHeader(h string) (field, unit string) {
	h = strings.TrimSpace(h)
	if i := strings.LastIndex(h, "["); i >= 0 && strings.HasSuffix(h, "]") {
		field = strings.TrimSpace(h[:i])
		unit = strings.TrimSpace(h[i+1 : len(h)-1])
		return
	}
	return h, ""
}

func normUnit(u string) string {
	if u == "" {
		return ""
	}
	if c, ok := unitSynonyms[strings.ToLower(strings.TrimSpace(u))]; ok {
		return c
	}
	return strings.TrimSpace(u) // unknown unit kept; conversion attempt flagged
}

// ParseFile turns one named file into a FileResult. project and parent are
// used to build the stable layer uid; nextVersion is the version number to
// assign on commit (versions are owned by the project service).
func ParseFile(project, name string, data []byte, nextVersion int, parent string) FileResult {
	fr := FileResult{Name: name, Raw: bytes.Clone(data)}
	text := strings.TrimPrefix(string(data), "\ufeff")
	lines := strings.Split(text, "\n")
	var rows [][]string
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		rows = append(rows, parseCSVLine(ln))
	}
	if len(rows) == 0 {
		fr.Diagnostic = &model.Diagnostic{Ref: name, Field: "<file>", Code: "empty_file",
			Message: "file contains no rows"}
		return fr
	}
	header := rows[0]
	kind := resolveKind(name, header)
	if kind == "" {
		fr.Diagnostic = &model.Diagnostic{Ref: name, Field: "<file>", Code: "unknown_layer",
			Message: fmt.Sprintf("cannot map %q to one of the six stratified layers", name)}
		return fr
	}
	fr.Kind = kind
	fr.OK = true

	fields := make([]string, len(header))
	units := make([]string, len(header))
	for i, h := range header {
		f, u := splitHeader(h)
		fields[i] = f
		units[i] = normUnit(u)
	}

	lv := &model.LayerVersion{
		UID:     layerUID(project, kind, nextVersion),
		Project: project,
		Kind:    kind,
		Version: nextVersion,
		Parent:  parent,
		Source:  name,
		SHA256:  fmt.Sprintf("%x", shaBytes(data)),
		Status:  model.ImportOK,
		Basis: map[string]string{
			"time_basis": "furnace_elapsed_min",
			"temp_basis": "sensor_degC",
			"mass_basis": "balance_mg",
		},
	}

	badRows := 0
	for ri, row := range rows[1:] {
		rec := model.Record{
			ID:     identity.Hash("rec_", project, kind, name, strconv.Itoa(ri+2), joinCells(row)),
			Row:    ri + 2,
			Raw:    map[string]string{},
			Parsed: map[string]float64{},
			Unit:   map[string]string{},
		}
		for ci, cell := range row {
			if ci >= len(fields) {
				continue
			}
			rec.Raw[fields[ci]] = cell
			rec.Unit[fields[ci]] = units[ci]
			v, err := convertCell(strings.TrimSpace(cell), units[ci])
			if err != nil {
				if numericField(fields[ci]) {
					lv.Diags = append(lv.Diags, model.Diagnostic{
						Ref: rec.ID, Field: fields[ci], Raw: cell,
						Code: errCode(err), Message: err.Error(),
					})
					rec.OK = false
					rec.Reason = err.Error()
				}
				continue
			}
			if v != nil {
				rec.Parsed[fields[ci]] = *v
			}
		}
		if rowOK(kind, fields, rec) {
			rec.OK = true
		} else {
			badRows++
		}
		lv.Records = append(lv.Records, rec)
	}
	if len(lv.Records) == 0 {
		lv.Status = model.ImportFailed
		fr.OK = false
		lv.Diags = append(lv.Diags, model.Diagnostic{Ref: name, Field: "<file>",
			Code: "no_data_rows", Message: "header present but no data rows"})
	} else if badRows > 0 {
		lv.Status = model.ImportPartial
	}
	fr.Layer = lv
	return fr
}

func joinCells(row []string) string { return strings.Join(row, "|") }

type cellErr struct{ code, msg string }

func (e *cellErr) Error() string { return e.msg }

func errCode(err error) string {
	if ce, ok := err.(*cellErr); ok {
		return ce.code
	}
	return "unparseable"
}

// convertCell returns nil,nil for genuinely textual columns (gas, phase,
// note). Numeric columns return a typed value or a coded error; unknown units
// are reported with unit_unknown rather than silently assumed.
func convertCell(raw, unit string) (*float64, error) {
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, &cellErr{"non_numeric", fmt.Sprintf("value %q is not numeric", raw)}
	}
	switch unit {
	case "":
		return &v, nil
	case "mg", "degC", "min", "%", "degF", "K", "g", "ug", "s":
	default:
		return nil, &cellErr{"unit_unknown", fmt.Sprintf("unit %q has no conversion", unit)}
	}
	out := v
	switch unit {
	case "g":
		out = v * 1000
	case "ug":
		out = v / 1000
	case "s":
		out = v / 60
	case "K":
		out = v - 273.15
	case "degF":
		out = (v - 32) * 5 / 9
	}
	return &out, nil
}

func numericField(f string) bool {
	switch f {
	case "gas", "phase", "note", "action", "reason":
		return false
	}
	return true
}

var requiredByKind = map[string][]string{
	model.LayerSamples: {"t_min", "mass_mg", "temp_c"},
	model.LayerBlank:   {"t_min", "blank_mg"},
	model.LayerDrift:   {"t_min", "drift_mg"},
	model.LayerFurnace: {"t_min", "setpoint_c"},
	model.LayerValves:  {"t_min", "gas"},
	model.LayerInitial: {"initial_mg"},
}

func rowOK(kind string, fields []string, rec model.Record) bool {
	for _, f := range requiredByKind[kind] {
		if _, ok := rec.Parsed[f]; !ok {
			if rec.Reason == "" {
				rec.Reason = "missing " + f
			}
			return false
		}
	}
	return rec.Reason == ""
}

func layerUID(project, kind string, version int) string {
	return identity.Hash("lyr_", project, kind, strconv.Itoa(version))
}

func parseCSVLine(line string) []string {
	var out []string
	var cur strings.Builder
	inQ := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			if inQ && i+1 < len(line) && line[i+1] == '"' {
				cur.WriteByte('"')
				i++
			} else {
				inQ = !inQ
			}
		case c == ',' && !inQ:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	out = append(out, cur.String())
	return out
}

// ResolveKindOnly reports which layer kind a file would map to, without
// parsing every row. Used for half-failure reporting before commit.
func ResolveKindOnly(name, body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		return resolveKind(name, parseCSVLine(strings.TrimPrefix(line, "\ufeff")))
	}
	return resolveKind(name, nil)
}
