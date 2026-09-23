package tga

import "math"

// flagNoise marks non-monotonic upward blips in the corrected mass within each
// gap-delimited segment. Blips stay visible (nothing is deleted); they are
// excluded from smoothing and peak detection.
func flagNoise(rep *Report, p Params) {
	if rep.Initial == nil || rep.Initial.ItemID == "" {
		rep.Warnings = append(rep.Warnings, "初重未知，非单调噪声检测跳过")
		return
	}
	band := p.NoiseBandPct / 100 * rep.Initial.Mg
	for _, seg := range segments(rep) {
		var runningMin float64
		for i, pt := range seg {
			cp := correctedFor(rep, pt.ID)
			if cp == nil || !cp.Valid {
				continue
			}
			if i == 0 || cp.CorrMg < runningMin {
				runningMin = cp.CorrMg
				continue
			}
			if cp.CorrMg-runningMin > band {
				cp.NoiseFlag = true
				pt.NoiseFlag = true
			}
		}
	}
}

func correctedFor(rep *Report, ptID string) *CorrectedPoint {
	for i := range rep.Corrected {
		if rep.Corrected[i].PtID == ptID {
			return &rep.Corrected[i]
		}
	}
	return nil
}

// normalizeTG converts corrected mass to percentage of the selected initial
// mass. An unparseable or revised initial mass leaves TG unknown rather than
// inventing a value.
func normalizeTG(rep *Report) {
	if rep.Initial == nil || rep.Initial.ItemID == "" {
		rep.Warnings = append(rep.Warnings, "缺少有效样品初重，TG%/DTG 留为未知")
		return
	}
	m0 := rep.Initial.Mg
	if m0 <= 0 {
		rep.Warnings = append(rep.Warnings, "样品初重非正数，TG%/DTG 留为未知")
		return
	}
	for i := range rep.Corrected {
		cp := &rep.Corrected[i]
		if !cp.Valid {
			continue
		}
		rep.Tg = append(rep.Tg, TgPoint{
			ID: HashID("tg", map[string]any{"cpt": cp.ID, "m0": rep.Initial.ItemID}),
			CPtID: cp.ID, TimeS: cp.TimeS, TempC: cp.TempC,
			TgPct: cp.CorrMg / m0 * 100,
			GapAfter: cp.GapAfter, NoiseFlag: cp.NoiseFlag,
		})
	}
}

// computeDTG differentiates TG within segments using a centered window,
// skipping noise-flagged points and never crossing a gap.
func computeDTG(rep *Report, p Params) {
	if len(rep.Tg) == 0 {
		return
	}
	win := p.DtgSmoothWinN
	if win < 1 {
		win = 1
	}
	if win%2 == 0 {
		win++
	}
	h := win / 2
	bySeg := splitTgByGaps(rep.Tg)
	for _, seg := range bySeg {
		for i := range seg {
			if seg[i].NoiseFlag {
				continue
			}
			lo, hi := i-h, i+h
			if lo < 0 {
				lo = 0
			}
			if hi > len(seg)-1 {
				hi = len(seg) - 1
			}
			// find clean bounds
			for lo < i && seg[lo].NoiseFlag {
				lo++
			}
			for hi > i && seg[hi].NoiseFlag {
				hi--
			}
			if lo == hi || seg[lo].TimeS == seg[hi].TimeS {
				continue
			}
			d := -(seg[hi].TgPct - seg[lo].TgPct) / (seg[hi].TimeS - seg[lo].TimeS) * 60
			rep.Dtg = append(rep.Dtg, DtgPoint{
				ID: HashID("dtg", map[string]any{"lo": seg[lo].ID, "hi": seg[hi].ID, "w": win}),
				TgID: seg[i].ID, TimeS: seg[i].TimeS, Dtg: d,
			})
		}
	}
}

func splitTgByGaps(tg []TgPoint) [][]TgPoint {
	seg := [][]TgPoint{}
	cur := []TgPoint{}
	for i, p := range tg {
		cur = append(cur, p)
		if p.GapAfter || i == len(tg)-1 {
			seg = append(seg, cur)
			cur = []TgPoint{}
		}
	}
	return seg
}

func dtgAt(rep *Report, t float64) (DtgPoint, bool) {
	best := DtgPoint{}
	found := false
	for _, d := range rep.Dtg {
		if math.Abs(d.TimeS-t) < 1e-9 {
			return d, true
		}
	}
	_ = best
	_ = found
	return best, false
}
