// Package parse imports the domain-specific "tga-1" fragment format.
// Original textual representations are preserved on every row; normalization
// (unit conversion, numeric parsing) happens only in the compute layer.
package parse

import (
	"fmt"
	"strconv"
	"strings"

	"tgareview/internal/domain"
)

const Format = "tga-1"

// Diag describes one format error while keeping the fragment usable.
type Diag struct {
	RowNo int
	Line  string
	Msg   string
}

// Result is one parsed fragment: batch header, accepted/failed rows, errors.
type Result struct {
	Batch domain.BatchImportedPayload
	Rows  []domain.SourceRowPayload
	Diags []Diag
}

// Parse parses a tga-1 fragment. kind is "import" or "revision"; parentBatch
// links revisions to the batch they amend.
func Parse(fragment, kind, parentBatch, origin string, rowIDSeed func(layer string, no int) string) (Result, error) {
	res := Result{}
	var section string
	accept, reject := 0, 0
	var diags []string

	addDiag := func(no int, line, msg string) {
		res.Diags = append(res.Diags, Diag{RowNo: no, Line: line, Msg: msg})
		diags = append(diags, fmt.Sprintf("line %d: %s", no, msg))
	}

	lines := strings.Split(strings.ReplaceAll(fragment, "\r\n", "\n"), "\n")
	for i, raw := range lines {
		no := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		if section == "" {
			addDiag(no, line, "data outside of any section")
			reject++
			continue
		}
		fields, err := tokenize(line)
		if err != nil {
			addDiag(no, line, err.Error())
			reject++
			res.Rows = append(res.Rows, failedRow("", no, line, err.Error()))
			continue
		}
		layer, perr := layerFor(section)
		if perr != "" {
			addDiag(no, line, perr)
			reject++
			res.Rows = append(res.Rows, failedRow(section, no, line, perr))
			continue
		}
		// Layer-specific field validation; unparseable quantities stay in place.
		if msg := validate(layer, fields); msg != "" {
			addDiag(no, line, msg)
			reject++
			res.Rows = append(res.Rows, failedRow(string(layer), no, line, msg))
			continue
		}
		accept++
		res.Rows = append(res.Rows, domain.SourceRowPayload{
			Layer: layer, RowNo: no, Status: domain.RowOK, Raw: line, Fields: fields,
		})
	}

	status := "ok"
	if reject > 0 && accept > 0 {
		status = "partial"
	}
	if reject > 0 && accept == 0 {
		status = "failed"
	}
	res.Batch = domain.BatchImportedPayload{
		Kind: kind, ParentBatch: parentBatch, Status: status, RawFragment: fragment,
		Format: Format, AcceptedRows: accept, RejectedRows: reject,
		Diag: strings.Join(diags, "; "), Origin: origin,
	}
	return res, nil
}

func failedRow(layer string, no int, line, diag string) domain.SourceRowPayload {
	return domain.SourceRowPayload{
		Layer: domain.SourceLayer(layer), RowNo: no, Status: domain.RowFailed,
		Raw: line, Diag: diag,
	}
}

// tokenize splits on whitespace; each token must be name=value or name="v u".
func tokenize(line string) ([]domain.Field, error) {
	var out []domain.Field
	tokens := strings.Fields(line)
	for _, tk := range tokens {
		eq := strings.IndexByte(tk, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("expected name=value, got %q", tk)
		}
		name := strings.ToLower(tk[:eq])
		val := strings.Trim(tk[eq+1:], `"`)
		out = append(out, domain.Field{Name: name, Raw: val})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty record")
	}
	return out, nil
}

func layerFor(section string) (domain.SourceLayer, string) {
	switch section {
	case "sample", "raw", "raw_points", "raw_sample":
		return domain.LayerRaw, ""
	case "drift", "balance_drift":
		return domain.LayerDrift, ""
	case "program", "furnace_program":
		return domain.LayerProgram, ""
	case "gas", "gas_valve", "gas_events":
		return domain.LayerGas, ""
	case "blank", "blank_curve":
		return domain.LayerBlank, ""
	case "weight", "sample_weight":
		return domain.LayerWeight, ""
	case "params", "parameters":
		return domain.LayerParams, ""
	default:
		return "", "unknown section [" + section + "]"
	}
}

