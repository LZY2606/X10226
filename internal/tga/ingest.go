package tga

import (
	"bufio"
	"fmt"
	"strings"
	"time"

	"tga-review/internal/quantity"
)

// Column schema per layer. unitHeader is the header suffix that carries the
// original declared unit (kept verbatim; normalisation happens downstream).
type colSpec struct {
	field      string
	headers    []string
	dim        quantity.Dimension
	required   bool
}

func layerSpecs(layer string) []colSpec {
	switch layer {
	case LayerRawPoint:
		return []colSpec{
			{"time_s", []string{"furnace_time", "time"}, quantity.DimTime, true},
			{"mass", []string{"mass"}, quantity.DimMass, true},
			{"temp", []string{"temperature", "temp", "furnace_temp"}, quantity.DimTemp, false},
		}
	case LayerDrift:
		return []colSpec{
			{"slope", []string{"drift_slope", "slope"}, quantity.DimMassRate, true},
			{"tref", []string{"reference_time", "t_ref", "time"}, quantity.DimTime, false},
			{"intercept", []string{"intercept"}, quantity.DimMass, false},
		}
	case LayerFurnace:
		return []colSpec{
			{"from", []string{"start_time", "from", "t_start"}, quantity.DimTime, true},
			{"to", []string{"end_time", "to", "t_end"}, quantity.DimTime, true},
			{"rate", []string{"ramp_rate", "rate", "heat_rate"}, quantity.DimHeatRate, false},
			{"target", []string{"target_temp", "target"}, quantity.DimTemp, false},
		}
	case LayerValve:
		return []colSpec{
			{"time_s", []string{"switch_time", "time", "t"}, quantity.DimTime, true},
			{"gas", []string{"gas", "atmosphere"}, quantity.DimDimensionless, true},
			{"flow", []string{"flow", "flow_rate"}, quantity.DimFlow, false},
		}
	case LayerBlank:
		return []colSpec{
			{"time_s", []string{"furnace_time", "time"}, quantity.DimTime, true},
			{"mass", []string{"blank_mass", "mass"}, quantity.DimMass, true},
			{"temp", []string{"temperature", "temp"}, quantity.DimTemp, false},
		}
	case LayerInitMass:
		return []colSpec{
			{"mass", []string{"m0", "initial_mass", "mass"}, quantity.DimMass, true},
			{"time_s", []string{"at_time", "time"}, quantity.DimTime, false},
		}
	}
	return nil
}

// ParsedRow is one CSV row after parsing, before being committed.
type ParsedRow struct {
	RowNo      int
	Line       string
	Fields     map[string]string
	Quant      map[string]quantity.Quantity
	Accepted   bool
	Diagnostic string
}

// IngestRequest is one import attempt for one layer.
type IngestRequest struct {
	Layer     string
	Label     string
	Note      string
	CSV       string
	At        time.Time
}

// ParsedIngest is the parsed but not yet committed result.
type ParsedIngest struct {
	Req     IngestRequest
	Rows    []ParsedRow
	Diags   []Diagnostic
	Status  string // ok | partial | failed
}

