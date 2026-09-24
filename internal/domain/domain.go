// Package domain 定义热重分析（TGA）阶段分解与气氛事件的核心领域类型。
// 原始层保留提交时的字符串表示；派生层才做单位规范化。
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// 原始层种类（分层保存：原始采样点 / 炉程序 / 气体阀事件 / 空白曲线 / 样品初重）
const (
	LayerSamplePoints   = "sample_points"
	LayerFurnaceProgram = "furnace_program"
	LayerGasEvents      = "gas_events"
	LayerBlankCurve     = "blank_curve"
	LayerInitialMass    = "initial_mass"
)

// LayerKinds 按固定顺序列出全部原始层种类。
var LayerKinds = []string{
	LayerSamplePoints, LayerFurnaceProgram, LayerGasEvents, LayerBlankCurve, LayerInitialMass,
}

// 派生结果种类。
const (
	ResultNormalized = "normalized_points" // 规范化的原始采样点
	ResultCorrected  = "corrected_points"  // 天平漂移校正后的质量
	ResultDTG        = "dtg"               // 微分失重曲线
	ResultAtmosphere = "atmosphere"        // 气氛时间线（含延迟模型）
	ResultStages     = "stage_candidates"  // 阶段候选集合
	ResultResidue    = "residue"           // 最终残留率
)

// AlgoVersion 是派生算法的版本号，任何算法变更都必须递增。
const AlgoVersion = "tga-decomp/1.2.0"

