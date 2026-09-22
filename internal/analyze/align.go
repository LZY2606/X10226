package analyze

import (
	"sort"

	"tga-review/internal/model"
)

// alignFurnace copies furnace segments onto the normalized time axis.
func alignFurnace(r *RunResult, in Inputs) {
	for _, f := range in.Furnace {
		if f.TimeS == nil || f.TempS == nil {
			continue
		}
		r.FurnaceTemp = append(r.FurnaceTemp, Anchor{
			TimeMin: *f.TimeS, TempC: *f.TempS, Kind: f.Kind, SourceSeq: f.Seq,
		})
	}
	sort.Slice(r.FurnaceTemp, func(i, j int) bool { return r.FurnaceTemp[i].TimeMin < r.FurnaceTemp[j].TimeMin })
}

// alignGas turns each commanded valve event into a sample-reaction-time anchor.
// reaction = valve + atmosphere delay + temperature lag offset. The valve
// switch therefore never coincides with a reaction by construction.
func alignGas(r *RunResult, in Inputs, p Params) {
	for _, g := range in.Gas {
		if g.TimeS == nil {
			continue
		}
		r.GasReaction = append(r.GasReaction, GasAnchor{
			ValveTimeMin:    *g.TimeS,
			ReactionTimeMin: *g.TimeS + p.AtmosphereDelayMin + p.TempLagOffsetMin,
			Gas:             g.Gas,
			Action:          g.Action,
			DelayMin:        p.AtmosphereDelayMin,
			TempLagMin:      p.TempLagOffsetMin,
			SourceSeq:       g.Seq,
		})
	}
	sort.Slice(r.GasReaction, func(i, j int) bool {
		return r.GasReaction[i].ValveTimeMin < r.GasReaction[j].ValveTimeMin
	})
	if len(r.GasReaction) > 0 {
		r.Findings = append(r.Findings, Finding{
			Code: CodeAtmosphereDelay, Severity: "info", Subject: "gas_timeline",
			Message: "valve switch times shifted by atmosphere delay and temperature lag; reaction time is not valve time",
			Detail: map[string]interface{}{
				"atmosphere_delay_min": p.AtmosphereDelayMin,
				"temp_lag_min":        p.TempLagOffsetMin,
				"events":              len(r.GasReaction),
			},
		})
	}
}

// gasAt reports the atmosphere estimated at reaction time t ("" if none).
func (r *RunResult) gasAt(t float64) string {
	cur := ""
	for _, g := range r.GasReaction {
		if g.ReactionTimeMin <= t {
			switch g.Action {
			case "on", "switch":
				cur = g.Gas
			case "off":
				cur = ""
			}
		}
	}
	return cur
}

// nearestGas returns the gas anchor whose reaction time is closest to t.
func (r *RunResult) nearestGas(t float64) *GasAnchor {
	var best *GasAnchor
	bestD := 0.0
	for i := range r.GasReaction {
		g := &r.GasReaction[i]
		d := t - g.ReactionTimeMin
		if d < 0 {
			d = -d
		}
		if best == nil || d < bestD {
			best, bestD = g, d
		}
	}
	return best
}

// tempAt interpolates the furnace program at t (used for stage peak temperature).
func (r *RunResult) tempAt(t float64) (float64, bool) {
	if len(r.FurnaceTemp) == 0 {
		// fall back to measured sample temperature
		for _, p := range r.Points {
			if p.TimeMin == t {
				return p.TempC, true
			}
		}
		return 0, false
	}
	xs := make([]float64, len(r.FurnaceTemp))
	ys := make([]float64, len(r.FurnaceTemp))
	for i, a := range r.FurnaceTemp {
		xs[i], ys[i] = a.TimeMin, a.TempC
	}
	return interp(xs, ys, t), true
}

var _ = model.KindGas
