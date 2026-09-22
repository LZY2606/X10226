// Package ingest parses small, copy-pasteable TG/A text samples into the six
// stratified source layers. The verbatim text is always retained by the caller;
// parsing never overwrites it. Malformed rows do not abort the whole import but
// become diagnostics, which is what makes "import half failure" inspectable.
package ingest

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"tga-review/internal/domain"
)

// FormatError is an explicitly malformed submission (wrong header, bad shape…).
type FormatError struct{ Layer string; Msg string }

func (e *FormatError) Error() string { return fmt.Sprintf("%s: %s", e.Layer, e.Msg) }

// Result holds parsed rows plus diagnostics. RowsBad rows stay out of the typed
// slices but their tokens survive inside Diagnostics.
type Result struct {
	Samples []domain.SamplePoint
	Drifts  []domain.DriftRecord
	Program []domain.ProgramSegment
	Gases   []domain.GasEvent
	Blanks  []domain.BlankPoint
	Masses  []domain.InitMass
	Diagnostics []domain.Diagnostic
}

func (r *Result) rowsOK() int {
	return len(r.Samples)+len(r.Drifts)+len(r.Program)+len(r.Gases)+len(r.Blanks)+len(r.Masses)
}

var layerHeaders = map[string][]string{
	domain.LayerSamples:  {"time", "temperature", "mass"},
	domain.LayerDrift:    {"time", "offset"},
	domain.LayerProgram:  {"start_time", "end_time", "start_temperature", "end_temperature", "kind"},
	domain.LayerGas:      {"time", "gas", "action", "flow"},
	domain.LayerBlank:    {"time", "temperature", "mass"},
	domain.LayerInitMass: {"time", "mass", "balance_id"},
}

// LayerSpec is human-readable format documentation, used by the UI and tests.
func LayerSpec(layer string) (string, error) {
	h, ok := layerHeaders[layer]
	if !ok {
		return "", fmt.Errorf("unknown layer %q", layer)
	}
	return strings.Join(h, ","), nil
}

func readRows(layer, text string) ([][]string, error) {
	rd := csv.NewReader(strings.NewReader(text))
	rd.FieldsPerRecord = -1 // we check explicitly to give precise format errors
	rd.TrimLeadingSpace = true
	all, err := rd.ReadAll()
	if err != nil {
		return nil, &FormatError{Layer: layer, Msg: "invalid CSV: " + err.Error()}
	}
	if len(all) == 0 {
		return nil, &FormatError{Layer: layer, Msg: "empty submission"}
	}
	return all, nil
}

func checkHeader(layer string, header []string) error {
	want := layerHeaders[layer]
	if len(header) < len(want) {
		return &FormatError{Layer: layer, Msg: fmt.Sprintf("header has %d columns, want at least %d (%s)", len(header), len(want), strings.Join(want, ","))}
	}
	for i, w := range want {
		if strings.TrimSpace(strings.ToLower(header[i])) != w {
			return &FormatError{Layer: layer, Msg: fmt.Sprintf("header column %d is %q, want %q", i+1, header[i], w)}
		}
	}
	return nil
}

func diag(session, layer string, row int, field, token, sev, msg string) domain.Diagnostic {
	id, _ := domain.ContentID(domain.PrefixDiagnostic, "diagnostic", []string{session, layer, strconv.Itoa(row), field, token})
	return domain.Diagnostic{ID:id, SessionID: session, Layer: layer, Row: row, Field: field, Token: token, Severity: sev, Message: msg}
}

// defaultUnits assumes minutes / Celsius / mg / sccm when the token omits a unit.
func qty(kind domain.DimKind, token string) (domain.Quantity, error) {
	def := ""
	switch kind {
	case domain.DimTime:
		def = domain.UnitMin
	case domain.DimTemp:
		def = domain.UnitC
	case domain.DimMass:
		def = domain.UnitMg
	case domain.DimFlow:
		def = domain.UnitSccm
	}
	return domain.ParseQuantity(kind, token, def)
}

// Parse dispatches on layer. text must include the header row.
func Parse(session, layer, text string) (*Result, error) {
	rows, err := readRows(layer, text)
	if err != nil {
		return nil, err
	}
	if err := checkHeader(layer, rows[0]); err != nil {
		return nil, err
	}
	res := &Result{}
	for i, rec := range rows[1:] {
		rowNo := i + 2 // 1-based, header is row 1
		if blank(rec) {
			continue
		}
		switch layer {
		case domain.LayerSamples:
			parseSample(session, rec, rowNo, res)
		case domain.LayerDrift:
			parseDrift(session, rec, rowNo, res)
		case domain.LayerProgram:
			parseProgram(session, rec, rowNo, res)
		case domain.LayerGas:
			parseGas(session, rec, rowNo, res)
		case domain.LayerBlank:
			parseBlank(session, rec, rowNo, res)
		case domain.LayerInitMass:
			parseInitMass(session, rec, rowNo, res)
		}
	}
	if res.rowsOK() == 0 && len(res.Diagnostics) == 0 {
		return nil, &FormatError{Layer: layer, Msg: "header only, no data rows"}
	}
	return res, nil
}

func blank(rec []string) bool {
	for _, f := range rec {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

func parseSample(session string, rec []string, row int, res *Result) {
	if len(rec) < 3 {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerSamples, row, "*", strings.Join(rec, "|"), "error", "row has fewer than 3 columns"))
		return
	}
	var bad bool
	t, err := qty(domain.DimTime, rec[0])
	if err != nil {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerSamples, row, "time", rec[0], "error", err.Error()))
		bad = true
	}
	temp, err := qty(domain.DimTemp, rec[1])
	if err != nil {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerSamples, row, "temperature", rec[1], "error", err.Error()))
		bad = true
	}
	m, err := qty(domain.DimMass, rec[2])
	if err != nil {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerSamples, row, "mass", rec[2], "error", err.Error()))
		bad = true
	}
	if bad {
		return
	}
	res.Samples = append(res.Samples, domain.SamplePoint{Row: row, Time: t, Temperature: temp, Mass: m})
}

