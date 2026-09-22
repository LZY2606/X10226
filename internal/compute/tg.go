package compute

import (
	"math"
	"sort"

	"tgasys/internal/domain"
	"tgasys/internal/ingest"
)

// buildFurnace produces setpoint and lagged sample-side temperature series.
// Sample temperature is modeled as a first-order lag against the setpoint;
// measured sample temperatures from the samples layer take precedence.
func buildFurnace(steps []domain.FurnaceStep, tauS float64, samples []domain.SamplePoint) []domain.FurnacePoint {
	if len(samples) == 0 {
		return nil
	}
	out := make([]domain.FurnacePoint, 0, len(samples))
	var prevSet, prevSample float64
	haveInit := false
	for idx, sp := range samples {
		set := setpointAt(steps, sp.T)
		var ts float64
		lagged := false
		if !sp.TS.Unconvertible && sp.TS.Unit != "" {
			ts = sp.TS.Value
		} else {
			if !haveInit {
				prevSet = set
				prevSample = set
				haveInit = true
			}
			dt := sp.T
			if idx > 0 {
				dt = sp.T - samples[idx-1].T
			}
			alpha := 1 - math.Exp(-math.Max(dt, 0)/math.Max(tauS, 1e-6))
			prevSample += alpha * (prevSet - prevSample)
			prevSet = set
			ts = prevSample
			lagged = true
		}
		out = append(out, domain.FurnacePoint{T: sp.T, Setpoint: set, Sample: ts, Lagged: lagged})
	}
	return out
}

func setpointAt(steps []domain.FurnaceStep, t float64) float64 {
	set := 25.0
	if len(steps) == 0 {
		return set
	}
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].FromT < steps[j].FromT })
	cur := steps[0].Target.Value
	for _, st := range steps {
		if t < st.FromT {
			break
		}
		if t <= st.ToT || st.ToT == st.FromT {
			frac := 0.0
			if st.ToT > st.FromT {
				frac = (t - st.FromT) / (st.ToT - st.FromT)
			}
			// interpolate from previous setpoint up to target
			cur = cur + frac*(st.Target.Value-cur)
			set = cur
			if t >= st.ToT {
				cur = st.Target.Value
			}
			return set
		}
		cur = st.Target.Value
	}
	return cur
}

// buildAtmosphere turns valve events into segments carrying both valve time
// and estimated sample-reaction time (valve + atmosphere transport delay).
func buildAtmosphere(gas []domain.GasEvent, delayS float64, furnace []domain.FurnacePoint) ([]domain.AtmSegment, []domain.Finding) {
	var findings []domain.Finding
	if len(gas) == 0 {
		return nil, findings
	}
	segs := make([]domain.AtmSegment, 0, len(gas))
	for i, g := range gas {
		valveEnd := math.Inf(1)
		if i+1 < len(gas) {
			valveEnd = gas[i+1].T
		}
		// StartT/EndT describe when gas is actually present at the sample
		// (valve time shifted by transport delay); ValveT stays raw.
		segs = append(segs, domain.AtmSegment{
			StartT:    g.T + delayS,
			EndT:      valveEnd + delayS,
			Gas:       gasName(g),
			ValveT:    g.T,
			ReactionT: g.T + delayS,
			DelayS:    delayS,
		})
	}
	findings = append(findings, domain.Finding{
		Code:     "atmosphere.delay",
		Severity: "info",
		Message:  "valve switch time is not sample reaction time; reaction offset = atmosphere delay (+ thermal lag at peak)",
		Data: map[string]any{
			"delay_s": delayS,
			"rule":    "reaction_t = valve_t + atm_delay_s; peak staging uses lagged sample temperature",
		},
	})
	return segs, findings
}

func gasName(g domain.GasEvent) string {
	switch g.Action {
	case "off":
		return "OFF"
	case "switch":
		return g.Gas
	default:
		return g.Gas
	}
}

// buildTG creates percentage mass series. Points adjacent to gaps are marked
// and no synthetic points are inserted between them.
func buildTG(pts []domain.SamplePoint, m0 float64, gaps []domain.Gap) []domain.TGPoint {
	out := make([]domain.TGPoint, len(pts))
	for i, sp := range pts {
		pct := 100 * sp.M.Value / m0
		out[i] = domain.TGPoint{
			T: sp.T, Pct: pct, Mass: sp.M.Value,
			InGap:  inGap(sp.T, gaps),
			Sparse: nearGapEdge(sp.T, gaps),
		}
	}
	return out
}

// buildDTG differentiates TG with a centered window and skips any window that
// straddles a gap (the gap is not bridged).
func buildDTG(tg []domain.TGPoint, gaps []domain.Gap, windowS float64) []domain.DTGPoint {
	out := make([]domain.DTGPoint, 0, len(tg))
	for i := range tg {
		t := tg[i].T
		l, r := i, i
		for l > 0 && t-tg[l-1].T <= windowS/2 {
			l--
		}
		for r < len(tg)-1 && tg[r+1].T-t <= windowS/2 {
			r++
		}
		straddle := false
		for k := l; k < r; k++ {
			if spansGap(tg[k].T, tg[k+1].T, gaps) {
				straddle = true
			}
		}
		if straddle || r <= l {
			out = append(out, domain.DTGPoint{T: t, Value: math.NaN(), InGap: true})
			continue
		}
		dtMin := (tg[r].T - tg[l].T) / 60
		if dtMin <= 0 {
			out = append(out, domain.DTGPoint{T: t, Value: math.NaN(), InGap: true})
			continue
		}
		v := (tg[r].Pct - tg[l].Pct) / dtMin
		out = append(out, domain.DTGPoint{T: t, Value: v, InGap: inGap(t, gaps)})
	}
	return out
}

func spansGap(a, b float64, gaps []domain.Gap) bool {
	for _, g := range gaps {
		if a <= g.StartT+1e-9 && b >= g.EndT-1e-9 {
			return true
		}
	}
	return false
}
