// Package domain: compute 是派生层。
//
// 校正链: correctedMass = rawMass - drift(t) - blank(t)
//   - drift(t) 由漂移锚点线性拟合(最小二乘); 锚点不足时退化为常数 0 并附 unknown。
//   - blank(t) 由空白曲线相邻点线性插值; 不做外推(超出空白支撑区间返回缺口)。
//   - 采样缺口(时间/质量无效, 或相邻时间不连续超过阈值)不跨越插值:
//     缺口处没有校正点, DTG 也不跨缺口计算。
//   - 非单调噪声: 校正后质量出现物理上不可能的上升(超阈值)时标记 nonmonotonic,
//     点保留, 不静默删除。
package domain

import (
	"math"
	"sort"
)

// 算法版本: 派生值必须携带。
const (
	AlgoCorrectedSeries = "corrected-series/v1"
	AlgoDTG             = "dtg/v1"
	AlgoStages          = "stages/v1"
	AlgoResidue         = "residue/v1"
)

// Params 是规范化后的计算参数; 由最新生效的 params 提交得到,
// 携带参数指纹所用的原始来源。
type Params struct {
	AtmoDelayMin       float64 `json:"atmoDelayMin"`
	TempLagMin         float64 `json:"tempLagMin"`
	DriftRateMgPerMin  float64 `json:"driftRateMgPerMin"`
	DriftOffsetMg      float64 `json:"driftOffsetMg"`
	BlankUse           bool    `json:"blankUse"`
	DtgWindowPoints    int     `json:"dtgWindowPoints"`
	PeakProminenceMg   float64 `json:"peakProminenceMg"`
	GapThresholdMin    float64 `json:"gapThresholdMin"`
	NonMonotoneTolMg   float64 `json:"nonMonotoneTolMg"`
	PeakMergeWindowMin float64 `json:"peakMergeWindowMin"`
	// Raw 保存被采纳的原始参数文本 key=value, 供指纹与溯源。
	Raw map[string]string `json:"raw,omitempty"`
}

// DefaultParams 返回基线参数(也用于参数缺失的样例)。
func DefaultParams() Params {
	return Params{
		AtmoDelayMin:       2.0,
		TempLagMin:         1.5,
		DriftRateMgPerMin:  0.0,
		DriftOffsetMg:      0.0,
		BlankUse:           true,
		DtgWindowPoints:    5,
		PeakProminenceMg:   0.05,
		GapThresholdMin:    1.5,
		NonMonotoneTolMg:   0.03,
		PeakMergeWindowMin: 8.0,
		Raw:                map[string]string{},
	}
}

// ResolveParams 把原始参数声明叠加到 base 上; 无法转换的值留下诊断而不静默采用。
func ResolveParams(base Params, raws []RawParam) (Params, []Diagnostic) {
	p := base
	diags := []Diagnostic{}
	set := func(key, text string, line int, apply func(float64)) {
		v, err := parseFloat(text)
		if err != nil {
			diags = append(diags, Diagnostic{
				Code: "bad_param", Line: line, Layer: "params",
				Message: "参数 " + key + "=" + text + " 无法解析, 保留默认/旧值",
			})
			return
		}
		if p.Raw == nil {
			p.Raw = map[string]string{}
		}
		p.Raw[key] = text
		apply(v)
	}
	for _, r := range raws {
		switch r.Key {
		case "atmoDelayMin":
			set(r.Key, r.RawText, r.Line, func(v float64) { p.AtmoDelayMin = v })
		case "tempLagMin":
			set(r.Key, r.RawText, r.Line, func(v float64) { p.TempLagMin = v })
		case "driftRateMgPerMin":
			set(r.Key, r.RawText, r.Line, func(v float64) { p.DriftRateMgPerMin = v })
		case "driftOffsetMg":
			set(r.Key, r.RawText, r.Line, func(v float64) { p.DriftOffsetMg = v })
		case "dtgWindowPoints":
			set(r.Key, r.RawText, r.Line, func(v float64) { p.DtgWindowPoints = int(math.Round(v)) })
		case "peakProminenceMg":
			set(r.Key, r.RawText, r.Line, func(v float64) { p.PeakProminenceMg = v })
		case "gapThresholdMin":
			set(r.Key, r.RawText, r.Line, func(v float64) { p.GapThresholdMin = v })
		case "nonMonotoneTolMg":
			set(r.Key, r.RawText, r.Line, func(v float64) { p.NonMonotoneTolMg = v })
		case "peakMergeWindowMin":
			set(r.Key, r.RawText, r.Line, func(v float64) { p.PeakMergeWindowMin = v })
		case "blankUse":
			if p.Raw == nil {
				p.Raw = map[string]string{}
			}
			p.Raw[r.Key] = r.RawText
			p.BlankUse = r.RawText == "true" || r.RawText == "1" || r.RawText == "yes"
		default:
			diags = append(diags, Diagnostic{
				Code: "unknown_param", Line: r.Line, Layer: "params",
				Message: "未知参数 " + r.Key + " 已保留但不参与计算",
			})
		}
	}
	if p.DtgWindowPoints < 2 {
		p.DtgWindowPoints = 2
	}
	return p, diags
}

