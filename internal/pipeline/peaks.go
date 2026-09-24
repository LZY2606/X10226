package pipeline

import (
	"fmt"
	"sort"

	"tga/internal/domain"
)

// findPeakGroups detects DTG loss peaks (negative minima) inside each segment.
// Two minima that sit inside the same broad envelope (the valley between them
// does not rise above -shoulderDepth) are kept as one group with two sub-peaks:
// that is how overlapping peaks produce more than one legal stage candidate.
func findPeakGroups(an *Analysis, p domain.AnalysisParams) {
	for _, seg := range an.Segments {
		var minima []int
		for i := seg.PointFrom + 1; i < seg.PointTo; i++ {
			if an.Points[i].DTGMGPM <= an.Points[i-1].DTGMGPM &&
				an.Points[i].DTGMGPM <= an.Points[i+1].DTGMGPM {
				minima = append(minima, i)
			}
		}
		// prominence filter: a minimum must protrude beyond the nearest shoulders.
		var prom []int
		for _, m := range minima {
			v := an.Points[m].DTGMGPM
			left := 0.0
			for j := m - 1; j >= seg.PointFrom; j-- {
				if an.Points[j].DTGMGPM > left {
					left = an.Points[j].DTGMGPM
				}
				if an.Points[j].DTGMGPM > -p.PeakProminenceMGPM {
					break
				}
			}
			prominence := left - v
			if prominence >= p.PeakProminenceMGPM {
				prom = append(prom, m)
			}
		}
		if len(prom) == 0 {
			continue
		}
		// group overlapping minima.
		groups := [][]int{{prom[0]}}
		for k := 1; k < len(prom); k++ {
			prev := prom[k-1]
			cur := prom[k]
			valley := an.Points[prev].DTGMGPM
			for j := prev; j <= cur; j++ {
				if an.Points[j].DTGMGPM > valley {
					valley = an.Points[j].DTGMGPM
				}
			}
			if valley < -p.ShoulderDepthMGPM {
				groups[len(groups)-1] = append(groups[len(groups)-1], cur)
			} else {
				groups = append(groups, []int{cur})
			}
		}
		for gi, g := range groups {
			pg := PeakGroup{ID: hnum("pk", an.AnalysisID, seg.Index, gi), Segment: seg.Index}
			pg.MergedStartS = an.Points[seg.PointFrom].TimeS
			pg.MergedEndS = an.Points[seg.PointTo].TimeS
			if len(g) == 1 {
				m := g[0]
				pg.MergedApexS = an.Points[m].TimeS
				pg.MergedApexDTG = an.Points[m].DTGMGPM
				st, en := envelopeBounds(an, seg, m, p)
				pg.MergedStartS, pg.MergedEndS = an.Points[st].TimeS, an.Points[en].TimeS
				pg.SubPeaks = []SubPeak{{
					ID: hnum("spk", pg.ID, 0),
					ApexS: an.Points[m].TimeS, ApexDTG: an.Points[m].DTGMGPM,
					StartS: pg.MergedStartS, EndS: pg.MergedEndS,
				}}
			} else {
				for si, m := range g {
					pg.SubPeaks = append(pg.SubPeaks, SubPeak{
						ID: hnum("spk", pg.ID, si), IsShoulder: true,
						ApexS: an.Points[m].TimeS, ApexDTG: an.Points[m].DTGMGPM,
					})
				}
				// merged envelope: first onset to last offset, split at mid-valley.
				st, en := envelopeBoundsMulti(an, seg, g[0], g[len(g)-1], p)
				pg.MergedStartS, pg.MergedEndS = an.Points[st].TimeS, an.Points[en].TimeS
				pg.MergedApexS = pg.SubPeaks[0].ApexS
				pg.MergedApexDTG = pg.SubPeaks[0].ApexDTG
				for si := 0; si < len(g); si++ {
					lo, hi := st, en
					if si > 0 {
						lo = valleySplit(an, g[si-1], g[si])
					}
					if si < len(g)-1 {
						hi = valleySplit(an, g[si], g[si+1])
					}
					pg.SubPeaks[si].StartS = an.Points[lo].TimeS
					pg.SubPeaks[si].EndS = an.Points[hi].TimeS
				}
				an.Findings = append(an.Findings, domain.Finding{
					Code: "overlapping_peaks", Severity: "warn",
					Message: fmt.Sprintf("%d overlapping DTG minima between t=%.1fs and %.1fs yield multiple legal stage splits", len(g),
						pg.MergedStartS, pg.MergedEndS),
					StartS: pg.MergedStartS, EndS: pg.MergedEndS, Ref: pg.ID,
				})
			}
			pg.TouchesGap = st_touchesGap(an, pg)
			an.PeakGroups = append(an.PeakGroups, pg)
		}
	}
	sort.SliceStable(an.PeakGroups, func(i, j int) bool { return an.PeakGroups[i].MergedStartS < an.PeakGroups[j].MergedStartS })
}

func envelopeBounds(an *Analysis, seg Segment, m int, p domain.AnalysisParams) (int, int) {
	st, en := m, m
	for j := m; j >= seg.PointFrom; j-- {
		if an.Points[j].DTGMGPM >= -p.PeakProminenceMGPM {
			st = j
			break
		}
		st = j
	}
	for j := m; j <= seg.PointTo; j++ {
		if an.Points[j].DTGMGPM >= -p.PeakProminenceMGPM {
			en = j
			break
		}
		en = j
	}
	return st, en
}

func envelopeBoundsMulti(an *Analysis, seg Segment, m1, m2 int, p domain.AnalysisParams) (int, int) {
	st, _ := envelopeBounds(an, seg, m1, p)
	_, en := envelopeBounds(an, seg, m2, p)
	return st, en
}

// valleySplit returns the least-negative (highest DTG, the saddle) index between
// two adjacent minima, used as the legal boundary between overlapping stages.
func valleySplit(an *Analysis, a, b int) int {
	idx := a
	for j := a; j <= b; j++ {
		if an.Points[j].DTGMGPM > an.Points[idx].DTGMGPM {
			idx = j
		}
	}
	return idx
}

func st_touchesGap(an *Analysis, pg PeakGroup) bool {
	for _, g := range an.Gaps {
		if near(pg.MergedStartS, g.EndS) || near(pg.MergedEndS, g.StartS) {
			return true
		}
	}
	return false
}

func near(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}
