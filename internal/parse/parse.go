// Package parse ingests the small copy-pasteable TGA tagged-text format.
//
// Design rules enforced here:
//   - the original textual representation of every value is preserved;
//   - every row carries declared units; normalization happens only later;
//   - unparseable values or unknown units stay in place with diagnostics,
//     they never get dropped "to make the plot work";
//   - an import that has some good rows is kept as status=partial.
package parse

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"tga/internal/model"
)

// timeUnits converts a declared unit to a factor yielding seconds.
var timeUnits = map[string]float64{
	"s": 1, "sec": 1, "secs": 1, "second": 1, "seconds": 1,
	"min": 60, "mins": 60, "minute": 60, "minutes": 60,
}

var massUnits = map[string]float64{
	"mg": 1, "milligram": 1, "milligrams": 1,
	"g": 1000, "gram": 1000, "grams": 1000,
}

var tempUnits = map[string]float64{
	"c": 1, "°c": 1, "degc": 1, "celsius": 1,
	"k": 0, "kelvin": 0,
}

func normTime(v float64, unit string) (float64, bool) {
	f, ok := timeUnits[strings.ToLower(strings.TrimSpace(unit))]
	return v * f, ok
}
func normMass(v float64, unit string) (float64, bool) {
	u := strings.ToLower(strings.TrimSpace(unit))
	f, ok := massUnits[u]
	return v * f, ok
}
func normTemp(v float64, unit string) (float64, bool) {
	u := strings.ToLower(strings.TrimSpace(unit))
	f, ok := tempUnits[u]
	if !ok {
		return 0, false
	}
	if f == 0 {
		return v - 273.15, true
	}
	return v, true
}

// Result is a parsed import for one layer.
type Result struct {
	Layer    model.Layer
	Samples  []model.Point
	Blank    []model.Point
	Program  []model.ProgramStep
	Gas      []model.GasEvent
	M0       *model.M0
	Units    map[string]string
	Diags    []model.Diag
	Accepted int
	Total    int
	Status   string
}

// Parse parses one tagged-text document for the given layer.
func Parse(layer model.Layer, text string) (*Result, error) {
	if !layer.Valid() {
		return nil, fmt.Errorf("unknown layer %q", layer)
	}
	r := &Result{Layer: layer, Units: map[string]string{}, Status: "accepted"}
	headerUnits := map[string]string{}
	lines := strings.Split(text, "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "unit:") {
			kv := strings.SplitN(line[5:], "=", 2)
			if len(kv) == 2 {
				headerUnits[strings.TrimSpace(strings.ToLower(kv[0]))] = strings.TrimSpace(kv[1])
				r.Units[strings.TrimSpace(strings.ToLower(kv[0]))] = strings.TrimSpace(kv[1])
			}
			continue
		}
		fields := splitCSV(line)
		switch layer {
		case model.LayerSamples, model.LayerBlank:
			r.Total++
			p := parsePoint(r.Total, fields, headerUnits, layer)
			if p.TimeOK && p.TempOK && p.MassOK {
				r.Accepted++
				if layer == model.LayerSamples {
					r.Samples = append(r.Samples, p)
				} else {
					r.Blank = append(r.Blank, p)
				}
			} else {
				r.Diags = append(r.Diags, model.Diag{
					Code: "ROW_UNPARSEABLE", Severity: model.SevError,
					Message: fmt.Sprintf("%s row %d has unconvertible values; retained raw text with diagnostics", layer, r.Total),
				})
				if layer == model.LayerSamples {
					r.Samples = append(r.Samples, p)
				} else {
					r.Blank = append(r.Blank, p)
				}
			}
		case model.LayerProgram:
			r.Total++
			st, ok := parseStep(r.Total, fields, headerUnits)
			if ok {
				r.Accepted++
			} else {
				r.Diags = append(r.Diags, model.Diag{Code: "PROGRAM_ROW_BAD", Severity: model.SevError,
					Message: fmt.Sprintf("program row %d unconvertible: %q", r.Total, line)})
			}
			r.Program = append(r.Program, st)
		case model.LayerGas:
			r.Total++
			g, ok := parseGas(r.Total, fields, headerUnits)
			if ok {
				r.Accepted++
			} else {
				r.Diags = append(r.Diags, model.Diag{Code: "GAS_ROW_BAD", Severity: model.SevError,
					Message: fmt.Sprintf("gas row %d unconvertible: %q", r.Total, line)})
			}
			r.Gas = append(r.Gas, g)
		case model.LayerM0:
			m, ok := parseM0(line, fields, headerUnits)
			r.Total++
			if ok {
				r.Accepted++
			} else {
				r.Diags = append(r.Diags, model.Diag{Code: "M0_BAD", Severity: model.SevError,
					Message: fmt.Sprintf("initial mass unconvertible: %q", line)})
			}
			r.M0 = m
		}
	}
	switch {
	case r.Total == 0:
		r.Status = "rejected"
		r.Diags = append(r.Diags, model.Diag{Code: "EMPTY_IMPORT", Severity: model.SevError, Message: "no data rows found"})
	case r.Accepted < r.Total:
		r.Status = "partial"
	default:
		r.Status = "accepted"
	}
	return r, nil
}

