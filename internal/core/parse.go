package core

import (
	"fmt"
	"strings"
)

// ParseResult is the outcome of parsing one layer submission.
type ParseResult struct {
	Payload     any
	Diagnostics []Diagnostic
	// Partial is true when some rows failed but usable content remains
	// (the "import half-failure" case: fragments are preserved, not dropped).
	Partial bool
}

// ParseInput parses the raw text of a layer submission. The original text is
// never modified; unparsable values stay in place carrying diagnostics.
func ParseInput(kind LayerKind, raw string) (*ParseResult, error) {
	header, rows, err := splitDocument(raw)
	if err != nil {
		return nil, err
	}
	if got := header["layer"]; got != string(kind) {
		return nil, fmt.Errorf("layer mismatch: document declares %q, expected %q", got, kind)
	}
	res := &ParseResult{}
	switch kind {
	case LayerRawPoints:
		res.Payload = parseRawPoints(header, rows, res)
	case LayerDriftCorrection:
		res.Payload = parseDriftCorrection(header, res)
	case LayerFurnaceProgram:
		res.Payload = parseFurnaceProgram(header, rows, res)
	case LayerGasEvents:
		res.Payload = parseGasEvents(header, rows, res)
	case LayerBlankCurve:
		res.Payload = parseBlankCurve(header, rows, res)
	case LayerInitialMass:
		res.Payload = parseInitialMass(header, res)
	default:
		return nil, fmt.Errorf("unknown layer kind %q", kind)
	}
	return res, nil
}

// splitDocument splits a submission into "key: value" headers and data rows.
func splitDocument(raw string) (map[string]string, []string, error) {
	header := map[string]string{}
	var rows []string
	inData := false
	for i, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !inData {
			if strings.EqualFold(trimmed, "data:") {
				inData = true
				continue
			}
			k, v, ok := strings.Cut(trimmed, ":")
			if !ok {
				return nil, nil, fmt.Errorf("line %d: expected 'key: value', got %q", i+1, trimmed)
			}
			header[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		} else {
			rows = append(rows, trimmed)
		}
	}
	if header["layer"] == "" {
		return nil, nil, fmt.Errorf("missing required 'layer:' header")
	}
	return header, rows, nil
}

