package calc

import (
	"math"
	"sort"
	"strconv"
)

type peakPt struct {
	rate float64 // smoothed loss rate in %/min (negative during loss)
	t    float64
	cidx int
}

// ComputeStages detects loss-rate peaks per connected corrected region.
//
// R8 overlap peaks: when a thresholded component contains several local
// maxima, BOTH a "merged" proposal and "part_a/part_b" split proposals are
// emitted; reviewers adjudicate instead of the algorithm picking one.
//
// R4 gaps: areas are trapezoid integrals of the finite-difference rate over
// consecutive usable points of one region. A missing point inside a window
// bounds the area; nothing is interpolated across a gap, and the candidate is
// marked unknown.
func ComputeStages(in Inputs, corr Corrected, trace FurnaceTrace, gas GasTimeline, dtg DTG) Stages {
	regions := map[int][]peakPt{}
	for i, p := range dtg.Points {
		if !p.Valid || p.SmoothPctMin >= -in.Params.DTGThreshold {
			continue
		}
		regions[p.Region] = append(regions[p.Region], peakPt{rate: p.SmoothPctMin, t: p.TS, cidx: i})
	}
	var regionIDs []int
	for r := range regions {
		regionIDs = append(regionIDs, r)
	}
	sort.Ints(regionIDs)

	effAt := traceInterp(trace)
	gasAt := func(t float64) string {
		for _, s := range gas.Segments {
			if t >= s.FromS && t < s.ToS {
				if s.Transition {
					return "transition"
				}
				return s.EffectiveGas
			}
		}
		return "unknown"
	}

	centralRate := func(i int) (float64, bool) {
		if i <= 0 || i >= len(corr.Points)-1 {
			return 0, false
		}
		cp := corr.Points[i]
		p := corr.Points[i-1]
		n := corr.Points[i+1]
		if !cp.Usable || !p.Usable || !n.Usable || p.Region != cp.Region || n.Region != cp.Region {
			return 0, false
		}
		dt := n.TS - p.TS
		if dt <= 0 {
			return 0, false
		}
		return (n.CorrMg - p.CorrMg) / dt, true
	}

	integrate := func(region int, a, b float64) (float64, bool) {
		area := 0.0
		bounded := false
		lastIdx := -1
		for i := range corr.Points {
			cp := &corr.Points[i]
			if !cp.Usable || cp.Region != region || cp.TS < a || cp.TS > b {
				continue
			}
			if lastIdx >= 0 && i != lastIdx+1 {
				bounded = true
				break
			}
			if lastIdx >= 0 {
				r0, ok0 := centralRate(lastIdx)
				r1, ok1 := centralRate(i)
				if !ok0 || !ok1 {
					bounded = true
					break
				}
				dt := cp.TS - corr.Points[lastIdx].TS
				area += 0.5 * (-r0 + -r1) * dt
			}
			lastIdx = i
		}
		if math.IsNaN(area) {
			bounded = true
		}
		return area, bounded
	}

	clusterNo := 0
	var cands []StageCandidate
	for _, r := range regionIDs {
		rp := regions[r]
		var comps [][]peakPt
		start := 0
		for i := 1; i <= len(rp); i++ {
			if i == len(rp) || rp[i].cidx-rp[i-1].cidx > 1 {
				comps = append(comps, rp[start:i])
				start = i
			}
		}
		for _, comp := range comps {
			maxima := localMaxima(comp)
			clusterNo++
			windowA := comp[0].t
			windowB := comp[len(comp)-1].t

			makeCandidate := func(proposal string, a, peak, b float64) StageCandidate {
				area, bounded := integrate(r, a, b)
				_, peakEff := effAt(peak)
				atmo := gasAt(peak)
				c := StageCandidate{
					Key:      "s" + strconv.Itoa(clusterNo) + "_" + proposal,
					Proposal: proposal, Cluster: clusterNo,
					StartS: a, PeakS: peak, EndS: b,
					PeakPctMin: rateAt(dtg, peak), PeakTempC: peakEff,
					AreaMg: area, BoundedByGap: bounded, Status: "ok",
					Atmosphere: atmo,
				}
				if dtg.M0Mg > 0 {
					c.AreaPct = area / dtg.M0Mg * 100
				}
				if atmo == "transition" {
					c.AtmosphereNote = "peak inside valve->sample delay band; attribution unknown"
				}
				if bounded {
					c.Status = "unknown"
				}
				return c
			}

			if len(maxima) == 1 {
				cands = append(cands, makeCandidate("single", windowA, comp[maxima[0]].t, windowB))
				continue
			}
			best := maxima[0]
			for _, mi := range maxima {
				if comp[mi].rate < comp[best].rate {
					best = mi
				}
			}
			cands = append(cands, makeCandidate("merged", windowA, comp[best].t, windowB))
			suffix := []string{"a", "b", "c"}
			for k, mi := range maxima {
				a, b := windowA, windowB
				if k > 0 {
					a = saddleTime(comp, maxima[k-1], mi)
				}
				if k < len(maxima)-1 {
					b = saddleTime(comp, mi, maxima[k+1])
				}
				cands = append(cands, makeCandidate("part_"+suffix[k], a, comp[mi].t, b))
			}
		}
	}
	return Stages{Candidates: cands}
}

// localMaxima returns plateau-aware strict local maximum positions.
func localMaxima(comp []peakPt) []int {
	var out []int
	for i := range comp {
		isMax := true
		if i > 0 && comp[i-1].rate < comp[i].rate {
			isMax = false
		}
		if i < len(comp)-1 && comp[i+1].rate < comp[i].rate {
			isMax = false
		}
		if isMax {
			if len(out) > 0 && i-out[len(out)-1] == 1 && comp[i].rate <= comp[out[len(out)-1]].rate {
				continue
			}
			out = append(out, i)
		}
	}
	return out
}

// saddleTime is the shallowest valley point (rate closest to zero) between
// two maxima: the natural split boundary for overlap proposals.
func saddleTime(comp []peakPt, i, j int) float64 {
	best := comp[i+1]
	for k := i + 1; k < j; k++ {
		if comp[k].rate > best.rate {
			best = comp[k]
		}
	}
	return best.t
}

func rateAt(d DTG, t float64) float64 {
	for _, p := range d.Points {
		if p.TS == t {
			return p.SmoothPctMin
		}
	}
	return 0
}
