// Package ingest parses the reproducible line-oriented TGA case format.
//
// Every accepted line becomes an immutable raw row preserving the original
// text and the original units. Structural or conversion problems stay on the
// row as error status + diagnostics (half-failure import).
package ingest

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"tgareview/internal/model"
)

// Row is one parsed source line.
type Row struct {
	LineNo      int               `json:"line_no"`
	Section     string            `json:"section"`
	Layer       string            `json:"layer"`
	Kind        string            `json:"kind"`
	Key         string            `json:"key"`
	RawLine     string            `json:"raw_line"`
	Repr        map[string]string `json:"repr"`
	Fields      map[string]any    `json:"fields"`
	Status      string            `json:"status"`
	Diagnostics []model.Diagnostic `json:"diagnostics"`
}

// ParsedBatch is the parse output handed to the store.
type ParsedBatch struct {
	Label      string
	SourceName string
	Rows       []Row
	Headers    map[string]string
}

func newRow(no int, line, section, layer, kind string) Row {
	return Row{LineNo: no, RawLine: line, Section: section, Layer: layer, Kind: kind,
		Repr: map[string]string{}, Fields: map[string]any{}, Status: model.StatusOK}
}

func (r *Row) fail(code, msg string) {
	r.Status = model.StatusError
	r.Diagnostics = append(r.Diagnostics, model.Diagnostic{Code: code, Message: msg, Source: "ingest"})
}

// Parse reads tga-casepack/1 text.
func Parse(input string) (*ParsedBatch, error) {
	pb := &ParsedBatch{Headers: map[string]string{}}
	sc := bufio.NewScanner(strings.NewReader(input))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	section := ""
	formatSeen := false
	var pointCount int
	for no := 1; sc.Scan(); no++ {
		line := sc.Scan()
		_ = line
	}
	_ = sc
	// rescanned below (kept simple and explicit)
	pb.Rows = pb.Rows[:0]
	sc2 := bufio.NewScanner(strings.NewReader(input))
	sc2.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for sc2.Scan() {
		lineNo++
		raw := sc2.Text()
		trim := strings.TrimSpace(raw)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.HasPrefix(trim, "@") {
			k, v, ok := strings.Cut(trim, " ")
			k = strings.ToLower(strings.TrimPrefix(k, "@"))
			if !ok {
				r := newRow(lineNo, raw, "header", "", "header")
				r.fail("bad_header", "header needs a value: "+trim)
				pb.Rows = append(pb.Rows, r)
				continue
			}
			pb.Headers[k] = strings.TrimSpace(v)
			if k == "label" && pb.Label == "" {
				pb.Label = strings.TrimSpace(v)
			}
			if k == "source" && pb.SourceName == "" {
				pb.SourceName = strings.TrimSpace(v)
			}
			if k == "format" {
				if strings.TrimSpace(v) != model.FormatVersion {
					return nil, fmt.Errorf("line %d: unsupported %q (want %s)", lineNo, trim, model.FormatVersion)
				}
				formatSeen = true
			}
			continue
		}
		if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
			section = strings.ToLower(strings.TrimSpace(trim[1 : len(trim)-1]))
			continue
		}
		if !formatSeen {
			r := newRow(lineNo, raw, section, "", "orphan")
			r.fail("missing_format_header", "first directive must be @format "+model.FormatVersion)
			pb.Rows = append(pb.Rows, r)
			continue
		}
		var row *Row
		switch section {
		case "params":
			row = parseParams(lineNo, raw)
		case "m0":
			row = parseM0(lineNo, raw)
		case "drift":
			row = parseDrift(lineNo, raw)
		case "furnace":
			row = parseFurnace(lineNo, raw)
		case "gas":
			row = parseGas(lineNo, raw)
		case "blank":
			row = parseBlank(lineNo, raw)
		case "points":
			pointCount++
			row = parsePoint(lineNo, raw, pointCount)
		default:
			r := newRow(lineNo, raw, section, "", "unknown_section")
			r.fail("unknown_section", "line outside known section: "+section)
			row = &r
		}
		if row != nil {
			pb.Rows = append(pb.Rows, *row)
		}
	}
	if pb.Label == "" {
		pb.Label = "import@" + strconv.Itoa(lineNo)
	}
	return pb, sc2.Err()
}

func fields(line string) []string {
	return strings.Fields(line)
}

func want(row *Row, n, got int, name string) {
	if got < n {
		row.fail("wrong_field_count", name+": expected >= "+strconv.Itoa(n)+" fields, got "+strconv.Itoa(got))
	}
}

func parseParams(no int, line string) *Row {
	f := fields(line)
	r := newRow(no, line, "params", model.LayerDrift, "params")
	r.Layer = "params"
	if len(f) == 0 {
		return &r
	}
	for _, kv := range f {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			r.fail("bad_param", "params entries must be key=value: "+kv)
			continue
		}
		r.Repr[k] = v
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			r.fail("bad_param_value", k+" not numeric: "+v)
			continue
		}
		r.Fields[k] = n
	}
	return &r
}

func parseM0(no int, line string) *Row {
	f := fields(line)
	r := newRow(no, line, "m0", model.LayerM0, "m0")
	want(r, 2, len(f), "m0")
	if len(f) >= 1 {
		q := ParseQuantity(f[0], "mass")
		r.Repr["m0"] = f[0]
		r.Fields["m0"] = q
		if !q.OK {
			r.fail(q.Reason, "initial mass not convertible: "+f[0])
		}
	}
	if len(f) >= 2 {
		r.Repr["sample"] = f[1]
		r.Fields["sample"] = strings.TrimPrefix(f[1], "sample=")
		if !strings.HasPrefix(f[1], "sample=") {
			r.fail("bad_m0_field", "second m0 field must be sample=<id>, got "+f[1])
		} else {
			r.Fields["sample"] = strings.TrimPrefix(f[1], "sample=")
		}
	}
	return &r
}

