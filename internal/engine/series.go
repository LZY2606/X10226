package engine

import (
	"fmt"
	"math"

	"tga/internal/model"
)

// validPoint requires every conversion to have succeeded.
func validPoint(p model.Point) bool { return p.TimeOK && p.TempOK && p.MassOK }

// fitLine fits y = a + b*x by least squares.
func fitLine(xs, ys []float64) (a, b float64) {
	var sx, sy, sxx, sxy float64
	n := float64(len(xs))
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
		sxx += xs[i] * xs[i]
		sxy += xs[i] * xs[i] * 0
		sxy += xs[i] * ys[i]
	}
	den := n*sxx - sx*sx
	if math.Abs(den) < 1e-18 {
		return sy / n, 0
	}
	b = (n*sxy - sx*sy) / den
	a = (sy - b*sx) / n
	return a, b
}

// fitQuadratic fits y = a + b*x + c*x^2 via 3x3 normal equations.
func fitQuadratic(xs, ys []float64) (a, b, c float64) {
	var S [4]float64
	var T [3]float64
	for i := range xs {
		x, y := xs[i], ys[i]
		x2, x3, x4 := x*x, x*x*x, x*x*x*x
		S[0] += 1
		S[1] += x
		S[2] += x2
		S[3] += x3
		_ = x4
		T[0] += y
		T[1] += x * y
		T[2] += x2 * y
	}
	// matrix
	A := [3][3]float64{
		{S[0], S[1], S[2]},
		{S[1], S[2], S[3]},
		{S[2], S[3], sumPow4(xs)},
	}
	coef := solve3(A, T)
	return coef[0], coef[1], coef[2]
}

func sumPow4(xs []float64) float64 {
	v := 0.0
	for _, x := range xs {
		v += x * x * x * x
	}
	return v
}

func solve3(A [3][3]float64, d [3]float64) [3]float64 {
	for col := 0; col < 3; col++ {
		piv := col
		for r := col + 1; r < 3; r++ {
			if math.Abs(A[r][col]) > math.Abs(A[piv][col]) {
				piv = r
			}
		}
		A[col], A[piv] = A[piv], A[col]
		d[col], d[piv] = d[piv], d[col]
		if math.Abs(A[col][col]) < 1e-18 {
			return d
		}
		for r := 0; r < 3; r++ {
			if r == col {
				continue
			}
			f := A[r][col] / A[col][col]
			for k := col; k < 3; k++ {
				A[r][k] -= f * A[col][k]
			}
			d[r] -= f * d[col]
		}
	}
	var x [3]float64
	for i := 0; i < 3; i++ {
		x[i] = d[i] / A[i][i]
	}
	return x
}

// interpBlank evaluates the fitted blank baseline at time t.
type blankFit struct {
	model   string
	a, b, c float64
	times   []float64
	masses  []float64
}

func (f blankFit) valueAt(t float64) float64 {
	if f.model == "quadratic" {
		return f.a + f.b*t + f.c*t*t
	}
	return f.a + f.b*t
}

func (e *Engine) blankModelArtifact(blank *model.Submission, params *model.Params) *model.Artifact {
	var xs, ys []float64
	for _, p := range blank.Blank {
		if validPoint(p) {
			xs = append(xs, p.TimeSec)
			ys = append(ys, p.MassMg)
		}
	}
	fit := blankFit{model: params.Values["drift_model"]}
	a := &model.Artifact{
		Kind: "blank_model", Title: "天平基线模型",
		Algorithm: AlgorithmVersion, ParamFP: params.Fingerprint,
		Sources:    []model.SourceRef{e.srcRef(blank, "blank_curve")},
		CreatedSeq: blank.EventSeq, CreatedAt: blank.CreatedAt,
	}
	idParams := ""
	switch fit.model {
	case "quadratic":
		fit.a, fit.b, fit.c = fitQuadratic(xs, ys)
		idParams = fmt.Sprintf("Q%.6f|%.9f|%.12f", fit.a, fit.b, fit.c)
	default:
		fit.model = "linear"
		fit.a, fit.b = fitLine(xs, ys)
		idParams = fmt.Sprintf("L%.6f|%.9f", fit.a, fit.b)
	}
	a.ID = e.hashID("blank_model", fmt.Sprint(blank.EventSeq), params.Fingerprint, fit.model)
	a.Quantities = []model.Quantity{
		{Name: "intercept", Label: "截距 a", Value: fit.a, Unit: "mg", State: "ok", Display: fmt.Sprintf("%.4f mg", fit.a)},
		{Name: "slope", Label: "斜率 b", Value: fit.b, Unit: "mg/s", State: "ok", Display: fmt.Sprintf("%.2e mg/s", fit.b)},
	}
	if fit.model == "quadratic" {
		a.Quantities = append(a.Quantities, model.Quantity{Name: "quadratic", Label: "二次项 c", Value: fit.c, Unit: "mg/s^2", State: "ok", Display: fmt.Sprintf("%.2e mg/s²", fit.c)})
	}
	a.Steps = []model.Step{
		{Label: "输入", Result: fmt.Sprintf("%d 个空白曲线点 (来源 seq=%d)", len(xs), blank.EventSeq),
			Inputs: []model.StepInput{{Label: "空白曲线", Display: blank.ID}}},
		{Label: "拟合", Formula: fit.model, Result: idParams},
	}
	a.Data = map[string]any{
		"model": fit.model, "a": fit.a, "b": fit.b, "c": fit.c,
		"points": len(xs),
	}
	if blank.Status == "partial" {
		a.Status = "partial"
		a.Diags = append(a.Diags, model.Diag{Code: "BLANK_PARTIAL_IMPORT", Severity: model.SevWarn, Message: "空白曲线导入存在失败行，仅用可转换点拟合"})
	}
	return a
}

