package domain

import "fmt"

// buildResidues emits one residue per meaningful point of the TG curve:
// post each stage end, and the final residual. Every residue carries an
// expandable computation chain back to the raw layer.
func buildResidues(in AnalysisInput, series []SeriesPoint, segs [][]int,
	p Params, m0Mg float64, m0OK bool, m0SubID string,
	slope, intercept float64, driftSrc, outID string, dseq int, projectID string) []Residue {

	var residues []Residue
	add := func(idx, n int, label string) {
		sp := series[idx]
		rid := ResidueID(projectID, dseq, n)
		r := Residue{
			ID: rid, Key: fmt.Sprintf("R%d", n), Label: label,
			AtS: sp.TimeS, AtTempC: sp.ReactTempC, Mg: sp.MassMgCorr,
			Status: "pending_review",
		}
		if m0OK {
			r.Pct = sp.MassPct
			r.ErrorPct = p.NoiseSigma / m0Mg * 100
		} else {
			r.Unknown = true
		}
		r.Chain = []ChainStep{
			{Label: "原始采样点", Value: fmt.Sprintf("line %d @ %.0f s, %.2f °C", sp.Line, sp.TimeS, sp.SensorTempC),
				SourceKind: "submission", Ref: fmt.Sprintf("%s-P%04d", outID, sp.Line)},
			{Label: "天平漂移校正", Value: fmt.Sprintf("slope %.5f mg/s, intercept %.4f mg", slope, intercept),
				SourceKind: "submission", Ref: driftSrc,
				Formula: "mass_corr = mass_raw - (b0 + b1*(t-t_first))"},
			{Label: "样品初重", Value: fmt.Sprintf("%.3f mg", m0Mg),
				SourceKind: "submission", Ref: m0SubID},
			{Label: "残留率", Value: fmt.Sprintf("%.3f mg / %.3f mg * 100 = %.2f %%", sp.MassMgCorr, m0Mg, r.Pct),
				SourceKind: "derived", Ref: rid, Formula: "mass_corr / m0 * 100"},
			{Label: "温度坐标基准", Value: fmt.Sprintf("传感器 %.2f °C -> 反应温度 %.2f °C (lag %.0f s)",
				sp.SensorTempC, sp.ReactTempC, p.TempLagS),
				SourceKind: "param", Ref: "params:" + p.Algorithm},
		}
		residues = append(residues, r)
	}

	// one residue at the final point of each contiguous segment
	n := 0
	for _, seg := range segs {
		n++
		label := "段末残留率"
		if seg[len(seg)-1] == lastValidIndex(series) {
			label = "最终残留率"
		}
		add(seg[len(seg)-1], n, label)
	}
	if len(residues) == 0 {
		residues = append(residues, Residue{
			ID: ResidueID(projectID, dseq, 1), Key: "R1",
			Label: "残留率（未知：无有效采样点）", Unknown: true, Status: "pending_review",
		})
	}
	return residues
}

func lastValidIndex(series []SeriesPoint) int {
	for i := len(series) - 1; i >= 0; i-- {
		if !series[i].Excluded {
			return i
		}
	}
	return -1
}
