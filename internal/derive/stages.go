package derive

import (
	"fmt"
	"sort"
	"strings"

	"tga-review/internal/domain"
)

type pk struct {
	idx       int // index within segment's dtg slice
	segPtsIdx int
}

func (e *Engine) runStages(cn *domain.CorrectNode, dn *domain.DTGNode) *domain.StagesNode {
	p := e.params
	sn := &domain.StagesNode{Params: p}

	// Gas windows: valve switch time != sample reaction time.
	if gas := e.optional(domain.KindGas); gas != nil {
		for _, g := range gas.Payload.Gas {
			if g.Time.Canon == nil {
				continue
			}
			valve := *g.Time.Canon
			reach := valve + p.AtmDelayS
			tok := strings.TrimSpace(g.Gas.Raw)
			sn.Windows = append(sn.Windows, domain.GasWindow{
				EventIdx: g.Index, ValveTimeS: round2(valve), ReachTimeS: round2(reach), Gas: tok,
			})
		}
	}
	sort.SliceStable(sn.Windows, func(i, j int) bool { return sn.Windows[i].ReachTimeS < sn.Windows[j].ReachTimeS })

	// group DTG points by segment
	bySeg := map[string][]domain.DTGPoint{}
	for _, d := range dn.Points {
		bySeg[d.SegID] = append(bySeg[d.SegID], d)
	}
	segOrder := []string{}
	for _, s := range cn.Segs {
		if _, ok := bySeg[s.ID]; ok {
			segOrder = append(segOrder, s.ID)
		}
	}

	altCounter := 0
	for _, segID := range segOrder {
		pts := bySeg[segID]
		// find local minima (mass loss peaks are negative DTG)
		type peak struct{ i int }
		var peaks []int
		for i := 1; i < len(pts)-1; i++ {
			if pts[i].Smoothed < pts[i-1].Smoothed && pts[i].Smoothed < pts[i+1].Smoothed {
				peaks = append(peaks, i)
			}
		}
		// prominence filter on absolute percent rate
		kept := []int{}
		for _, pi := range peaks {
			prom := -pts[pi].Smoothed
			if dn.M0Mg != nil && *dn.M0Mg != 0 {
				prom = -pts[pi].DtgPctMin
			}
			if prom < p.PeakPromPct {
				sn.Rejections = append(sn.Rejections, domain.StageCandidate{
					PeakIdx: pts[pi].Idx, PeakTimeS: pts[pi].TimeS, PeakTempC: pts[pi].SampleC,
					DtgPeak: pts[pi].Smoothed, SegID: segID,
					Status: "rejected", RejectReason: fmt.Sprintf("PROMINENCE_LOW depth %.4f < %.4f", prom, p.PeakPromPct),
					Confidence: "high",
				})
				continue
			}
			kept = append(kept, pi)
		}
		// enforce min distance: keep stronger peak
		kept = separatePeaks(kept, pts, p.PeakMinDistS)

		if len(kept) == 0 {
			continue
		}

		// build groups: peaks close together with a shallow valley are an
		// overlap cluster producing multiple legal decomposition alternatives.
		groups := groupOverlaps(kept, pts, p)
		for gi, g := range groups {
			groupID := fmt.Sprintf("ovl-%s-%d", segID, gi+1)
			if len(g) == 1 {
				altCounter++
				c := e.makeCandidate(cn, dn, pts, g[0], g[0], g, segID, 1, 0, "")
				sn.Candidates = append(sn.Candidates, c)
				continue
			}
			// Alternative 1: one merged stage across the cluster.
			altCounter++
			merged := e.makeCandidate(cn, dn, pts, g[0], g[len(g)-1], g, segID, 1, altCounter, groupID)
			merged.Evidence = append(merged.Evidence, fmt.Sprintf("alt %d: merged overlap cluster (%d peaks)", 1, len(g)))
			merged.Confidence = "medium"
			sn.Candidates = append(sn.Candidates, merged)
			// Alternative 2: resolved into separate stages per peak.
			for k, pi := range g {
				altCounter++
				sc := e.makeCandidate(cn, dn, pts, pi, pi, g, segID, 2, altCounter, groupID)
				sc.Evidence = append(sc.Evidence, fmt.Sprintf("alt %d: resolved peak %d/%d of overlap cluster", 2, k+1, len(g)))
				sc.Confidence = "low"
				sc.Status = "unknown"
				sc.UnknownReason = "OVERLAP_NOT_UNIQUE: merged vs resolved decomposition both legal"
				sn.Candidates = append(sn.Candidates, sc)
			}
		}
	}

	// attribute gas by reach time (delay-aware), never by valve time
	for i := range sn.Candidates {
		c := &sn.Candidates[i]
		gas, note := attributeGas(sn.Windows, c.PeakTimeS, p.AtmDelayS)
		c.AttributedGas = gas
		c.GasNote = note
	}

	sn.Diags = append(sn.Diags, domain.Diagnostic{Code: "CANDIDATES", Msg: fmt.Sprintf("%d candidate(s), %d rejected; overlapping stages allowed, no 100%% sum constraint", len(sn.Candidates), len(sn.Rejections))})
	return sn
}

