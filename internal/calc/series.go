package calc

import (
	"math"
	"sort"

	"tgasys/internal/domain"
	"tgasys/internal/ingest"
	"tgasys/internal/quant"
)

// Point is a normalized sample point. Invalid/unknown masses are kept as gaps
// (MassOK=false) and are never interpolated across.
type Point struct {
	T        float64
	Temp     float64
	TempOK   bool
	Mass     float64
	MassOK   bool
	RawTime  string
	RawMass  string
	RawTemp  string
}

// Segment is a maximal run of usable points. Sampling gaps split series into
// segments; no interpolation crosses a boundary.
type Segment struct {
	Index  int
	Start  int
	End    int
	Points []Point
	GapAfterS float64 // 0 unless followed by a gap
}

// Model is the normalized working data derived from preserved layers.
type Model struct {
	Params    domain.Params
	Points    []Point
	Segments  []Segment
	Blank     []Point // blank curve in (s, mg)
	Initial   []InitialReading
	Drift     DriftSpec
	Gas       []GasSwitch
	Program   []ProgramStep

	// corrected
	Corrected []Point
}

type InitialReading struct {
	T    float64
	Mass float64
	Raw  string
	Note string
}

type DriftSpec struct {
	Method    string
	Slope     float64
	Intercept float64
	HasLinear bool
}

type GasSwitch struct {
	At      float64
	FromGas string
	ToGas   string
	Flow    string
}

type ProgramStep struct {
	StartS float64
	EndS   float64
	Rate   float64 // °C/min, 0 if not given
	Target float64
}

func qCell(c ingest.Cell) quant.Q { return c.Q }

// BuildModel normalizes parsed layers into the calculation model. It retains
// invalid points as gaps and records drift / blank / gas / program info.
func BuildModel(layers map[domain.Layer]*ingest.Layer, p domain.Params) *Model {
	m := &Model{Params: p}
	if l := layers[domain.LayerSample]; l != nil {
		for _, r := range l.Rows {
			pt := Point{RawTime: r.Cells[0].Raw, RawMass: r.Cells[1].Raw, RawTemp: r.Cells[2].Raw}
			if qCell(r.Cells[0]).Finite() {
				pt.T = qCell(r.Cells[0]).Value
			}
			if qCell(r.Cells[1]).Finite() {
				pt.Mass, pt.MassOK = qCell(r.Cells[1]).Value, true
			}
			if qCell(r.Cells[2]).Finite() {
				pt.Temp, pt.TempOK = qCell(r.Cells[2]).Value, true
			}
			m.Points = append(m.Points, pt)
		}
		sort.SliceStable(m.Points, func(i, j int) bool { return m.Points[i].T < m.Points[j].T })
	}
	if l := layers[domain.LayerBlank]; l != nil {
		for _, r := range l.Rows {
			if len(r.Cells) >= 2 && qCell(r.Cells[0]).Finite() && qCell(r.Cells[1]).Finite() {
				m.Blank = append(m.Blank, Point{T: qCell(r.Cells[0]).Value, Mass: qCell(r.Cells[1]).Value, MassOK: true})
			}
		}
	}
	if l := layers[domain.LayerInitial]; l != nil {
		for _, r := range l.Rows {
			if qCell(r.Cells[0]).Finite() && qCell(r.Cells[1]).Finite() {
				note := ""
				if len(r.Cells) > 2 {
					note = r.Cells[2].Raw
				}
				m.Initial = append(m.Initial, InitialReading{
					T: qCell(r.Cells[0]).Value, Mass: qCell(r.Cells[1]).Value,
					Raw: r.Cells[1].Raw, Note: note,
				})
			}
		}
	}
	if l := layers[domain.LayerDrift]; l != nil && len(l.Rows) > 0 {
		r := l.Rows[0]
		m.Drift = DriftSpec{Method: r.Cells[0].Raw}
		if v, err := ingest.AsFloat(r.Cells[1].Raw); err == nil {
			m.Drift.Slope, m.Drift.HasLinear = v, true
		}
		if len(r.Cells) > 2 {
			if v, err := ingest.AsFloat(r.Cells[2].Raw); err == nil {
				m.Drift.Intercept = v
			}
		}
	}
	if l := layers[domain.LayerGas]; l != nil {
		for _, r := range l.Rows {
			if qCell(r.Cells[0]).Finite() {
				m.Gas = append(m.Gas, GasSwitch{
					At: qCell(r.Cells[0]).Value, FromGas: r.Cells[1].Raw,
					ToGas: r.Cells[2].Raw, Flow: r.Cells[3].Raw,
				})
			}
		}
		sort.SliceStable(m.Gas, func(i, j int) bool { return m.Gas[i].At < m.Gas[j].At })
	}
	if l := layers[domain.LayerProgram]; l != nil {
		for _, r := range l.Rows {
			if qCell(r.Cells[0]).Finite() && qCell(r.Cells[1]).Finite() && qCell(r.Cells[3]).Finite() {
				st := ProgramStep{StartS: qCell(r.Cells[0]).Value, EndS: qCell(r.Cells[1]).Value, Target: qCell(r.Cells[3]).Value}
				if qCell(r.Cells[2]).Finite() {
					st.Rate = qCell(r.Cells[2]).Value
				}
				m.Program = append(m.Program, st)
			}
		}
	}
	m.correct()
	m.segment()
	return m
}

