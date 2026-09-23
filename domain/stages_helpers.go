package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"sort"
	"strings"
)

// DependencyRefs 汇总派生结果直接来源的稳定身份与参数指纹。
type DependencyRefs struct {
	CorrectedSeriesID string
	DTGID             string
	ParamFingerprint  string
	ParamVersionID    string
	SubmissionIDs     []string
}

// FingerprintParams 生成参数指纹: 规范化键排序后哈希。
func FingerprintParams(p Params) string {
	keys := []string{
		"atmoDelayMin", "tempLagMin", "driftRateMgPerMin", "driftOffsetMg",
		"blankUse", "dtgWindowPoints", "peakProminenceMg", "gapThresholdMin",
		"nonMonotoneTolMg", "peakMergeWindowMin",
	}
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		switch k {
		case "atmoDelayMin":
			b.WriteString(ftoa(p.AtmoDelayMin))
		case "tempLagMin":
			b.WriteString(ftoa(p.TempLagMin))
		case "driftRateMgPerMin":
			b.WriteString(ftoa(p.DriftRateMgPerMin))
		case "driftOffsetMg":
			b.WriteString(ftoa(p.DriftOffsetMg))
		case "blankUse":
			b.WriteString(boolStr(p.BlankUse))
		case "dtgWindowPoints":
			b.WriteString(itoa(p.DtgWindowPoints))
		case "peakProminenceMg":
			b.WriteString(ftoa(p.PeakProminenceMg))
		case "gapThresholdMin":
			b.WriteString(ftoa(p.GapThresholdMin))
		case "nonMonotoneTolMg":
			b.WriteString(ftoa(p.NonMonotoneTolMg))
		case "peakMergeWindowMin":
			b.WriteString(ftoa(p.PeakMergeWindowMin))
		}
		b.WriteString(";")
	}
	sum := sha256.Sum256([]byte(b.String()))
	return "pf_" + hex.EncodeToString(sum[:])[:12]
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// stableID 生成内容寻址的稳定身份; 相同输入 => 相同 ID。
func stableID(prefix string, parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return prefix + "_" + hex.EncodeToString(h[:])[:14]
}

func stageKey(stages []Stage) string {
	var xs []string
	for _, s := range stages {
		xs = append(xs, ftoa(s.StartMin)+"-"+ftoa(s.PeakTimeMin)+"-"+ftoa(s.EndMin))
	}
	sort.Strings(xs)
	return strings.Join(xs, ",")
}

func appendUnique(xs []int, v int) []int {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}

func intervalsOverlap(a, b Stage) bool {
	return a.StartMin < b.EndMin && b.StartMin < a.EndMin
}

// gasNameAt 返回阀事件切换之后样品“名义上”的气氛(阀时间, 非反应时间)。
func gasNameAt(gas []GasEvent, t float64) string {
	name := "ambient"
	best := math.Inf(-1)
	for _, g := range gas {
		if g.Time.Diagnostic != "" {
			continue
		}
		if g.Time.Minutes <= t && g.Time.Minutes >= best {
			best = g.Time.Minutes
			name = g.ToGas
		}
	}
	return name
}

