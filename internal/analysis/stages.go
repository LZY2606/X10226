package analysis

import (
	"fmt"
	"sort"

	"tga-review/internal/domain"
)

// massAt returns the corrected mass at time t using the nearest present sample
// at-or-before t. It reports false if no present sample exists at or before t
// or if a gap separates t from the last present sample (never interpolating).
func massAt(tr domain.CorrectedTrace, t float64) (float64, int, bool) {
	idx := -1
	for i := range tr.Times {
		if tr.Present[i] && tr.Times[i] <= t {
			idx = i
		}
	}
	if idx < 0 {
		return 0, -1, false
	}
	for _, g := range tr.Gaps {
		if t > g.StartT && t < g.EndT {
			return tr.CorrectedMg[idx], idx, false
		}
	}
	return tr.CorrectedMg[idx], idx, true
}

// zeroCrossing finds the last time near peak where DTG returns to near zero
// going backwards (start) or forwards (end), i.e. the local support window.
func supportWindow(dtg []domain.DTGPoint, peakIdx int, p domain.Params) (int, int) {
	start, end := peakIdx, peakIdx
	for start-1 >= 0 {
		k := start - 1
		if dtg[k].AcrossGap {
			break
		}
		if dtg[k].DTGMgPerS >= -p.MinProminenceMgPerS*0.25 {
			break
		}
		start = k
	}
	for end+1 < len(dtg) {
		k := end + 1
		if dtg[k].AcrossGap {
			break
		}
		if dtg[k].DTGMgPerS >= -p.MinProminenceMgPerS*0.25 {
			break
		}
		end = k
	}
	return start, end
}

func peakIndex(dtg []domain.DTGPoint, pk domain.Peak) int {
	best, bestD := -1, 1e30
	for i := range dtg {
		if dtg[i].Time == pk.Time {
			return i
		}
		if d := abs(dtg[i].Time - pk.Time); d < bestD {
			bestD, best = d, i
		}
	}
	return best
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// buildStages turns each loss peak into a stage candidate. Adjacent support
// windows are allowed to overlap; percentage loss is relative to each stage
// entry mass and is explicitly NOT renormalised to sum to 100%. When a gap
// or overlapping peak makes a boundary non-unique, every legal alternative is
// retained instead of guessing.
func buildStages(in Inputs, b *domain.AnalysisBundle, p domain.Params) []domain.StageCandidate {
	tr := b.Trace
	var stages []domain.StageCandidate
	type win struct {
		pk     domain.Peak
		sI, eI int
	}
	var wins []win
	for _, pk := range b.Peaks {
		pi := peakIndex(b.DTG, pk)
		if pi < 0 {
			continue
		}
		sI, eI := supportWindow(b.DTG, pi, p)
		wins = append(wins, win{pk, sI, eI})
	}
	// overlap allowed: mark stages whose windows intersect
	overlap := make([][]string, len(wins))
	for i := range wins {
		for k := range wins {
			if i != k && wins[k].sI <= wins[i].eI && wins[i].sI <= wins[k].eI {
				overlap[i] = append(overlap[i], wins[k].pk.ID)
			}
		}
	}
	for i, w := range wins {
		startT, endT := b.DTG[w.sI].Time, b.DTG[w.eI].Time
		startMg, sIdx, sOK := massAt(tr, startT)
		endMg, eIdx, eOK := massAt(tr, endT)
		st := domain.StageCandidate{
			ID:             fmt.Sprintf("stage-%d", i+1),
			Label:          fmt.Sprintf("Stage %d", i+1),
			StartT:         startT,
			EndT:           endT,
			StartTempC:     tr.TempC[sIdx],
			EndTempC:       tr.TempC[eIdx],
			StartMg:        startMg,
			EndMg:          endMg,
			LossMg:         startMg - endMg,
			PeakIDs:        []string{w.pk.ID},
			GasAtStart:     gasAt(in.GasValves.Events, startT),
			GasAtEnd:       gasAt(in.GasValves.Events, endT),
			OverlapsStages: overlap[i],
		}
		if startMg != 0 {
			st.LossPercent = (startMg - endMg) / startMg * 100
		}
		if !sOK || !eOK {
			st.BoundaryGap = true
			st.Ambiguous = true
			st.RejectReason = "stage boundary touches a sampling gap; mass cannot be uniquely fixed"
			// retain every legal boundary: support-window edge and the two
			// points adjacent to the gap
			if !sOK {
				st.Alternatives = append(st.Alternatives, legalAlternatives(tr, w.sI, true)...)
			}
			if !eOK {
				st.Alternatives = append(st.Alternatives, legalAlternatives(tr, w.eI, false)...)
			}
		}
		if len(overlap[i]) > 0 {
			st.Ambiguous = true
		}
		// reject candidates with non-positive apparent loss that are noise
		if st.LossMg <= p.NonMonotonicThresholdMg && len(overlap[i]) == 0 {
			st.RejectReason = "no significant monotone loss around this peak; likely non-monotonic noise"
		}
		stages = append(stages, st)
	}
	sort.SliceStable(stages, func(a, c int) bool { return stages[a].StartT < stages[c].StartT })
	// renumber in time order but keep stable ids
	for i := range stages {
		stages[i].ID = fmt.Sprintf("stage-%d", i+1)
		stages[i].Label = fmt.Sprintf("Stage %d", i+1)
	}
	return stages
}

// legalAlternatives returns every present-sample boundary legal next to a gap.
func legalAlternatives(tr domain.CorrectedTrace, nearIdx int, isStart bool) []domain.Alternative {
	var out []domain.Alternative
	for _, g := range tr.Gaps {
		if tr.Times[nearIdx] >= g.StartT-pseudoEps && tr.Times[nearIdx] <= g.EndT+pseudoEps {
			out = append(out,
				domain.Alternative{Boundary: boundary(isStart), Time: tr.Times[g.BeforeIndex], TempC: tr.TempC[g.BeforeIndex], Reason: "last present sample before gap"},
				domain.Alternative{Boundary: boundary(isStart), Time: tr.Times[g.AfterIndex], TempC: tr.TempC[g.AfterIndex], Reason: "first present sample after gap"},
			)
		}
	}
	return out
}

const pseudoEps = 1e-9

func boundary(isStart bool) string {
	if isStart {
		return "start"
	}
	return "end"
}

func stageJudgements(stages []domain.StageCandidate) []domain.Judgement {
	var js []domain.Judgement
	total := 0.0
	for _, s := range stages {
		total += s.LossPercent
		if s.BoundaryGap {
			js = append(js, j(domain.SevWarning, "stage_gap_ambiguous",
				"stage boundary not unique across sampling gap; all legal candidates retained", s.ID,
				map[string]any{"alternatives": len(s.Alternatives)}))
		}
		if len(s.OverlapsStages) > 0 {
			js = append(js, j(domain.SevInfo, "stages_overlap",
				"overlapping decomposition stages are allowed and kept as separate candidates", s.ID,
				map[string]any{"overlaps": s.OverlapsStages}))
		}
		if s.RejectReason != "" {
			js = append(js, j(domain.SevInfo, "stage_rejected", s.RejectReason, s.ID, nil))
		}
	}
	js = append(js, j(domain.SevInfo, "stage_percentages_not_closed",
		"stage losses are relative to each stage entry mass; they are not forced to sum to 100%",
		"stages", map[string]any{"sum_of_stage_loss_percent": total}))
	return js
}
