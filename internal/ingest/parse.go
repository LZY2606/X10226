package ingest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"tga/internal/domain"
)

// Row is one parsed-but-not-normalised record: the raw line is always kept.
type Row struct {
	Index      int
	RawLine    string
	Fields     map[string]domain.Quantity
	ParseOK    bool
	Diagnostic string
}

// Result is the output of parsing one submission.
type Result struct {
	Kind       string
	Rows       []Row
	Headers    map[string]string
	Rejected   int
	Accepted   int
	FatalError string
}

// Parse dispatches by declared kind.
func Parse(kind, rawText string) *Result {
	res := &Result{Kind: kind, Headers: map[string]string{}}
	tokeniseHeaders(rawText, res.Headers)
	switch kind {
	case domain.KindSamples:
		parseSamples(rawText, res)
	case domain.KindDrift:
		parseDrift(rawText, res)
	case domain.KindFurnace:
		parseFurnace(rawText, res)
	case domain.KindGas:
		parseGas(rawText, res)
	case domain.KindBlank:
		parseBlank(rawText, res)
	case domain.KindInitialMass:
		parseInitialMass(rawText, res)
	case domain.KindReplayScript:
		parseReplay(rawText, res)
	default:
		res.FatalError = "unknown source kind: " + kind
	}
	if res.FatalError == "" && res.Accepted == 0 {
		res.FatalError = "no accepted data rows"
	}
	return res
}

func tokeniseHeaders(raw string, h map[string]string) {
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "#") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if i := strings.IndexByte(body, ':'); i > 0 {
			k := strings.ToLower(strings.TrimSpace(body[:i]))
			v := strings.TrimSpace(body[i+1:])
			h[k] = v
		}
	}
}

// splitDataLines returns non-comment, non-blank lines with original index.
func splitDataLines(raw string) []struct {
	N    int
	Line string
} {
	var out []struct {
		N    int
		Line string
	}
	sc := bufio.NewScanner(strings.NewReader(raw))
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, struct {
			N    int
			Line string
		}{n, line})
	}
	return out
}

func fieldsJSON(r *Row) string {
	b, _ := json.Marshal(r.Fields)
	return string(b)
}

// RowToDomain adapts a parsed row for persistence.
func RowToDomain(sourceID string, r *Row) *domain.SourceRow {
	return &domain.SourceRow{
		SourceID:   sourceID,
		RowIndex:   r.Index,
		RawLine:    r.RawLine,
		Fields:     fieldsJSON(r),
		ParseOK:    r.ParseOK,
		Diagnostic: r.Diagnostic,
	}
}

func reject(res *Result, idx int, line, msg string) {
	res.Rejected++
	res.Rows = append(res.Rows, Row{
		Index:      idx,
		RawLine:    line,
		Fields:     map[string]domain.Quantity{},
		ParseOK:    false,
		Diagnostic: msg,
	})
}

func add(res *Result, idx int, line string, fields map[string]domain.Quantity) {
	res.Accepted++
	res.Rows = append(res.Rows, Row{Index: idx, RawLine: line, Fields: fields, ParseOK: true})
}

// ---- samples: time,time_unit,temp,temp_unit,mass,mass_unit ----------------

func parseSamples(raw string, res *Result) {
	tu := headerOr(res.Headers, "time_unit", "s")
	pu := headerOr(res.Headers, "temp_unit", "C")
	mu := headerOr(res.Headers, "mass_unit", "mg")
	for i, dl := range splitDataLines(raw) {
		parts := strings.Split(dl.Line, ",")
		if len(parts) < 3 {
			reject(res, dl.N, dl.Line, "expected >=3 columns: time,temp,mass")
			continue
		}
		t := QuantityFromText(parts[0], tu)
		p := QuantityFromText(parts[1], pu)
		m := QuantityFromText(parts[2], mu)
		rowOK := t.Valid && p.Valid && m.Valid
		f := map[string]domain.Quantity{"time": t, "temp": p, "mass": m}
		if !rowOK {
			msg := joinInvalid(t, p, m)
			res.Rejected++
			res.Rows = append(res.Rows, Row{Index: dl.N, RawLine: dl.Line, Fields: f, ParseOK: false, Diagnostic: msg})
			continue
		}
		if i > 0 {
			prev := lastAccepted(res)
			if prev >= 0 && t.Value <= res.Rows[prev].Fields["time"].Value {
				// non-monotonic / duplicate time is preserved as accepted data but flagged later
			}
		}
		add(res, dl.N, dl.Line, f)
	}
}

