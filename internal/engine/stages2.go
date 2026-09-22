package engine

import (
	"fmt"
	"math"

	"tga/internal/model"
)

// stageCandidate is one legal decomposition: ordered boundary variant picks.
type stageCandidate struct {
	Index       int
	Peak        peak
	T0Sec       float64 // start boundary (saddle from previous pair, or run start)
	T1Sec       float64 // end boundary
	T0C, T1C    float64
	MassLossMg  float64
	VariantFrom string
	VariantTo   string
}

func (e *Engine) stageArtifacts(cs *model.Artifact, peakArts []*model.Artifact, dtgArts []*model.Artifact, params *model.Params) []*model.Artifact {
	if len(peakArts) == 0 {
		return nil
	}
	dtg := dtgArts[0].Data["points"].([]DTGPoint)
	rows := cs.Data["rows"].([]CorrectedPoint)
	peaks := make([]peak, 0, len(peakArts))
	for _, a := range peakArts {
		peaks = append(peaks, peak{
			Index: int(a.Data["index"].(int)), TimeSec: a.Data["time_sec"].(float64),
			TempC: a.Data["temp_c"].(float64), RateMgPerSec: a.Data["rate"].(float64),
			ValleyTimeSec: valleyTime(a),
		})
	}
	groups := proposeBoundaries(dtg, peaks)

	// Enumerate all legal combinations of boundary variants (capped to keep
	// output readable).
	combos := enumerateCombos(groups)
	if len(combos) > 9 {
		combos = combos[:9]
	}

	tStart, tEnd := seriesExtent(rows)
	var out []*model.Artifact
	for ci, combo := range combos {
		// build boundaries: [start ... pair boundaries ... end]
		bounds := []boundary{{Variant: "run_start", TimeSec: tStart, TempC: tempAt(dtg, tStart)}}
		for gi, b := range combo {
			bounds = append(bounds, b)
			_ = gi
		}
		bounds = append(bounds, boundary{Variant: "run_end", TimeSec: tEnd, TempC: tempAt(dtg, tEnd)})

		for pi := range peaks {
			from, to := bounds[pi], bounds[pi+1]
			m0, ok0 := massAt(rows, from.TimeSec)
			m1, ok1 := massAt(rows, to.TimeSec)
			status := "ok"
			loss := m0 - m1
			if !ok0 || !ok1 {
				status = "unknown"
			}
			cand := &model.Artifact{
				Kind: "stage_candidate", Title: fmt.Sprintf("阶段 S%d · 候选 %s", pi+1, variantLabel(ci, len(combos))),
				Algorithm: AlgorithmVersion, ParamFP: params.Fingerprint,
				Sources: []model.SourceRef{
					artRef(cs, "corrected_series"),
					artRef(dtgArts[0], "dtg_series"),
				},
				CreatedSeq: cs.CreatedSeq, CreatedAt: cs.CreatedAt,
			}
			for _, pa := range peakArts {
				if int(pa.Data["index"].(int)) == peaks[pi].Index {
					cand.Sources = append(cand.Sources, artRef(pa, "dtg_peak"))
				}
			}
			cand.ID = e.hashID("stage", cs.ID, dtgArts[0].ID,
				fmt.Sprintf("%d", pi), fmt.Sprintf("%.0f", from.TimeSec), fmt.Sprintf("%.0f", to.TimeSec),
				fmt.Sprintf("c%d", ci))
			cand.Quantities = []model.Quantity{
				{Name: "t0", Label: "起始时间", Value: from.TimeSec, Unit: "s", State: "ok", Display: fmt.Sprintf("%.0f s (%.1f °C)", from.TimeSec, from.TempC)},
				{Name: "t1", Label: "结束时间", Value: to.TimeSec, Unit: "s", State: "ok", Display: fmt.Sprintf("%.0f s (%.1f °C)", to.TimeSec, to.TempC)},
				{Name: "peak_T", Label: "DTG 峰温", Value: peaks[pi].TempC, Unit: "°C", State: "ok", Display: fmt.Sprintf("%.1f °C", peaks[pi].TempC)},
				{Name: "loss", Label: "失重", Value: loss, Unit: "mg", State: status,
					Display: ternary(status == "ok", fmt.Sprintf("%.4f mg", loss), "未知（边界跨越缺口）")},
			}
			cand.Steps = []model.Step{
				{Label: "起点", Formula: from.Variant, Result: fmt.Sprintf("%.0f s", from.TimeSec)},
				{Label: "终点", Formula: to.Variant, Result: fmt.Sprintf("%.0f s", to.TimeSec)},
				{Label: "失重", Formula: "m(t0) - m(t1)", Result: fmt.Sprintf("%.4f mg（阶段可重叠，不强制总和=100%%）", loss)},
			}
			cand.Data = map[string]any{
				"stage_index": pi, "combo_index": ci, "combo_count": len(combos),
				"t0_sec": from.TimeSec, "t1_sec": to.TimeSec,
				"t0_variant": from.Variant, "t1_variant": to.Variant,
				"loss_mg": loss, "peak_time_sec": peaks[pi].TimeSec,
				"peak_temp_c": peaks[pi].TempC, "loss_state": status,
			}
			if ci > 0 {
				cand.Diags = append(cand.Diags, model.Diag{Code: "ALTERNATIVE_BOUNDARY", Severity: model.SevInfo,
					Message: "重叠峰无法唯一切分，本候选为保留的合法边界组合之一"})
			}
			out = append(out, cand)
			_ = stageCandidate{}
		}
	}
	return out
}

func valleyTime(a *model.Artifact) float64 {
	if v, ok := a.Data["saddle_time_sec"].(float64); ok {
		return v
	}
	return 0
}

func seriesExtent(rows []CorrectedPoint) (float64, float64) {
	first, last := math.MaxFloat64, -math.MaxFloat64
	for _, r := range rows {
		if !r.Valid {
			continue
		}
		first = math.Min(first, r.TimeSec)
		last = math.Max(last, r.TimeSec)
	}
	if first == math.MaxFloat64 {
		return 0, 0
	}
	return first, last
}

// enumerateCombos returns the cartesian product of boundary variant options.
func enumerateCombos(groups [][]boundary) [][]boundary {
	combos := [][]boundary{{}}
	for _, g := range groups {
		var next [][]boundary
		for _, c := range combos {
			for _, b := range g {
				cp := append(append([]boundary{}, c...), b)
				next = append(next, cp)
			}
		}
		combos = next
	}
	return combos
}

func variantLabel(i, n int) string {
	if n <= 1 {
		return "唯一"
	}
	return fmt.Sprintf("%d/%d", i+1, n)
}
