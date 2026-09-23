package domain

import "math"

// DTGPoint 是一个 DTG 采样点; RateMgPerMin = -dm/dt (失重为正峰)。
type DTGPoint struct {
	TimeMin      float64 `json:"timeMin"`
	TempDegC     float64 `json:"tempDegC"`
	RateMgPerMin float64 `json:"rateMgPerMin"`
	Connected    bool    `json:"connected"` // 与前一点之间无缺口
	Peak         bool    `json:"peak,omitempty"`
}

// DTGSeries 是 DTG 计算结果。
type DTGSeries struct {
	Points []DTGPoint `json:"points"`
	Peaks  []DTGPeak  `json:"peaks"`
}

// DTGPeak 是一个显著 DTG 峰。
type DTGPeak struct {
	ID           string  `json:"id"`
	TimeMin      float64 `json:"timeMin"`
	TempDegC     float64 `json:"tempDegC"`
	RateMgPerMin float64 `json:"rateMgPerMin"`
	ProminenceMg float64 `json:"prominenceMg"`
	MergeGroup   int     `json:"mergeGroup,omitempty"`
}

// BuildDTG 用局部最小二乘斜率估计 DTG; 窗口不跨越缺口。
// 直接来源: CorrectedSeries + params(dtgWindowPoints/prominence/mergeWindow)。
func BuildDTG(cs CorrectedSeries, params Params) DTGSeries {
	type pt struct {
		t, m, temp float64
		gapBefore  bool
	}
	var valid []pt
	prevT := math.NaN()
	for _, c := range cs.Points {
		if c.Gap || math.IsNaN(c.TimeMin) {
			continue
		}
		gap := false
		if c.hasFlag("gap_start") || math.IsNaN(prevT) {
			gap = true
		}
		valid = append(valid, pt{c.TimeMin, c.CorrectedMg, c.TempDegC, gap})
		prevT = c.TimeMin
	}

	half := (params.DtgWindowPoints - 1) / 2
	if half < 1 {
		half = 1
	}
	out := DTGSeries{}
	for i := range valid {
		lo, hi := i, i
		for lo-1 >= 0 && i-lo < half {
			if valid[lo].gapBefore {
				break
			}
			lo--
		}
		for hi+1 < len(valid) && hi-i < half {
			if valid[hi+1].gapBefore {
				break
			}
			hi++
		}
		var xs, ys []float64
		for k := lo; k <= hi; k++ {
			xs = append(xs, valid[k].t)
			ys = append(ys, valid[k].m)
		}
		slope := 0.0
		if len(xs) >= 2 {
			slope = leastSquares(xs, ys).Rate
		}
		out.Points = append(out.Points, DTGPoint{
			TimeMin:      valid[i].t,
			TempDegC:     valid[i].temp,
			RateMgPerMin: -slope,
			Connected:    !valid[i].gapBefore,
		})
	}

	// 峰检测: 局部极大且 prominence 达标
	for i := 1; i+1 < len(out.Points); i++ {
		c := out.Points[i].RateMgPerMin
		if c > out.Points[i-1].RateMgPerMin && c >= out.Points[i+1].RateMgPerMin {
			prom := c - math.Min(out.Points[i-1].RateMgPerMin, out.Points[i+1].RateMgPerMin)
			if prom >= params.PeakProminenceMg {
				out.Points[i].Peak = true
				out.Peaks = append(out.Peaks, DTGPeak{
					TimeMin:      out.Points[i].TimeMin,
					TempDegC:     out.Points[i].TempDegC,
					RateMgPerMin: c,
					ProminenceMg: prom,
				})
			}
		}
	}
	// 重叠峰分组: merge window 内的多个峰属同一组
	group := 0
	for i := range out.Peaks {
		if i == 0 || out.Peaks[i].TimeMin-out.Peaks[i-1].TimeMin > params.PeakMergeWindowMin {
			group++
		}
		out.Peaks[i].MergeGroup = group
	}
	return out
}

func (c CorrectedPoint) hasFlag(f string) bool {
	for _, x := range c.Flags {
		if x == f {
			return true
		}
	}
	return false
}