// validate checks arity + unit suffix presence. Numeric/unit conversion of
// values is deliberately deferred: bad units are diagnosed, but the raw token
// stays attached so nothing is dropped for plotting.
func validate(layer domain.SourceLayer, fields []domain.Field) string {
	want := map[string]bool{}
	switch layer {
	case domain.LayerRaw:
		want = map[string]bool{"t": true, "temp": true, "mass": true}
	case domain.LayerDrift:
		want = map[string]bool{"start": true, "rate": true}
	case domain.LayerBlank:
		want = map[string]bool{"t": true, "mass": true}
	case domain.LayerProgram:
		want = map[string]bool{"start": true, "end": true, "target": true, "ramp": true}
	case domain.LayerGas:
		want = map[string]bool{"at": true, "from": true, "to": true}
	case domain.LayerWeight:
		want = map[string]bool{"mg": true}
	case domain.LayerParams:
		return validateParams(fields)
	}
	seen := map[string]bool{}
	for _, f := range fields {
		if !want[f.Name] {
			return "unexpected field " + f.Name
		}
		seen[f.Name] = true
	}
	for k := range want {
		if !seen[k] {
			return "missing field " + k
		}
	}
	// Unit-aware value checks. Bad conversions remain as first-class diagnostics.
	for _, f := range fields {
		if msg := checkQuantity(layer, f); msg != "" {
			return msg
		}
	}
	return ""
}

func checkQuantity(layer domain.SourceLayer, f domain.Field) string {
	switch layer {
	case domain.LayerRaw:
		switch f.Name {
		case "t":
			return expectUnit(f, "s", "min")
		case "temp":
			return expectUnit(f, "C")
		case "mass":
			return expectUnit(f, "mg")
		}
	case domain.LayerDrift:
		if f.Name == "start" {
			return expectUnit(f, "s")
		}
		if f.Name == "rate" {
			return expectUnit(f, "mg/min")
		}
	case domain.LayerBlank:
		if f.Name == "t" {
			return expectUnit(f, "s")
		}
		return expectUnit(f, "mg")
	case domain.LayerProgram:
		switch f.Name {
		case "start", "end":
			return expectUnit(f, "s")
		case "target":
			return expectUnit(f, "C")
		case "ramp":
			return expectUnit(f, "C/min")
		}
	case domain.LayerGas:
		if f.Name == "at" {
			return expectUnit(f, "s")
		}
	case domain.LayerWeight:
		return expectUnit(f, "mg")
	}
	return ""
}

func expectUnit(f domain.Field, units ...string) string {
	val, unit := splitUnit(f.Raw)
	if val == "" {
		return "field " + f.Name + " has no numeric value"
	}
	if _, err := strconv.ParseFloat(val, 64); err != nil {
		return "field " + f.Name + " non-numeric value " + val
	}
	for _, u := range units {
		if unit == u {
			return ""
		}
	}
	return "field " + f.Name + " unsupported unit " + unit
}

// splitUnit splits "9.5mg" / "-0.02 mg/min" / "500" into (value, unit).
func splitUnit(s string) (string, string) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && (isDigit(rune(s[i])) || s[i] == '.' || s[i] == '-' || s[i] == '+') {
		i++
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i:])
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

func validateParams(fields []domain.Field) string {
	known := map[string]bool{
		"atmosphere_delay_s": true, "temperature_lag_c": true,
		"drift_rate_mg_per_min": true, "peak_prominence_mg_s": true,
		"peak_merge_window_s": true, "noise_tol_mg": true,
		"gap_threshold_s": true, "blank_scale": true,
	}
	for _, f := range fields {
		if !known[f.Name] {
			return "unknown parameter " + f.Name
		}
		if _, err := strconv.ParseFloat(f.Raw, 64); err != nil {
			return "parameter " + f.Name + " not numeric: " + f.Raw
		}
	}
	return ""
}