func parseFloat(s string) (float64, error) {
	return strconvParseFloat(s)
}

// CorrectedPoint 是校正序列中的一个点。
type CorrectedPoint struct {
	Seq          int      `json:"seq"`
	TimeMin      float64  `json:"timeMin"`
	RawMassMg    float64  `json:"rawMassMg"`
	TempDegC     float64  `json:"tempDegC"`
	CorrectedMg  float64  `json:"correctedMg"`
	DriftMg      float64  `json:"driftMg"`
	BlankMg      float64  `json:"blankMg"`
	Gap          bool     `json:"gap"`
	NonMonotonic bool     `json:"nonMonotonic"`
	Flags        []string `json:"flags,omitempty"`
	RawSampleSeq int      `json:"rawSampleSeq"`
}

// GapSpan 描述一个未插值的采样缺口区间。
type GapSpan struct {
	AfterSeq int     `json:"afterSeq"`
	FromMin  float64 `json:"fromMin"`
	ToMin    float64 `json:"toMin"`
	Reason   string  `json:"reason"`
}

// CorrectedSeries 是校正链输出。
type CorrectedSeries struct {
	Points           []CorrectedPoint `json:"points"`
	Gaps             []GapSpan        `json:"gaps"`
	DriftModel       LinearModel      `json:"driftModel"`
	DriftFromAnchors bool             `json:"driftFromAnchors"`
	Diagnostics      []Diagnostic     `json:"diagnostics"`
}

// LinearModel 是 m(t)=rate*t+offset。
type LinearModel struct {
	Rate   float64 `json:"rate"`
	Offset float64 `json:"offset"`
	Known  bool    `json:"known"`
}

func (m LinearModel) At(t float64) float64 { return m.Rate*t + m.Offset }

// BuildCorrectedSeries 执行 raw -> drift -> blank 校正链, 并标记缺口/非单调。
//
// 直接来源: 采样点、漂移锚点、空白曲线(以及 params 的漂移模型/阈值)。
// 该结果不依赖样品初重: 初重修订不会使其过期。
func BuildCorrectedSeries(layers LayerData, params Params) CorrectedSeries {
	out := CorrectedSeries{}
	drift, fromAnchors, ddiags := fitDrift(layers.DriftAnchors, params)
	out.DriftModel = drift
	out.DriftFromAnchors = fromAnchors
	out.Diagnostics = append(out.Diagnostics, ddiags...)

	blankPts := sortedBlank(layers.Blank)
	lastT := math.NaN()
	lastCorrected := math.NaN()
	for _, s := range layers.Samples {
		cp := CorrectedPoint{Seq: s.Seq, RawSampleSeq: s.Seq}
		// 时间无效 => 结构性缺口点(保留在序列中但不绘图为正常值)
		if s.Time.Diagnostic != "" {
			cp.Gap = true
			cp.Flags = append(cp.Flags, "time_unparseable")
			cp.TimeMin = math.NaN()
			out.Points = append(out.Points, cp)
			out.Diagnostics = append(out.Diagnostics, Diagnostic{
				Code: "sample_gap", Layer: "samples",
				Message: "采样点 seq=" + itoa(s.Seq) + " 时间无法解析, 不插值",
			})
			continue
		}
		cp.TimeMin = s.Time.Minutes
		// 采样间隔超阈值 => 缺口(不跨越插值)
		if !math.IsNaN(lastT) && s.Time.Minutes-lastT > params.GapThresholdMin {
			g := GapSpan{
				AfterSeq: out.Points[len(out.Points)-1].Seq,
				FromMin:  lastT, ToMin: s.Time.Minutes,
				Reason: "采样间隔 " + ftoa(s.Time.Minutes-lastT) +
					" min 超过阈值 " + ftoa(params.GapThresholdMin) + " min, 不跨越插值",
			}
			out.Gaps = append(out.Gaps, g)
			out.Diagnostics = append(out.Diagnostics, Diagnostic{
				Code: "sample_gap", Layer: "samples", Message: g.Reason,
			})
			// 当前点仍参与, 但 DTG 不允许跨越(由 prevConnected 控制)
			cp.Gap = true
			cp.Flags = append(cp.Flags, "gap_start")
		}
		switch {
		case s.Mass.Status == StatusInvalid:
			cp.Gap = true
			cp.Flags = append(cp.Flags, "mass_invalid")
			out.Diagnostics = append(out.Diagnostics, Diagnostic{
				Code: "mass_invalid", Layer: "samples",
				Message: "seq=" + itoa(s.Seq) + " 质量无法转换, 原值保留, 不插值",
			})
		case s.Mass.Status == StatusUnknown:
			cp.Gap = true
			cp.Flags = append(cp.Flags, "mass_missing")
		default:
			cp.RawMassMg = s.Mass.Value
		}
		if s.Temp.Status == StatusOK {
			cp.TempDegC = s.Temp.Value
		} else {
			cp.Flags = append(cp.Flags, "temp_unknown")
		}
		if !cp.Gap && s.Mass.Status == StatusOK {
			cp.DriftMg = drift.At(cp.TimeMin)
			cp.BlankMg = 0
			if params.BlankUse && len(blankPts) > 0 {
				bv, ok := interpBlank(blankPts, cp.TimeMin)
				if !ok {
					cp.Gap = true
					cp.Flags = append(cp.Flags, "blank_outside_support")
				} else {
					cp.BlankMg = bv
				}
			}
			if !cp.Gap {
				cp.CorrectedMg = cp.RawMassMg - cp.DriftMg - cp.BlankMg
				if !math.IsNaN(lastCorrected) &&
					cp.CorrectedMg > lastCorrected+params.NonMonotoneTolMg {
					cp.NonMonotonic = true
					cp.Flags = append(cp.Flags, "nonmonotonic")
					out.Diagnostics = append(out.Diagnostics, Diagnostic{
						Code: "nonmonotonic", Layer: "samples",
						Message: "seq=" + itoa(s.Seq) + " 校正质量异常上升 " +
							ftoa(cp.CorrectedMg-lastCorrected) + " mg (非单调噪声, 点保留)",
					})
				}
				lastCorrected = cp.CorrectedMg
			}
		}
		lastT = s.Time.Minutes
		out.Points = append(out.Points, cp)
	}
	return out
}

