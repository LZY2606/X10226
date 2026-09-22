package engine

import (
	"fmt"
	"math"
	"sort"

	"tga/internal/model"
)

// gap is a detected acquisition hole between two valid samples.
type gap struct {
	Index        int
	T0Sec, T1Sec float64
	T0C, T1C     float64
	DtSec        float64
	MedianDtSec  float64
}

func detectGaps(rows []CorrectedPoint, gapFactor float64) []gap {
	var dts []float64
	for i := 1; i < len(rows); i++ {
		if rows[i].Valid && rows[i-1].Valid {
			dts = append(dts, rows[i].TimeSec-rows[i-1].TimeSec)
		}
	}
	if len(dts) == 0 {
		return nil
	}
	med := median(dts)
	var gaps []gap
	for i := 1; i < len(rows); i++ {
		if !rows[i].Valid || !rows[i-1].Valid {
			continue
		}
		dt := rows[i].TimeSec - rows[i-1].TimeSec
		if dt > gapFactor*med && dt > med+1e-9 {
			gaps = append(gaps, gap{
				Index: i, T0Sec: rows[i-1].TimeSec, T1Sec: rows[i].TimeSec,
				T0C: rows[i-1].TempC, T1C: rows[i].TempC,
				DtSec: dt, MedianDtSec: med,
			})
		}
	}
	return gaps
}