// Diagnostic 描述一条无法转换或被标记的原始值；原始值留在原位置。
type Diagnostic struct {
	Layer  string `json:"layer"`
	Row    int    `json:"row"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

// Params 是一次分析运行的全部参数；参数指纹由它派生。
type Params struct {
	GapFactor        float64 `json:"gap_factor"`          // 采样缺口判定：dt > GapFactor * 中位 dt
	BlankGapS        float64 `json:"blank_gap_s"`         // 空白曲线允许内插的最大跨度（秒）
	NoiseBandMg      float64 `json:"noise_band_mg"`       // 非单调噪声判定带宽（mg）
	PeakThresholdMgS float64 `json:"peak_threshold_mg_s"` // DTG 峰阈值（mg/s）
	PeakProminence   float64 `json:"peak_prominence"`     // 峰基线判定用的突出度比例
	GasDelayS        float64 `json:"gas_delay_s"`         // 气氛延迟：阀切换到气体到达样品
	TempLagS         float64 `json:"temp_lag_s"`          // 温度滞后：气体到达到样品反应
	MinStageLossPct  float64 `json:"min_stage_loss_pct"`  // 阶段候选的最小失重百分比
}

// DefaultParams 返回演示与测试共用的默认参数。
func DefaultParams() Params {
	return Params{
		GapFactor:        3.0,
		BlankGapS:        180,
		NoiseBandMg:      0.05,
		PeakThresholdMgS: 0.002,
		PeakProminence:   0.2,
		GasDelayS:        40,
		TempLagS:         25,
		MinStageLossPct:  0.5,
	}
}

// Fingerprint 返回算法版本 + 参数的规范 JSON 的 SHA-256 指纹。
func (p Params) Fingerprint() string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(append([]byte(AlgoVersion+"\x00"), b...))
	return hex.EncodeToString(sum[:])[:16]
}

// Series 是一条规范化后的采样序列；null 表示该点未知（例如空白曲线缺口内）。
type Series struct {
	Units  map[string]string `json:"units"`
	TimeS  []float64         `json:"time_s"`
	MassMg []*float64        `json:"mass_mg"`
	TempC  []*float64        `json:"temp_c,omitempty"`
	Gaps   [][2]float64      `json:"gaps,omitempty"`  // 采样缺口区间（不跨越插值）
	Flags  []PointFlag       `json:"flags,omitempty"` // 非单调噪声等点级标记
}

// PointFlag 标记单个采样点的异常。
type PointFlag struct {
	Index  int    `json:"index"`
	Kind   string `json:"kind"` // non_monotonic | blank_unknown | ...
	Detail string `json:"detail"`
}

// DTG 是微分失重序列，按区间中点取值；跨越缺口或未知端点时为 null。
type DTG struct {
	Units map[string]string `json:"units"`
	TimeS []float64         `json:"time_s"`
	Rate  []*float64        `json:"rate_mg_s"` // 负值表示失重
}

// AtmosphereEvent 把一个气体阀事件展开为三个时间：阀切换、气体到达、样品反应。
// 判断依据：气氛延迟和温度滞后使阀切换时间不等于样品反应时间。
type AtmosphereEvent struct {
	ValveTimeS     float64 `json:"valve_time_s"`
	EffectiveTimeS float64 `json:"effective_time_s"`
	ReactionTimeS  float64 `json:"reaction_time_s"`
	FromGas        string  `json:"from_gas"`
	ToGas          string  `json:"to_gas"`
	FlowMlMin      float64 `json:"flow_ml_min"`
}

// Atmosphere 是派生的气氛时间线。
type Atmosphere struct {
	Units     map[string]string `json:"units"`
	GasDelayS float64           `json:"gas_delay_s"`
	TempLagS  float64           `json:"temp_lag_s"`
	Events    []AtmosphereEvent `json:"events"`
}

// GasAt 返回 t 时刻（按反应时间轴）样品处实际的气体。
func (a *Atmosphere) GasAt(t float64) string {
	gas := ""
	for _, e := range a.Events {
		if e.ReactionTimeS <= t {
			gas = e.ToGas
		}
	}
	return gas
}

// Candidate 是一个阶段候选。阶段可重叠，且不强制总和为百分之百。
type Candidate struct {
	Index           int      `json:"index"`
	StartS          float64  `json:"start_s"`
	EndS            float64  `json:"end_s"`
	StartC          *float64 `json:"start_c,omitempty"`
	EndC            *float64 `json:"end_c,omitempty"`
	LossMg          *float64 `json:"loss_mg"`
	LossPct         *float64 `json:"loss_pct"`
	ResidueAfterPct *float64 `json:"residue_after_pct"`
	Gas             string   `json:"gas"`
	Status          string   `json:"status"` // candidate | rejected | unknown
	Reasons         []string `json:"reasons,omitempty"`
	OverlapsWith    []int    `json:"overlaps_with,omitempty"`
	ParentID        string   `json:"parent_id,omitempty"` // 人工调整时的父候选
}

// StageSet 是一次分析产生的全部阶段候选（含被拒绝者，拒绝原因是一等数据）。
type StageSet struct {
	Candidates       []Candidate `json:"candidates"`
	TotalAssignedPct *float64    `json:"total_assigned_pct"` // 可能超过或不足 100%，不做归一
	UnassignedPct    *float64    `json:"unassigned_pct"`
	Note             string      `json:"note"`
}

// ChainStep 是残留率计算链中的一步，携带来源实体的稳定身份与版本。
type ChainStep struct {
	Step     string `json:"step"`
	Detail   string `json:"detail"`
	EntityID string `json:"entity_id"`
	Version  int    `json:"version"`
}

// Residue 是最终残留率及其完整计算链。
type Residue struct {
	ResiduePct    *float64    `json:"residue_pct"`
	FinalMassMg   *float64    `json:"final_mass_mg"`
	InitialMassMg float64     `json:"initial_mass_mg"`
	Chain         []ChainStep `json:"chain"`
}

// SourceRef 记录派生结果的直接来源（层 ID + 版本）。
type SourceRef struct {
	LayerID string `json:"layer_id"`
	Kind    string `json:"kind"`
	Version int    `json:"version"`
}

// F 返回 float64 指针的辅助函数。
func F(v float64) *float64 { return &v }

// FmtPct 格式化百分比显示。
func FmtPct(v *float64) string {
	if v == nil {
		return "未知"
	}
	return fmt.Sprintf("%.2f%%", *v)
}

// FmtMg 格式化质量显示。
func FmtMg(v *float64) string {
	if v == nil {
		return "未知"
	}
	return fmt.Sprintf("%.4f mg", *v)
}
