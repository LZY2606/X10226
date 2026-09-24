// Package tga 实现热重分析领域计算：单位规范化、天平漂移与空白校正、
// TG/DTG 曲线、气氛时间线（阀切换延迟与热滞后）、阶段分解候选与残留率。
//
// 设计原则：
//   - 所有领域量携带单位或坐标基准，原始表示在导入层保留，规范化只发生在派生层。
//   - 候选、拒绝原因、误差与未知状态都是一等数据。
//   - 数据缺口不跨越插值；阶段可重叠且不强制总和为 100%。
package tga

import (
	"fmt"
	"strconv"
	"strings"
)

// Diagnostic 描述一个无法转换或被标记的原始值，值仍留在原位置。
type Diagnostic struct {
	Where  string `json:"where"` // 位置，如 "samples 第 42 行 mass_mg 列"
	Raw    string `json:"raw"`   // 原始表示
	Reason string `json:"reason"`
}

// Quantity 是携带单位与坐标基准的领域量。Raw 保留导入时的原始字符串；
// Value 为规范化后的数值（规范单位制），无法转换时为 nil。
type Quantity struct {
	Raw   string   `json:"raw"`
	Value *float64 `json:"value,omitempty"`
	Unit  string   `json:"unit"`  // 原始单位，如 "mg"、"min"
	Basis string   `json:"basis"` // 坐标基准，如 "pan-tared@25C"、"furnace-thermocouple"
}

// 规范单位：时间 s，质量 mg，温度 C，百分比 pct。
var unitToCanonical = map[string]struct {
	factor float64
	kind   string // time|mass|temp|pct|gas-label
}{
	"s":    {1, "time"},
	"sec":  {1, "time"},
	"min":  {60, "time"},
	"h":    {3600, "time"},
	"mg":   {1, "mass"},
	"g":    {1000, "mass"},
	"ug":   {0.001, "mass"},
	"C":    {1, "temp"},
	"degC": {1, "temp"},
	"pct":  {1, "pct"},
	"%":    {1, "pct"},
}

// Convert 解析原始字符串并按单位换算到规范单位。
// 无法转换时返回带诊断的 Quantity（Value 为 nil），原始表示保留。
func Convert(raw, unit, basis, where string) (Quantity, []Diagnostic) {
	q := Quantity{Raw: raw, Unit: unit, Basis: basis}
	s := strings.TrimSpace(raw)
	if s == "" {
		return q, []Diagnostic{{Where: where, Raw: raw, Reason: "空值，无法转换，保留在原位置"}}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return q, []Diagnostic{{Where: where, Raw: raw, Reason: fmt.Sprintf("数值解析失败: %v", err)}}
	}
	u, ok := unitToCanonical[unit]
	if !ok {
		return q, []Diagnostic{{Where: where, Raw: raw, Reason: fmt.Sprintf("未知单位 %q，未做规范化", unit)}}
	}
	v = v * u.factor
	q.Value = &v
	return q, nil
}

// Point 是曲线上的一个采样点（规范单位）。
type Point struct {
	TS    float64 `json:"t_s"`   // 时间 s
	TempC float64 `json:"temp_c"`
	MassMg float64 `json:"mass_mg"`
}

// ValveEvent 是气体阀切换事件（原始记录时间，不等于样品反应时间）。
type ValveEvent struct {
	TS  float64 `json:"t_s"`
	Gas string  `json:"gas"`
}

// AtmoSegment 是样品处实际生效的气氛段：阀切换时间 + 气氛延迟 + 温度滞后。
type AtmoSegment struct {
	Gas          string  `json:"gas"`
	ValveTS      float64 `json:"valve_t_s"`      // 阀切换时刻（原始）
	EffectiveTS  float64 `json:"effective_t_s"`  // 样品处生效时刻 = 阀时刻 + 延迟 + 滞后
	EndTS        float64 `json:"end_t_s"`
	DelayS       float64 `json:"delay_s"`
	LagS         float64 `json:"lag_s"`
}

// NoiseFlag 标记非单调噪声点（质量回升但处于失重趋势中）。
type NoiseFlag struct {
	TS     float64 `json:"t_s"`
	Reason string  `json:"reason"`
}

// StageCandidate 是一个阶段分解候选。阶段可重叠，不强制总和为 100%。
type StageCandidate struct {
	ID          string   `json:"id"`
	SetID       string   `json:"set_id"`       // 所属候选方案（同一记录内可有多套合法方案）
	StartS      float64  `json:"start_s"`
	EndS        float64  `json:"end_s"`
	PeakTempC   float64  `json:"peak_temp_c"`
	MassLossPct float64  `json:"mass_loss_pct"`
	AtmoGas     string   `json:"atmo_gas"`     // 该阶段生效气氛（按延迟后时间线判定）
	Overlaps    []string `json:"overlaps,omitempty"`
	Basis       string   `json:"basis"` // 判定依据（机器可读）
}

// RejectedCandidate 是被拒绝的候选及其原因（一等数据）。
type RejectedCandidate struct {
	StartS float64 `json:"start_s"`
	EndS   float64 `json:"end_s"`
	Reason string  `json:"reason"`
}

// ChainStep 是计算链中的一步，携带算法版本、参数指纹与直接来源。
type ChainStep struct {
	Label       string   `json:"label"`
	Algo        string   `json:"algo"`
	AlgoVersion string   `json:"algo_version"`
	ParamFP     string   `json:"param_fp"`
	Sources     []string `json:"sources"`
	Detail      string   `json:"detail"`
}
