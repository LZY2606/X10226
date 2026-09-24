package core

import (
	"fmt"
	"math"
	"sort"
)

// NormSet 是参与一次派生的规范化层集合及其来源引用。
type NormSet struct {
	Samples *SampleSeries
	Drift   *DriftCurve
	Program *FurnaceProgram
	Gas     *GasEventList
	Blank   *BlankCurve
	Mass    *InitialMass
	Srcs    map[string]SourceRef // kind -> 直接来源
}

// CandidateSpec 是一组候选分解（ID 由存储层分配）。
type CandidateSpec struct {
	Label        string
	Stages       []Stage
	TotalLossPct float64
	Verdict      string
	RejectReason string
}

// DeriveOutput 是一次派生的全部产物。
type DeriveOutput struct {
	Corrected   *CorrectedCurve
	DTG         *DTGCurve
	Candidates  []CandidateSpec
	Residue     *Residue
	Atmosphere  []AtmosphereAssessment
	GapPolicies []GapPolicy
}

// interpCurve 对校正曲线做线性插值（端点外取端点值）。
// 注意：插值只用于校正曲线；样品数据缺口绝不跨越插值。
func interpCurve(pts []CurvePoint, t float64) float64 {
	if len(pts) == 0 {
		return 0
	}
	if t <= pts[0].T {
		return pts[0].V
	}
	if t >= pts[len(pts)-1].T {
		return pts[len(pts)-1].V
	}
	lo, hi := 0, len(pts)-1
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if pts[mid].T <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	f := (t - pts[lo].T) / (pts[hi].T - pts[lo].T)
	return pts[lo].V + f*(pts[hi].V-pts[lo].V)
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	return s[len(s)/2]
}