// fitDrift 优先用漂移锚点做最小二乘线性拟合; 锚点不足两个时使用参数模型,
// 参数模型 rate=offset=0 时标记 unknown(无法量化漂移)。
func fitDrift(anchors []DriftAnchor, params Params) (LinearModel, bool, []Diagnostic) {
	var xs, ys []float64
	for _, a := range anchors {
		if a.Time.Diagnostic != "" || a.Offset.Status != StatusOK {
			continue
		}
		xs = append(xs, a.Time.Minutes)
		ys = append(ys, a.Offset.Value)
	}
	if len(xs) >= 2 {
		return leastSquares(xs, ys), true, nil
	}
	diags := []Diagnostic{}
	if len(xs) == 1 {
		diags = append(diags, Diagnostic{
			Code: "drift_single_anchor", Layer: "drift",
			Message: "仅有一个漂移锚点, 无法拟合斜率, 退化为参数漂移模型",
		})
	} else {
		diags = append(diags, Diagnostic{
			Code: "drift_unknown", Layer: "drift",
			Message: "没有有效漂移锚点, 使用参数漂移模型(可能为零, 状态未知)",
		})
	}
	return LinearModel{
		Rate: params.DriftRateMgPerMin, Offset: params.DriftOffsetMg, Known: false,
	}, false, diags
}

func leastSquares(xs, ys []float64) LinearModel {
	n := float64(len(xs))
	var sx, sy, sxx, sxy float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
		sxx += xs[i] * xs[i]
		sxy += xs[i] * ys[i]
	}
	den := n*sxx - sx*sx
	if math.Abs(den) < 1e-12 {
		return LinearModel{Offset: sy / n, Known: true}
	}
	rate := (n*sxy - sx*sy) / den
	offset := (sy - rate*sx) / n
	return LinearModel{Rate: rate, Offset: offset, Known: true}
}

type blankPt struct {
	t, m float64
}

func sortedBlank(blank []BlankPoint) []blankPt {
	var pts []blankPt
	for _, b := range blank {
		if b.Time.Diagnostic != "" || b.Mass.Status != StatusOK {
			continue
		}
		pts = append(pts, blankPt{b.Time.Minutes, b.Mass.Value})
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].t < pts[j].t })
	return pts
}

// interpBlank 仅在空白曲线支撑区间内做相邻线性插值; 不做外推。
func interpBlank(pts []blankPt, t float64) (float64, bool) {
	if t < pts[0].t || t > pts[len(pts)-1].t {
		return 0, false
	}
	for i := 0; i+1 < len(pts); i++ {
		if t >= pts[i].t && t <= pts[i+1].t {
			if pts[i+1].t == pts[i].t {
				return pts[i].m, true
			}
			frac := (t - pts[i].t) / (pts[i+1].t - pts[i].t)
			return pts[i].m + frac*(pts[i+1].m-pts[i].m), true
		}
	}
	return pts[len(pts)-1].m, true
}
