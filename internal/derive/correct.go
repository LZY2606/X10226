package derive

import (
	"fmt"

	"tga-review/internal/domain"
)

// runCorrect builds the corrected mass series. It:
//   - preserves every original point (invalid points are kept but marked),
//   - subtracts blank curve and balance drift only inside their anchor ranges,
//   - splits segments at gaps (never interpolates across),
//   - flags non-monotonic noise excursions relative to initial mass.
func (e *Engine) runCorrect() *domain.CorrectNode {
	p := e.params
	node := &domain.CorrectNode{Params: p}
	samples := e.current(domain.KindSamples).Payload.Samples

	var bT, bV []float64
	if bl := e.optional(domain.KindBlank); bl != nil {
		for _, bp := range bl.Payload.Blank {
			if bp.Time.Canon != nil && bp.Mass.Canon != nil {
				bT = append(bT, *bp.Time.Canon)
				bV = append(bV, *bp.Mass.Canon)
			}
		}
	}
	var dT, dV []float64
	if dr := e.optional(domain.KindDrift); dr != nil {
		for _, dp := range dr.Payload.Drift {
			if dp.Time.Canon != nil && dp.Drift.Canon != nil {
				dT = append(dT, *dp.Time.Canon)
				dV = append(dV, *dp.Drift.Canon)
			}
		}
	}
	if len(samples) == 0 {
		node.Diags = append(node.Diags, domain.Diagnostic{Code: "NO_SAMPLES", Msg: "no valid sample layer; node blocked"})
		return node
	}

	m0 := e.m0Mg()

	segStart := 0
	flushSeg := func(end int) {
		node.Segs = append(node.Segs, domain.Segment{
			ID:       fmt.Sprintf("seg-%d", len(node.Segs)+1),
			StartIdx: segStart, EndIdx: end,
			T0: node.Points[segStart].TimeS, T1: node.Points[end].TimeS,
		})
	}
	for i, s := range samples {
		cp := domain.CorrectPoint{Idx: i + 1}
		if s.Time.Canon != nil {
			cp.TimeS = *s.Time.Canon
		}
		if s.FTemp.Canon != nil {
			cp.FurnaceC = s.FTemp.Canon
		}
		if s.SampleT.Canon != nil {
			cp.SampleC = s.SampleT.Canon
		}
		if s.Mass.Canon != nil {
			cp.RawMassMg = s.Mass.Canon
		}
		if s.Time.Canon == nil || s.Mass.Canon == nil {
			cp.Valid = false
			node.Points = append(node.Points, cp)
			if s.Time.Canon != nil && s.Mass.Canon == nil {
				node.Diags = append(node.Diags, domain.Diagnostic{Code: "UNCONVERTIBLE", AtTimeS: cp.TimeS, Msg: "mass not numeric; point retained but excluded"})
			}
			continue
		}
		t := *s.Time.Canon
		raw := *s.Mass.Canon
		corr := raw

		if p.BlankCorrect && len(bT) > 0 {
			if bv, ok := linInterp(bT, bV, t); ok {
				cp.BlankMg = fptr(round4(bv))
				corr -= bv
			} else {
				node.Diags = append(node.Diags, domain.Diagnostic{Code: "BLANK_RANGE", AtTimeS: t, Msg: "blank anchors do not cover sample time; subtraction skipped locally"})
			}
		}
		if p.DriftCorrect && len(dT) > 0 {
			if dv, ok := linInterp(dT, dV, t); ok {
				cp.DriftMg = fptr(round4(dv))
				corr -= dv
			} else {
				node.Diags = append(node.Diags, domain.Diagnostic{Code: "DRIFT_RANGE", AtTimeS: t, Msg: "drift anchors do not cover sample time; correction skipped locally"})
			}
		}
		cp.CorrectedMg = fptr(round4(corr))
		cp.Valid = true

		// gap detection relative to previous valid point
		if i > 0 {
			if prev := prevValid(node.Points); prev != nil {
				span := t - prev.TimeS
				if span > p.GapThresholdS {
					cp.GapBefore = true
					node.Gaps = append(node.Gaps, domain.Gap{
						BeforeIdx: prev.Idx, AfterIdx: i + 1,
						TBefore: prev.TimeS, TAfter: t, SpanS: round2(span),
						Missing: missingRows(samples, prev.Idx-1, i),
					})
					flushSeg(prev.Idx - 1)
					segStart = i
				}
				if m0 != nil && prev.CorrectedMg != nil {
					jump := corr - *prev.CorrectedMg
					pct := jump / *m0 * 100
					if pct > p.NoiseSpikePct {
						cp.Noisy = true
						node.Noise = append(node.Noise, domain.NoiseFlag{
							Idx: i + 1, TimeS: t, JumpMg: round4(jump), PctM0: round4(pct),
						})
					}
				}
			}
		}
		node.Points = append(node.Points, cp)
	}
	// close final segment at last valid point
	lastValid := -1
	for j := len(node.Points) - 1; j >= 0; j-- {
		if node.Points[j].Valid {
			lastValid = j
			break
		}
	}
	if lastValid >= 0 {
		if len(node.Segs) == 0 || node.Segs[len(node.Segs)-1].StartIdx != segStart {
			flushSeg(lastValid)
		} else {
			seg := &node.Segs[len(node.Segs)-1]
			seg.EndIdx = lastValid
			seg.T1 = node.Points[lastValid].TimeS
		}
	}
	if len(node.Gaps) > 0 {
		node.Diags = append(node.Diags, domain.Diagnostic{Code: "GAP_PRESENT", Msg: fmt.Sprintf("%d sampling gap(s); no interpolation across gaps", len(node.Gaps))})
	}
	if len(node.Noise) > 0 {
		node.Diags = append(node.Diags, domain.Diagnostic{Code: "NOISE_PRESENT", Msg: fmt.Sprintf("%d non-monotonic excursion(s) flagged", len(node.Noise))})
	}
	return node
}

func prevValid(pts []domain.CorrectPoint) *domain.CorrectPoint {
	for j := len(pts) - 1; j >= 0; j-- {
		if pts[j].Valid {
			return &pts[j]
		}
	}
	return nil
}

func missingRows(samples []domain.SamplePoint, a, b int) int {
	n := 0
	for k := a + 1; k < b; k++ {
		n++
	}
	return n
}
