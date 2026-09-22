package pipeline

import (
	"fmt"
	"math"
	"sort"

	"tga-review/internal/domain"
)

// detectStages finds DTG minima (loss-rate peaks). Overlapping peaks are
// kept as separate, possibly overlapping stages; spike-driven events are
// emitted as rejected noise candidates.
func detectStages(pts []domain.SamplePoint, dtg []DTGPoint, stemp map[int]float64,
	atm []domain.AtmWindow, p domain.Params) ([]domain.Stage, []domain.Unknown) {

	var totalLoss float64
	if len(pts) > 1 {
		m0 := pts[0].Mass
		mend := pts[len(pts)-1].Mass
		totalLoss = math.Max(0, m0-mend)
	}
	promAbs := math.Max(0.02, p.ProminencePct/100.0*totalLoss) // mg/min-ish scale
	_ = promAbs

	// locate local minima of rate (most negative) on contiguous segments
	type min struct{ idx int }
	var mins []min
	for i := 2; i < len(dtg)-2; i++ {
		r := dtg[i].Rate
		if isNaN(r) || pts[i].Status != domain.RowOK {
			continue
		}
		if r < dtg[i-1].Rate && r < dtg[i+1].Rate && r < -0.02 {
			mins = append(mins, min{i})
		}
	}
	// merge minima closer than half smoothing window into an overlap group
	sort.Slice(mins, func(a, b int) bool { return mins[a].idx < mins[b].idx })
	groups := groupMinima(mins, dtg, pts, math.Max(120, p.SmoothWindowS*1.5))

	stages := []domain.Stage{}
	unknowns := []domain.Unknown{}
	idx := 0
	for gi, g := range groups {
		members := []int{}
		for _, m := range g {
			members = append(members, m.idx)
		}
		// boundaries: nearest local maxima in rate around the group
		lb, rb := groupBounds(members, dtg, pts)
		for _, m := range g {
			idx++
			peak := pts[m.idx]
			st := domain.Stage{
				Index: idx,
				UID:   stageUID(pts[lb].T, peak.T, pts[rb].T, gi),
				StartT: pts[lb].T,
				EndT:   pts[rb].T,
				PeakT:  peak.T,
				PeakSampleT: stemp[m.idx],
				StartMass: pts[lb].Mass,
				EndMass:   pts[rb].Mass,
				Atmosphere: gasAt(atm, peak.T),
				Status:    "candidate",
			}
			st.LossMg = math.Max(0, st.StartMass-st.EndMass)
			if pts[0].Mass > 0 {
				st.LossPctOfM0 = st.LossMg / pts[0].Mass * 100
			}
			for _, w := range atm {
				if peak.T >= w.StartT && peak.T < w.EndT {
					st.ValveT = w.ValveT
				}
			}
			if peak.Spike {
				st.NoiseSpike = true
				st.Status = "candidate"
				st.Diag = "peak anchored on non-monotonic noise spike; kept as candidate with reject_reason=noise_spike"
			}
			if len(g) > 1 {
				st.OverlapsGroup = groupPeakTimes(g, pts)
			}
			stages = append(stages, st)
		}
		if len(g) > 1 {
			unknowns = append(unknowns, domain.Unknown{
				Path:   fmt.Sprintf("overlap_group.%d", gi),
				Reason: "multiple DTG minima inside one unresolved envelope; stage assignment is non-unique and boundaries overlap",
				Options: func() []string {
					opts := []string{}
					for _, m := range g {
						opts = append(opts, fmt.Sprintf("peak@%.0fs", pts[m.idx].T))
					}
					return opts
				}(),
				Affects: func() []string {
					af := []string{}
					for _, m := range g {
						af = append(af, stageUID(pts[lb].T, pts[m.idx].T, pts[rb].T, gi))
					}
					return af
				}(),
			})
		}
	}
	// standalone spikes far from any real peak: noise-only candidates
	for i := range pts {
		if !pts[i].Spike {
			continue
		}
		anchorsReal := false
		for _, st := range stages {
			if pts[i].T >= st.StartT && pts[i].T <= st.EndT && !st.NoiseSpike {
				anchorsReal = true
			}
		}
		if !anchorsReal {
			idx++
			lb, rb := spikeBounds(i, dtg, pts)
			stages = append(stages, domain.Stage{
				Index: idx, UID: stageUID(pts[lb].T, pts[i].T, pts[rb].T, 9000+i),
				StartT: pts[lb].T, EndT: pts[rb].T, PeakT: pts[i].T,
				PeakSampleT: stemp[i], StartMass: pts[lb].Mass, EndMass: pts[rb].Mass,
				Atmosphere: gasAt(atm, pts[i].T), NoiseSpike: true, Status: "candidate",
				Diag: "reject_reason=noise_spike: isolated non-monotonic upward point",
			})
		}
	}
	sort.SliceStable(stages, func(a, b int) bool { return stages[a].PeakT < stages[b].PeakT })
	for i := range stages {
		stages[i].Index = i + 1
	}
	return stages, unknowns
}

func groupPeakTimes(g []min, pts []domain.SamplePoint) []int {
	out := []int{}
	for _, m := range g {
		out = append(out, int(pts[m.idx].T))
	}
	return out
}

// groupMinima clusters minima that are close in time.
func groupMinima(mins []min, dtg []DTGPoint, pts []domain.SamplePoint, minGapS float64) [][]min {
	groups := [][]min{}
	for _, m := range mins {
		if len(groups) == 0 {
			groups = append(groups, []min{m})
			continue
		}
		last := groups[len(groups)-1]
		if pts[m.idx].T-pts[last[len(last)-1].idx].T < minGapS {
			groups[len(groups)-1] = append(last, m)
		} else {
			groups = append(groups, []min{m})
		}
	}
	return groups
}

// groupBounds finds the shoulders (rate maxima / zero crossings) around a
// group of minima, never crossing a sampling gap.
func groupBounds(members []int, dtg []DTGPoint, pts []domain.SamplePoint) (int, int) {
	l := members[0]
	r := members[len(members)-1]
	lb := l
	for lb > 0 && !pts[lb].GapBefore && dtg[lb-1].Rate < dtg[lb].Rate && dtg[lb].Rate < 0 {
		lb--
	}
	rb := r
	for rb < len(dtg)-1 && !pts[rb].GapAfter && dtg[rb+1].Rate < dtg[rb].Rate && dtg[rb].Rate < 0 {
		rb++
	}
	if lb == l {
		lb = maxIdx(0, l-2)
	}
	if rb == r {
		rb = minIdx(len(pts)-1, r+2)
	}
	return lb, rb
}

func spikeBounds(i int, dtg []DTGPoint, pts []domain.SamplePoint) (int, int) {
	lb := maxIdx(0, i-2)
	rb := minIdx(len(pts)-1, i+2)
	return lb, rb
}

func stageUID(start, peak, end float64, gi int) string {
	return domain.StableID("st", round1(start), round1(peak), round1(end), gi)
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

func maxIdx(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func minIdx(a, b int) int {
	if a < b {
		return a
	}
	return b
}
