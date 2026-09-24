// Package casepack 生成并内嵌“失重谱系：TGA 阶段分解、气氛切换与残留物证据链”
// 的离线领域案例包。生成过程使用固定 seed，不访问公网，不依赖机器状态。
package casepack

import (
	"fmt"
	"math"
	"strings"
)

// Spec 描述案例包初始版本（基线）。
type Spec struct {
	NPoints    int     // 采样点数（0..NPoints-1，步长 DtS）
	DtS        float64
	Seed       uint64
	GapTimes   []float64 // 从采样序列中删除的时间点（形成采样缺口）
	BumpTimes  []float64 // 在这些时间点注入非单调质量噪声
	DeclaredM0 float64   // 声明初重（含天平偏置）
	TrueM0     float64   // 生成器使用的真实初重
	AirSwitchS float64   // 空气阀切换时间（炉侧）
}

// V1Spec 是初始版本规格。
func V1Spec() Spec {
	return Spec{
		NPoints:    61, // 0..300s, 步长 5s
		DtS:        5,
		Seed:       20260919,
		GapTimes:   []float64{130, 135}, // 125s→140s 的采样缺口
		BumpTimes:  []float64{60, 65},
		DeclaredM0: 10.012,
		TrueM0:     10.000,
		AirSwitchS: 150,
	}
}

// 四个失重过程（平滑累积）：中心时间、幅度 mg、宽度 s。
type loss struct {
	center float64
	amp    float64
	width  float64
}

var v1Losses = []loss{
	{50, 0.15, 12},   // 失水
	{100, 0.80, 14},  // 分解阶段 I（与 II 重叠）
	{115, 0.40, 12},  // 分解阶段 II（重叠峰；缺口紧邻其后）
	{200, 2.40, 16},  // 空气氧化（阀 150s + 延迟 45s → 样品侧 195s）
}

// trueMass 是无噪声的真实样品质量。
func trueMass(t float64) float64 {
	m := V1Spec().TrueM0
	for _, l := range v1Losses {
		m -= l.amp / (1 + math.Exp(-(t-l.center)/l.width*3))
	}
	return m
}

// furnaceTemp 炉温程序：30 → 25°C(0s) 线性升到 475°C(300s)。
func furnaceTemp(t float64) float64 { return 25 + (475-25)*t/300 }

// lcg 固定 seed 线性同余（足够此场景的可重复“噪声”）。
type lcg struct{ s uint64 }

func newLCG(seed uint64) *lcg { return &lcg{s: seed} }
func (g *lcg) float() float64 {
	g.s = g.s*6364136223846793005 + 1442695040888963407
	return float64(g.s>>11) / float64(1<<53)
}

// RawPoint 生成器输出的单个原始点（未四舍五入）。
type RawPoint struct {
	T        float64
	TempC    float64
	MassMg   float64
	InGap    bool
	BumpUp   bool
	BumpDown bool
}

// GeneratePoints 返回基线全部采样点（含缺口标记；缺口点不写入包）。
func GeneratePoints(s Spec) []RawPoint {
	rng := newLCG(s.Seed)
	gap := map[float64]bool{}
	for _, t := range s.GapTimes {
		gap[t] = true
	}
	bump := map[float64]bool{}
	for i, t := range s.BumpTimes {
		bump[t] = i == 0 // 第一个上凸、第二个下凹
	}
	out := make([]RawPoint, 0, s.NPoints)
	for i := 0; i < s.NPoints; i++ {
		t := float64(i) * s.DtS
		m := trueMass(t)
		// 天平示值偏置 + 慢漂移（与空白曲线同一物理来源）。
		balanceBias := 0.012 - 0.00006*t
		noise := (rng.float() - 0.5) * 0.006
		shown := m + balanceBias + noise
		p := RawPoint{T: t, TempC: furnaceTemp(t) + (rng.float()-0.5)*0.4, MassMg: shown, InGap: gap[t]}
		if up, ok := bump[t]; ok {
			if up {
				p.MassMg += 0.02
				p.BumpUp = true
			} else {
				p.MassMg -= 0.02
				p.BumpDown = true
			}
		}
		out = append(out, p)
	}
	return out
}

// blankMass 空白曲线示值（漂移项 b=-0.00006 mg/s，截距 0.012 mg）。
func blankMass(t float64) float64 { return 0.012 - 0.00006*t }

func f3(v float64) string { return fmt.Sprintf("%.3f", math.Round(v*1000)/1000) }
func f2(v float64) string { return fmt.Sprintf("%.2f", math.Round(v*100)/100) }

// BundleV1 生成初始版本 .tgab 文本（六层齐全）。
func BundleV1() string {
	s := V1Spec()
	var b strings.Builder
	b.WriteString("# 失重谱系离线案例包 v1 —— TGA 阶段分解 / 气氛切换 / 残留物证据链\n")
	b.WriteString("# 固定 seed 生成；原始表示带单位；派生层才做规范化。\n\n")

	b.WriteString("[SAMPLES]\n|time_s|temp_c|mass_mg|\n")
	for _, p := range GeneratePoints(s) {
		if p.InGap {
			continue // 采样缺口：点缺失，禁止跨缺口插值
		}
		b.WriteString(fmt.Sprintf("|%ss|%s°C|%smg|\n", f2(p.T), f2(p.TempC), f3(p.MassMg)))
	}

	b.WriteString("\n[CORRECTION]\n|method|a_mg|b_mg_per_s|\n")
	b.WriteString("|blank|0.012mg|-0.000060mg/s|\n")

	b.WriteString("\n[FURNACE]\n|time_s|target_temp_c|\n")
	for _, t := range []float64{0, 60, 120, 180, 240, 300} {
		b.WriteString(fmt.Sprintf("|%ss|%s°C|\n", f2(t), f2(furnaceTemp(t))))
	}

	b.WriteString(fmt.Sprintf("\n[GAS]\n|time_s|valve|gas|\n|0s|open|N2|\n|%ss|switch|air|\n", f2(s.AirSwitchS)))

	b.WriteString("\n[BLANK]\n|time_s|blank_mass_mg|\n")
	for _, t := range []float64{0, 50, 100, 150, 200, 250, 300} {
		b.WriteString(fmt.Sprintf("|%ss|%smg|\n", f2(t), f3(blankMass(t))))
	}

	b.WriteString("\n[INITIAL]\n|mass_mg|basis|note|\n")
	b.WriteString(fmt.Sprintf("|%smg|declared_balance_reading|导入初称（天平示值，含偏置，待修订）|\n", f3(s.DeclaredM0)))
	return b.String()
}
