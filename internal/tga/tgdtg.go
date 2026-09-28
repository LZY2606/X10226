package tga

import "math"

// buildTG derives TG (mass %) and DTG (-dm/dt) within each segment. Slopes are
// never computed across segment boundaries (sampling gaps stay explicit).
func buildTG(series *NormSeries, initialMass *float64, p Params) *TGResult {
	res := &TGResult{Algorithm: AlgoTG, ParamFP: p.fingerprint(), Sources: series.Sources}
	// reference mass: declared initial mass when available, else first sample
	ref := math.NaN()
	if initialMass != nil && *initialMass > 0 {
		ref = *initialMass
	} else if len(series.Samples) > 0 {
		ref = series.Samples[0].MassMg
	}
	rawDTG := make([]float64, len(series.Samples))
	for _, seg := range series.Segments {
		for i := seg.Start + 1; i < seg.End; i++ {
			a := series.Samples[i-1]
			b := series.Samples[i+1]
			dt := b.TimeMin - a.TimeMin
			if dt > 0 {
				rawDTG[i] = (a.MassMg - b.MassMg) / dt // central difference
			}
		}
		// forward/backward at segment edges (within segment only)
		if seg.End-seg.Start >= 1 {
			a := series.Samples[seg.Start]
			b := series.Samples[seg.Start+1]
			if b.TimeMin-a.Time > 0 {
				rawDTG[seg.Start] = (a.MassMg - b.MassMg) / (b.TimeMin - a.TimeMin)
			}
			a = series.Samples[seg.End-1]
			b = series.Samples[seg.End]
			if b.TimeMin-a.Time > 0 {
				rawDTG[seg.End] = (a.MassMg - b.MassMg) / (b.TimeMin - a.TimeMin)
			}
		}
	}
	sm := p.SmoothWindow
	if sm%2 == 0 {
		sm++
	}
	if sm < 1 {
		sm = 1
	}
	half := sm / 2
	smoothed := make([]float64, len(rawDTG))
	for i := range rawDTG {
		var sum, w float64
		for k := -half; k <= half; k++ {
			j := i + k
			if j < 0 || j >= len(rawDTG) {
				continue
			}
			if !sameSegment(series, i, j) {
				continue // never smooth across a gap
			}
			wt := 1.0
			sum += rawDTG[j] * wt
			w += wt
		}
		if w > 0 {
			smoothed[i] = sum / w
		}
	}
	segByIndex := map[int]string{}
	for _, s := range series.Segments {
		for i := s.Start; i <= s.End; i++ {
			segByIndex[i] = s.ID
		}
	}
	for i, s := range series.Samples {
		pt := DTGPoint{
			Index: i, TimeMin: s.TimeMin, TempC: s.TempC, MassMg: s.MassMg,
			DTG: smoothed[i], Segment: segByIndex[i],
		}
		if ref > 0 {
			pt.MassPct = s.MassMg / ref * 100.0
		}
		res.Points = append(res.Points, pt)
	}
	res.ArtifactID = artifactID(AlgoTG, p.fingerprint(), series.Sources, map[string]any{
		"series": series.ArtifactID, "ref": ref, "smooth": sm,
	})
	return res
}

func sameSegment(series *NormSeries, i, j int) bool {
	for _, s := range series.Segments {
		if i >= s.Start && i <= s.End {
			return j >= s.Start && j <= s.End
		}
	}
	return false
}
