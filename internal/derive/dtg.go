package derive

import "tga-review/internal/domain"

func (e *Engine) runDTG(cn *domain.CorrectNode) *domain.DTGNode {
	p := e.params
	dn := &domain.DTGNode{Params: p, M0Mg: e.m0Mg()}
	w := p.SmoothWindowPts
	if w < 1 {
		w = 1
	}

	tempOf := func(cp domain.CorrectPoint) (float64, bool) {
		if cp.SampleC != nil {
			return *cp.SampleC, true
		}
		if cp.FurnaceC != nil {
			return *cp.FurnaceC, true
		}
		return 0, false
	}

	for _, seg := range cn.Segs {
		var segPts []domain.DTGPoint
		for i := seg.StartIdx; i <= seg.EndIdx; i++ {
			cp := cn.Points[i]
			if !cp.Valid || cp.CorrectedMg == nil {
				continue
			}
			tc, ok := tempOf(cp)
			if !ok {
				continue
			}
			segPts = append(segPts, domain.DTGPoint{
				Idx: cp.Idx, TimeS: cp.TimeS, SampleC: tc, SegID: seg.ID, Noisy: cp.Noisy,
			})
		}
		// raw central derivative inside the segment only
		for i := range segPts {
			if i == 0 || i == len(segPts)-1 {
				segPts[i].Dtg = 0
			} else {
				a, b := segPts[i-1], segPts[i+1]
				dt := (b.TimeS - a.TimeS) / 60.0 // min
				if dt > 0 {
					// find corrected masses by idx
					ma := cn.Points[a.Idx-1].CorrectedMg
					mb := cn.Points[b.Idx-1].CorrectedMg
					segPts[i].Dtg = round4((*mb - *ma) / dt)
				}
			}
			if dn.M0Mg != nil && *dn.M0Mg != 0 {
				segPts[i].DtgPctMin = round4(segPts[i].Dtg / *dn.M0Mg * 100)
			}
		}
		// centered moving average; gap-adjacent points only use within-segment window
		for i := range segPts {
			lo := i - w/2
			hi := i + w/2
			if lo < 0 {
				lo = 0
			}
			if hi > len(segPts)-1 {
				hi = len(segPts) - 1
			}
			sum := 0.0
			for k := lo; k <= hi; k++ {
				sum += segPts[k].Dtg
			}
			segPts[i].Smoothed = round4(sum / float64(hi-lo+1))
			if i == 0 || i == len(segPts)-1 {
				segPts[i].GapAdj = true
			}
		}
		dn.Points = append(dn.Points, segPts...)
	}
	if len(dn.Points) == 0 {
		dn.Diags = append(dn.Diags, domain.Diagnostic{Code: "NO_DTG", Msg: "not enough valid points to build DTG"})
	}
	return dn
}