func median(xs []float64) float64 {
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

func (e *Engine) gapArtifacts(samples *model.Submission, params *model.Params) []*model.Artifact {
	rows := samplesRows(samples)
	_ = rows
	csRows := samplesCorrectedRows(samples)
	factor := num(params.Values["gap_factor"], 2.0)
	gaps := detectGaps(csRows, factor)
	out := make([]*model.Artifact, 0, len(gaps))
	for i, g := range gaps {
		a := &model.Artifact{
			Kind: "data_gap", Title: fmt.Sprintf("采样缺口 #%d", i+1),
			Algorithm: AlgorithmVersion, ParamFP: params.Fingerprint,
			Sources:    []model.SourceRef{e.srcRef(samples, "mass_samples")},
			CreatedSeq: samples.EventSeq, CreatedAt: samples.CreatedAt,
		}
		a.ID = e.hashID("data_gap", fmt.Sprint(samples.EventSeq), fmt.Sprintf("%.0f", g.T0Sec), fmt.Sprintf("%.0f", g.T1Sec))
		rng := [2]float64{g.T0Sec, g.T1Sec}
		a.Quantities = []model.Quantity{
			{Name: "t0", Label: "缺口起点", Value: g.T0Sec, Unit: "s", State: "ok", Display: fmt.Sprintf("%.0f s (%.1f °C)", g.T0Sec, g.T0C)},
			{Name: "t1", Label: "缺口终点", Value: g.T1Sec, Unit: "s", State: "ok", Display: fmt.Sprintf("%.0f s (%.1f °C)", g.T1Sec, g.T1C)},
			{Name: "dt", Label: "缺口宽度", Value: g.DtSec, Unit: "s", State: "ok", Display: fmt.Sprintf("%.0f s (中位步长 %.0f s)", g.DtSec, g.MedianDtSec), Range: &rng},
		}
		a.Steps = []model.Step{
			{Label: "规则", Formula: fmt.Sprintf("dt > %.1f × median(dt)", factor), Result: "缺口内不做插值，DTG 在缺口两侧断开"},
		}
		a.Data = map[string]any{"t0_sec": g.T0Sec, "t1_sec": g.T1Sec, "t0_c": g.T0C, "t1_c": g.T1C,
			"dt_sec": g.DtSec, "median_dt_sec": g.MedianDtSec, "no_bridge": true}
		out = append(out, a)
	}
	return out
}

func samplesRows(s *model.Submission) []model.Point { return s.Samples }

func samplesCorrectedRows(s *model.Submission) []CorrectedPoint {
	out := make([]CorrectedPoint, 0, len(s.Samples))
	for _, p := range s.Samples {
		cp := CorrectedPoint{Seq: p.Seq, TimeSec: p.TimeSec, TempC: p.TempC, RawMg: p.MassMg, Valid: validPoint(p), Diags: p.Diags}
		out = append(out, cp)
	}
	return out
}

// NonMonotoneRun is one contiguous mass-increasing (or noisy) segment.
type NonMonotoneRun struct {
	StartIdx, EndIdx int
	T0Sec, T1Sec     float64
	MaxUpMg          float64
	Count            int
}

func detectNonMonotone(rows []CorrectedPoint) []NonMonotoneRun {
	var runs []NonMonotoneRun
	var cur *NonMonotoneRun
	for i := 1; i < len(rows); i++ {
		if !rows[i].Valid || !rows[i-1].Valid {
			cur = nil
			continue
		}
		diff := rows[i].CorrMg - rows[i-1].CorrMg
		if diff > 0.002 { // >2 µg increase counts as non-monotone behavior
			if cur == nil {
				cur = &NonMonotoneRun{StartIdx: i - 1, T0Sec: rows[i-1].TimeSec}
			}
			cur.EndIdx = i
			cur.T1Sec = rows[i].TimeSec
			cur.MaxUpMg = math.Max(cur.MaxUpMg, diff)
			cur.Count++
		} else {
			if cur != nil && cur.Count >= 2 {
				runs = append(runs, *cur)
			}
			cur = nil
		}
	}
	if cur != nil && cur.Count >= 2 {
		runs = append(runs, *cur)
	}
	return runs
}

func (e *Engine) nonMonotoneArtifacts(cs *model.Artifact) []*model.Artifact {
	rows := cs.Data["rows"].([]CorrectedPoint)
	runs := detectNonMonotone(rows)
	out := make([]*model.Artifact, 0, len(runs))
	for i, r := range runs {
		a := &model.Artifact{
			Kind: "nonmonotone_segment", Title: fmt.Sprintf("非单调噪声段 #%d", i+1),
			Algorithm:  AlgorithmVersion,
			ParamFP:    cs.ParamFP,
			Sources:    []model.SourceRef{artRef(cs, "corrected_series")},
			CreatedSeq: cs.CreatedSeq, CreatedAt: cs.CreatedAt,
		}
		a.ID = e.hashID("nonmonotone", cs.ID, fmt.Sprintf("%.0f", r.T0Sec), fmt.Sprintf("%.0f", r.T1Sec))
		a.Quantities = []model.Quantity{
			{Name: "t0", Label: "段开始", Value: r.T0Sec, Unit: "s", State: "ok", Display: fmt.Sprintf("%.0f s", r.T0Sec)},
			{Name: "t1", Label: "段结束", Value: r.T1Sec, Unit: "s", State: "ok", Display: fmt.Sprintf("%.0f s", r.T1Sec)},
			{Name: "max_up", Label: "最大回升", Value: r.MaxUpMg, Unit: "mg", State: "ok", Display: fmt.Sprintf("%.4f mg", r.MaxUpMg)},
		}
		a.Steps = []model.Step{{Label: "规则", Formula: "Δm > +0.002 mg 连续 ≥2 点", Result: "标记为非单调；不强行单调化"}}
		a.Data = map[string]any{"t0_sec": r.T0Sec, "t1_sec": r.T1Sec, "max_up_mg": r.MaxUpMg, "points": r.Count}
		out = append(out, a)
	}
	return out
}

// DTGPoint is a derived DTG sample. Unknown across a gap => State=unknown.
type DTGPoint struct {
	TimeSec, TempC, DtgMgPerSec float64
	State                       string
	AcrossGap                   bool
}

func buildDTG(rows []CorrectedPoint, gaps []gap) []DTGPoint {
	gapAfter := map[int]bool{}
	for _, g := range gaps {
		gapAfter[g.Index-1] = true
	}
	out := make([]DTGPoint, 0, len(rows))
	for i := 0; i < len(rows); i++ {
		if !rows[i].Valid {
			out = append(out, DTGPoint{TimeSec: rows[i].TimeSec, TempC: rows[i].TempC, State: "unknown"})
			continue
		}
		var d float64
		state, across := "ok", false
		switch {
		case i > 0 && i < len(rows)-1 && rows[i-1].Valid && rows[i+1].Valid && !gapAfter[i-1] && !gapAfter[i]:
			d = (rows[i+1].CorrMg - rows[i-1].CorrMg) / (rows[i+1].TimeSec - rows[i-1].TimeSec)
		case i > 0 && rows[i-1].Valid && !gapAfter[i-1]:
			d = (rows[i].CorrMg - rows[i-1].CorrMg) / (rows[i].TimeSec - rows[i-1].TimeSec)
		case i < len(rows)-1 && rows[i+1].Valid && !gapAfter[i]:
			d = (rows[i+1].CorrMg - rows[i].CorrMg) / (rows[i+1].TimeSec - rows[i].TimeSec)
		default:
			state, across = "unknown", true
		}
		out = append(out, DTGPoint{TimeSec: rows[i].TimeSec, TempC: rows[i].TempC, DtgMgPerSec: d, State: state, AcrossGap: across})
	}
	return out
}

func (e *Engine) dtgArtifacts(cs *model.Artifact, params *model.Params, gapArts []*model.Artifact) []*model.Artifact {
	rows := cs.Data["rows"].([]CorrectedPoint)
	var gs []gap
	for _, ga := range gapArts {
		gs = append(gs, gap{
			T0Sec: ga.Data["t0_sec"].(float64), T1Sec: ga.Data["t1_sec"].(float64),
		})
	}
	dtg := buildDTG(rows, gs)
	a := &model.Artifact{
		Kind: "dtg_series", Title: "DTG 序列（中心差分）",
		Algorithm: AlgorithmVersion, ParamFP: params.Fingerprint,
		Sources:    []model.SourceRef{artRef(cs, "corrected_series")},
		CreatedSeq: cs.CreatedSeq, CreatedAt: cs.CreatedAt,
	}
	a.ID = e.hashID("dtg_series", cs.ID, params.Fingerprint)
	unknown := 0
	for _, p := range dtg {
		if p.State != "ok" {
			unknown++
		}
	}
	a.Quantities = []model.Quantity{
		{Name: "points", Label: "DTG 点", Value: float64(len(dtg)), Unit: "points", State: "ok", Display: fmt.Sprintf("%d 点", len(dtg))},
		{Name: "unknown", Label: "未知点（缺口/失败行）", Value: float64(unknown), Unit: "points", State: ternary(unknown > 0, "unknown", "ok"), Display: fmt.Sprintf("%d 点", unknown)},
	}
	a.Steps = []model.Step{{Label: "公式", Formula: "dm/dt ≈ [m(t+1) - m(t-1)] / [t+1 - t-1]", Result: "中心差分；绝不跨越缺口"}}
	a.Data = map[string]any{"points": dtg}
	return []*model.Artifact{a}
}
