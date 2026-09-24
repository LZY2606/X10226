package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"tgareview/internal/domain"
)

// RowFailure describes one rejected import row (kept as first-class data).
type RowFailure struct {
	Line   int    `json:"line"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

// parseResult carries the parsed doc plus row-level diagnostics.
type parseResult struct {
	doc    any
	failed []RowFailure
}

// parseQuantity converts a raw cell to a Quantity. Unconvertible values are
// kept in place (OK=false) with a diagnostic instead of being dropped.
func parseQuantity(raw, unit string) domain.Quantity {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return domain.BadQ(raw, unit, "empty value")
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return domain.BadQ(raw, unit, fmt.Sprintf("cannot convert %q to %s: %v", raw, unit, err))
	}
	return domain.Quantity{Value: v, Unit: unit, Raw: raw + " " + unit, OK: true}
}

// splitCSVLines returns trimmed non-empty, non-comment lines with 1-based
// line numbers.
func splitCSVLines(raw string) [][2]string {
	var out [][2]string
	for i, ln := range strings.Split(raw, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, [2]string{strconv.Itoa(i + 1), t})
	}
	return out
}

func atoi(s string, line, field string, failed *[]RowFailure, rawLine string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		ln, _ := strconv.Atoi(line)
		*failed = append(*failed, RowFailure{Line: ln, Raw: rawLine, Reason: fmt.Sprintf("field %s: %v", field, err)})
		return 0, false
	}
	return n, true
}

// parseDoc parses raw text into the doc type for kind. JSON is accepted for
// every kind; CSV (with a header row) is accepted for tabular kinds. Row
// failures are collected; a structural error (bad header / bad JSON) aborts.
func parseDoc(kind, raw string) (*parseResult, error) {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "{") {
		return parseJSONDoc(kind, trimmed)
	}
	switch kind {
	case domain.InRawPoints:
		return parseRawPointsCSV(raw)
	case domain.InDriftCorr:
		return parseDriftCSV(raw)
	case domain.InBlankCurve:
		return parseBlankCSV(raw)
	case domain.InFurnaceProg:
		return parseFurnaceCSV(raw)
	case domain.InGasEvents:
		return parseGasCSV(raw)
	case domain.InMassLayers:
		return nil, fmt.Errorf("mass_layers must be JSON, e.g. {\"layers\":[{\"label\":\"net\",\"mass\":12.5}]}")
	}
	return nil, fmt.Errorf("unknown input kind %q", kind)
}

// massLayerJSON accepts mass as number, string or full Quantity object.
type massLayerJSON struct {
	Label string          `json:"label"`
	Mass  json.RawMessage `json:"mass"`
}

func parseMassLayersJSON(raw string) (*parseResult, error) {
	var body struct {
		Layers []massLayerJSON `json:"layers"`
	}
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		return nil, fmt.Errorf("mass_layers JSON: %v", err)
	}
	if len(body.Layers) == 0 {
		return nil, fmt.Errorf("mass_layers: no layers")
	}
	doc := domain.MassLayersDoc{}
	for i, l := range body.Layers {
		if l.Label == "" {
			return nil, fmt.Errorf("mass_layers: layer %d has no label", i)
		}
		var q domain.Quantity
		var num float64
		var str string
		switch {
		case json.Unmarshal(l.Mass, &num) == nil:
			q = domain.Quantity{Value: num, Unit: "mg", Raw: fmt.Sprintf("%g mg", num), OK: true}
		case json.Unmarshal(l.Mass, &str) == nil:
			fields := strings.Fields(str)
			if v, err := strconv.ParseFloat(fields[0], 64); err == nil && len(fields) > 0 {
				unit := "mg"
				if len(fields) > 1 {
					unit = fields[1]
				}
				q = domain.Quantity{Value: v, Unit: unit, Raw: str, OK: true}
			} else {
				q = domain.BadQ(str, "mg", "cannot convert layer mass")
			}
		case json.Unmarshal(l.Mass, &q) == nil && q.Unit != "":
			// full Quantity object; keep as submitted
		default:
			q = domain.BadQ(string(l.Mass), "mg", "unrecognized mass representation")
		}
		doc.Layers = append(doc.Layers, domain.MassLayer{Label: l.Label, Mass: q})
	}
	return &parseResult{doc: doc}, nil
}

func parseJSONDoc(kind, raw string) (*parseResult, error) {
	if kind == domain.InMassLayers {
		return parseMassLayersJSON(raw)
	}
	res := &parseResult{}
	var err error
	switch kind {
	case domain.InRawPoints:
		var d domain.RawPointsDoc
		err = json.Unmarshal([]byte(raw), &d)
		res.doc = d
	case domain.InDriftCorr:
		var d domain.DriftDoc
		err = json.Unmarshal([]byte(raw), &d)
		res.doc = d
	case domain.InBlankCurve:
		var d domain.BlankDoc
		err = json.Unmarshal([]byte(raw), &d)
		res.doc = d
	case domain.InFurnaceProg:
		var d domain.FurnaceDoc
		err = json.Unmarshal([]byte(raw), &d)
		res.doc = d
	case domain.InGasEvents:
		var d domain.GasEventsDoc
		err = json.Unmarshal([]byte(raw), &d)
		res.doc = d
	default:
		return nil, fmt.Errorf("unknown input kind %q", kind)
	}
	if err != nil {
		return nil, fmt.Errorf("%s JSON: %v", kind, err)
	}
	return res, nil
}

// csvTable validates the header and iterates data rows.
func csvTable(raw string, wantHeader []string, minCols int) ([][2]string, error) {
	lines := splitCSVLines(raw)
	if len(lines) == 0 {
		return nil, fmt.Errorf("empty input")
	}
	head := strings.Split(lines[0][1], ",")
	if len(head) < len(wantHeader) {
		return nil, fmt.Errorf("header has %d columns, want at least %d (%s)", len(head), len(wantHeader), strings.Join(wantHeader, ","))
	}
	for i, w := range wantHeader {
		if strings.TrimSpace(head[i]) != w {
			return nil, fmt.Errorf("header column %d is %q, want %q", i+1, strings.TrimSpace(head[i]), w)
		}
	}
	_ = minCols
	return lines[1:], nil
}

func parseRawPointsCSV(raw string) (*parseResult, error) {
	rows, err := csvTable(raw, []string{"seq", "time_s", "temp_c", "mass_mg"}, 4)
	if err != nil {
		return nil, err
	}
	res := &parseResult{doc: domain.RawPointsDoc{}}
	doc := domain.RawPointsDoc{}
	for _, row := range rows {
		ln, _ := strconv.Atoi(row[0])
		cols := strings.Split(row[1], ",")
		if len(cols) < 4 || len(cols) > 5 {
			res.failed = append(res.failed, RowFailure{Line: ln, Raw: row[1],
				Reason: fmt.Sprintf("want 4 or 5 columns, got %d", len(cols))})
			continue
		}
		seq, ok := atoi(cols[0], row[0], "seq", &res.failed, row[1])
		if !ok {
			continue
		}
		p := domain.SamplePoint{
			Seq:  seq,
			Time: parseQuantity(cols[1], "s"),
			Temp: parseQuantity(cols[2], "°C"),
			Mass: parseQuantity(cols[3], "mg"),
		}
		if len(cols) == 5 {
			g := strings.TrimSpace(cols[4])
			p.GapAfter = g == "1" || g == "true" || g == "yes"
		}
		doc.Points = append(doc.Points, p)
	}
	res.doc = doc
	return res, nil
}

func parseDriftCSV(raw string) (*parseResult, error) {
	rows, err := csvTable(raw, []string{"time_s", "drift_mg"}, 2)
	if err != nil {
		return nil, err
	}
	res := &parseResult{}
	doc := domain.DriftDoc{}
	for _, row := range rows {
		ln, _ := strconv.Atoi(row[0])
		cols := strings.Split(row[1], ",")
		if len(cols) != 2 {
			res.failed = append(res.failed, RowFailure{Line: ln, Raw: row[1], Reason: fmt.Sprintf("want 2 columns, got %d", len(cols))})
			continue
		}
		doc.Points = append(doc.Points, domain.DriftPoint{
			Time: parseQuantity(cols[0], "s"), Drift: parseQuantity(cols[1], "mg")})
	}
	res.doc = doc
	return res, nil
}

func parseBlankCSV(raw string) (*parseResult, error) {
	rows, err := csvTable(raw, []string{"time_s", "mass_mg"}, 2)
	if err != nil {
		return nil, err
	}
	res := &parseResult{}
	doc := domain.BlankDoc{}
	for _, row := range rows {
		ln, _ := strconv.Atoi(row[0])
		cols := strings.Split(row[1], ",")
		if len(cols) != 2 {
			res.failed = append(res.failed, RowFailure{Line: ln, Raw: row[1], Reason: fmt.Sprintf("want 2 columns, got %d", len(cols))})
			continue
		}
		doc.Points = append(doc.Points, domain.BlankPoint{
			Time: parseQuantity(cols[0], "s"), Mass: parseQuantity(cols[1], "mg")})
	}
	res.doc = doc
	return res, nil
}

func parseFurnaceCSV(raw string) (*parseResult, error) {
	rows, err := csvTable(raw, []string{"start_s", "rate_c_per_min", "target_c"}, 3)
	if err != nil {
		return nil, err
	}
	res := &parseResult{}
	doc := domain.FurnaceDoc{}
	for _, row := range rows {
		ln, _ := strconv.Atoi(row[0])
		cols := strings.Split(row[1], ",")
		if len(cols) < 3 || len(cols) > 4 {
			res.failed = append(res.failed, RowFailure{Line: ln, Raw: row[1], Reason: fmt.Sprintf("want 3 or 4 columns, got %d", len(cols))})
			continue
		}
		seg := domain.FurnaceSegment{
			StartTime:  parseQuantity(cols[0], "s"),
			TargetTemp: parseQuantity(cols[2], "°C"),
		}
		if r := strings.TrimSpace(cols[1]); r != "" {
			if v, err := strconv.ParseFloat(r, 64); err == nil {
				seg.RateCPerMin = &v
			} else {
				res.failed = append(res.failed, RowFailure{Line: ln, Raw: row[1], Reason: fmt.Sprintf("rate_c_per_min: %v", err)})
				continue
			}
		}
		if len(cols) == 4 {
			seg.Comment = strings.TrimSpace(cols[3])
		}
		doc.Segments = append(doc.Segments, seg)
	}
	res.doc = doc
	return res, nil
}

func parseGasCSV(raw string) (*parseResult, error) {
	rows, err := csvTable(raw, []string{"time_s", "from_gas", "to_gas", "flow_ml_min"}, 4)
	if err != nil {
		return nil, err
	}
	res := &parseResult{}
	doc := domain.GasEventsDoc{}
	for _, row := range rows {
		ln, _ := strconv.Atoi(row[0])
		cols := strings.Split(row[1], ",")
		if len(cols) != 4 {
			res.failed = append(res.failed, RowFailure{Line: ln, Raw: row[1], Reason: fmt.Sprintf("want 4 columns, got %d", len(cols))})
			continue
		}
		doc.Events = append(doc.Events, domain.GasEvent{
			Time:      parseQuantity(cols[0], "s"),
			FromGas:   strings.TrimSpace(cols[1]),
			ToGas:     strings.TrimSpace(cols[2]),
			FlowMlMin: parseQuantity(cols[3], "ml/min"),
		})
	}
	res.doc = doc
	return res, nil
}
