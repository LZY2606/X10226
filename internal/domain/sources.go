package domain

import (
	"fmt"
	"sort"
	"strings"
)

// SourceKind enumerates the six layered raw inputs of a TGA review.
type SourceKind string

const (
	SamplePoints    SourceKind = "sample_points"     // 原始采样点
	DriftCorrection SourceKind = "drift_correction"  // 天平漂移校正
	FurnaceProgram  SourceKind = "furnace_program"   // 炉程序
	GasValveEvents  SourceKind = "gas_valve_events"  // 气体阀事件
	BlankCurve      SourceKind = "blank_curve"       // 空白曲线
	InitialWeight   SourceKind = "initial_weight"    // 样品初重
)

// AllSourceKinds in canonical import order.
var AllSourceKinds = []SourceKind{
	SamplePoints, DriftCorrection, FurnaceProgram, GasValveEvents, BlankCurve, InitialWeight,
}

// Point is one raw sample: time, furnace temperature, mass.
type Point struct {
	T    Quantity `json:"t"`
	Temp Quantity `json:"temp"`
	Mass Quantity `json:"mass"`
}

// ValveEvent is a gas valve switch as recorded at the valve (not at the sample).
type ValveEvent struct {
	T    Quantity `json:"t"`
	Gas  string   `json:"gas"`
	Flow Quantity `json:"flow"`
}

// FurnacePoint is a (time, setpoint) breakpoint of the furnace program.
type FurnacePoint struct {
	T    Quantity `json:"t"`
	Temp Quantity `json:"temp"`
}

// BlankPoint is one (temperature, apparent mass) point of the blank curve.
type BlankPoint struct {
	Temp Quantity `json:"temp"`
	Mass Quantity `json:"mass"`
}

// SourcePayload is the parsed, still raw-representation-preserving content
// of one source version. Only the field matching the kind is populated.
type SourcePayload struct {
	Kind   SourceKind `json:"kind"`
	Points []Point    `json:"points,omitempty"`   // sample_points
	Drift  Quantity   `json:"drift,omitempty"`    // drift_correction: drift rate
	Program []FurnacePoint `json:"program,omitempty"` // furnace_program
	Valves []ValveEvent `json:"valves,omitempty"`  // gas_valve_events
	Blank  []BlankPoint `json:"blank,omitempty"`   // blank_curve
	Weight Quantity   `json:"weight,omitempty"`   // initial_weight
	Note   string     `json:"note,omitempty"`
	// Diagnostics collected while parsing; unconvertible values stay in place.
	Diags []string `json:"diags,omitempty"`
}

// ParseSource parses the raw text of one layer into a SourcePayload.
// Format is line based: "key: value" headers and "a, b, c" data rows.
// The raw text itself is stored separately and never overwritten.
func ParseSource(kind SourceKind, raw string) (*SourcePayload, error) {
	p := &SourcePayload{Kind: kind}
	addDiag := func(format string, args ...any) {
		p.Diags = append(p.Diags, fmt.Sprintf(format, args...))
	}
	var dataLines []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "note:") {
			p.Note = strings.TrimSpace(strings.TrimPrefix(line, "note:"))
			continue
		}
		dataLines = append(dataLines, line)
	}
	switch kind {
	case SamplePoints:
		for i, l := range dataLines {
			cols := strings.Split(l, ",")
			if len(cols) != 3 {
				return nil, fmt.Errorf("sample_points line %d: want 3 columns (t, temp, mass), got %d", i+1, len(cols))
			}
			pt := Point{ParseQuantity(cols[0]), ParseQuantity(cols[1]), ParseQuantity(cols[2])}
			for _, q := range []Quantity{pt.T, pt.Temp, pt.Mass} {
				if !q.OK {
					addDiag("line %d: %s", i+1, q.Diag)
				}
			}
			p.Points = append(p.Points, pt)
		}
		if len(p.Points) == 0 {
			return nil, fmt.Errorf("sample_points: no data rows")
		}
	case DriftCorrection:
		if len(dataLines) != 1 {
			return nil, fmt.Errorf("drift_correction: want exactly 1 data row (drift rate), got %d", len(dataLines))
		}
		p.Drift = ParseQuantity(dataLines[0])
		if !p.Drift.OK {
			addDiag("drift rate: %s", p.Drift.Diag)
		}
	case FurnaceProgram:
		for i, l := range dataLines {
			cols := strings.Split(l, ",")
			if len(cols) != 2 {
				return nil, fmt.Errorf("furnace_program line %d: want 2 columns (t, temp)", i+1)
			}
			fp := FurnacePoint{ParseQuantity(cols[0]), ParseQuantity(cols[1])}
			if !fp.T.OK || !fp.Temp.OK {
				addDiag("line %d: unconvertible furnace point", i+1)
			}
			p.Program = append(p.Program, fp)
		}
		if len(p.Program) == 0 {
			return nil, fmt.Errorf("furnace_program: no data rows")
		}
	case GasValveEvents:
		for i, l := range dataLines {
			cols := strings.Split(l, ",")
			if len(cols) != 3 {
				return nil, fmt.Errorf("gas_valve_events line %d: want 3 columns (t, gas, flow)", i+1)
			}
			ve := ValveEvent{T: ParseQuantity(cols[0]), Gas: strings.TrimSpace(cols[1]), Flow: ParseQuantity(cols[2])}
			if !ve.T.OK || !ve.Flow.OK {
				addDiag("line %d: unconvertible valve event", i+1)
			}
			if ve.Gas == "" {
				return nil, fmt.Errorf("gas_valve_events line %d: empty gas name", i+1)
			}
			p.Valves = append(p.Valves, ve)
		}
		if len(p.Valves) == 0 {
			return nil, fmt.Errorf("gas_valve_events: no data rows")
		}
	case BlankCurve:
		for i, l := range dataLines {
			cols := strings.Split(l, ",")
			if len(cols) != 2 {
				return nil, fmt.Errorf("blank_curve line %d: want 2 columns (temp, mass)", i+1)
			}
			bp := BlankPoint{ParseQuantity(cols[0]), ParseQuantity(cols[1])}
			if !bp.Temp.OK || !bp.Mass.OK {
				addDiag("line %d: unconvertible blank point", i+1)
			}
			p.Blank = append(p.Blank, bp)
		}
		if len(p.Blank) == 0 {
			return nil, fmt.Errorf("blank_curve: no data rows")
		}
	case InitialWeight:
		if len(dataLines) != 1 {
			return nil, fmt.Errorf("initial_weight: want exactly 1 data row, got %d", len(dataLines))
		}
		p.Weight = ParseQuantity(dataLines[0])
		if !p.Weight.OK {
			addDiag("initial weight: %s", p.Weight.Diag)
		}
	default:
		return nil, fmt.Errorf("unknown source kind %q", kind)
	}
	return p, nil
}

// OKPoints returns sample points whose quantities all converted, sorted by time.
func (p *SourcePayload) OKPoints() []Point {
	var out []Point
	for _, pt := range p.Points {
		if pt.T.OK && pt.Temp.OK && pt.Mass.OK {
			out = append(out, pt)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T.Canon < out[j].T.Canon })
	return out
}
