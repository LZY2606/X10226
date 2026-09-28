package tga

import (
	"math"
	"sort"
)

// buildStages detects DTG peaks and enumerates *all legal* decomposition
// hypotheses. Overlapping peaks (a shoulder inside a broad peak) therefore
// produce both a "merged" and "shoulder/sub" candidates rather than a single
// forced answer. Candidates that cross a sampling gap are kept but rejected
// with a structured reason.
func buildStages(tg *TGResult, series *NormSeries, gas *NormGas, p Params) *StageResult {
	res := &StageResult{Algorithm: AlgoStages, ParamFP: p.fingerprint(), Sources: tg.Sources}
	idxBySeg := map[string][]*DTGPoint{}
	for i := range tg.Points {
		pt := &tg.Points[i]
		idxBySeg[pt.Segment] = append(idxBySeg[pt.Segment], pt)
	}
	segOrder := make([]string, 0, len(series.Segments))
	for _, s := range series.Segments {
		segOrder = append(segOrder, s.ID)
	}

	for segN, segID := range segOrder {
		pts := idxBySeg[segID]
		if len(pts) < 3 {
			continue
		}
		// 1) Prominence-based local maxima of the positive DTG.
		var peaks []*Peak
		for i := 1; i < len(pts)-1; i++ {
			if pts[i].DTG >= pts[i-1].DTG && pts[i].DTG > pts[i+1].DTG && pts[i].DTG >= p.ProminenceMg {
				pk := &Peak{
					TimeMin: pts[i].TimeMin, TempC: pts[i].DTG, DTG: pts[i].DTG,
					ClusterID: clusterID(tg.ArtifactID, segN, len(peaks)),
				}
				pk.TempC = pts[i].TempC
				pk.ID = peakID(tg.ArtifactID, segID, i)
				peaks = append(peaks, pk)
			}
		}
		for _, pk := range peaks {
			res.Peaks = append(res.Peaks, *pk)
		}
		if len(peaks) == 0 {
			continue
		}
		// 2) Cluster peaks whose basins overlap (valley between them stays high):
		// these are overlapping / shoulder peaks and become legal alternatives.
		clusters := clusterPeaks(pts, peaks, p)
		for ci, cl := range clusters {
			clusterKey := clusterID(tg.ArtifactID, segN, ci)
			// ensure cluster ids match those stamped on peaks
			for _, pk := range cl {
				pk.ClusterID = clusterKey
			}
			// merged candidate: outer baseline to baseline across the cluster
			mergedStart, mergedEnd := clusterBounds(pts, cl, p)
			res.Candidates = append(res.Candidates, makeCandidate(series, pts, tg, gas, p, clusterKey, "merged",
				mergedStart, mergedEnd, peakIDList(cl), segN))
			if len(cl) > 1 {
				// shoulder split: each peak also becomes its own candidate;
				// these overlap in time and are all retained as legal options.
				for k, pk := range cl {
					s, e := peakBounds(pts, indexOf(pts, pk), p)
					c := makeCandidate(series, pts, tg, gas, p, clusterKey, variantName(k), s, e,
						[]string{pk.ID}, segN)
					res.Candidates = append(res.Candidates, c)
				}
			}
		}
	}
	sort.SliceStable(res.Candidates, func(i, j int) bool {
		if res.Candidates[i].StartMin != res.Candidates[j].StartMin {
			return res.Candidates[i].StartMin < res.Candidates[j].StartMin
		}
		return res.Candidates[i].Variant < res.Candidates[j].Variant
	})
	res.ArtifactID = artifactID(AlgoStages, p.fingerprint(), tg.Sources, map[string]any{
		"tg": tg.ArtifactID, "peaks": len(res.Peaks), "cands": len(res.Candidates),
	})
	return res
}

func variantName(k int) string {
	return "sub" + string(rune('1'+k))
}

func indexOf(pts []*DTGPoint, pk *Peak) int {
	for i, q := range pts {
		if q.TimeMin == pk.TimeMin && q.TempC == pk.TempC {
			return i
		}
	}
	return 0
}

// clusterPeaks groups peaks whose intervening valley is shallow (above half of
// the lower peak height) — the overlapping/shoulder signature.
func clusterPeaks(pts []*DTGPoint, peaks []*Peak, p Params) [][]*Peak {
	var out [][]*Peak
	used := map[int]bool{}
	for i, pk := range peaks {
		if used[i] {
			continue
		}
		group := []*Peak{pk}
		used[i] = true
		for j := i + 1; j < len(peaks); j++ {
			if used[j] {
				continue
			}
			q := peaks[j]
			pi, pj := indexOf(pts, pk), indexOf(pts, q)
			valley := math.MaxFloat64
			for k := pi; k <= pj; k++ {
				if pts[k].DTG < valley {
					valley = pts[k].DTG
				}
			}
			threshold := 0.5 * math.Min(pk.DTG, q.DTG)
			if valley >= threshold {
				group = append(group, q)
				used[j] = true
			}
		}
		out = append(out, group)
	}
	return out
}

