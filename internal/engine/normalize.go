package engine

import (
	"fmt"
	"sort"

	"tga-review/internal/types"
	"tga-review/internal/units"
)

// Normalized inputs in canonical coordinates. Every value still references the
// originating fragment via FragRef so displays can trace back to raw text.
type (
	samplePoint struct {
		T, Temp, Mass float64
		Frag          int
		Raw           string
	}
	driftPoint struct {
		T, Offset float64
		Frag      int
	}
	segment struct {
		Seg                  int
		T0, T1, Temp0, Temp1 float64
		Frag                 int
	}
	gasEvent struct {
		T        float64
		From, To string
		Frag     int
	}
	blankPoint struct {
		T, Mass float64
		Frag    int
	}
	massLayer struct {
		Layer string
		Mass  float64
		Frag  int
	}
	normalized struct {
		sample  []samplePoint
		drift   []driftPoint
		program []segment
		gas     []gasEvent
		blank   []blankPoint
		layers  []massLayer
	}
)

type versionBundle struct {
	byKind map[string]*types.SourceVersion
}

func normalize(bundle versionBundle) (normalized, []string, error) {
	var n normalized
	var diags []string

	for _, k := range types.SourceKinds {
		v, ok := bundle.byKind[k]
		if !ok || v == nil {
			return n, nil, fmt.Errorf("missing layer %s", k)
		}
		for _, f := range v.Fragments {
			if f.Status != types.FragAccepted {
				continue
			}
			switch k {
			case types.SampleSeries:
				t, e1 := units.Parse(f.Fields["time"])
				p, e2 := units.Parse(f.Fields["temp"])
				m, e3 := units.Parse(f.Fields["mass"])
				if e1 != nil || e2 != nil || e3 != nil {
					diags = append(diags, fmt.Sprintf("sample row %d skipped", f.LocalID))
					continue
				}
				tc, _ := t.ToCanon(units.DimTime)
				pc, _ := p.ToCanon(units.DimTemp)
				mc, _ := m.ToCanon(units.DimMass)
				n.sample = append(n.sample, samplePoint{tc, pc, mc, f.LocalID, f.RawText})
			case types.DriftCorrection:
				t, _ := units.Parse(f.Fields["time"])
				o, _ := units.Parse(f.Fields["offset"])
				tc, _ := t.ToCanon(units.DimTime)
				oc, _ := o.ToCanon(units.DimMass)
				n.drift = append(n.drift, driftPoint{tc, oc, f.LocalID})
			case types.FurnaceProgram:
				seg := 0
				fmt.Sscanf(f.Fields["segment"], "%d", &seg)
				t0, _ := units.Parse(f.Fields["start_time"])
				t1, _ := units.Parse(f.Fields["end_time"])
				p0, _ := units.Parse(f.Fields["start_temp"])
				p1, _ := units.Parse(f.Fields["end_temp"])
				t0c, _ := t0.ToCanon(units.DimTime)
				t1c, _ := t1.ToCanon(units.DimTime)
				p0c, _ := p0.ToCanon(units.DimTemp)
				p1c, _ := p1.ToCanon(units.DimTemp)
				n.program = append(n.program, segment{seg, t0c, t1c, p0c, p1c, f.LocalID})
			case types.GasValveEvents:
				t, _ := units.Parse(f.Fields["time"])
				tc, _ := t.ToCanon(units.DimTime)
				n.gas = append(n.gas, gasEvent{tc, f.Fields["from_gas"], f.Fields["to_gas"], f.LocalID})
			case types.BlankCurve:
				t, _ := units.Parse(f.Fields["time"])
				m, _ := units.Parse(f.Fields["mass"])
				tc, _ := t.ToCanon(units.DimTime)
				mc, _ := m.ToCanon(units.DimMass)
				n.blank = append(n.blank, blankPoint{tc, mc, f.LocalID})
			case types.InitialMass:
				m, _ := units.Parse(f.Fields["value"])
				mc, _ := m.ToCanon(units.DimMass)
				n.layers = append(n.layers, massLayer{f.Fields["layer"], mc, f.LocalID})
			}
		}
	}

	sort.Slice(n.sample, func(i, j int) bool { return n.sample[i].T < n.sample[j].T })
	sort.Slice(n.drift, func(i, j int) bool { return n.drift[i].T < n.drift[j].T })
	sort.Slice(n.blank, func(i, j int) bool { return n.blank[i].T < n.blank[j].T })
	sort.Slice(n.gas, func(i, j int) bool { return n.gas[i].T < n.gas[j].T })
	sort.Slice(n.program, func(i, j int) bool { return n.program[i].T0 < n.program[j].T0 })

	if len(n.sample) < 3 {
		return n, diags, fmt.Errorf("need at least 3 accepted sample points, got %d", len(n.sample))
	}
	return n, diags, nil
}

func (n normalized) layer(name string) (massLayer, bool) {
	for _, l := range n.layers {
		if l.Layer == name {
			return l, true
		}
	}
	return massLayer{}, false
}