// parseUnits reads "units: time=s, mass=mg, temp=C" into a map.
func parseUnits(header map[string]string) map[string]string {
	units := map[string]string{}
	for _, part := range strings.Split(header["units"], ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			units[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return units
}

func (r *ParseResult) fail(code, msg, ref string) {
	r.Partial = true
	r.Diagnostics = append(r.Diagnostics, Diagnostic{Code: code, Message: msg, Ref: ref})
}

func parseRawPoints(header map[string]string, rows []string, res *ParseResult) *RawPointsPayload {
	units := parseUnits(header)
	p := &RawPointsPayload{Units: units}
	for i, row := range rows {
		rowNo := i + 1
		cols := strings.Split(row, ",")
		if len(cols) != 3 {
			res.fail("bad_row", fmt.Sprintf("expected 3 columns (time,mass,temp), got %d", len(cols)), fmt.Sprintf("row %d", rowNo))
			continue
		}
		pt := SamplePoint{
			Row:  rowNo,
			Time: ParseScalar(strings.TrimSpace(cols[0]), "time", "furnace_clock"),
			Mass: ParseScalar(strings.TrimSpace(cols[1]), "mass", ""),
			Temp: ParseScalar(strings.TrimSpace(cols[2]), "temp", "furnace_program"),
		}
		flagRowDiags(res, rowNo, pt.Time.Diagnostic, pt.Mass.Diagnostic, pt.Temp.Diagnostic)
		p.Points = append(p.Points, pt)
	}
	return p
}

func parseDriftCorrection(header map[string]string, res *ParseResult) *DriftCorrectionPayload {
	p := &DriftCorrectionPayload{Model: header["model"]}
	if p.Model == "" {
		p.Model = "linear"
	}
	p.Rate = ParseScalar(header["rate"], "rate", "")
	p.Anchor = ParseScalar(header["anchor"], "time", "furnace_clock")
	p.Offset = ParseScalar(header["offset"], "mass", "")
	for _, rv := range []RawValue{p.Rate, p.Anchor, p.Offset} {
		if rv.Diagnostic != nil {
			res.fail(rv.Diagnostic.Code, rv.Diagnostic.Message, "header")
		}
	}
	return p
}

func parseFurnaceProgram(header map[string]string, rows []string, res *ParseResult) *FurnaceProgramPayload {
	p := &FurnaceProgramPayload{Units: parseUnits(header)}
	for i, row := range rows {
		rowNo := i + 1
		cols := strings.Split(row, ",")
		if len(cols) != 4 {
			res.fail("bad_row", fmt.Sprintf("expected 4 columns (start,end,start_temp,end_temp), got %d", len(cols)), fmt.Sprintf("row %d", rowNo))
			continue
		}
		seg := FurnaceSegment{
			Row:       rowNo,
			Start:     ParseScalar(strings.TrimSpace(cols[0]), "time", "furnace_clock"),
			End:       ParseScalar(strings.TrimSpace(cols[1]), "time", "furnace_clock"),
			StartTemp: ParseScalar(strings.TrimSpace(cols[2]), "temp", "furnace_program"),
			EndTemp:   ParseScalar(strings.TrimSpace(cols[3]), "temp", "furnace_program"),
		}
		flagRowDiags(res, rowNo, seg.Start.Diagnostic, seg.End.Diagnostic, seg.StartTemp.Diagnostic, seg.EndTemp.Diagnostic)
		p.Segments = append(p.Segments, seg)
	}
	return p
}

func parseGasEvents(header map[string]string, rows []string, res *ParseResult) *GasEventsPayload {
	p := &GasEventsPayload{Units: parseUnits(header)}
	p.TransportDelay = ParseScalar(header["transport_delay"], "time", "furnace_clock")
	p.ThermalLag = ParseScalar(header["thermal_lag"], "time", "furnace_clock")
	for i, row := range rows {
		rowNo := i + 1
		cols := strings.Split(row, ",")
		if len(cols) != 4 {
			res.fail("bad_row", fmt.Sprintf("expected 4 columns (time,from,to,flow), got %d", len(cols)), fmt.Sprintf("row %d", rowNo))
			continue
		}
		ev := GasEvent{
			Row:     rowNo,
			Time:    ParseScalar(strings.TrimSpace(cols[0]), "time", "furnace_clock"),
			FromGas: strings.TrimSpace(cols[1]),
			ToGas:   strings.TrimSpace(cols[2]),
			Flow:    ParseScalar(strings.TrimSpace(cols[3]), "flow", ""),
		}
		flagRowDiags(res, rowNo, ev.Time.Diagnostic, ev.Flow.Diagnostic)
		p.Events = append(p.Events, ev)
	}
	return p
}

func parseBlankCurve(header map[string]string, rows []string, res *ParseResult) *BlankCurvePayload {
	p := &BlankCurvePayload{Units: parseUnits(header)}
	for i, row := range rows {
		rowNo := i + 1
		cols := strings.Split(row, ",")
		if len(cols) != 2 {
			res.fail("bad_row", fmt.Sprintf("expected 2 columns (time,mass), got %d", len(cols)), fmt.Sprintf("row %d", rowNo))
			continue
		}
		pt := BlankPoint{
			Row:  rowNo,
			Time: ParseScalar(strings.TrimSpace(cols[0]), "time", "furnace_clock"),
			Mass: ParseScalar(strings.TrimSpace(cols[1]), "mass", ""),
		}
		flagRowDiags(res, rowNo, pt.Time.Diagnostic, pt.Mass.Diagnostic)
		p.Points = append(p.Points, pt)
	}
	return p
}

func parseInitialMass(header map[string]string, res *ParseResult) *InitialMassPayload {
	p := &InitialMassPayload{
		Tare:  ParseScalar(header["tare"], "mass", ""),
		Gross: ParseScalar(header["gross"], "mass", ""),
		Net:   ParseScalar(header["net"], "mass", ""),
		Basis: header["basis"],
	}
	if p.Basis == "" {
		p.Basis = "as_received"
	}
	for _, rv := range []RawValue{p.Tare, p.Gross, p.Net} {
		if rv.Diagnostic != nil {
			res.fail(rv.Diagnostic.Code, rv.Diagnostic.Message, "header")
		}
	}
	return p
}

func flagRowDiags(res *ParseResult, row int, diags ...*Diagnostic) {
	for _, d := range diags {
		if d != nil {
			res.fail(d.Code, d.Message, fmt.Sprintf("row %d", row))
		}
	}
}