// ParseIngest validates layer and CSV without mutating any state.
func ParseIngest(req IngestRequest) (*ParsedIngest, error) {
	if layerSpecs(req.Layer) == nil {
		return nil, fmt.Errorf("unknown layer %q", req.Layer)
	}
	if req.At.IsZero() {
		req.At = time.Now()
	}
	pi := &ParsedIngest{Req: req}
	specs := layerSpecs(req.Layer)

	scanner := bufio.NewScanner(strings.NewReader(req.CSV))
	scanner.Buffer(make([]byte, 1<<20), 1<<24)
	var headers []string
	headerLine := 0
	rowNo := 0
	for scanner.Scan() {
		raw := strings.TrimRight(scanner.Text(), "\r")
		trim := strings.TrimSpace(raw)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		rowNo++
		cells := splitCSV(trim)
		if headers == nil {
			headers = cells
			headerLine = rowNo
			for i := range headers {
				headers[i] = strings.ToLower(strings.TrimSpace(headers[i]))
			}
			if err := validateHeaders(headers, specs); err != nil {
				// A bad header fails the whole job ("导入半失败" leaves nothing
				// committed for this layer slice).
				pi.Diags = append(pi.Diags, Diagnostic{Row: rowNo, Message: err.Error(), RawLine: raw})
				pi.Status = "failed"
				return pi, nil
			}
			continue
		}
		pr := ParsedRow{RowNo: rowNo, Line: raw, Fields: map[string]string{}, Quant: map[string]quantity.Quantity{}}
		var rowDiag []string
		for _, sp := range specs {
			idx := findHeader(headers, sp.headers)
			if idx < 0 || idx >= len(cells) {
				if sp.required {
					rowDiag = append(rowDiag, fmt.Sprintf("missing column %q", sp.field))
				}
				continue
			}
			unit := ""
			if uidx := unitColumn(headers, headers[idx]); uidx >= 0 && uidx < len(cells) {
				unit = strings.TrimSpace(cells[uidx])
			}
			rawVal := strings.TrimSpace(cells[idx])
			pr.Fields[sp.field] = rawVal
			if unit != "" {
				pr.Fields[sp.field+"_unit"] = unit
			}
			q := quantity.Parse(rawVal, unit)
			if q.Dimension == quantity.DimUnknown && sp.dim != quantity.DimDimensionless {
				q.Dimension = sp.dim
			}
			pr.Quant[sp.field] = q
			if !q.OK() {
				rowDiag = append(rowDiag, fmt.Sprintf("%s: %s", sp.field, q.Diagnostic))
			}
		}
		// Loose gas name tokens are acceptable (dimensionless string).
		if req.Layer == LayerValve {
			if idx := findHeader(headers, []string{"gas", "atmosphere"}); idx >= 0 && idx < len(cells) {
				if strings.TrimSpace(cells[idx]) == "" {
					rowDiag = append(rowDiag, "gas: empty value")
				}
			}
		}
		if len(cells) != len(headers) {
			rowDiag = append(rowDiag, fmt.Sprintf("column count %d != header %d", len(cells), len(headers)))
		}
		if len(rowDiag) > 0 {
			pr.Accepted = false
			pr.Diagnostic = strings.Join(rowDiag, "; ")
			pi.Diags = append(pi.Diags, Diagnostic{Row: rowNo, Message: pr.Diagnostic, RawLine: raw})
		} else {
			pr.Accepted = true
		}
		pi.Rows = append(pi.Rows, pr)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if headers == nil {
		pi.Diags = append(pi.Diags, Diagnostic{Message: "no header row"})
		pi.Status = "failed"
		return pi, nil
	}
	good, bad := 0, 0
	for _, r := range pi.Rows {
		if r.Accepted {
			good++
		} else {
			bad++
		}
	}
	switch {
	case bad == 0 && good > 0:
		pi.Status = "ok"
	case good > 0:
		pi.Status = "partial"
	default:
		pi.Status = "failed"
	}
	_ = headerLine
	return pi, nil
}

func validateHeaders(headers []string, specs []colSpec) error {
	for _, sp := range specs {
		if !sp.required {
			continue
		}
		if findHeader(headers, sp.headers) < 0 {
			return fmt.Errorf("required header missing, want one of %v", sp.headers)
		}
	}
	return nil
}

func findHeader(headers []string, names []string) int {
	for _, n := range names {
		for i, h := range headers {
			if h == n {
				return i
			}
		}
	}
	return -1
}

// unitColumn finds an adjacent "<name>_unit" column.
func unitColumn(headers []string, base string) int {
	wants := []string{base + "_unit", base + ".unit", base + " unit"}
	return findHeader(headers, wants)
}

func splitCSV(line string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == ',' && !inQuote:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	out = append(out, cur.String())
	return out
}
