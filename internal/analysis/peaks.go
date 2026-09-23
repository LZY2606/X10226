package analysis

import (
	"fmt"
	"sort"

	"tga-review/internal/domain"
)

// DetectPeaks finds DTG minima (mass loss, dM/dt << 0) per segment and
// groups peaks whose temperature separation is within OverlapMergeC.
// Groups retain both the merged and separated readings as legal options.
func (e *Engine) DetectPeaks(in Input, dtg DTGPayload) PeakPayload {
	var peaks []Peak
	var diags []domain.Diagnostic
	nextPeak := 0
	for _, seg := range dtg.Segments {
		pts := seg.Points
		for i := 1; i < len(pts)-1; i++ {
			if pts[i].MGperMin < pts[i-1].MGperMin && pts[i].MGperMin <= pts[i+1].MGperMin {
				prom := prominence(pts, i)
				if prom < in.Params.PeakPromMGMin {
					diags = append(diags, domain.Diagnostic{Code: "PEAK_BELOW_PROMINENCE", Severity: domain.SevInfo,
						Ref: fmt.Sprintf("%s#%d", seg.ID, pts[i].Seq),
						Message: fmt.Sprintf("minimum at %.0fC prominence %.3f below threshold %.3f; retained as minor, not a stage",
							pts[i].TempC, prom, in.Params.PeakPromMGMin)})
					continue
				}
				peaks = append(peaks, Peak{
					LocalID: fmt.Sprintf("peak-%d", nextPeak), SegmentID: seg.ID,
					TimeS: pts[i].TimeS, TempC: pts[i].TempC, MGperMin: pts[i].MGperMin, Prominence: prom,
				})
				nextPeak++
			}
		}
	}
	sort.Slice(peaks, func(i, j int) bool { return peaks[i].TimeS < peaks[j].TimeS })
	groups := groupPeaks(peaks, in.Params.OverlapMergeC)
	gi := 0
	for _, g := range groups {
		if len(g) > 1 {
			gid := fmt.Sprintf("cluster-%d", gi)
			for k := range g {
				g[k].GroupID = gid
				peaks = replacePeak(peaks, g[k])
			}
			diags = append(diags, domain.Diagnostic{Code: "OVERLAPPING_PEAKS", Severity: domain.SevInfo,
				Ref: gid,
				Message: fmt.Sprintf("%d peaks within %.0fC at %s; merged and separated decompositions both retained",
					len(g), in.Params.OverlapMergeC, tempList(g))})
			gi++
		}
	}
	return PeakPayload{Peaks: peaks, Diagnostics: diags}
}

func replacePeak(all []Peak, p Peak) []Peak {
	for i := range all {
		if all[i].LocalID == p.LocalID {
			all[i] = p
		}
	}
	return all
}

func tempList(g []Peak) string {
	s := ""
	for i, p := range g {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%.0fC", p.TempC)
	}
	return s
}

func prominence(pts []DTGPoint, i int) float64 {
	// depth below the shallower neighboring shoulder (>=0)
	left := pts[i].MGperMin - pts[i-1].MGperMin
	right := pts[i].MGperMin - pts[i+1].MGperMin
	if left < 0 {
		left = 0
	}
	if right < 0 {
		right = 0
	}
	if left < right {
		return left
	}
	return right
}

// groupPeaks clusters consecutive peaks within mergeC temperature units;
// peaks in different segments are never clustered (gap cannot be crossed).
func groupPeaks(peaks []Peak, mergeC float64) [][]Peak {
	var groups [][]Peak
	var cur []Peak
	for _, p := range peaks {
		if len(cur) == 0 {
			cur = []Peak{p}
			continue
		}
		last := cur[len(cur)-1]
		if last.SegmentID == p.SegmentID && absF(p.TempC-last.TempC) <= mergeC {
			cur = append(cur, p)
		} else {
			groups = append(groups, cur)
			cur = []Peak{p}
		}
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	return groups
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