func separatePeaks(peaks []int, pts []domain.DTGPoint, minDist float64) []int {
	out := []int{}
	for _, pi := range peaks {
		if len(out) == 0 || pts[pi].TimeS-pts[out[len(out)-1]].TimeS >= minDist {
			out = append(out, pi)
		} else {
			// keep the deeper one
			if -pts[pi].Smoothed > -pts[out[len(out)-1]].Smoothed {
				out[len(out)-1] = pi
			}
		}
	}
	return out
}

func groupOverlaps(peaks []int, pts []domain.DTGPoint, p domain.Params) [][]int {
	groups := [][]int{}
	cur := []int{peaks[0]}
	for k := 1; k < len(peaks); k++ {
		a, b := peaks[k-1], peaks[k]
		// valley between
		valley := 0.0
		for j := a; j <= b; j++ {
			if pts[j].Smoothed > valley {
				valley = pts[j].Smoothed
			}
		}
		depthA, depthB := -pts[a].Smoothed, -pts[b].Smoothed
		shallow := valley > -minF(depthA, depthB)*p.OverlapSepPct
		close := pts[b].TimeS-pts[a].TimeS < p.PeakMinDistS*2.5
		if shallow && close {
			cur = append(cur, b)
		} else {
			groups = append(groups, cur)
			cur = []int{b}
		}
	}
	groups = append(groups, cur)
	return groups
}

func (e *Engine) makeCandidate(cn *domain.CorrectNode, dn *domain.DTGNode, pts []domain.DTGPoint, firstPeak, lastPeak int, cluster []int, segID string, alt, altN int, group string) domain.StageCandidate {
	// boundaries: from half-prominence crossing around the peaks
	startI := boundaryBefore(pts, firstPeak)
	endI := boundaryAfter(pts, lastPeak)
	startP, endP := pts[startI], pts[endI]
	peakP := pts[firstPeak]
	if lastPeak != firstPeak {
		peakP = pts[firstPeak]
	}

	mStart := cn.Points[startP.Idx-1].CorrectedMg
	mEnd := cn.Points[endP.Idx-1].CorrectedMg
	loss := round4(*mStart - *mEnd)

	c := domain.StageCandidate{
		Alternative: alt,
		Group:       group,
		PeakIdx:     peakP.Idx,
		PeakTimeS:   round2(peakP.TimeS),
		PeakTempC:   round2(peakP.SampleC),
		DtgPeak:     peakP.Smoothed,
		StartS:      round2(startP.TimeS),
		EndS:        round2(endP.TimeS),
		StartC:      round2(startP.SampleC),
		EndC:        round2(endP.SampleC),
		StartIdx:    startP.Idx,
		EndIdx:      endP.Idx,
		MassLossMg:  loss,
		SegID:       segID,
		Status:      "proposed",
		Confidence:  "high",
	}
	if dn.M0Mg != nil && *dn.M0Mg != 0 {
		pct := round4(loss / *dn.M0Mg * 100)
		c.MassLossPct = &pct
	}
	c.Evidence = []string{
		fmt.Sprintf("peak idx=%d t=%.2fs T=%.2fC smoothedDTG=%.4f", peakP.Idx, c.PeakTimeS, c.PeakTempC, c.DtgPeak),
		fmt.Sprintf("boundaries by half-prominence: [%.2fs..%.2fs] within %s (gaps excluded)", c.StartS, c.EndS, segID),
		fmt.Sprintf("mass loss = m(start) - m(end) = %.4f - %.4f = %.4f mg", *mStart, *mEnd, loss),
	}
	// unknown when a cluster gap/noise sits inside the window
	for _, nf := range cn.Noise {
		if nf.TimeS >= c.StartS && nf.TimeS <= c.EndS {
			c.Status = "unknown"
			c.UnknownReason = "NONMONOTONIC_NOISE_IN_WINDOW"
			c.Confidence = "low"
			c.Evidence = append(c.Evidence, fmt.Sprintf("non-monotonic excursion at %.2fs inside window", nf.TimeS))
		}
	}
	for _, g := range cn.Gaps {
		if g.TBefore >= c.StartS && g.TAfter <= c.EndS {
			c.Status = "unknown"
			c.UnknownReason = "GAP_IN_WINDOW_NO_INTERPOLATION"
			c.Confidence = "low"
			c.Evidence = append(c.Evidence, fmt.Sprintf("sampling gap %.2f..%.2fs crosses window; no interpolation", g.TBefore, g.TAfter))
		}
	}
	return c
}

func boundaryBefore(pts []domain.DTGPoint, peakI int) int {
	base := pts[0].Smoothed
	target := base + (pts[peakI].Smoothed-base)*0.5
	i := peakI
	for i > 0 && pts[i-1].Smoothed < target {
		i--
	}
	return i
}

func boundaryAfter(pts []domain.DTGPoint, peakI int) int {
	base := pts[len(pts)-1].Smoothed
	target := base + (pts[peakI].Smoothed-base)*0.5
	i := peakI
	for i < len(pts)-1 && pts[i+1].Smoothed < target {
		i++
	}
	return i
}

func attributeGas(windows []domain.GasWindow, peakTime, delay float64) (string, string) {
	if len(windows) == 0 {
		return "", "no gas events imported"
	}
	// the gas active at the sample at peak time is the last window whose
	// delayed reach time is <= peak time
	active := windows[0].Gas
	for _, w := range windows {
		if w.ReachTimeS <= peakTime {
			active = w.Gas
		}
	}
	return active, fmt.Sprintf("assigned by delay-aware reach time (valve+%.0fs), not valve switch time", delay)
}