// valleyBefore/After: 在 DTG 上寻找峰两侧的“谷底”(失重速率局部最小),
// 若途中遇到不连接(缺口)则返回 unknown。
func valleyBefore(dtg DTGSeries, peakT float64) (float64, string) {
	idx := -1
	for i, p := range dtg.Points {
		if p.TimeMin == peakT {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return peakT, "peak"
	}
	j := idx
	for j-1 >= 0 {
		if !dtg.Points[j].Connected {
			return math.NaN(), "gap_unknown"
		}
		if dtg.Points[j-1].RateMgPerMin >= dtg.Points[j].RateMgPerMin {
			break
		}
		j--
	}
	return dtg.Points[j].TimeMin, "valley"
}

func valleyAfter(dtg DTGSeries, peakT float64) (float64, string) {
	idx := -1
	for i, p := range dtg.Points {
		if p.TimeMin == peakT {
			idx = i
			break
		}
	}
	if idx < 0 || idx >= len(dtg.Points)-1 {
		return peakT, "peak"
	}
	j := idx
	for j+1 < len(dtg.Points) {
		if !dtg.Points[j+1].Connected {
			return math.NaN(), "gap_unknown"
		}
		if dtg.Points[j+1].RateMgPerMin >= dtg.Points[j].RateMgPerMin {
			break
		}
		j++
	}
	return dtg.Points[j].TimeMin, "valley"
}

// groupBounds 计算一个峰组整体的边界。
func groupBounds(dtg DTGSeries, cs CorrectedSeries, spanStart, spanEnd float64, params Params) bounds {
	st, ss := valleyBefore(dtg, spanStart)
	en, es := valleyAfter(dtg, spanEnd)
	sb := bounds{startMin: st, endMin: en, startStatus: ss, endStatus: es}
	if ss == "gap_unknown" || es == "gap_unknown" {
		sb.crossesGap = true
	}
	if crossesGapSpan(cs, sb.startMin, sb.endMin) {
		sb.crossesGap = true
	}
	return sb
}

func perPeakBounds(dtg DTGSeries, pk DTGPeak, params Params) bounds {
	st, ss := valleyBefore(dtg, pk.TimeMin)
	en, es := valleyAfter(dtg, pk.TimeMin)
	return bounds{
		startMin: st, endMin: en, startStatus: ss, endStatus: es,
		crossesGap: ss == "gap_unknown" || es == "gap_unknown",
	}
}

func crossesGapSpan(cs CorrectedSeries, a, b float64) bool {
	if math.IsNaN(a) || math.IsNaN(b) {
		return true
	}
	for _, g := range cs.Gaps {
		if g.FromMin >= a && g.FromMin <= b {
			return true
		}
	}
	return false
}

// massAt 返回 t 时刻校正质量: 仅允许取真实采样点时刻; 非采样点用相邻
// 真实点线性插值, 但绝不跨越缺口。
func massAt(cs CorrectedSeries, t float64) (float64, bool) {
	type tp struct {
		t, m float64
	}
	var pts []tp
	prevT := math.NaN()
	for _, c := range cs.Points {
		if c.Gap || math.IsNaN(c.TimeMin) {
			prevT = math.NaN()
			continue
		}
		if !math.IsNaN(prevT) && c.hasFlag("gap_start") {
			prevT = math.NaN()
		}
		pts = append(pts, tp{c.TimeMin, c.CorrectedMg})
		prevT = c.TimeMin
	}
	if len(pts) == 0 || t < pts[0].t || t > pts[len(pts)-1].t {
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
	return 0, false
}

func buildStage(idx int, sb bounds, pk DTGPeak,
	cs CorrectedSeries, gasAt func(float64) string) Stage {
	st := Stage{
		Index:          idx,
		StartMin:       sb.startMin,
		EndMin:         sb.endMin,
		PeakTimeMin:    pk.TimeMin,
		PeakTempC:      pk.TempDegC,
		GasDuring:      gasAt(pk.TimeMin),
		CrossesGap:     sb.crossesGap,
		BoundaryStatus: sb.startStatus + "+" + sb.endStatus,
	}
	if m, ok := massAt(cs, sb.startMin); ok {
		st.StartMassMg = m
	} else {
		st.Unknown = true
	}
	if m, ok := massAt(cs, sb.endMin); ok {
		st.EndMassMg = m
	} else {
		st.Unknown = true
	}
	if !st.Unknown {
		st.MassLossMg = st.StartMassMg - st.EndMassMg
	}
	return st
}

func validateCandidate(c *StageCandidate, cs CorrectedSeries, groupSB bounds) {
	for i := range c.Stages {
		s := &c.Stages[i]
		if s.Unknown || strings.Contains(s.BoundaryStatus, "gap_unknown") || s.CrossesGap {
			c.RejectReasons = append(c.RejectReasons,
				"阶段 "+itoa(s.Index)+" 边界跨越采样缺口, 失重无法唯一确定(不插值)")
		}
		if !s.Unknown && s.MassLossMg < 0 {
			c.RejectReasons = append(c.RejectReasons,
				"阶段 "+itoa(s.Index)+" 净质量变化为正, 非单调噪声下不作为失重阶段")
		}
	}
	if len(c.RejectReasons) == 0 && c.Status != "rejected" {
		c.Status = "legal"
	}
	if c.Status == "" {
		c.Status = "rejected"
	}
}
