package calc

import (
	"sort"

	"tga-review/internal/domain"
)

func payloadFurnace(s *domain.Source) []domain.FurnaceSegment {
	if s == nil {
		return nil
	}
	raw, _ := s.Payload["segments"].([]domain.FurnaceSegment)
	return raw
}

func payloadValves(s *domain.Source) []domain.ValveEvent {
	if s == nil {
		return nil
	}
	raw, _ := s.Payload["events"].([]domain.ValveEvent)
	return raw
}

// ComputeFurnace builds setpoint and effective sample temperature curves.
// R2 temperature lag: first-order response with time constant TempLagS, so the
// effective sample temperature at time t is not the controller setpoint.
func ComputeFurnace(in Inputs, times []float64) FurnaceTrace {
	segs := payloadFurnaceX(in.Furnace)
	lag := in.Params.TempLagS
	if lag <= 0 {
		lag = 1e-9
	}

	type span struct{ t0, t1, t0c, t1c, rate float64 }
	spans := make([]span, 0, len(segs))
	for _, sg := range segs {
		t1 := sg.StartS + sg.DurationS
		rate := 0.0
		if sg.DurationS > 0 {
			rate = (sg.ToC - sg.FromC) / sg.DurationS
		}
		spans = append(spans, span{sg.StartS, t1, sg.FromC, sg.ToC, rate})
	}
	setpoint := func(t float64) float64 {
		if len(spans) == 0 {
			return 0
		}
		if t <= spans[0].t0 {
			return spans[0].t0c
		}
		for _, sp := range spans {
			if t >= sp.t0 && t <= sp.t1 {
				return sp.t0c + sp.rate*(t-sp.t0)
			}
		}
		return spans[len(spans)-1].t1c
	}

	grid := append([]float64(nil), times...)
	if len(grid) == 0 && len(spans) > 0 {
		grid = append(grid, spans[0].t0)
	}
	for _, sp := range spans {
		grid = append(grid, sp.t0, sp.t1)
	}
	for _, v := range payloadValvesX(in.Valves) {
		grid = append(grid, v.TS)
	}
	sort.Float64s(grid)
	grid = uniqueTS(grid, 1e-9)

	pts := make([]TracePoint, len(grid))
	eff := setpoint(grid[0])
	for i, t := range grid {
		sp := setpoint(t)
		var dt float64
		if i > 0 {
			dt = t - grid[i-1]
		}
		alpha := 1 - mathExp(-dt/lag)
		eff = eff + alpha*(sp-eff)
		pts[i] = TracePoint{TS: t, SetpointC: sp, EffectiveC: eff}
	}
	out := FurnaceTrace{Points: pts, TotalS: grid[len(grid)-1]}
	for _, p := range pts {
		if p.SetpointC > out.PeakSetpointC {
			out.PeakSetpointC = p.SetpointC
		}
		if p.EffectiveC > out.PeakEffectiveC {
			out.PeakEffectiveC = p.EffectiveC
		}
	}
	effAt := func(t float64) (float64, float64) {
		i := sort.Search(len(pts), func(i int) bool { return pts[i].TS >= t })
		j := i
		if i > 0 && (i >= len(pts) || pts[i].TS > t) {
			a, b := pts[i-1], pts[i]
			f := (t - a.TS) / (b.TS - a.TS)
			return a.SetpointC + f*(b.SetpointC-a.SetpointC), a.EffectiveC + f*(b.EffectiveC-a.EffectiveC)
		}
		if j >= len(pts) {
			j = len(pts) - 1
		}
		return pts[j].SetpointC, pts[j].EffectiveC
	}
	for _, v := range payloadValvesX(in.Valves) {
		sp, ef := effAt(v.TS)
		out.AtValves = append(out.AtValves, AtValve{ValveTS: v.TS, Gas: v.Gas, SetpointC: sp, EffectiveC: ef})
	}
	return out
}

// ComputeGas shifts valve events by the configured atmosphere transport delay.
// R1: effective switch time != valve switch time; the open delay band is a
// transition where reaction attribution is unknown.
func ComputeGas(in Inputs, totalS float64) GasTimeline {
	evs := payloadValvesX(in.Valves)
	out := GasTimeline{}
	type gpoint struct{ t float64; gas string }
	var valveSeq, effSeq []gpoint
	bounds := []float64{0, totalS}
	for _, e := range evs {
		delay := in.Params.AtmoDelayS[normGas(e.Gas)]
		valveSeq = append(valveSeq, gpoint{e.TS, e.Gas})
		effSeq = append(effSeq, gpoint{e.TS + delay, e.Gas})
		bounds = append(bounds, e.TS, e.TS+delay)
		out.Switches = append(out.Switches, Switch{ValveTS: e.TS, EffectiveTS: e.TS + delay, Gas: e.Gas, DelayS: delay})
	}
	valveAt := func(t float64) string {
		g := "N2"
		for _, v := range valveSeq {
			if v.t <= t {
				g = v.gas
			}
		}
		return g
	}
	effAt := func(t float64) (string, bool) {
		g := "N2"
		found := false
		for _, v := range effSeq {
			if v.t <= t {
				g = v.gas
				found = true
			}
		}
		return g, found
	}
	bounds = uniqueTS(bounds, 1e-9)
	for i := 0; i+1 < len(bounds); i++ {
		mid := (bounds[i] + bounds[i+1]) / 2
		vg := valveAt(mid)
		eg, effFound := effAt(mid)
		seg := GasSegment{FromS: bounds[i], ToS: bounds[i+1], ValveGas: vg, EffectiveGas: eg, Transition: !effFound || vg != eg}
		if seg.Transition {
			seg.DelayS = in.Params.AtmoDelayS[normGas(vg)]
		}
		out.Segments = append(out.Segments, seg)
	}
	return out
}

func normGas(g string) string {
	switch g {
	case "O2", "o2", "oxygen":
		return "O2"
	case "Air", "air":
		return "Air"
	case "N2", "n2", "nitrogen":
		return "N2"
	}
	return g
}

func uniqueTS(ts []float64, eps float64) []float64 {
	sort.Float64s(ts)
	out := ts[:0]
	for _, t := range ts {
		if len(out) == 0 || t-out[len(out)-1] > eps {
			out = append(out, t)
		}
	}
	return out
}
