// Package parse imports small, copyable layered samples (raw sample points,
// drift correction, furnace program, gas valve events, blank curve, initial
// mass). Original representations are preserved verbatim; conversion problems
// stay on the offending fragment instead of deleting it for plotting.
package parse

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"tga-review/internal/types"
	"tga-review/internal/units"
)

// Section is one parsed layer from a bundle.
type Section struct {
	Kind       string
	Header     []string
	Rows       []types.Fragment
	HasRejects bool
}

// BundleResult is the outcome of parsing a multi-section text.
type BundleResult struct {
	Sections      []Section
	MissingKinds  []string
	UnknownBlocks []string
	Mode          string // all | partial_failed
}

var requiredColumns = map[string][]string{
	types.SampleSeries:    {"time", "temp", "mass"},
	types.DriftCorrection: {"time", "offset"},
	types.FurnaceProgram:  {"segment", "start_time", "end_time", "start_temp", "end_temp"},
	types.GasValveEvents:  {"time", "from_gas", "to_gas"},
	types.BlankCurve:      {"time", "mass"},
	types.InitialMass:     {"layer", "value"},
}

// ParseBundle splits a text on "## <kind>" section headers and parses each
// section as CSV. Every data row becomes a fragment, accepted or rejected, so
// a single bad row produces a partial_failed import rather than aborting.
func ParseBundle(text string) (BundleResult, error) {
	var res BundleResult
	blocks := splitSections(text)
	if len(blocks) == 0 {
		return res, fmt.Errorf("no '## <kind>' sections found")
	}
	seen := map[string]bool{}
	for _, b := range blocks {
		if _, ok := requiredColumns[b.kind]; !ok {
			res.UnknownBlocks = append(res.UnknownBlocks, b.kind)
			continue
		}
		sec, err := parseSection(b.kind, b.body)
		if err != nil {
			res.UnknownBlocks = append(res.UnknownBlocks, b.kind+":"+err.Error())
			continue
		}
		if sec.HasRejects {
			res.Mode = "partial_failed"
		}
		res.Sections = append(res.Sections, sec)
		seen[b.kind] = true
	}
	for _, k := range types.SourceKinds {
		if !seen[k] {
			res.MissingKinds = append(res.MissingKinds, k)
		}
	}
	if res.Mode == "" {
		res.Mode = "all"
	}
	return res, nil
}

type block struct{ kind, body string }

func splitSections(text string) []block {
	var blocks []block
	var cur *block
	var body []string
	flush := func() {
		if cur != nil {
			cur.body = strings.TrimSpace(strings.Join(body, "\n"))
			blocks = append(blocks, *cur)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "## ") {
			flush()
			body = nil
			cur = &block{kind: strings.ToLower(strings.TrimSpace(trim[3:]))}
			continue
		}
		if cur != nil {
			body = append(body, line)
		}
	}
	flush()
	return blocks
}

// ParseSection parses one layer.
func ParseSection(kind, body string) (Section, error) {
	if _, ok := requiredColumns[kind]; !ok {
		return Section{}, fmt.Errorf("unknown kind %q", kind)
	}
	return parseSection(kind, body)
}

func parseSection(kind, body string) (Section, error) {
	sec := Section{Kind: kind}
	r := csv.NewReader(strings.NewReader(body))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err == io.EOF {
		return sec, fmt.Errorf("empty section")
	}
	if err != nil {
		return sec, err
	}
	for i := range header {
		header[i] = normalizeCol(header[i])
	}
	sec.Header = header
	if err := requireCols(kind, header); err != nil {
		return sec, err
	}

	id := 0
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			id++
			sec.Rows = append(sec.Rows, types.Fragment{
				LocalID:     id,
				RawText:     strings.Join(rec, ","),
				Status:      types.FragRejected,
				Diagnostics: []string{"csv: " + err.Error()},
			})
			sec.HasRejects = true
			continue
		}
		if blankRow(rec) {
			continue
		}
		id++
		fields, diags := validateRow(kind, header, rec)
		frag := types.Fragment{
			LocalID: id, RawText: strings.Join(rec, ","), Fields: fields,
		}
		if len(diags) > 0 {
			frag.Status = types.FragRejected
			frag.Diagnostics = diags
			sec.HasRejects = true
		} else {
			frag.Status = types.FragAccepted
		}
		sec.Rows = append(sec.Rows, frag)
	}
	if len(sec.Rows) == 0 {
		return sec, fmt.Errorf("section %s has no data rows", kind)
	}
	return sec, nil
}

