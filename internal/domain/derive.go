package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// AlgoVersion 派生算法版本；任何算法变更都必须提升版本号。
const AlgoVersion = "tga-derive/v1"

// Params 是派生参数集；参数指纹用于绑定每个派生值。
type Params struct {
	AtmosphereDelayS float64 `json:"atmosphere_delay_s"` // 气体输运延迟
	TempLagS         float64 `json:"temp_lag_s"`         // 温度滞后
	GapFactor        float64 `json:"gap_factor"`         // 缺口判定：间隔 > GapFactor * 中位间隔
	SmoothWindow     int     `json:"smooth_window"`      // DTG 平滑窗口（点数，奇数）
	ProminencePctMin float64 `json:"prominence_pct_min"` // 峰显著度阈值 (%/min)
	MinStageWidthS   float64 `json:"min_stage_width_s"`
}

// DefaultParams 返回默认参数。
func DefaultParams() Params {
	return Params{
		AtmosphereDelayS: 45,
		TempLagS:         20,
		GapFactor:        3.0,
		SmoothWindow:     7,
		ProminencePctMin: 0.15,
		MinStageWidthS:   60,
	}
}

// Fingerprint 计算参数指纹（规范 JSON 的 SHA-256）。
func Fingerprint(p Params) string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// Point 是规范化后的曲线点。Unconvertible 为 true 时原值保留但标记。
type Point struct {
	T            float64 `json:"t"`   // s
	Mass         float64 `json:"mass"`// mg（已校正）
	RawMass      float64 `json:"raw_mass"`
	Temp         float64 `json:"temp"`// C
	Row          int     `json:"row"`
	Unconvertible bool   `json:"unconvertible,omitempty"`
}