// clusterBounds finds the outer zero/prominence crossings around a cluster.
func clusterBounds(pts []*DTGPoint, cl []*Peak, p Params) (int, int) {
	first := indexOf(pts, cl[0])
	last := indexOf(pts, cl[len(cl)-1])
	s := crossingDown(pts, first, p.ProminenceMg)
	e := crossingUp(pts, last, p.ProminenceMg)
	return s, e
}

func peakBounds(pts []*DTGPoint, i int, p Params) (int, int) {
	return crossingDown(pts, i, p.ProminenceMg), crossingUp(pts, i, p.ProminenceMg)
}

func crossingDown(pts []*DTGPoint, i int, level float64) int {
	for i > 0 && pts[i-1].DTG >= level*0.5 {
		i--
	}
	return i
}
func crossingUp(pts []*DTGPoint, i int, level float64) int {
	for i < len(pts)-1 && pts[i+1].DTG >= level*0.5 {
		i++
	}
	return i
}

func makeCandidate(series *NormSeries, pts []*DTGPoint, tg *TGResult, gas *NormGas, p Params, clusterKey, variant string, si, ei int, peakIDs []string, segN int) StageCandidate {
	start, end := pts[si].TimeMin, pts[ei].TimeMin
	m0 := massAt(series, start)
	m1 := massAt(series, end)
	loss := m0 - m1
	var pct float64
	if m0 != 0 {
		pct = loss / m0 * 100
	}
	c := StageCandidate{
		ID: candidateID(tg.ArtifactID, clusterKey, variant, si, ei),
		ClusterID: clusterKey, Variant: variant,
		StartMin: start, EndMin: end,
		StartTempC: pts[si].TempC, EndTempC: pts[ei].TempC,
		MassLossMg: loss, MassLossPct: pct, PeakIDs: peakIDs,
		Status: "proposed", ReactionStartMin: start + p.AtmosDelayMin,
	}
	// Atmosphere evidence: nearest valve event is shifted by the atmosphere
	// delay; valve switching time != sample reaction time.
	if gas != nil {
		if v, ok := nearestValve(gas, start); ok {
			vv := v
			c.ValveNearMin = &vv
		}
	}
	// Gap crossing: candidate spans two segments => rejected but retained.
	if segSpan(series, start, end) > 1 {
		c.CrossesGap = true
		c.Status = "rejected"
		c.RejectReasons = append(c.RejectReasons, "candidate spans a sampling gap; interpolation across gaps is forbidden")
	}
	return c
}

func segSpan(series *NormSeries, t0, t1 float64) int {
	n := 0
	for _, s := range series.Segments {
		a := series.Samples[s.Start].TimeMin
		b := series.Samples[s.End].TimeMin
		if b >= t0 && a <= t1 {
			n++
		}
	}
	return n
}

func massAt(series *NormSeries, t float64) float64 {
	// linear interpolation INSIDE one segment only; for t at/after endpoints use
	// endpoint mass. Never interpolate across segments.
	for _, s := range series.Segments {
		a := series.Samples[s.Start]
		b := series.Samples[s.End]
		if t >= a.TimeMin && t <= b.TimeMin {
			if b.TimeMin == a.TimeMin {
				return a.MassMg
			}
			f := (t - a.TimeMin) / (b.TimeMin - a.TimeMin)
			return a.MassMg + f*(b.MassMg-a.MassMg)
		}
	}
	if len(series.Samples) > 0 {
		if t <= series.Samples[0].TimeMin {
			return series.Samples[0].MassMg
		}
		return series.Samples[len(series.Samples)-1].MassMg
	}
	return 0
}

func nearestValve(gas *NormGas, t float64) (float64, bool) {
	best := math.MaxFloat64
	var bv float64
	found := false
	for _, e := range gas.Events {
		if at, ok := e.At.NormFloat(); ok {
			if d := math.Abs(at - t); d < best {
				best, bv, found = d, at, true
			}
		}
	}
	return bv, found
}

func clusterID(artID string, segN, k int) string {
	return "cl_" + short(hashCanonical([]any{artID, segN, k}))
}
func peakID(artID, segID string, i int) string {
	return "pk_" + short(hashCanonical([]any{artID, segID, i}))
}
func candidateID(artID, clusterKey, variant string, si, ei int) string {
	return "cand_" + short(hashCanonical([]any{artID, clusterKey, variant, si, ei}))
}
func peakIDList(cl []*Peak) []string {
	out := make([]string, len(cl))
	for i, p := range cl {
		out[i] = p.ID
	}
	return out
}
