// Package core 定义热重分析（TGA）阶段分解与气氛事件系统的领域模型。
// 所有领域量都携带单位或坐标基准；原始表示在导入层保留，派生层才做规范化。
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// 层类型：基线由六类分层保存的输入构成。
const (
	LayerRawSamples     = "raw_samples"      // 原始采样点
	LayerDrift          = "drift_correction" // 天平漂移校正
	LayerFurnaceProgram = "furnace_program"  // 炉程序
	LayerGasEvents      = "gas_events"       // 气体阀事件
	LayerBlankCurve     = "blank_curve"      // 空白曲线
	LayerInitialMass    = "initial_mass"     // 样品初重
)

// LayerKinds 按导入顺序列出全部层类型（顺序稳定，导出/导入保持）。
var LayerKinds = []string{
	LayerRawSamples, LayerDrift, LayerFurnaceProgram,
	LayerGasEvents, LayerBlankCurve, LayerInitialMass,
}

// 层与结果状态。
const (
	StatusAccepted = "accepted"
	StatusRejected = "rejected"
	StatusCurrent  = "current"
	StatusStale    = "stale" // 过期：仍可查看，但不能冒充当前结果
)

// Diagnostic 描述无法规范化或格式错误的值；原值留在原位置并附此诊断。
type Diagnostic struct {
	LayerID string `json:"layer_id"`
	Index   int    `json:"index"`
	Field   string `json:"field"`
	Raw     string `json:"raw"`
	Message string `json:"message"`
}

// Layer 是一份不可覆盖的原始输入版本。修订以新版本 + ParentID 表达。
type Layer struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"project_id"`
	Kind        string       `json:"kind"`
	Version     int          `json:"version"`
	ParentID    string       `json:"parent_id,omitempty"`
	Format      string       `json:"format"`
	Raw         string       `json:"raw"` // 原始表示，一经提交不可覆盖
	Status      string       `json:"status"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	SubmittedAt int64        `json:"submitted_at"` // 虚拟时钟刻度
}

// Quantity 是带单位与坐标基准的领域量。
type Quantity struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Basis string  `json:"basis,omitempty"` // 例如 furnace_setpoint / sample / valve
}

// ---- 规范化后的层负载（派生层） ----

// SamplePoint 是一个采样点。Valid*=false 表示原值无法转换，留在原位置。
type SamplePoint struct {
	T        float64 `json:"t_s"`
	Mass     float64 `json:"mass_mg"`
	Temp     float64 `json:"temp_c"`
	ValidT   bool    `json:"valid_t"`
	ValidM   bool    `json:"valid_m"`
	ValidTmp bool    `json:"valid_temp"`
	RawT     string  `json:"raw_t"`
	RawM     string  `json:"raw_m"`
	RawTmp   string  `json:"raw_temp"`
}

type SampleSeries struct {
	Points   []SamplePoint `json:"points"`
	TimeUnit string        `json:"time_unit"` // 规范化后固定为 s
	MassUnit string        `json:"mass_unit"` // mg
	TempUnit string        `json:"temp_unit"` // C
}

// DriftPoint / BlankPoint 是校正曲线上的点（t_s, mg）。
type CurvePoint struct {
	T float64 `json:"t_s"`
	V float64 `json:"v_mg"`
}

type DriftCurve struct {
	Points []CurvePoint `json:"points"`
	Unit   string       `json:"unit"` // mg
}

type BlankCurve struct {
	Points []CurvePoint `json:"points"`
	Unit   string       `json:"unit"`
}

// FurnaceSegment 是炉程序的一段（等温或斜坡）。
type FurnaceSegment struct {
	StartS      float64 `json:"start_s"`
	EndS        float64 `json:"end_s"`
	StartC      float64 `json:"start_c"`
	EndC        float64 `json:"end_c"`
	RateCPerMin float64 `json:"rate_c_per_min"`
}

type FurnaceProgram struct {
	Segments []FurnaceSegment `json:"segments"`
	Basis    string           `json:"basis"` // furnace_setpoint
}