// CorrectedCurve 是漂移/空白校正后的规范曲线。
type CorrectedCurve struct {
	Points      []Point      `json:"points"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	BlankUsed   bool         `json:"blank_used"`
	DriftUsed   bool         `json:"drift_used"`
}

// BuildCorrectedCurve 由原始采样点 + 漂移校正 + 空白曲线构造规范曲线。
// 无法转换的值留在原位置并附诊断，不为了画图丢弃。
func BuildCorrectedCurve(raw *RawPointsDoc, drift *DriftCorrection, blank *BlankCurve) (*CorrectedCurve, []Judgment) {
	cc := &CorrectedCurve{}
	var judgments []Judgment
	var slopeMgPerS, interceptMg float64
	if drift != nil {
		cc.DriftUsed = true
		sv, d1 := MustCanonical(drift.Slope, DimMass, "drift.slope")
		if d1 != nil {
			cc.Diagnostics = append(cc.Diagnostics, *d1)
		}
		// 斜率单位按 mg/min 解释（漂移模型约定），换算到 mg/s。
		slopeMgPerS = sv / 60
		iv, d2 := MustCanonical(drift.Intercept, DimMass, "drift.intercept")
		if d2 != nil {
			cc.Diagnostics = append(cc.Diagnostics, *d2)
		}
		interceptMg = iv
		judgments = append(judgments, Judgment{
			Code:    JDriftApplied,
			Subject: InputDrift,
			Detail: map[string]any{
				"model":            drift.Model,
				"slope_mg_per_min": sv,
				"intercept_mg":     iv,
			},
		})
	}
	type blankPt struct{ t, m float64 }
	var blankNorm []blankPt
	if blank != nil && len(blank.Points) > 0 {
		cc.BlankUsed = true
		for _, bp := range blank.Points {
			t, d1 := MustCanonical(bp.Time, DimTime, "blank.time")
			m, d2 := MustCanonical(bp.Mass, DimMass, "blank.mass")
			if d1 != nil || d2 != nil {
				continue // 无法转换的空白点不参与插值，但不影响其他点
			}
			blankNorm = append(blankNorm, blankPt{t, m})
		}
		sort.Slice(blankNorm, func(i, j int) bool { return blankNorm[i].t < blankNorm[j].t })
	}
	blankAt := func(t float64) (float64, bool) {
		if len(blankNorm) == 0 || t < blankNorm[0].t || t > blankNorm[len(blankNorm)-1].t {
			return 0, false // 不在空白曲线范围内：不外推
		}
		for i := 1; i < len(blankNorm); i++ {
			if t <= blankNorm[i].t {
				a, b := blankNorm[i-1], blankNorm[i]
				if b.t == a.t {
					return b.m, true
				}
				f := (t - a.t) / (b.t - a.t)
				return a.m + f*(b.m-a.m), true
			}
		}
		return 0, false
	}
	for _, rp := range raw.Points {
		t, d1 := MustCanonical(rp.Time, DimTime, fmt.Sprintf("row %d time", rp.Row))
		m, d2 := MustCanonical(rp.Mass, DimMass, fmt.Sprintf("row %d mass", rp.Row))
		tp, d3 := MustCanonical(rp.Temp, DimTemp, fmt.Sprintf("row %d temp", rp.Row))
		bad := d1 != nil || d2 != nil || d3 != nil
		for _, d := range []*Diagnostic{d1, d2, d3} {
			if d != nil {
				cc.Diagnostics = append(cc.Diagnostics, *d)
			}
		}
		corr := m
		if !bad {
			corr = m - (interceptMg + slopeMgPerS*t)
			if bm, ok := blankAt(t); ok {
				corr -= bm
			}
		}
		cc.Points = append(cc.Points, Point{T: t, Mass: corr, RawMass: m, Temp: tp, Row: rp.Row, Unconvertible: bad})
	}
	sort.Slice(cc.Points, func(i, j int) bool { return cc.Points[i].T < cc.Points[j].T })
	return cc, judgments
}

// Segment 是由采样缺口切分出的连续区间；缺口不跨越插值。
type Segment struct {
	Index     int     `json:"index"`
	StartIdx  int     `json:"start_idx"`
	EndIdx    int     `json:"end_idx"` // 含
	StartT    float64 `json:"start_t"`
	EndT      float64 `json:"end_t"`
	GapAfterS float64 `json:"gap_after_s,omitempty"`
}

// FindSegments 按缺口切分曲线。判定事件：数据缺口不跨越插值。
func FindSegments(cc *CorrectedCurve, p Params) ([]Segment, []Judgment) {
	pts := usablePoints(cc)
	if len(pts) == 0 {
		return nil, nil
	}
	var dts []float64
	for i := 1; i < len(pts); i++ {
		dts = append(dts, pts[i].T-pts[i-1].T)
	}
	median := medianOf(dts)
	threshold := p.GapFactor * median
	var segs []Segment
	start := 0
	var judgments []Judgment
	for i := 1; i < len(pts); i++ {
		dt := pts[i].T - pts[i-1].T
		if dt > threshold {
			segs = append(segs, Segment{
				Index: len(segs), StartIdx: start, EndIdx: i - 1,
				StartT: pts[start].T, EndT: pts[i-1].T, GapAfterS: dt,
			})
			judgments = append(judgments, Judgment{
				Code:    JGapNoInterp,
				Subject: fmt.Sprintf("gap@%gs", pts[i-1].T),
				Detail: map[string]any{
					"gap_start_s": pts[i-1].T, "gap_end_s": pts[i].T, "gap_s": dt,
					"rule": "sampling gap is never interpolated across; derivative and stages stop at the gap",
				},
			})
			start = i
		}
	}
	segs = append(segs, Segment{
		Index: len(segs), StartIdx: start, EndIdx: len(pts) - 1,
		StartT: pts[start].T, EndT: pts[len(pts)-1].T,
	})
	return segs, judgments
}

// DTGPoint 是 DTG 曲线点（失重速率，%/min 相对初重，正值表示失重）。
type DTGPoint struct {
	T       float64 `json:"t"`
	DTG     float64 `json:"dtg"`      // mg/s 平滑后（取负号：失重为正）
	Segment int     `json:"segment"`
}

// DTGCurve 保存 DTG 与平滑信息。
type DTGCurve struct {
	Points       []DTGPoint `json:"points"`
	SmoothWindow int        `json:"smooth_window"`
}

// BuildDTG 逐段计算 DTG；缺口边界不做跨段差分。
// 非单调噪声通过平滑窗口 + 显著度阈值容忍（判定在峰检测处给出）。
func BuildDTG(cc *CorrectedCurve, segs []Segment, p Params) *DTGCurve {
	pts := usablePoints(cc)
	w := p.SmoothWindow
	if w < 3 {
		w = 3
	}
	if w%2 == 0 {
		w++
	}
	half := w / 2
	out := &DTGCurve{SmoothWindow: w}
	for _, seg := range segs {
		n := seg.EndIdx - seg.StartIdx + 1
		if n < 3 {
			continue
		}
		raw := make([]float64, n)
		for i := 0; i < n; i++ {
			lo := i - 1
			hi := i + 1
			if lo < 0 {
				lo = 0
			}
			if hi > n-1 {
				hi = n - 1
			}
			a := pts[seg.StartIdx+lo]
			b := pts[seg.StartIdx+hi]
			if b.T == a.T {
				raw[i] = 0
				continue
			}
			raw[i] = -(b.Mass - a.Mass) / (b.T - a.T) // 失重为正
		}
		for i := 0; i < n; i++ {
			lo := i - half
			if lo < 0 {
				lo = 0
			}
			hi := i + half
			if hi > n-1 {
				hi = n - 1
			}
			sum := 0.0
			for j := lo; j <= hi; j++ {
				sum += raw[j]
			}
			idx := seg.StartIdx + i
			out.Points = append(out.Points, DTGPoint{T: pts[idx].T, DTG: sum / float64(hi-lo+1), Segment: seg.Index})
		}
	}
	return out
}

// AtmosphereInterval 是样品实际经历的一段气氛。
type AtmosphereInterval struct {
	Gas           string  `json:"gas"`
	ValveStartS   float64 `json:"valve_start_s"`   // 阀切换时间
	SampleStartS  float64 `json:"sample_start_s"`  // 样品反应时间 = 阀时间 + 延迟 + 滞后
	SampleEndS    float64 `json:"sample_end_s"`
	FlowMLMin     float64 `json:"flow_ml_min"`
}

// AtmosphereTimeline 是映射到样品时间的气氛时间线。
type AtmosphereTimeline struct {
	Intervals   []AtmosphereInterval `json:"intervals"`
	DelayS      float64              `json:"delay_s"`
	LagS        float64              `json:"lag_s"`
	InitialGas  string               `json:"initial_gas"`
}

// BuildAtmosphereTimeline 把阀事件映射为样品反应时间。
// 判定事件：气氛延迟和温度滞后使阀切换时间不等于样品反应时间。
func BuildAtmosphereTimeline(ev *GasEventsDoc, runEndS float64, p Params) (*AtmosphereTimeline, []Judgment) {
	tl := &AtmosphereTimeline{DelayS: p.AtmosphereDelayS, LagS: p.TempLagS, InitialGas: "unknown"}
	var judgments []Judgment
	if ev == nil || len(ev.Events) == 0 {
		return tl, nil
	}
	tl.InitialGas = ev.Events[0].FromGas
	shift := p.AtmosphereDelayS + p.TempLagS
	type evN struct {
		valveT float64
		to     string
		flow   float64
	}
	var norm []evN
	for _, e := range ev.Events {
		t, ok := Canonical(e.Time, DimTime)
		if !ok {
			continue
		}
		fl, _ := Canonical(e.Flow, DimFlow)
		norm = append(norm, evN{t, e.ToGas, fl})
	}
	sort.Slice(norm, func(i, j int) bool { return norm[i].valveT < norm[j].valveT })
	prevGas := tl.InitialGas
	prevSample := 0.0
	prevValve := 0.0
	prevFlow := 0.0
	for _, e := range norm {
		sampleT := e.valveT + shift
		tl.Intervals = append(tl.Intervals, AtmosphereInterval{
			Gas: prevGas, ValveStartS: prevValve, SampleStartS: prevSample, SampleEndS: sampleT, FlowMLMin: prevFlow,
		})
		judgments = append(judgments, Judgment{
			Code:    JAtmosphereDelay,
			Subject: fmt.Sprintf("valve@%gs->%s", e.valveT, e.to),
			Detail: map[string]any{
				"valve_time_s": e.valveT, "sample_reaction_time_s": sampleT,
				"delay_s": p.AtmosphereDelayS, "temp_lag_s": p.TempLagS,
				"rule": "valve switch time is not the sample reaction time; atmosphere delay and thermal lag shift it",
			},
		})
		prevGas = e.to
		prevSample = sampleT
		prevValve = e.valveT
		prevFlow = e.flow
	}
	tl.Intervals = append(tl.Intervals, AtmosphereInterval{
		Gas: prevGas, ValveStartS: prevValve, SampleStartS: prevSample, SampleEndS: runEndS, FlowMLMin: prevFlow,
	})
	return tl, judgments
}

func usablePoints(cc *CorrectedCurve) []Point {
	var out []Point
	for _, p := range cc.Points {
		if !p.Unconvertible {
			out = append(out, p)
		}
	}
	return out
}

func medianOf(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	cp := append([]float64(nil), v...)
	sort.Float64s(cp)
	return cp[len(cp)/2]
}