func parseDrift(no int, line string) *Row {
	f := fields(line)
	r := newRow(no, line, "drift", model.LayerDrift, "drift_node")
	want(r, 2, len(f), "drift")
	if len(f) >= 1 {
		t := ParseQuantity(f[0], "time")
		r.Repr["t"] = f[0]
		r.Fields["t"] = t
		if !t.OK {
			r.fail(t.Reason, "drift time not convertible: "+f[0])
		}
	}
	if len(f) >= 2 {
		m := ParseQuantity(f[1], "mass")
		r.Repr["offset"] = f[1]
		r.Fields["offset"] = m
		if !m.OK {
			r.fail(m.Reason, "drift offset not convertible: "+f[1])
		}
	}
	return &r
}

func parseFurnace(no int, line string) *Row {
	f := fields(line)
	r := newRow(no, line, "furnace", model.LayerFurnace, "segment")
	want(r, 2, len(f), "furnace")
	if len(f) >= 1 {
		t := ParseQuantity(f[0], "time")
		r.Repr["t"] = f[0]
		r.Fields["t"] = t
		if !t.OK {
			r.fail(t.Reason, "furnace time not convertible: "+f[0])
		}
	}
	if len(f) >= 2 {
		temp := ParseQuantity(f[1], "temp")
		r.Repr["temp"] = f[1]
		r.Fields["temp"] = temp
		if !temp.OK {
			r.fail(temp.Reason, "furnace temperature not convertible: "+f[1])
		}
	}
	if len(f) >= 3 {
		r.Repr["rate"] = f[2]
		rate := ParseQuantity(f[2], "rate")
		kind := "hold"
		if strings.Contains(strings.ToLower(f[2]), "c/min") || strings.Contains(f[2], "K/min") {
			kind = "ramp"
		}
		r.Fields["rate"] = rate
		r.Fields["kind"] = kind
		if !rate.OK {
			r.fail(rate.Reason, "furnace rate not convertible: "+f[2])
		}
	}
	return &r
}

func parseGas(no int, line string) *Row {
	f := fields(line)
	r := newRow(no, line, "gas", model.LayerGas, "valve_event")
	want(r, 2, len(f), "gas")
	if len(f) >= 1 {
		t := ParseQuantity(f[0], "time")
		r.Repr["t"] = f[0]
		r.Fields["t"] = t
		if !t.OK {
			r.fail(t.Reason, "gas time not convertible: "+f[0])
		}
	}
	if len(f) >= 2 {
		g := strings.ToUpper(f[1])
		r.Repr["gas"] = f[1]
		r.Fields["gas"] = g
		switch g {
		case "N2", "AR", "HE", "AIR", "O2", "CO2":
		default:
			r.fail("unknown_gas", "gas not in known list: "+g)
		}
	}
	if len(f) >= 3 {
		flow := ParseQuantity(f[2], "rate")
		r.Repr["flow"] = f[2]
		r.Fields["flow"] = flow
		if !flow.OK {
			r.fail(flow.Reason, "gas flow not convertible: "+f[2])
		}
	}
	if len(f) >= 4 {
		delay := ParseQuantity(f[3], "time")
		r.Repr["delay"] = f[3]
		r.Fields["transport_delay"] = delay
		if !delay.OK {
			r.fail(delay.Reason, "gas transport delay not convertible: "+f[3])
		}
	}
	return &r
}

func parseBlank(no int, line string) *Row {
	f := fields(line)
	r := newRow(no, line, "blank", model.LayerBlank, "blank_node")
	want(r, 2, len(f), "blank")
	if len(f) >= 1 {
		temp := ParseQuantity(f[0], "temp")
		r.Repr["temp"] = f[0]
		r.Fields["temp"] = temp
		if !temp.OK {
			r.fail(temp.Reason, "blank temperature not convertible: "+f[0])
		}
	}
	if len(f) >= 2 {
		m := ParseQuantity(f[1], "mass")
		r.Repr["offset"] = f[1]
		r.Fields["offset"] = m
		if !m.OK {
			r.fail(m.Reason, "blank offset not convertible: "+f[1])
		}
	}
	return &r
}

func parsePoint(no int, line string, idx int) *Row {
	f := fields(line)
	r := newRow(no, line, "points", model.LayerPoint, "sample_point")
	r.Key = "p" + strconv.Itoa(idx)
	want(r, 3, len(f), "point")
	if len(f) >= 1 {
		t := ParseQuantity(f[0], "time")
		r.Repr["t"] = f[0]
		r.Fields["t"] = t
		if !t.OK {
			r.fail(t.Reason, "point time not convertible: "+f[0])
		}
	}
	if len(f) >= 2 {
		temp := ParseQuantity(f[2-1+1-1+1-1], "temp")
		temp = ParseQuantity(f[1], "temp")
		r.Repr["temp"] = f[1]
		r.Fields["temp"] = temp
		if !temp.OK {
			r.fail(temp.Reason, "point temperature not convertible: "+f[1])
		}
	}
	if len(f) >= 3 {
		m := ParseQuantity(f[2], "mass")
		r.Repr["mass"] = f[2]
		r.Fields["mass"] = m
		if !m.OK {
			r.fail(m.Reason, "point mass not convertible: "+f[2])
		}
	}
	return &r
}
