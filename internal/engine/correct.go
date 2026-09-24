package engine

import (
	"fmt"

	"tgaline/internal/domain"
)

// correctedResult is the drift+blank corrected mass trace with gap/noise info.
type correctedResult struct {
	series      *domain.Series
	corrections []correctionNote
	noise       noiseReport
	gapCount    int
}

type correctionNote struct {
	index   int
	timeS   float64
	driftMG float64
	blankMG float64
}

// buildCorrected applies the balance drift correction (integral of drift rate
// table up to t) and subtracts the blank curve. Drift/blank values are only
// interpolated within their own support; unsupported points are left unknown.
func buildCorrected(in layerInput, p map[string]float64) correctedResult {
	res := correctedResult{}
	n := len(in.sample)
	s := &domain.Series{
		TimeS: make([]float64, n), TK: make([]float64, n),
		MassMG: make([]float64, n), Mask: make([]bool, n),
	}
	driftTable := integrateDrift(in.drift)
	gapMin := p["gap_min_s"]
	jumpThr := p["noise_jump_mg"]

	for i, pt := range in.sample {
		s.TimeS[i] = pt.t
		s.TK[i] = pt.T
		mass := pt.m
		dOK, bOK := false, false
		dv := 0.0
		if len(driftTable) > 0 {
			if v, ok := interp(driftTable, pt.t); ok {
				dv = v
				dOK = true
			}
		} else {
			dOK = true
		}
		bv := 0.0
		if len(in.blank) > 0 {
			if v, ok := interp(in.blank, pt.t); ok {
				bv = v
				bOK = true
			}
		} else {
			bOK = true
		}
		note := correctionNote{index: i, timeS: pt.t}
		if dOK {
			mass -= dv
			note.driftMG = dv
		}
		if bOK {
			mass -= bv
			note.blankMG = bv
		}
		known := dOK && bOK
		s.Mask[i] = known
		if known {
			s.MassMG[i] = mass
			res.corrections = append(res.corrections, note)
		}
	}

	// sampling gaps: large missing spans between consecutive sample points.
	for i := 0; i+1 < n; i++ {
		if in.sample[i+1].t-in.sample[i].t >= gapMin {
			s.GapAfter = append(s.GapAfter, i)
			res.gapCount++
		}
	}

	// non-monotonic noise: on a mass-loss TGA trace, a short positive jump
	// (gain) larger than the threshold is noise evidence, not chemistry.
	// Points are kept (never smoothed away) and reported as first-class data.
	nb := int(p["noise_neighbors"])
	if nb < 1 {
		nb = 1
	}
	for i := 1; i < n; i++ {
		if !s.Mask[i] || !s.Mask[i-1] {
			continue
		}
		jump := s.MassMG[i] - s.MassMG[i-1]
		if jump <= jumpThr {
			continue
		}
		// confirm it is a local excursion: surrounding trend is downward.
		l, r := i-nb, i+nb
		if l < 0 {
			l = 0
		}
		if r >= n {
			r = n - 1
		}
		trend := 0.0
		cnt := 0
		for j := l; j < r; j++ {
			if !s.Mask[j] || !s.Mask[j+1] {
				continue
			}
			if j+1 == i || j == i {
				continue
			}
			trend += s.MassMG[j+1] - s.MassMG[j]
			cnt++
		}
		if cnt > 0 && trend/float64(cnt) < 0 {
			res.noise.Records = append(res.noise.Records, NoiseRecord{
				Index: i, TimeS: s.TimeS[i], JumpMG: jump, Kind: "uptick",
				Note: fmt.Sprintf("非单调向上跳变 %.3f mg 高于阈值 %.3f mg，局部趋势向下；保留原點不做平滑", jump, jumpThr),
			})
		}
	}
	res.noise.ThresholdMG = jumpThr
	res.series = s
	return res
}

// integrateDrift turns drift-rate points (mg/min) into cumulative drift (mg),
// integrating piecewise-linear rate over time, reset at the first time.
func integrateDrift(rate []kv) []kv {
	if len(rate) == 0 {
		return nil
	}
	out := make([]kv, len(rate))
	cum := 0.0
	for i, r := range rate {
		if i > 0 {
			dtMin := (r.t - rate[i-1].t) / 60.0
			mid := (r.v + rate[i-1].v) / 2.0
			cum += mid * dtMin
		}
		// absolute furnace time preserved; run starts near t=0
		out[i] = kv{t: r.t, v: cum}
	}
	return out
}