func lastAccepted(res *Result) int {
	for i := len(res.Rows) - 1; i >= 0; i-- {
		if res.Rows[i].ParseOK {
			return i
		}
	}
	return -1
}

func joinInvalid(qs ...domain.Quantity) string {
	var bad []string
	names := []string{"time", "temp", "mass"}
	for i, q := range qs {
		if !q.Valid {
			bad = append(bad, names[i]+": "+q.Reason)
		}
	}
	return strings.Join(bad, "; ")
}

func headerOr(h map[string]string, k, def string) string {
	if v, ok := h[k]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

// ---- drift: linear balance drift in mass/time ----------------------------

func parseDrift(raw string, res *Result) {
	want := map[string]string{
		"slope": headerOr(res.Headers, "slope_unit", "mg/h"),
	}
	_ = want
	for _, dl := range splitDataLines(raw) {
		parts := strings.Split(dl.Line, ",")
		if len(parts) < 3 {
			reject(res, dl.N, dl.Line, "expected: t0,mass_offset,slope")
			continue
		}
		tu := headerOr(res.Headers, "time_unit", "s")
		mu := headerOr(res.Headers, "offset_unit", "mg")
		su := headerOr(res.Headers, "slope_unit", "mg/h")
		t0 := QuantityFromText(parts[0], tu)
		off := QuantityFromText(parts[1], mu)
		sl := QuantityFromText(parts[2], su)
		f := map[string]domain.Quantity{"t0": t0, "offset": off, "slope": sl}
		if !(t0.Valid && off.Valid && sl.Valid) {
			res.Rejected++
			res.Rows = append(res.Rows, Row{Index: dl.N, RawLine: dl.Line, Fields: f, ParseOK: false,
				Diagnostic: "drift row contains invalid quantity"});
			continue
		}
		add(res, dl.N, dl.Line, f)
	}
}

// ---- furnace: start,end,target_temp / or hold -----------------------------

func parseFurnace(raw string, res *Result) {
	tu := headerOr(res.Headers, "time_unit", "min")
	pu := headerOr(res.Headers, "temp_unit", "C")
	for _, dl := range splitDataLines(raw) {
		parts := strings.Split(dl.Line, ",")
		if len(parts) < 3 {
			reject(res, dl.N, dl.Line, "expected: start,end,target_temp[,mode]")
			continue
		}
		st := QuantityFromText(parts[0], tu)
		en := QuantityFromText(parts[1], tu)
		tp := QuantityFromText(parts[2], pu)
		mode := "ramp"
		if len(parts) >= 4 && strings.TrimSpace(parts[3]) != "" {
			mode = strings.TrimSpace(parts[3])
		}
		f := map[string]domain.Quantity{"start": st, "end": en, "target": tp}
		f["mode"] = domain.Quantity{Raw: mode, Value: 0, Unit: "label", Valid: true}
		if !(st.Valid && en.Valid && tp.Valid) {
			res.Rejected++
			res.Rows = append(res.Rows, Row{Index: dl.N, RawLine: dl.Line, Fields: f, ParseOK: false,
				Diagnostic: "furnace segment invalid"});
			continue
		}
		add(res, dl.N, dl.Line, f)
	}
}

// ---- gas: time,from_gas,to_gas -------------------------------------------

func parseGas(raw string, res *Result) {
	tu := headerOr(res.Headers, "time_unit", "s")
	for _, dl := range splitDataLines(raw) {
		parts := strings.Split(dl.Line, ",")
		if len(parts) < 3 {
			reject(res, dl.N, dl.Line, "expected: time,from_gas,to_gas")
			continue
		}
		t := QuantityFromText(parts[0], tu)
		from := strings.TrimSpace(parts[1])
		to := strings.TrimSpace(parts[2])
		f := map[string]domain.Quantity{"time": t}
		f["from_gas"] = domain.Quantity{Raw: from, Unit: "gas", Valid: from != ""}
		f["to_gas"] = domain.Quantity{Raw: to, Unit: "gas", Valid: to != ""}
		if !t.Valid || from == "" || to == "" {
			res.Rejected++
			res.Rows = append(res.Rows, Row{Index: dl.N, RawLine: dl.Line, Fields: f, ParseOK: false,
				Diagnostic: "gas valve event invalid"});
			continue
		}
		add(res, dl.N, dl.Line, f)
	}
}

// ---- blank: time,temp,mass (baseline curve) ------------------------------

func parseBlank(raw string, res *Result) {
	tu := headerOr(res.Headers, "time_unit", "s")
	pu := headerOr(res.Headers, "temp_unit", "C")
	mu := headerOr(res.Headers, "mass_unit", "mg")
	for _, dl := range splitDataLines(raw) {
		parts := strings.Split(dl.Line, ",")
		if len(parts) < 3 {
			reject(res, dl.N, dl.Line, "expected: time,temp,mass")
			continue
		}
		t := QuantityFromText(parts[0], tu)
		p := QuantityFromText(parts[1], pu)
		m := QuantityFromText(parts[2], mu)
		f := map[string]domain.Quantity{"time": t, "temp": p, "mass": m}
		if !(t.Valid && p.Valid && m.Valid) {
			res.Rejected++
			res.Rows = append(res.Rows, Row{Index: dl.N, RawLine: dl.Line, Fields: f, ParseOK: false,
				Diagnostic: "blank point invalid"});
			continue
		}
		add(res, dl.N, dl.Line, f)
	}
}

// ---- initial mass: mass,unit[,basis,version_note] ------------------------

func parseInitialMass(raw string, res *Result) {
	mu := headerOr(res.Headers, "mass_unit", "mg")
	for _, dl := range splitDataLines(raw) {
		parts := strings.Split(dl.Line, ",")
		if len(parts) < 1 {
			reject(res, dl.N, dl.Line, "expected: mass[,basis,note]")
			continue
		}
		m := QuantityFromText(parts[0], mu)
		basis, note := "as_loaded", ""
		if len(parts) >= 2 {
			basis = strings.TrimSpace(parts[1])
		}
		if len(parts) >= 3 {
			note = strings.TrimSpace(parts[2])
		}
		f := map[string]domain.Quantity{"mass": m}
		f["basis"] = domain.Quantity{Raw: basis, Unit: "label", Valid: basis != ""}
		f["note"] = domain.Quantity{Raw: note, Unit: "label", Valid: true}
		if !m.Valid {
			res.Rejected++
			res.Rows = append(res.Rows, Row{Index: dl.N, RawLine: dl.Line, Fields: f, ParseOK: false,
				Diagnostic: "initial mass invalid: " + m.Reason});
			continue
		}
		add(res, dl.N, dl.Line, f)
	}
}

// ---- replay script: step,kind,target,arg ---------------------------------

func parseReplay(raw string, res *Result) {
	for _, dl := range splitDataLines(raw) {
		parts := strings.Split(dl.Line, ",")
		if len(parts) < 3 {
			reject(res, dl.N, dl.Line, "expected: seq,action,payload...")
			continue
		}
		if _, err := strconv.Atoi(strings.TrimSpace(parts[0])); err != nil {
			reject(res, dl.N, dl.Line, "seq must be integer: "+err.Error())
			continue
		}
		f := map[string]domain.Quantity{
			"seq":    QuantityFromText(parts[0], "int"),
			"action": {Raw: strings.TrimSpace(parts[1]), Unit: "label", Valid: true},
			"arg":    {Raw: strings.Join(parts[2:], ","), Unit: "text", Valid: true},
		}
		add(res, dl.N, dl.Line, f)
	}
}

var _ = fmt.Sprintf
