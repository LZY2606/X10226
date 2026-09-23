package tga

import (
	"math"
	"sort"
)

// detectPeaks finds DTG minima per gap-delimited segment, then groups peaks
// whose separation is within the overlap window as an overlapping cluster.
func detectPeaks(rep *Report, p Params) {
	if len(rep.Dtg) == 0 {
		return
	}
	gapEnds := map[float64]bool{}
	for _, g := range rep.Gaps {
		gapEnds[g.FromS] = true
	}
	segStart := 0
	for i := range rep.Dtg {
		end := i == len(rep.Dtg)-1 || gapEnds[rep.Dtg[i].TimeS]
		if !end {
			continue
		}
		seg := rep.Dtg[segStart : i+1]
		segStart = i + 1
		peaksIn := findMinima(seg, p)
		groups := groupOverlap(peaksIn, p.OverlapWindowS)
		groupOf := map[string]string{}
		for _, g := range groups {
			if len(g) < 2 {
				continue
			}
			times := []float64{}
			for _, q := range g {
				times = append(times, q.TimeS)
			}
			gid := HashID("pgroup", map[string]any{"peaks": times})
			for _, q := range g {
				groupOf[fmtKey(q.TimeS)] = gid
			}
		}
		for _, pk := range peaksIn {
			rep.Peaks = append(rep.Peaks, Peak{
				ID:      HashID("peak", map[string]any{"t": pk.TimeS, "dtg": pk.Dtg}),
				TimeS:   pk.TimeS,
				Dtg:     pk.Dtg,
				PromPct: pk.PromPct,
				GroupID: groupOf[fmtKey(pk.TimeS)],
			})
		}
	}
	sort.SliceStable(rep.Peaks, func(i, j int) bool { return rep.Peaks[i].TimeS < rep.Peaks[j].TimeS })
}

func fmtKey(v float64) string { return fmtF(v) }

type fPeak struct {
	TimeS, Dtg, PromPct float64
}

// findMinima returns local minima with depth at least PeakMinPromPct of the
// segment's deepest minimum (default 2%).
func findMinima(d []DtgPoint, p Params) []fPeak {
	if len(d) < 3 {
		return nil
	}
	minV := 0.0
	for _, q := range d {
		if q.Dtg < minV {
			minV = q.Dtg
		}
	}
	if minV >= 0 {
		return nil
	}
	threshold := p.PeakMinPromPct * minV // both negative
	var out []fPeak
	for i := 1; i < len(d)-1; i++ {
		if d[i].Dtg <= d[i-1].Dtg && d[i].Dtg <= d[i+1].Dtg && d[i].Dtg <= threshold {
			out = append(out, fPeak{TimeS: d[i].TimeS, Dtg: d[i].Dtg, PromPct: -d[i].Dtg})
		}
	}
	merged := []fPeak{}
	for _, pk := range out {
		if len(merged) > 0 && math.Abs(pk.TimeS-merged[len(merged)-1].TimeS) < 1e-9 {
			if pk.Dtg < merged[len(merged)-1].Dtg {
				merged[len(merged)-1] = pk
			}
			continue
		}
		merged = append(merged, pk)
	}
	return merged
}

// groupOverlap clusters peaks transitively by proximity window.
func groupOverlap(peaks []fPeak, win float64) [][]fPeak {
	groups := [][]fPeak{}
	for _, pk := range peaks {
		placed := false
		for gi := range groups {
			for _, q := range groups[gi] {
				if math.Abs(pk.TimeS-q.TimeS) <= win {
					groups[gi] = append(groups[gi], pk)
					placed = true
					break
				}
			}
			if placed {
				break
			}
		}
		if !placed {
			groups = append(groups, []fPeak{pk})
		}
	}
	return groups
}