// CorrectedPoint is one row of the derived corrected series.
type CorrectedPoint struct {
	Seq                           int
	TimeSec, TempC, RawMg, CorrMg float64
	BlankMg                       float64
	Valid                         bool
	Diags                         []model.Diag
}

func (e *Engine) correctedSeriesArtifact(samples, blank *model.Submission, params *model.Params) *model.Artifact {
	sources := []model.SourceRef{e.srcRef(samples, "mass_samples")}
	var fit blankFit
	fit.model = params.Values["drift_model"]
	if blank != nil {
		sources = append(sources, e.srcRef(blank, "blank_curve"))
		var xs, ys []float64
		for _, p := range blank.Blank {
			if validPoint(p) {
				xs = append(xs, p.TimeSec)
				ys = append(ys, p.MassMg)
			}
		}
		switch fit.model {
		case "quadratic":
			fit.a, fit.b, fit.c = fitQuadratic(xs, ys)
		default:
			fit.model = "linear"
			fit.a, fit.b = fitLine(xs, ys)
		}
	}
	rows := make([]CorrectedPoint, 0, len(samples.Samples))
	good, bad := 0, 0
	for _, p := range samples.Samples {
		cp := CorrectedPoint{Seq: p.Seq, TimeSec: p.TimeSec, TempC: p.TempC, RawMg: p.MassMg}
		if !validPoint(p) {
			cp.Valid = false
			cp.Diags = p.Diags
			bad++
		} else {
			cp.BlankMg = fit.valueAt(p.TimeSec)
			cp.CorrMg = p.MassMg - cp.BlankMg
			cp.Valid = true
			good++
		}
		rows = append(rows, cp)
	}
	a := &model.Artifact{
		Kind: "corrected_series", Title: "漂移校正后 TG 序列",
		Algorithm: AlgorithmVersion, ParamFP: params.Fingerprint,
		Sources: sources, CreatedSeq: samples.EventSeq, CreatedAt: samples.CreatedAt,
	}
	a.ID = e.hashID("corrected_series", sourceSeqKey(sources), params.Fingerprint)
	driftTotal := 0.0
	if len(rows) > 1 {
		first := fit.valueAt(rows[0].TimeSec)
		last := fit.valueAt(rows[len(rows)-1].TimeSec)
		driftTotal = last - first
	}
	a.Quantities = []model.Quantity{
		{Name: "good_rows", Label: "有效点", Value: float64(good), Unit: "points", State: "ok", Display: fmt.Sprintf("%d 点", good)},
		{Name: "bad_rows", Label: "失败点", Value: float64(bad), Unit: "points", State: ternary(bad > 0, "unknown", "ok"), Display: fmt.Sprintf("%d 点", bad)},
		{Name: "drift_span", Label: "全程序基线漂移", Value: driftTotal, Unit: "mg", State: "ok", Display: fmt.Sprintf("%+.4f mg", driftTotal)},
	}
	a.Steps = []model.Step{
		{Label: "公式", Formula: "m_corr(t) = m_raw(t) - b(t)", Result: "逐点扣除空白基线"},
		{Label: "漂移模型", Result: fit.model, Inputs: []model.StepInput{{Label: "参数指纹", Display: params.Fingerprint}}},
	}
	a.Data = map[string]any{"rows": rows}
	if bad > 0 {
		a.Status = "partial"
		a.Diags = append(a.Diags, model.Diag{Code: "PARTIAL_IMPORT", Severity: model.SevWarn, Message: fmt.Sprintf("%d 个采样点不可转换，保留原位且不参与计算", bad)})
	}
	return a
}

func ternary(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func sourceSeqKey(ss []model.SourceRef) string {
	key := ""
	for _, s := range ss {
		key += fmt.Sprintf("%s@%d;", s.Role, s.EventSeq)
	}
	return key
}
