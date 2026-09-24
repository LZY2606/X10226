package tga

import (
	"fmt"
	"strconv"
	"strings"
)

// Layer is a parsed raw document. Raw is the verbatim submission; Records
// carry both the original string cells and converted values. Conversion
// failures never drop data: the raw cell stays and a Diagnostic is added.
type Layer struct {
	Kind    string
	Raw     string
	Headers map[string]string
	Records []map[string]string
	Diags   []Diagnostic
}

// ParseError describes a hard format error (the layer is rejected as a
// whole, which may cause a partial import of the surrounding bundle).
type ParseError struct {
	Kind   string
	Reason string
}

func (e *ParseError) Error() string { return fmt.Sprintf("layer %s: %s", e.Kind, e.Reason) }

// ParseLayer parses a small copy-pasteable document:
//
//	# layer: raw_samples
//	# units: time=s,temp=C,mass=mg
//	time,temp,mass
//	0,25.1,12.500
//
// Lines starting with '#' are headers; the first non-header line is the
// column row; the rest are records.
func ParseLayer(kind, raw string) (*Layer, error) {
	l := &Layer{Kind: kind, Raw: raw, Headers: map[string]string{}}
	var cols []string
	for ln, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			kv := strings.SplitN(strings.TrimPrefix(line, "#"), ":", 2)
			if len(kv) == 2 {
				l.Headers[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			}
			continue
		}
		if cols == nil {
			cols = splitCSV(line)
			continue
		}
		cells := splitCSV(line)
		if len(cells) != len(cols) {
			return nil, &ParseError{Kind: kind, Reason: fmt.Sprintf("line %d: expected %d columns, got %d", ln+1, len(cols), len(cells))}
		}
		rec := map[string]string{}
		for i, c := range cols {
			rec[c] = cells[i]
		}
		l.Records = append(l.Records, rec)
	}
	if cols == nil {
		return nil, &ParseError{Kind: kind, Reason: "missing column header row"}
	}
	if lk, ok := l.Headers["layer"]; ok && lk != kind {
		return nil, &ParseError{Kind: kind, Reason: fmt.Sprintf("document declares layer %q", lk)}
	}
	if len(l.Records) == 0 {
		return nil, &ParseError{Kind: kind, Reason: "no data records"}
	}
	return l, nil
}