// Derive 执行完整派生管线。所有产物都可通过 NormSet.Srcs 回溯直接来源。
func Derive(norm *NormSet, p Params) (*DeriveOutput, error) {
	if norm.Samples == nil || norm.Mass == nil {
		return nil, fmt.Errorf("派生至少需要 raw_samples 与 initial_mass 两层")
	}
	out := &DeriveOutput{}

	// 1. 漂移 + 空白校正；同时检测采样缺口。
	pts := norm.Samples.Points
	var dts []float64
	prevT, prevOK := 0.0, false
	for _, sp := range pts {
		if sp.ValidT {
			if prevOK {
				dts = append(dts, sp.T-prevT)
			}
			prevT, prevOK = sp.T, true
		}
	}
	med := median(dts)
	gapAfter := map[int]bool{} // 下标 i 与 i+1 之间存在缺口
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		if a.ValidT && b.ValidT && med > 0 && b.T-a.T > p.GapFactor*med {
			gapAfter[i] = true
			out.GapPolicies = append(out.GapPolicies, GapPolicy{
				GapStartS: a.T, GapEndS: b.T,
				Policy: "no_interpolation", MedianDtS: med,
			})
		}
	}

	cc := &CorrectedCurve{MassUnit: "mg"}
	seg := 0
	for i, sp := range pts {
		cp := CorrectedPoint{T: sp.T, Temp: sp.Temp, Segment: seg}
		switch {
		case !sp.ValidT || !sp.ValidM:
			cp.Valid = false // 无法转换的值留在原位置
		default:
			d := 0.0
			if norm.Drift != nil {
				d = interpCurve(norm.Drift.Points, sp.T)
			}
			b := 0.0
			if norm.Blank != nil {
				b = interpCurve(norm.Blank.Points, sp.T)
			}
			cp.Mass = sp.Mass - d - b
			cp.Valid = true
		}
		cc.Points = append(cc.Points, cp)
		if gapAfter[i] {
			cc.Gaps = append(cc.Gaps, [2]float64{sp.T, pts[i+1].T})
			seg++
		}
	}
	out.Corrected = cc

	// 2. 分段平滑 + DTG。缺口把曲线切成连续段，段间不求导、不插值。
	type run struct{ lo, hi int } // cc.Points 下标 [lo,hi)
	var runs []run
	start := -1
	for i, cp := range cc.Points {
		if cp.Valid && start < 0 {
			start = i
		}
		end := i == len(cc.Points)-1 || !cc.Points[i+1].Valid || cc.Points[i+1].Segment != cp.Segment
		if start >= 0 && end {
			runs = append(runs, run{start, i + 1})
			start = -1
		}
	}
	w := p.SmoothWindow
	if w < 1 {
		w = 1
	}
	if w%2 == 0 {
		w++
	}
	half := w / 2
	smooth := make([]float64, len(cc.Points))
	for _, r := range runs {
		for i := r.lo; i < r.hi; i++ {
			lo := max(i-half, r.lo)
			hi := min(i+half, r.hi-1)
			sum, n := 0.0, 0
			for j := lo; j <= hi; j++ {
				sum += cc.Points[j].Mass
				n++
			}
			smooth[i] = sum / float64(n)
		}
	}
	dtg := &DTGCurve{}
	for _, r := range runs {
		loIdx := len(dtg.Points)
		for i := r.lo + 1; i < r.hi-1; i++ {
			dm := smooth[i+1] - smooth[i-1]
			dt := cc.Points[i+1].T - cc.Points[i-1].T
			if dt <= 0 {
				continue
			}
			d := dm / dt
			dtg.Points = append(dtg.Points, DTGPoint{
				T: cc.Points[i].T, DMG: d,
				PctMin: d / norm.Mass.MassMg * 100 * 60,
			})
		}
		dtg.Segments = append(dtg.Segments, [2]int{loIdx, len(dtg.Points)})
	}
	out.DTG = dtg

	// 3. DTG 峰检测：突出度阈值以下的极小值视为非单调噪声，
	//    作为被拒绝候选保留（一等数据），不产生阶段。
	type peak struct {
		idx        int
		t, v       float64
		prominence float64
		loS, hiS   float64 // 半突出度宽度（时间）
	}
	var peaks []peak
	var noiseRejects []peak
	for _, segIdx := range dtg.Segments {
		lo, hi := segIdx[0], segIdx[1]
		for i := lo + 1; i < hi-1; i++ {
			v := dtg.Points[i].DMG
			if v >= dtg.Points[i-1].DMG || v > dtg.Points[i+1].DMG || v >= 0 {
				continue // 不是向下的局部极小
			}
			// 突出度：左右两侧最近的局部极大。
			lMax, rMax := 0.0, 0.0
			for j := i - 1; j >= lo; j-- {
				if dtg.Points[j].DMG > lMax {
					lMax = dtg.Points[j].DMG
				}
				if j > lo && dtg.Points[j].DMG >= dtg.Points[j-1].DMG && dtg.Points[j].DMG > dtg.Points[j+1].DMG {
					break
				}
			}
			for j := i + 1; j < hi; j++ {
				if dtg.Points[j].DMG > rMax {
					rMax = dtg.Points[j].DMG
				}
				if j < hi-1 && dtg.Points[j].DMG >= dtg.Points[j-1].DMG && dtg.Points[j].DMG > dtg.Points[j+1].DMG {
					break
				}
			}
			prom := min(lMax, rMax) - v
			pk := peak{idx: i, t: dtg.Points[i].T, v: v, prominence: prom}
			if prom < p.ProminenceMgS {
				if prom >= p.NoiseFloorMgS {
					noiseRejects = append(noiseRejects, pk)
				}
				continue
			}
			// 半突出度宽度决定阶段范围（可与其他阶段重叠）。
			th := v + prom/2
			pk.loS, pk.hiS = dtg.Points[lo].T, dtg.Points[hi-1].T
			for j := i; j > lo; j-- {
				if dtg.Points[j-1].DMG > th {
					pk.loS = dtg.Points[j].T
					break
				}
			}
			for j := i; j < hi-1; j++ {
				if dtg.Points[j+1].DMG > th {
					pk.hiS = dtg.Points[j].T
					break
				}
			}
			peaks = append(peaks, pk)
		}
	}

	// 校正曲线上按时间取质量（只在连续段内，不跨越缺口）。
	massAt := func(t float64) (float64, bool) {
		for _, r := range runs {
			if t < cc.Points[r.lo].T || t > cc.Points[r.hi-1].T {
				continue
			}
			for i := r.lo; i < r.hi-1; i++ {
				a, b := cc.Points[i], cc.Points[i+1]
				if t >= a.T && t <= b.T {
					f := (t - a.T) / (b.T - a.T)
					return a.Mass + f*(b.Mass-a.Mass), true
				}
			}
			return cc.Points[r.hi-1].Mass, true
		}
		return 0, false
	}
	mkStage := func(id, label string, loS, hiS float64, pk peak) Stage {
		m0, ok0 := massAt(loS)
		m1, ok1 := massAt(hiS)
		loss := 0.0
		if ok0 && ok1 {
			loss = m0 - m1
		}
		return Stage{
			ID: id, Label: label, StartS: loS, EndS: hiS,
			PeakS: pk.t, PeakDMgS: pk.v,
			LossMg:  loss,
			LossPct: loss / norm.Mass.MassMg * 100,
		}
	}

	// 4. 阶段候选：重叠峰产生多组合法候选，全部保留。
	var specs []CandidateSpec
	if len(peaks) > 0 {
		// 检测相邻峰之间的歧义谷。
		ambiguous := map[int]bool{} // peaks[i] 与 peaks[i+1] 之间
		for i := 0; i+1 < len(peaks); i++ {
			a, b := peaks[i], peaks[i+1]
			valley := math.Inf(1)
			for j := a.idx; j <= b.idx; j++ {
				if dtg.Points[j].DMG < valley {
					valley = dtg.Points[j].DMG
				}
			}
			if math.Abs(valley) > p.OverlapRatio*math.Min(math.Abs(a.v), math.Abs(b.v)) {
				ambiguous[i] = true
			}
		}
		build := func(merge map[int]bool) CandidateSpec {
			var stages []Stage
			total := 0.0
			i := 0
			n := 0
			for i < len(peaks) {
				j := i
				for j < len(peaks)-1 && merge[j] {
					j++
				}
				loS, hiS := peaks[i].loS, peaks[j].hiS
				deepest := peaks[i]
				for k := i; k <= j; k++ {
					if peaks[k].v < deepest.v {
						deepest = peaks[k]
					}
				}
				n++
				st := mkStage(fmt.Sprintf("st_%d", n), fmt.Sprintf("阶段 %d", n), loS, hiS, deepest)
				stages = append(stages, st)
				total += st.LossPct
				i = j + 1
			}
			// 标记重叠（阶段可重叠，不强制总和为百分之百）。
			for a := 0; a < len(stages); a++ {
				for b := a + 1; b < len(stages); b++ {
					if stages[a].EndS > stages[b].StartS && stages[b].EndS > stages[a].StartS {
						stages[a].Overlaps = append(stages[a].Overlaps, stages[b].ID)
						stages[b].Overlaps = append(stages[b].Overlaps, stages[a].ID)
					}
				}
			}
			return CandidateSpec{Stages: stages, TotalLossPct: total, Verdict: "pending"}
		}
		if len(ambiguous) == 0 {
			sp := build(nil)
			sp.Label = "唯一分解"
			specs = append(specs, sp)
		} else {
			split := build(nil)
			split.Label = "重叠峰拆分"
			merged := build(ambiguous)
			merged.Label = "重叠峰合并"
			specs = append(specs, split, merged)
		}
	}
	// 非单调噪声：被拒绝候选作为一等数据保留。
	for _, nr := range noiseRejects {
		specs = append(specs, CandidateSpec{
			Label:   fmt.Sprintf("噪声伪峰 @%.0fs", nr.t),
			Verdict: "rejected",
			RejectReason: fmt.Sprintf("below_prominence: 突出度 %.5f mg/s 低于阈值 %.5f，判定为非单调噪声",
				nr.prominence, p.ProminenceMgS),
		})
	}
	out.Candidates = specs

	// 5. 最终残留率（含计算链）。
	var last *CorrectedPoint
	for i := len(cc.Points) - 1; i >= 0; i-- {
		if cc.Points[i].Valid {
			last = &cc.Points[i]
			break
		}
	}
	if last == nil {
		return nil, fmt.Errorf("没有可用的校正后采样点，无法计算残留率")
	}
	res := &Residue{
		FinalMassMg: Quantity{Value: last.Mass, Unit: "mg", Basis: "corrected_sample"},
		InitialMass: Quantity{Value: norm.Mass.MassMg, Unit: "mg", Basis: norm.Mass.Basis},
		ResiduePct:  last.Mass / norm.Mass.MassMg * 100,
		AtS:         last.T,
	}
	res.CalcChain = []string{
		fmt.Sprintf("初重 %.4f mg (%s, %s v%d)", norm.Mass.MassMg, norm.Mass.Basis,
			norm.Srcs[LayerInitialMass].LayerID, norm.Srcs[LayerInitialMass].Version),
		fmt.Sprintf("末点原始质量经天平漂移校正与空白曲线扣除 → %.4f mg @ %.0f s", last.Mass, last.T),
		fmt.Sprintf("残留率 = %.4f / %.4f × 100 = %.2f %%", last.Mass, norm.Mass.MassMg, res.ResiduePct),
	}
	out.Residue = res

	// 6. 气氛延迟判定（机器可读取）：阀切换时间 ≠ 样品反应时间。
	if norm.Gas != nil {
		for _, ge := range norm.Gas.Events {
			delay := p.TransportDelayS + p.ThermalLagS
			out.Atmosphere = append(out.Atmosphere, AtmosphereAssessment{
				GasEventID: ge.ID, ValveTimeS: ge.ValveT,
				SampleTimeS: ge.ValveT + delay, DelayS: delay,
				TransportDelayS: p.TransportDelayS, ThermalLagS: p.ThermalLagS,
				ValveEqualsSample: false,
				Basis:             "gas_transport_delay+thermal_lag: 气体输运与温度滞后使阀切换时间不等于样品反应时间",
				FromGas:           ge.FromGas, ToGas: ge.ToGas,
			})
		}
	}
	return out, nil
}
