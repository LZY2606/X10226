package derive

import (
	"math"
	"sort"

	"tgareview/internal/domain"
)

// BuildTG runs the correction pipeline and returns the corrected TG artifact
// data. Invalid/raw-only cells never silently disappear: gaps and diagnostics
// are first-class outputs.
func BuildTG(in Inputs, p domain.Params) TGResult {
	res := TGResult{Basis: domain.UnitBasis}
	if in.Sample == nil {
		res.Diagnostics = append(res.Diagnostics, "no sample_points source")
		return res
	}
	// deterministic linear tables for drift and blank
	driftTab := buildTable(in.Drift, "t", "drift")
	blankTab := buildTable(in.Blank, "t", "blank")
	res.DriftApplied = p.DriftCorrection && driftTab != nil
	res.BlankApplied = p.BlankCorrection && blankTab != nil

	for _, row := range in.Sample.Rows {
		if !row.Accepted {
			continue
		}
		t := row.Cells["t"]
		te := row.Cells["temp"]
		m := row.Cells["mass"]
		if !t.Valid || !m.Valid {
			continue // rejected rows already keep their raw text in the source
		}
		temp := math.NaN()
		if te.Valid {
			temp = te.Value
		}
		res.Points = append(res.Points, Point{
			Line: row.Line, TS: t.Value, T: temp, MassRaw: m.Value, MassCorr: m.Value,
		})
	}
	sort.SliceStable(res.Points, func(i, j int) bool { return res.Points[i].TS < res.Points[j].TS })

	// initial mass: stratified layer wins; its raw representation is cited.
	m0, m0Chain := resolveInitialMass(in, res.Points)
	res.InitialMass, res.M0Source = m0, m0Chain

	for i := range res.Points {
		pt := &res.Points[i]
		if res.DriftApplied {
			if d, ok := driftTab.at(pt.TS); ok {
				pt.MassCorr -= d
			} else {
				res.Diagnostics = append(res.Diagnostics,
					"drift correction unavailable at t="+f3(pt.TS)+"s (outside drift table); left uncorrected")
			}
		}
		if res.BlankApplied {
			if b, ok := blankTab.at(pt.TS); ok {
				pt.MassCorr -= b
			} else {
				res.Diagnostics = append(res.Diagnostics,
					"blank correction unavailable at t="+f3(pt.TS)+"s; left uncorrected")
			}
		}
		if m0 > 0 {
			pt.MassPct = pt.MassCorr / m0 * 100
		}
	}

	// segments: a gap is a real discontinuity; nothing is interpolated across
	seg := 0
	for i := range res.Points {
		res.Points[i].Segment = seg
		if i+1 < len(res.Points) {
			dt := res.Points[i+1].TS - res.Points[i].TS
			if dt > p.GapMaxDtS {
				res.Points[i].GapAfter = true
				res.Gaps = append(res.Gaps, [2]float64{r3(res.Points[i].TS), r3(res.Points[i+1].TS)})
				seg++
			}
		}
	}

	// non-monotonic noise flag: tiny upward steps within a segment are noise
	for i := 1; i < len(res.Points); i++ {
		a, b := res.Points[i-1], res.Points[i]
		if a.Segment != b.Segment || m0 <= 0 {
			continue
		}
		dPct := (b.MassCorr - a.MassCorr) / m0 * 100
		if dPct > 0 && dPct < p.NoiseNonMonotonicPct {
			res.Points[i].NonMonot = true
		}
	}
	if len(res.Points) > 0 {
		res.FinalMass = r3(res.Points[len(res.Points)-1].MassCorr)
	}

	res.Chain = []domain.ChainNode{
		{Step: "1 规范化", Detail: "原始质量/温度/炉时间按列声明单位换算到 mg、°C、s；原始表示保留在来源行", Value: in.Sample.ID},
		{Step: "2 天平漂移校正", Detail: boolStep(res.DriftApplied, "mass_corr = mass_raw − drift(t)，漂移表外不外推", in.Drift), Value: yn(res.DriftApplied)},
		{Step: "3 空白曲线扣除", Detail: boolStep(res.BlankApplied, "mass_corr = mass_corr − blank(t)，表区间外不插值", in.Blank), Value: yn(res.BlankApplied)},
		{Step: "4 采样缺口分段", Detail: "相邻 Δt > 参数 gap_max_dt_s 即缺口；缺口两侧不做任何插值", Value: f3(p.GapMaxDtS) + " s"},
		{Step: "5 非单调噪声", Detail: "段内微小正向跳动（< 噪声阈值）标记为噪声但不删除", Value: f3(p.NoiseNonMonotonicPct) + " %"},
		{Step: "6 初重归一", Detail: m0Chain, Value: f3(m0) + " mg"},
	}
	return res
}

func boolStep(on bool, text string, src *domain.Source) string {
	if on {
		return text + "；表=" + src.ID
	}
	if src == nil {
		return text + "；未提供该来源，跳过"
	}
	return text + "；参数关闭，跳过"
}

func yn(b bool) string {
	if b {
		return "应用"
	}
	return "未应用"
}

func resolveInitialMass(in Inputs, pts []Point) (float64, string) {
	if in.InitialMass != nil {
		for _, row := range in.InitialMass.Rows {
			if row.Accepted {
				q := row.Cells["m0"]
				return q.Value, "采用分层初重层 " + in.InitialMass.ID +
					" v" + itoa(in.InitialMass.Version) + " 行" + itoa(row.Line) +
					"（原始表示 “" + row.Raw["m0"] + "”）"
			}
		}
	}
	if len(pts) > 0 {
		return pts[0].MassRaw, "无分层初重层；回退到首个采样点表观质量 " + f3(pts[0].MassRaw) + " mg（已在诊断中标记）"
	}
	return 0, "无可用初重"
}

// --- simple deterministic (t,v) table with linear interp within range ---

type table struct{ xs, ys []float64 }

func buildTable(src *domain.Source, xk, yk string) *table {
	if src == nil {
		return nil
	}
	tb := &table{}
	for _, r := range src.Rows {
		if !r.Accepted {
			continue
		}
		x, y := r.Cells[xk], r.Cells[yk]
		if x.Valid && y.Valid {
			tb.xs = append(tb.xs, x.Value)
			tb.ys = append(tb.ys, y.Value)
		}
	}
	if len(tb.xs) < 1 {
		return nil
	}
	idx := make([]int, len(tb.xs))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return tb.xs[idx[a]] < tb.xs[idx[b]] })
	nx := make([]float64, len(tb.xs))
	ny := make([]float64, len(tb.ys))
	for i, j := range idx {
		nx[i], ny[i] = tb.xs[j], tb.ys[j]
	}
	tb.xs, tb.ys = nx, ny
	return tb
}

func (tb *table) at(x float64) (float64, bool) {
	if len(tb.xs) == 0 || x < tb.xs[0] || x > tb.xs[len(tb.xs)-1] {
		return 0, false
	}
	i := sort.Search(len(tb.xs), func(i int) bool { return tb.xs[i] >= x })
	if i < len(tb.xs) && math.Abs(tb.xs[i]-x) < 1e-9 {
		return tb.ys[i], true
	}
	if i == 0 || i >= len(tb.xs) {
		return 0, false
	}
	x0, x1 := tb.xs[i-1], tb.xs[i]
	y0, y1 := tb.ys[i-1], tb.ys[i]
	return y0 + (x-x0)*(y1-y0)/(x1-x0), true
}

func f3(v float64) string { return strconvF(v) }
func itoa(i int) string   { return strconvI(i) }