func blankRow(rec []string) bool {
	for _, c := range rec {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func normalizeCol(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	c = strings.TrimPrefix(c, "#")
	switch {
	case c == "t" || strings.HasPrefix(c, "time"):
		return "time"
	case c == "temperature" || c == "temp_c" || c == "t_c" || c == "temp":
		return "temp"
	case c == "m" || strings.HasPrefix(c, "mass"):
		return "mass"
	case c == "offset" || strings.Contains(c, "drift"):
		return "offset"
	case strings.Contains(c, "segment"):
		return "segment"
	case strings.Contains(c, "start_time"):
		return "start_time"
	case strings.Contains(c, "end_time"):
		return "end_time"
	case strings.Contains(c, "start_temp"):
		return "start_temp"
	case strings.Contains(c, "end_temp"):
		return "end_temp"
	case strings.Contains(c, "from"):
		return "from_gas"
	case c == "to_gas" || (strings.HasPrefix(c, "to") && c != "t"):
		return "to_gas"
	case c == "layer" || strings.Contains(c, "layer"):
		return "layer"
	case c == "value" || strings.Contains(c, "value"):
		return "value"
	}
	return c
}

func requireCols(kind string, header []string) error {
	have := map[string]bool{}
	for _, h := range header {
		have[h] = true
	}
	var missing []string
	for _, c := range requiredColumns[kind] {
		if !have[c] {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("section %s missing columns: %s", kind, strings.Join(missing, ", "))
	}
	return nil
}

func validateRow(kind string, header, rec []string) (map[string]string, []string) {
	fields := map[string]string{}
	var diags []string
	for i, h := range header {
		if i < len(rec) {
			fields[h] = strings.TrimSpace(rec[i])
		}
	}
	get := func(c string) string { return fields[c] }

	checkTime := func(c string) {
		q, err := units.Parse(get(c))
		if err != nil {
			diags = append(diags, fmt.Sprintf("%s: %v", c, err))
			return
		}
		if q.Dim != units.DimTime {
			diags = append(diags, fmt.Sprintf("%s: %q is not a time", c, q.Raw))
		}
	}
	checkTemp := func(c string) {
		q, err := units.Parse(get(c))
		if err != nil {
			diags = append(diags, fmt.Sprintf("%s: %v", c, err))
			return
		}
		if q.Dim != units.DimTemp {
			diags = append(diags, fmt.Sprintf("%s: %q is not a temperature", c, q.Raw))
		}
	}
	checkMass := func(c string) {
		q, err := units.Parse(get(c))
		if err != nil {
			diags = append(diags, fmt.Sprintf("%s: %v", c, err))
			return
		}
		if q.Dim != units.DimMass {
			diags = append(diags, fmt.Sprintf("%s: %q is not a mass", c, q.Raw))
		}
	}

	switch kind {
	case types.SampleSeries:
		checkTime("time")
		checkTemp("temp")
		checkMass("mass")
	case types.DriftCorrection:
		checkTime("time")
		checkMass("offset")
	case types.BlankCurve:
		checkTime("time")
		checkMass("mass")
	case types.FurnaceProgram:
		if _, err := strconv.Atoi(get("segment")); err != nil {
			diags = append(diags, "segment: must be an integer")
		}
		checkTime("start_time")
		checkTime("end_time")
		checkTemp("start_temp")
		checkTemp("end_temp")
	case types.GasValveEvents:
		checkTime("time")
		if get("from_gas") == "" {
			diags = append(diags, "from_gas: empty")
		}
		if get("to_gas") == "" {
			diags = append(diags, "to_gas: empty")
		}
	case types.InitialMass:
		switch get("layer") {
		case "tare", "gross", "net_revision":
		default:
			diags = append(diags, "layer: must be one of tare, gross, net_revision")
		}
		checkMass("value")
	}
	return fields, diags
}