func splitCSV(line string) []string {
	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// unitFor resolves the unit of a column from the `units:` header, falling
// back to the column-name suffix convention (e.g. "time_s" => "s").
func (l *Layer) unitFor(col string) string {
	if u, ok := l.Headers["units"]; ok {
		for _, kv := range strings.Split(u, ",") {
			p := strings.SplitN(kv, "=", 2)
			if len(p) == 2 && strings.TrimSpace(p[0]) == col {
				return strings.TrimSpace(p[1])
			}
		}
	}
	if i := strings.LastIndex(col, "_"); i >= 0 && i < len(col)-1 {
		return col[i+1:]
	}
	return ""
}

// num reads a float cell; the raw string is preserved on failure.
func (l *Layer) num(rec map[string]string, col string, row int) (float64, bool) {
	raw, ok := rec[col]
	if !ok {
		l.Diags = append(l.Diags, Diagnostic{Where: fmt.Sprintf("row %d", row), Raw: "", Reason: "missing column " + col})
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		l.Diags = append(l.Diags, Diagnostic{Where: fmt.Sprintf("row %d col %s", row, col), Raw: raw, Reason: "not a number; left unconverted"})
		return 0, false
	}
	return v, true
}

// Samples converts a raw_samples layer into normalized points with gap
// detection. Unconvertible masses stay as Raw with MassOK=false.
func (l *Layer) Samples(p Params) (*NormSamples, error) {
	if l.Kind != LayerRawSamples {
		return nil, &ParseError{Kind: l.Kind, Reason: "not a raw_samples layer"}
	}
	out := &NormSamples{}
	tUnit, tempUnit, massUnit := l.unitFor("time"), l.unitFor("temp"), l.unitFor("mass")
	for i, rec := range l.Records {
		var pt Point
		if v, ok := l.num(rec, "time", i); ok {
			if cv, cok := ConvertTime(v, tUnit); cok {
				pt.T = cv
			} else {
				l.Diags = append(l.Diags, Diagnostic{Where: fmt.Sprintf("row %d col time", i), Raw: rec["time"], Reason: "unknown time unit " + tUnit})
				continue
			}
		} else {
			continue
		}
		if v, ok := l.num(rec, "temp", i); ok {
			if cv, cok := ConvertTemp(v, tempUnit); cok {
				pt.Temp = cv
			} else {
				l.Diags = append(l.Diags, Diagnostic{Where: fmt.Sprintf("row %d col temp", i), Raw: rec["temp"], Reason: "unknown temp unit " + tempUnit})
				continue
			}
		} else {
			continue
		}
		rawMass := rec["mass"]
		if v, ok := l.num(rec, "mass", i); ok {
			if cv, cok := ConvertMass(v, massUnit); cok {
				pt.Mass, pt.MassOK = cv, true
			} else {
				pt.RawMass = rawMass
				l.Diags = append(l.Diags, Diagnostic{Where: fmt.Sprintf("row %d col mass", i), Raw: rawMass, Reason: "unknown mass unit " + massUnit + "; left unconverted"})
			}
		} else {
			pt.RawMass = rawMass
		}
		out.Points = append(out.Points, pt)
	}
	if len(out.Points) < 2 {
		return nil, &ParseError{Kind: l.Kind, Reason: "fewer than 2 usable sample points"}
	}
	out.Gaps = DetectGaps(timesOf(out.Points), p.GapFactor)
	out.Diags = l.Diags
	return out, nil
}

func timesOf(pts []Point) []float64 {
	ts := make([]float64, len(pts))
	for i, p := range pts {
		ts[i] = p.T
	}
	return ts
}

// DetectGaps finds sampling gaps: intervals longer than factor*median(dt).
func DetectGaps(ts []float64, factor float64) []Gap {
	if len(ts) < 3 {
		return nil
	}
	dts := make([]float64, 0, len(ts)-1)
	for i := 1; i < len(ts); i++ {
		dts = append(dts, ts[i]-ts[i-1])
	}
	med := median(dts)
	var gaps []Gap
	for i, d := range dts {
		if d > factor*med {
			gaps = append(gaps, Gap{StartT: ts[i], EndT: ts[i+1]})
		}
	}
	return gaps
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s[len(s)/2]
}

// DriftPoint is one (time, cumulative drift) anchor in mg.
type DriftPoint struct{ T, Drift float64 }

// Drift parses a drift_correction layer into anchors.
func (l *Layer) Drift() ([]DriftPoint, error) {
	if l.Kind != LayerDrift {
		return nil, &ParseError{Kind: l.Kind, Reason: "not a drift_correction layer"}
	}
	var out []DriftPoint
	for i, rec := range l.Records {
		t, ok1 := l.num(rec, "start_s", i)
		d, ok2 := l.num(rec, "drift_mg", i)
		if ok1 && ok2 {
			out = append(out, DriftPoint{T: t, Drift: d})
		}
	}
	if len(out) == 0 {
		return nil, &ParseError{Kind: l.Kind, Reason: "no usable drift anchors"}
	}
	return out, nil
}

// FurnaceSegment is one ramp/hold of the furnace program.
type FurnaceSegment struct {
	StartT, EndT       float64
	StartTemp, EndTemp float64
}

// Furnace parses a furnace_program layer.
func (l *Layer) Furnace() ([]FurnaceSegment, error) {
	if l.Kind != LayerFurnace {
		return nil, &ParseError{Kind: l.Kind, Reason: "not a furnace_program layer"}
	}
	var out []FurnaceSegment
	for i, rec := range l.Records {
		st, ok1 := l.num(rec, "start_s", i)
		en, ok2 := l.num(rec, "end_s", i)
		t0, ok3 := l.num(rec, "temp_start_C", i)
		t1, ok4 := l.num(rec, "temp_end_C", i)
		if ok1 && ok2 && ok3 && ok4 {
			out = append(out, FurnaceSegment{st, en, t0, t1})
		}
	}
	if len(out) == 0 {
		return nil, &ParseError{Kind: l.Kind, Reason: "no usable furnace segments"}
	}
	return out, nil
}

// GasEvent is one valve switch.
type GasEvent struct {
	T         float64
	FromGas   string
	ToGas     string
	FlowMLMin float64
}

// Gas parses a gas_events layer.
func (l *Layer) Gas() ([]GasEvent, error) {
	if l.Kind != LayerGasEvents {
		return nil, &ParseError{Kind: l.Kind, Reason: "not a gas_events layer"}
	}
	var out []GasEvent
	for i, rec := range l.Records {
		t, ok := l.num(rec, "time_s", i)
		if !ok {
			continue
		}
		flow, _ := l.num(rec, "flow_mL_min", i)
		out = append(out, GasEvent{T: t, FromGas: rec["from_gas"], ToGas: rec["to_gas"], FlowMLMin: flow})
	}
	if len(out) == 0 {
		return nil, &ParseError{Kind: l.Kind, Reason: "no usable gas events"}
	}
	return out, nil
}

// BlankPoint is one (temp, apparent mass) anchor of the blank curve.
type BlankPoint struct{ Temp, Mass float64 }

// Blank parses a blank_curve layer.
func (l *Layer) Blank() ([]BlankPoint, error) {
	if l.Kind != LayerBlank {
		return nil, &ParseError{Kind: l.Kind, Reason: "not a blank_curve layer"}
	}
	var out []BlankPoint
	for i, rec := range l.Records {
		t, ok1 := l.num(rec, "temp_C", i)
		m, ok2 := l.num(rec, "blank_mg", i)
		if ok1 && ok2 {
			out = append(out, BlankPoint{Temp: t, Mass: m})
		}
	}
	if len(out) == 0 {
		return nil, &ParseError{Kind: l.Kind, Reason: "no usable blank anchors"}
	}
	return out, nil
}

// InitialMass parses an initial_mass layer, preserving the basis.
func (l *Layer) InitialMass() (mass float64, basis string, diags []Diagnostic, err error) {
	if l.Kind != LayerInitialMass {
		return nil, "", nil, &ParseError{Kind: l.Kind, Reason: "not an initial_mass layer"}
	}
	basis = l.Headers["basis"]
	if basis == "" {
		basis = "unspecified"
	}
	v, ok := l.num(l.Records[0], "mass", 0)
	if !ok {
		return 0, basis, l.Diags, &ParseError{Kind: l.Kind, Reason: "initial mass not a number"}
	}
	unit := l.Records[0]["unit"]
	if unit == "" {
		unit = l.unitFor("mass")
	}
	cv, cok := ConvertMass(v, unit)
	if !cok {
		l.Diags = append(l.Diags, Diagnostic{Where: "row 0 col mass", Raw: l.Records[0]["mass"] + " " + unit, Reason: "unknown mass unit; left unconverted"})
		return 0, basis, l.Diags, nil // unknown state, not a hard error
	}
	return cv, basis, l.Diags, nil
}