// GasEvent 是气体阀切换事件。阀切换时间不等于样品反应时间。
type GasEvent struct {
	ID        string  `json:"id"`
	ValveT    float64 `json:"valve_t_s"` // 阀切换时刻（阀坐标）
	FromGas   string  `json:"from_gas"`
	ToGas     string  `json:"to_gas"`
	FlowMlMin float64 `json:"flow_ml_min"`
}

type GasEventList struct {
	Events []GasEvent `json:"events"`
	Basis  string     `json:"basis"` // valve
}

// InitialMass 是样品初重（分层保存：称量记录 / 修订）。
type InitialMass struct {
	MassMg float64 `json:"mass_mg"`
	Basis  string  `json:"basis"` // as_weighed / dried / revised
	Note   string  `json:"note,omitempty"`
}

// ---- 派生结果 ----

// SourceRef 指向一个直接来源（层 + 版本）。
type SourceRef struct {
	LayerID string `json:"layer_id"`
	Kind    string `json:"kind"`
	Version int    `json:"version"`
}

// Result 是派生值，携带算法版本、参数指纹与直接来源。
type Result struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"project_id"`
	Kind        string          `json:"kind"` // corrected_curve|dtg|stage_set|residue|atmosphere_assessment|gap_policy
	AlgoVersion string          `json:"algo_version"`
	ParamFP     string          `json:"param_fp"` // 参数指纹
	Inputs      []SourceRef     `json:"inputs"`   // 直接来源
	Payload     json.RawMessage `json:"payload"`
	CreatedSeq  int64           `json:"created_seq"` // 产生它的事件序号
}

// 派生结果类型。
const (
	ResultCorrectedCurve = "corrected_curve"
	ResultDTG            = "dtg"
	ResultStageSet       = "stage_set"
	ResultResidue        = "residue"
	ResultAtmosphere     = "atmosphere_assessment"
	ResultGapPolicy      = "gap_policy"
)

// CorrectedPoint 是漂移/空白校正后的点。
type CorrectedPoint struct {
	T       float64 `json:"t_s"`
	Mass    float64 `json:"mass_mg"` // 校正后
	Temp    float64 `json:"temp_c"`
	Valid   bool    `json:"valid"`
	Segment int     `json:"segment"` // 采样缺口切分出的连续段编号
}

type CorrectedCurve struct {
	Points   []CorrectedPoint `json:"points"`
	Gaps     [][2]float64     `json:"gaps"` // 采样缺口 [start_s, end_s]
	MassUnit string           `json:"mass_unit"`
}

// DTGPoint 是微分热重点；缺口两侧不跨越插值，边缘点无值。
type DTGPoint struct {
	T      float64 `json:"t_s"`
	DMG    float64 `json:"dmg_s"`   // dm/dt, mg/s（失重为负）
	PctMin float64 `json:"pct_min"` // 相对初重 %/min
}

type DTGCurve struct {
	Points   []DTGPoint `json:"points"`
	Segments [][2]int   `json:"segments"` // 连续段在 Points 中的索引区间 [lo,hi)
}

// Stage 是一个分解阶段。阶段可重叠，且不强制总和为百分之百。
type Stage struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	StartS     float64  `json:"start_s"`
	EndS       float64  `json:"end_s"`
	PeakS      float64  `json:"peak_s"`
	PeakDMgS   float64  `json:"peak_dmg_s"`
	LossMg     float64  `json:"loss_mg"`
	LossPct    float64  `json:"loss_pct"` // 相对初重
	Overlaps   []string `json:"overlaps,omitempty"`
	CrossesGap bool     `json:"crosses_gap"` // 必须恒为 false：缺口不跨越插值
}

// StageCandidate 是一组合法候选分解；无法唯一决定时全部保留。
type StageCandidate struct {
	ID           string  `json:"id"`
	ResultID     string  `json:"result_id"`
	ProjectID    string  `json:"project_id"`
	Label        string  `json:"label"`
	Stages       []Stage `json:"stages"`
	TotalLossPct float64 `json:"total_loss_pct"` // 不强制 = 100 - residue
	Verdict      string  `json:"verdict"`        // pending|accepted|rejected
	RejectReason string  `json:"reject_reason,omitempty"`
	DecidedSeq   int64   `json:"decided_seq,omitempty"`
}

// Residue 是最终残留率，可展开计算链。
type Residue struct {
	FinalMassMg Quantity `json:"final_mass"`
	InitialMass Quantity `json:"initial_mass"`
	ResiduePct  float64  `json:"residue_pct"`
	AtS         float64  `json:"at_s"`
	CalcChain   []string `json:"calc_chain"` // 人类可读的计算步骤
}

// AtmosphereAssessment 是机器可读取的气氛延迟判定：
// 阀切换时间不等于样品反应时间。
type AtmosphereAssessment struct {
	GasEventID        string  `json:"gas_event_id"`
	ValveTimeS        float64 `json:"valve_time_s"`
	SampleTimeS       float64 `json:"sample_time_s"`
	DelayS            float64 `json:"delay_s"`
	TransportDelayS   float64 `json:"transport_delay_s"`
	ThermalLagS       float64 `json:"thermal_lag_s"`
	ValveEqualsSample bool    `json:"valve_time_equals_sample_time"` // 恒为 false
	Basis             string  `json:"basis"`                         // 判定依据
	FromGas, ToGas    string  `json:"from_gas,to_gas"`
}

// GapPolicy 是机器可读取的缺口策略：数据缺口不跨越插值。
type GapPolicy struct {
	GapStartS float64 `json:"gap_start_s"`
	GapEndS   float64 `json:"gap_end_s"`
	Policy    string  `json:"policy"` // no_interpolation
	MedianDtS float64 `json:"median_dt_s"`
}

// Params 是派生算法参数；指纹参与每个派生值。
type Params struct {
	TransportDelayS float64 `json:"transport_delay_s"` // 气体输运延迟
	ThermalLagS     float64 `json:"thermal_lag_s"`     // 温度滞后
	GapFactor       float64 `json:"gap_factor"`        // dt > factor*median 视为缺口
	SmoothWindow    int     `json:"smooth_window"`     // 平滑窗口（点）
	ProminenceMgS   float64 `json:"prominence_mg_s"`   // DTG 峰突出度阈值
	OverlapRatio    float64 `json:"overlap_ratio"`     // 谷/峰比超过则产生重叠峰歧义
	NoiseFloorMgS   float64 `json:"noise_floor_mg_s"`  // 非单调噪声下限
	AlgoVersion     string  `json:"algo_version"`
}

func DefaultParams() Params {
	return Params{
		TransportDelayS: 30,
		ThermalLagS:     12,
		GapFactor:       5,
		SmoothWindow:    5,
		ProminenceMgS:   0.004,
		OverlapRatio:    0.55,
		NoiseFloorMgS:   0.0015,
		AlgoVersion:     "tga-derive/1.0.0",
	}
}

// Fingerprint 返回参数指纹（稳定、机器可校验）。
func (p Params) Fingerprint() string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8])
}

// Event 是追加式历史记录；冲突解决与撤销也写入历史。
type Event struct {
	Seq       int64           `json:"seq"`
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Kind      string          `json:"kind"`
	Payload   json.RawMessage `json:"payload"`
	At        int64           `json:"at"`
}

// 事件类型。
const (
	EvLayerImported        = "layer_imported"
	EvLayerRejected        = "layer_rejected"
	EvImportPartialFailure = "import_partial_failure"
	EvDerivationRun        = "derivation_run"
	EvCandidateGenerated   = "candidate_generated"
	EvAdjudication         = "adjudication"      // 接受/否定/参数调整
	EvConflict             = "conflict_recorded" // 互相冲突的人工决定
	EvRevert               = "revert"            // 撤销/回到旧事件
	EvAtmosphereJudgment   = "atmosphere_delay_assessment"
	EvGapPolicy            = "gap_policy_applied"
	EvProjectExported      = "project_exported"
	EvProjectImported      = "project_imported"
)

func (e Event) String() string {
	return fmt.Sprintf("#%d %s", e.Seq, e.Kind)
}