func parseDrift(session string, rec []string, row int, res *Result) {
	if len(rec) < 2 {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerDrift, row, "*", strings.Join(rec, "|"), "error", "row has fewer than 2 columns"))
		return
	}
	t, err1 := qty(domain.DimTime, rec[0])
	o, err2 := qty(domain.DimMass, rec[1])
	if err1 != nil || err2 != nil {
		if err1 != nil { res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerDrift, row, "time", rec[0], "error", err1.Error())) }
		if err2 != nil { res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerDrift, row, "offset", rec[1], "error", err2.Error())) }
		return
	}
	note := ""
	if len(rec) > 2 { note = strings.TrimSpace(rec[2]) }
	res.Drifts = append(res.Drifts, domain.DriftRecord{Row: row, Time: t, Offset: o, Note: note})
}

func parseProgram(session string, rec []string, row int, res *Result) {
	if len(rec) < 5 {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerDrift, row, "*", strings.Join(rec, "|"), "error", "row has fewer than 5 columns"))
		return
	}
	t0, e1 := qty(domain.DimTime, rec[0])
	t1, e2 := qty(domain.DimTime, rec[1])
	p0, e3 := qty(domain.DimTemp, rec[2])
	p1, e4 := qty(domain.DimTemp, rec[3])
	for _, e := range []struct{f string; t string; err error}{{"start_time", rec[0], e1}, {"end_time", rec[1], e2}, {"start_temperature", rec[2], e3}, {"end_temperature", rec[3], e4}} {
		if e.err != nil {
			res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerProgram, row, e.f, e.t, "error", e.err.Error()))
		}
	}
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil { return }
	kind := strings.ToLower(strings.TrimSpace(rec[4]))
	if kind != "ramp" && kind != "isothermal" {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerProgram, row, "kind", rec[4], "error", "kind must be ramp or isothermal"))
		return
	}
	label := ""
	if len(rec) > 5 { label = strings.TrimSpace(rec[5]) }
	res.Program = append(res.Program, domain.ProgramSegment{Row: row, StartTime: t0, EndTime: t1, StartTemp: p0, EndTemp: p1, Kind: kind, Label: label})
}

func parseGas(session string, rec []string, row int, res *Result) {
	if len(rec) < 4 {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerGas, row, "*", strings.Join(rec, "|"), "error", "row has fewer than 4 columns"))
		return
	}
	t, e1 := qty(domain.DimTime, rec[0])
	if e1 != nil {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerGas, row, "time", rec[0], "error", e1.Error()))
		return
	}
	gas := strings.TrimSpace(rec[1])
	action := strings.ToLower(strings.TrimSpace(rec[2]))
	if gas == "" {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerGas, row, "gas", rec[1], "error", "gas is empty"))
		return
	}
	if action != "open" && action != "close" && action != "switch" {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerGas, row, "action", rec[2], "error", "action must be open, close or switch"))
		return
	}
	flow, err := qty(domain.DimFlow, rec[3])
	if err != nil {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerGas, row, "flow", rec[3], "error", err.Error()))
		return
	}
	note := ""
	if len(rec) > 4 { note = strings.TrimSpace(rec[4]) }
	res.Gases = append(res.Gases, domain.GasEvent{Row: row, Time: t, Gas: gas, Action: action, Flow: flow, Note: note})
}

func parseBlank(session string, rec []string, row int, res *Result) {
	if len(rec) < 3 {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerBlank, row, "*", strings.Join(rec, "|"), "error", "row has fewer than 3 columns"))
		return
	}
	t, e1 := qty(domain.DimTime, rec[0])
	temp, e2 := qty(domain.DimTemp, rec[1])
	m, e3 := qty(domain.DimMass, rec[2])
	if e1 != nil { res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerBlank, row, "time", rec[0], "error", e1.Error())) }
	if e2 != nil { res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerBlank, row, "temperature", rec[1], "error", e2.Error())) }
	if e3 != nil { res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerBlank, row, "mass", rec[2], "error", e3.Error())) }
	if e1 != nil || e2 != nil || e3 != nil { return }
	res.Blanks = append(res.Blanks, domain.BlankPoint{Row: row, Time: t, Temperature: temp, Mass: m})
}

func parseInitMass(session string, rec []string, row int, res *Result) {
	if len(rec) < 2 {
		res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerInitMass, row, "*", strings.Join(rec, "|"), "error", "row has fewer than 2 columns"))
		return
	}
	t, e1 := qty(domain.DimTime, rec[0])
	m, e2 := qty(domain.DimMass, rec[1])
	if e1 != nil { res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerInitMass, row, "time", rec[0], "error", e1.Error())) }
	if e2 != nil { res.Diagnostics = append(res.Diagnostics, diag(session, domain.LayerInitMass, row, "mass", rec[1], "error", e2.Error())) }
	if e1 != nil || e2 != nil { return }
	bal := ""
	if len(rec) > 2 { bal = strings.TrimSpace(rec[2]) }
	note := ""
	if len(rec) > 3 { note = strings.TrimSpace(rec[3]) }
	res.Masses = append(res.Masses, domain.InitMass{Row: row, Time: t, Mass: m, BalanceID: bal, Note: note})
}

// CountLines is a small helper for UI summaries.
func CountLines(text string) int {
	n := 0
	br := bufio.NewReader(strings.NewReader(text))
	for {
		_, err := br.ReadString('\n')
		n++
		if err == io.EOF { break }
		if err != nil { break }
	}
	return n
}
