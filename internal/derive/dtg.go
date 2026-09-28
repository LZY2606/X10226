package derive

import (
	"math"
	"sort"

	"tgareview/internal/domain"
)

// BuildDTG computes percent/min central differences within each contiguous
// segment. At segment (gap) edges the derivative is explicitly undefined —
// there is no interpolation across a sampling gap.
func BuildDTG(tg TGResult, p domain.Params) DTGResult {
	res := DTGResult{}
	raw := make([]float64, len(tg.Points))
	defined := make([]bool, len(tg.Points))
	for i := range tg.Points {
		if i == 0 || i == len(tg.Points)-1 {
			continue
		}
		a, b := tg.Points[i-1], tg.Points[i+1]
		c := tg.Points[i]
		if a.Segment != c.Segment || b.Segment != c.Segment {
			continue // gap-adjacent: undefined by construction
		}
		dt := b.TS - a.TS
		if dt <= 0 {
			continue
		}
		raw[i] = (b.MassPct - a.MassPct) / (dt / 60.0)
		defined[i] = true
	}
	w := p.SmoothingWindow
	if w < 3 {
		w = 3
	}
	if w%2 == 0 {
		w++
	}
	h := w / 2
	smoothed := make([]float64, len(tg.Points))
	for i := range tg.Points {
		if !defined[i] {
			continue
		}
		var sum float64
		n := 0
		for k := i - h; k <= i+h; k++ {
			if k >= 0 && k < len(tg.Points) && defined[k] && tg.Points[k].Segment == tg.Points[i].Segment {
				sum += raw[k]
				n++
			}
		}
		smoothed[i] = sum / float64(n)
	}
	for i := range tg.Points {
		res.Series = append(res.Series, DTGPoint{
			Line: tg.Points[i].Line, TS: r3(tg.Points[i].TS), T: tg.Points[i].T,
			DTGPctMin: r3(smoothed[i]), Segment: tg.Points[i].Segment, Defined: defined[i],
		})
	}
	res.Peaks = detectPeaks(res.Series, p)
	res.Chain = []domain.ChainNode{
		{Step: "1 DTG", Detail: "段内中心差分 (m%[i+1]−m%[i−1]) / Δt；缺口边缘与段端点显式 undefined，不跨缺口插值"},
		{Step: "2 平滑", Detail: "同段内居中窗口平均，窗口=" + itoa(w) + " 点；绝不跨越缺口取点"},
		{Step: "3 峰检测", Detail: "质量损失为 DTG 极小值，按 prominence 阈值=" + f3(p.PeakProminencePct) + " %/min 筛选"},
		{Step: "4 重叠峰", Detail: "prominence 在 [" + f3(p.OverlapPeakPromPct) + "," + f3(p.PeakProminencePct) + ") 的肩部/重叠峰保留为替代候选，不强行合并"},
	}
	return res
}

// detectPeaks finds local minima (loss events) segment by segment.
func detectPeaks(s []DTGPoint, p domain.Params) []Peak {
	var peaks []Peak
	n := len(s)
	// local minima, ignore non-monotonic-noise jitter via prominence
	for i := 1; i < n-1; i++ {
		if !s[i].Defined {
			continue
		}
		if s[i].DTGPctMin >= s[i-1].DTGPctMin || s[i].DTGPctMin >= s[i+1].DTGPctMin {
			// allow plateaus
			if !(s[i].DTGPctMin <= s[i-1].DTGPctMin && s[i].DTGPctMin <= s[i+1].DTGPctMin) {
				continue
			}
		}
		prom := prominence(s, i)
		if prom < p.OverlapPeakPromPct {
			continue
		}
		pk := Peak{Line: s[i].Line, TS: s[i].TS, T: s[i].T, DTG: s[i].DTGPctMin, Prominence: r3(prom)}
		switch {
		case prom >= p.PeakProminencePct:
			pk.Note = "primary"
		default:
			pk.Note = "overlap/shoulder candidate"
		}
		peaks = append(peaks, pk)
	}
	// mark close pairs as overlapping
	for i := range peaks {
		for j := range peaks {
			if i == j {
				continue
			}
			if math.Abs(peaks[i].TS-peaks[j].TS) < 150 && peaks[j].TS > peaks[i].TS {
				if peaks[i].OverlapsWith == 0 {
					peaks[i].OverlapsWith = peaks[j].Line
				}
			}
		}
	}
	sort.SliceStable(peaks, func(a, b int) bool { return peaks[a].TS < peaks[b].TS })
	return peaks
}

// prominence of a local minimum at i using surrounding DTG values.
func prominence(s []DTGPoint, i int) float64 {
	val := s[i].DTGPctMin
	// walk left/right while DTG stays below the level val+window; the shoulder
	// level is max DTG reached until values greater than val appear on both sides
	left := val
	for k := i - 1; k >= 0 && s[k].Defined && s[k].Segment == s[i].Segment; k-- {
		if s[k].DTGPctMin > val {
			left = math.Max(left, s[k].DTGPctMin)
			break
		}
		left = math.Max(left, s[k].DTGPctMin)
	}
	right := val
	for k := i + 1; k < len(s) && s[k].Defined && s[k].Segment == s[i].Segment; k++ {
		if s[k].DTGPctMin > val {
			right = math.Max(right, s[k].DTGPctMin)
			break
		}
		right = math.Max(right, s[k].DTGPctMin)
	}
	base := math.Min(left, right)
	return math.Abs(val - base)
}