// correct applies balance baseline drift correction and blank subtraction.
func (m *Model) correct() {
	for _, p := range m.Points {
		if !p.MassOK {
			m.Corrected = append(m.Corrected, p)
			continue
		}
		corr := p.Mass
		switch m.Params.DriftMode {
		case "linear":
			if m.Drift.HasLinear {
				corr -= m.Drift.Slope*p.T + m.Drift.Intercept
			}
		case "blank":
			if b := m.blankAt(p.T); b != nil {
				corr -= b.Mass
			} else if m.Drift.HasLinear {
				corr -= m.Drift.Slope*p.T + m.Drift.Intercept
			}
		}
		p2 := p
		p2.Mass = corr
		m.Corrected = append(m.Corrected, p2)
	}
}

// blankAt interpolates the blank only WITHIN its measured time range. Outside
// the range there is no correction (no extrapolation).
func (m *Model) blankAt(t float64) *Point {
	if len(m.Blank) == 0 || t < m.Blank[0].T || t > m.Blank[len(m.Blank)-1].T {
		return nil
	}
	for i := 0; i+1 < len(m.Blank); i++ {
		a, b := m.Blank[i], m.Blank[i+1]
		if t >= a.T && t <= b.T {
			f := 0.0
			if b.T > a.T {
				f = (t - a.T) / (b.T - a.T)
			}
			return &Point{T: t, Mass: a.Mass + f*(b.Mass-a.Mass), MassOK: true}
		}
	}
	return nil
}

// segment splits corrected points wherever mass is missing, wherever the time
// step is larger than 2x the median spacing (sampling gap), and at non-finite
// values. Nothing is interpolated across those boundaries.
func (m *Model) segment() {
	dts := []float64{}
	for i := 1; i < len(m.Corrected); i++ {
		if d := m.Corrected[i].T - m.Corrected[i-1].T; d > 0 {
			dts = append(dts, d)
		}
	}
	med := median(dts)
	gapThr := 2 * med
	start := -1
	flush := func(end int) {
		if start < 0 {
			return
		}
		pts := append([]Point(nil), m.Corrected[start:end+1]...)
		m.Segments = append(m.Segments, Segment{
			Index: len(m.Segments), Start: start, End: end, Points: pts,
		})
		start = -1
	}
	for i, p := range m.Corrected {
		if !p.MassOK {
			flush(i - 1)
			if len(m.Segments) > 0 {
				m.Segments[len(m.Segments)-1].GapAfterS = p.T
			}
			continue
		}
		if i > 0 && start >= 0 && med > 0 && p.T-m.Corrected[i-1].T > gapThr {
			flush(i - 1)
			m.Segments[len(m.Segments)-1].GapAfterS = p.T
		}
		if start < 0 {
			start = i
		}
	}
	flush(len(m.Corrected) - 1)
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

var _ = math.Pi
