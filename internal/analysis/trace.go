package analysis

import (
	"fmt"

	"tga-review/internal/domain"
)

func correctTrace(in Inputs, p domain.Params) domain.CorrectedTrace {
	sp := in.SamplePoints
	tr := domain.CorrectedTrace{DriftApplied: in.Drift != nil}
	for i, pt := range sp.Points {
		tr.Times = append(tr.Times, pt.Time.Value)
		tr.TempC = append(tr.TempC, pt.Temp.Value)
		tr.Present = append(tr.Present, pt.Present)
		if pt.Present {
			raw := pt.Mass.Value
			corr := raw
			if in.Drift != nil {
				corr = raw - (in.Drift.Slope.Value*pt.Time.Value + in.Drift.Intercept.Value)
			}
			tr.RawMg = append(tr.RawMg, raw)
			tr.CorrectedMg = append(tr.CorrectedMg, corr)
		} else {
			tr.RawMg = append(tr.RawMg, 0)
			tr.CorrectedMg = append(tr.CorrectedMg, 0)
		}
		if i > 0 && !pt.Present && sp.Points[i-1].Present {
			start := sp.Points[i-1].Time.Value
			k := i
			for k < len(sp.Points) && !sp.Points[k].Present {
				k++
			}
			if k < len(sp.Points) {
				end := sp.Points[k].Time.Value
				width := end - start
				g := domain.Gap{
					ID:          fmt.Sprintf("gap-%d", len(tr.Gaps)+1),
					StartT:      start,
					EndT:        end,
					WidthS:      width,
					Bridged:     false,
					BeforeIndex: i - 1,
					AfterIndex:  k,
					Reason:      "gap preserved; no interpolation across missing samples",
				}
				if width > p.GapMaxS {
					g.Reason = fmt.Sprintf("gap %.1fs exceeds max %.1fs; mass balance not interpolated", width, p.GapMaxS)
				}
				tr.Gaps = append(tr.Gaps, g)
			}
		}
	}
	for i := 1; i < len(tr.CorrectedMg); i++ {
		if !tr.Present[i] || !tr.Present[i-1] {
			continue
		}
		d := tr.CorrectedMg[i] - tr.CorrectedMg[i-1]
		if d > p.NonMonotonicThresholdMg {
			tr.NoiseFlags = append(tr.NoiseFlags, domain.NoiseFlag{
				ID:      fmt.Sprintf("noise-%d", len(tr.NoiseFlags)+1),
				Index:   i,
				Time:    tr.Times[i],
				DeltaMg: d,
				Message: fmt.Sprintf("apparent mass gain of %.3f mg exceeds noise threshold %.3f mg; flagged as noise, not a stage", d, p.NonMonotonicThresholdMg),
			})
		}
	}
	return tr
}

func traceJudgements(tr domain.CorrectedTrace, p domain.Params) []domain.Judgement {
	var js []domain.Judgement
	if !tr.DriftApplied {
		js = append(js, j(domain.SevWarning, "no_drift_correction", "no balance drift model committed; raw masses used", "trace", nil))
	}
	for _, g := range tr.Gaps {
		js = append(js, j(domain.SevWarning, "gap_preserved", g.Reason, g.ID,
			map[string]any{"start_t": g.StartT, "end_t": g.EndT, "width_s": g.WidthS, "gap_max_s": p.GapMaxS}))
	}
	for _, n := range tr.NoiseFlags {
		js = append(js, j(domain.SevWarning, "nonmonotonic_noise", n.Message, n.ID,
			map[string]any{"time": n.Time, "delta_mg": n.DeltaMg}))
	}
	return js
}