func splitCSV(line string) []string {
	var out []string
	cur := strings.Builder{}
	inQ := false
	for _, ch := range line {
		switch {
		case ch == '"':
			inQ = !inQ
		case ch == ',' && !inQ:
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(ch)
		}
	}
	out = append(out, strings.TrimSpace(cur.String()))
	return out
}

func unitOf(header map[string]string, key, fallback string) string {
	if u, ok := header[key]; ok {
		return u
	}
	return fallback
}

func parsePoint(seq int, f []string, units map[string]string, layer model.Layer) model.Point {
	p := model.Point{Seq: seq}
	get := func(i int) string {
		if i < len(f) {
			return f[i]
		}
		return ""
	}
	p.TimeRaw = get(0)
	p.TempRaw = get(1)
	p.MassRaw = get(2)
	if v, err := strconv.ParseFloat(p.TimeRaw, 64); err == nil {
		if nv, ok := normTime(v, unitOf(units, "time", "s")); ok {
			p.TimeSec, p.TimeOK = nv, true
		}
	}
	if v, err := strconv.ParseFloat(p.TempRaw, 64); err == nil {
		if nv, ok := normTemp(v, unitOf(units, "temp", "C")); ok {
			p.TempC, p.TempOK = nv, true
		}
	}
	if v, err := strconv.ParseFloat(p.MassRaw, 64); err == nil {
		if nv, ok := normMass(v, unitOf(units, "mass", "mg")); ok {
			p.MassMg, p.MassOK = nv, true
		}
	}
	if !p.TimeOK {
		p.Diags = append(p.Diags, model.Diag{Code: "TIME_UNCONVERTIBLE", Severity: model.SevError, Message: "time value or unit cannot be converted to s"})
	}
	if !p.TempOK {
		p.Diags = append(p.Diags, model.Diag{Code: "TEMP_UNCONVERTIBLE", Severity: model.SevError, Message: "temperature value or unit cannot be converted to C"})
	}
	if !p.MassOK {
		p.Diags = append(p.Diags, model.Diag{Code: "MASS_UNCONVERTIBLE", Severity: model.SevError, Message: "mass value or unit cannot be converted to mg"})
	}
	return p
}

func parseStep(seq int, f []string, units map[string]string) (model.ProgramStep, bool) {
	st := model.ProgramStep{Seq: seq}
	get := func(i int) string {
		if i < len(f) {
			return f[i]
		}
		return ""
	}
	st.Name = get(0)
	t0, e0 := strconv.ParseFloat(get(1), 64)
	t1, e1 := strconv.ParseFloat(get(2), 64)
	a, ea := strconv.ParseFloat(get(3), 64)
	b, eb := strconv.ParseFloat(get(4), 64)
	ok := e0 == nil && e1 == nil && ea == nil && eb == nil
	if ok {
		t0, _ = normTime(t0, unitOf(units, "time", "s"))
		t1, _ = normTime(t1, unitOf(units, "time", "s"))
		b, ok = normTemp(b, unitOf(units, "temp", "C"))
		a, _ = normTemp(a, unitOf(units, "temp", "C"))
		if t1 > t0 {
			st.RateCMin = (b - a) / ((t1 - t0) / 60.0)
		}
	}
	st.T0Sec, st.T1Sec, st.StartC, st.EndC = t0, t1, a, b
	return st, ok
}

func parseGas(seq int, f []string, units map[string]string) (model.GasEvent, bool) {
	g := model.GasEvent{Seq: seq}
	get := func(i int) string {
		if i < len(f) {
			return f[i]
		}
		return ""
	}
	g.TimeRaw = get(0)
	g.FromGas, g.ToGas, g.Note = get(1), get(2), get(3)
	if v, err := strconv.ParseFloat(g.TimeRaw, 64); err == nil {
		if nv, ok := normTime(v, unitOf(units, "time", "s")); ok {
			g.TimeSec, g.TimeOK = nv, true
		}
	}
	return g, g.TimeOK && g.FromGas != "" && g.ToGas != ""
}

func parseM0(line string, f []string, units map[string]string) (*model.M0, bool) {
	m := &model.M0{}
	if strings.Contains(strings.ToLower(line), "=") && len(f) <= 1 {
		kv := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(strings.ToLower(kv[0]))
		val := strings.TrimSpace(kv[1])
		switch key {
		case "mass", "m0", "initial_mass":
			m.ValueRaw = val
		case "unit":
			m.Unit = val
		case "method":
			m.Method = val
		case "weighed_at":
			m.WeighedAt = val
		case "note":
			m.Note = val
		}
	} else if len(f) > 0 {
		m.ValueRaw = f[0]
		if len(f) > 1 {
			m.Unit = f[1]
		}
	}
	if m.Unit == "" {
		m.Unit = unitOf(units, "mass", "mg")
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(m.ValueRaw), 64); err == nil {
		if nv, ok := normMass(v, m.Unit); ok {
			m.ValueMg, m.ValueOK = nv, true
		}
	}
	return m, m.ValueOK
}

// now is overridable in tests.
var now = time.Now

// SetClock injects a clock (used by packages that build submissions).
func SetClock(f func() time.Time) { now = f }
