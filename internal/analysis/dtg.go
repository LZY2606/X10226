package analysis

import (
	"fmt"
	"math"

	"tga-review/internal/domain"
)

// computeDTG derives mg/s by a central difference inside contiguous present
// runs. Any derivative whose window touches a gap is marked AcrossGap and is
// excluded from peak detection. Gaps are never bridged.
func computeDTG(tr domain.CorrectedTrace, p domain.Params) []domain.DTGPoint {
	n := len(tr.Times)
	raw := make([]float64, n)
	for i := range raw {
		if tr.Present[i] {
			raw[i] = tr.CorrectedMg[i]
		}
	}
	dtg := make([]domain.DTGPoint, n)
	for i := 0; i < n; i++ {
		dtg[i] = domain.DTGPoint{Time: tr.Times[i], TempC: tr.TempC[i]}
		if !tr.Present[i] {
			continue
		}
		l, r := i-1, i+1
		if l < 0 || r >= n || !tr.Present[l] || !tr.Present[r] {
			continue
		}
		dt := tr.Times[r] - tr.Times[l]
		if dt <= 0 {
			continue
		}
		dtg[i].DTGMgPerS = (raw[r] - raw[l]) / dt
	}
	// robust central smoothing over fixed time window, never across gaps
	smoothed := smoothDTG(dtg, tr, p)
	out := make([]domain.DTGPoint, n)
	for i := range out {
		out[i] = smoothed[i]
		if !tr.Present[i] {
			out[i].DTGMgPerS = 0
			continue
		}
		// mark if any point inside the smoothing window spanned a gap
		lo, hi := tr.Times[i]-p.DTGWindowS, tr.Times[i]+p.DTGWindowS
		for _, g := range tr.Gaps {
			if g.EndT > lo && g.StartT < hi {
				out[i].AcrossGap = true
				out[i].DTGMgPerS = 0
				break
			}
		}
	}
	return out
}

func smoothDTG(dtg []domain.DTGPoint, tr domain.CorrectedTrace, p domain.Params) []domain.DTGPoint {
	cur := dtg
	for pass := 0; pass < p.SmoothPasses; pass++ {
		nxt := make([]domain.DTGPoint, len(cur))
		copy(nxt, cur)
		for i := range cur {
			if !tr.Present[i] {
				continue
			}
			sum, cnt := 0.0, 0
			for k := range cur {
				if !tr.Present[k] {
					continue
				}
				if math.Abs(tr.Times[k]-tr.Times[i]) <= p.DTGWindowS {
					sum += cur[k].DTGMgPerS
					cnt++
				}
			}
			if cnt > 0 {
				nxt[i].DTGMgPerS = sum / float64(cnt)
			}
		}
		cur = nxt
	}
	return cur
}

// findPeaks locates DTG minima (mass-loss peaks). Overlapping peaks are all
// kept; overlap is recorded both ways on Peak.OverlapsWith.
func findPeaks(dtg []domain.DTGPoint, p domain.Params) []domain.Peak {
	type loc struct {
		idx int
		val float64
	}
	var minima []loc
	for i := 1; i+1 < len(dtg); i++ {
		if dtg[i].AcrossGap || dtg[i].DTGMgPerS >= 0 {
			continue
		}
		if dtg[i].DTGMgPerS <= dtg[i-1].DTGMgPerS && dtg[i].DTGMgPerS <= dtg[i+1].DTGMgPerS &&
			-dtg[i].DTGMgPerS >= p.MinProminenceMgPerS {
			minima = append(minima, loc{i, dtg[i].DTGMgPerS})
		}
	}
	// merge within min separation, keeping the stronger minimum
	var picked []loc
	for _, m := range minima {
		if len(picked) > 0 && dtg[m.idx].Time-dtg[picked[len(picked)-1].idx].Time < p.MinSeparationS {
			if m.val < picked[len(picked)-1].val {
				picked[len(picked)-1] = m
			}
			continue
		}
		picked = append(picked, m)
	}
	peaks := make([]domain.Peak, 0, len(picked))
	for i, m := range picked {
		prom := -m.val
		peaks = append(peaks, domain.Peak{
			ID:         fmt.Sprintf("peak-%d", i+1),
			Time:       dtg[m.idx].Time,
			TempC:      dtg[m.idx].TempC,
			DTGMgPerS:  m.val,
			Prominence: prom,
		})
	}
	// determine overlapping support windows (peak +/- min separation)
	for i := range peaks {
		for k := range peaks {
			if i == k {
				continue
			}
			d := math.Abs(peaks[i].Time - peaks[k].Time)
			if d < 2*p.MinSeparationS {
				peaks[i].OverlapsWith = append(peaks[i].OverlapsWith, peaks[k].ID)
			}
		}
	}
	return peaks
}
