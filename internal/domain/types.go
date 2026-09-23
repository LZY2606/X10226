// Package domain 定义热重分析（TGA）阶段分解系统的核心领域类型。
// 所有领域量都携带单位或坐标基准；原始表示在导入层保留，派生层才做规范化。
package domain

import "fmt"

// SourceKind 是分层保存的原始输入类别。
type SourceKind string

const (
	SrcRawSamples   SourceKind = "raw_samples"       // 原始采样点（质量/温度/炉时间）
	SrcDrift        SourceKind = "drift_correction"  // 天平漂移校正
	SrcFurnaceProg  SourceKind = "furnace_program"   // 炉程序
	SrcGasEvents    SourceKind = "gas_events"        // 气体阀事件
	SrcBlankCurve   SourceKind = "blank_curve"       // 空白曲线
	SrcInitialMass  SourceKind = "initial_mass"      // 样品初重（可分层多条）
)

// AllSourceKinds 按固定顺序列出全部源类别（顺序稳定，用于展示与导出）。
var AllSourceKinds = []SourceKind{
	SrcRawSamples, SrcDrift, SrcFurnaceProg, SrcGasEvents, SrcBlankCurve, SrcInitialMass,
}

// SourceVersion 表示一份不可覆盖的原始输入版本。
// 后续修订以新版本 + ParentID 表达，旧版本永远保留。
type SourceVersion struct {
	ID          string     `json:"id"`
	Kind        SourceKind `json:"kind"`
	ParentID    string     `json:"parent_id,omitempty"` // 修订链父版本
	Seq         int64      `json:"seq"`                 // 全局单调序号（决定可见性/时间旅行）
	SubmittedAt string     `json:"submitted_at"`        // 虚拟时钟时间戳
	Raw         string     `json:"raw"`                 // 原始表示，逐字保留
	Diagnostics []string   `json:"diagnostics,omitempty"`
}

// DerivedKind 是派生数据类别。
type DerivedKind string

const (
	DerCurve   DerivedKind = "tg_dtg_curve"    // 规范化 + 漂移/空白校正后的 TG/DTG 曲线
	DerStages  DerivedKind = "stage_candidates" // 阶段分解候选（可重叠、可多解）
	DerResidue DerivedKind = "residue"          // 最终残留率（含计算链）
)

// DerivedStatus 标记派生结果是否仍为当前结果。
type DerivedStatus string

const (
	StatusCurrent DerivedStatus = "current" // 当前有效
	StatusStale   DerivedStatus = "stale"   // 依赖已过期：可查看，但不能冒充当前结果
)

// Derived 是一条派生值，携带算法版本、参数指纹与直接来源。
type Derived struct {
	ID         string       `json:"id"`
	Kind       DerivedKind  `json:"kind"`
	AlgoVersion string      `json:"algo_version"`
	ParamFP    string       `json:"param_fingerprint"` // 参数指纹
	SourceIDs  []string     `json:"source_ids"`        // 直接来源（仅直接依赖）
	Seq        int64        `json:"seq"`
	Status     DerivedStatus `json:"status"`
	Payload    string       `json:"payload"` // JSON
}

// Point 是曲线上的一个采样点。T 为炉时间（秒），Temp 为温度（°C），Mass 为质量（mg）。
type Point struct {
	T    float64 `json:"t"`
	Temp float64 `json:"temp"`
	Mass float64 `json:"mass"`
}

// CurvePayload 是 tg_dtg_curve 的载荷。
// Gaps 记录采样缺口区间：缺口不跨越插值，DTG 在缺口处为未知。
type CurvePayload struct {
	Points     []Point   `json:"points"`
	DTG        []float64 `json:"dtg"`          // 与 Points 对齐；缺口处为 NaN 标记为 NullDTG
	DTGKnown   []bool    `json:"dtg_known"`    // 对应 DTG 是否有效（缺口边界为 false）
	Gaps       [][2]float64 `json:"gaps"`      // 缺口时间区间
	MassPct    []float64 `json:"mass_pct"`     // 相对初重的质量百分比
	InitialMassID string `json:"initial_mass_id"` // 使用的初重版本
	InitialMassMg float64 `json:"initial_mass_mg"`
}

// StageCandidate 是一个阶段分解候选。阶段可重叠，不强制总和为 100%。
type StageCandidate struct {
	ID          string   `json:"id"`           // 稳定身份（候选级）
	Label       string   `json:"label"`
	SegIndex    int      `json:"seg_index"`    // 所属连续段（缺口分段）
	TStart      float64  `json:"t_start"`      // 坐标基准：炉时间秒
	TEnd        float64  `json:"t_end"`
	TempStart   float64  `json:"temp_start"`   // °C
	TempEnd     float64  `json:"temp_end"`
	PeakT       float64  `json:"peak_t"`       // DTG 峰位置
	MassLossPct float64  `json:"mass_loss_pct"`
	OverlapGroup int     `json:"overlap_group"` // 同一重叠组的候选不唯一确定
	Atmosphere  string   `json:"atmosphere"`    // 经气氛延迟换算后的有效气氛
	Rejected    bool     `json:"rejected"`
	RejectReason string  `json:"reject_reason,omitempty"`
	Unknown     bool     `json:"unknown"`       // 无法唯一决定时的合法候选标记
}

// StagesPayload 是 stage_candidates 的载荷。
type StagesPayload struct {
	Candidates []StageCandidate `json:"candidates"`
	Notes      []string         `json:"notes,omitempty"` // 机器可读的判定依据
}

// ResiduePayload 是 residue 的载荷，包含可展开的计算链。
type ResiduePayload struct {
	ResiduePct   float64  `json:"residue_pct"`
	FinalMassMg  float64  `json:"final_mass_mg"`
	Chain        []string `json:"chain"` // 计算链：源版本 ID、算法版本、参数指纹、裁定事件
	AcceptedStages []string `json:"accepted_stages"`
	StageSumPct  float64  `json:"stage_sum_pct"` // 不强制等于 100-残留
}

// DecisionAction 是人工决定/系统事件的类型。
type DecisionAction string

const (
	ActAccept    DecisionAction = "accept"          // 采纳候选
	ActReject    DecisionAction = "reject"          // 否定候选
	ActAdjust    DecisionAction = "adjust_params"   // 参数调整（如气氛延迟）
	ActUndo      DecisionAction = "undo"            // 撤销某事件
	ActConflict  DecisionAction = "conflict"        // 记录冲突
	ActResolve   DecisionAction = "resolve"         // 冲突解决
	ActImport    DecisionAction = "import"          // 导入批次事件
	ActRuling    DecisionAction = "ruling"          // 机器可读的领域判定依据
)

// Event 是追加式事件日志条目。冲突解决与撤销都写入历史。
type Event struct {
	ID       string         `json:"id"`
	Seq      int64          `json:"seq"`
	At       string         `json:"at"`
	Actor    string         `json:"actor"`
	Action   DecisionAction `json:"action"`
	TargetID string         `json:"target_id,omitempty"`
	Payload  string         `json:"payload,omitempty"` // JSON
	UndoneBy string         `json:"undone_by,omitempty"`
}

// Params 是影响派生的全局参数；其指纹写入每条派生值。
type Params struct {
	AtmosphereDelayS float64 `json:"atmosphere_delay_s"` // 气氛延迟：阀切换时间 ≠ 样品反应时间
	TempLagS         float64  `json:"temp_lag_s"`         // 温度滞后
	GapThresholdS    float64  `json:"gap_threshold_s"`    // 采样缺口阈值
	SmoothWindow     int      `json:"smooth_window"`      // DTG 平滑窗口（抑制非单调噪声）
	PeakMinPromPct   float64  `json:"peak_min_prom_pct"`  // 峰最小显著度（%/min）
}

// DefaultParams 返回默认参数。
func DefaultParams() Params {
	return Params{
		AtmosphereDelayS: 30,
		TempLagS:         15,
		GapThresholdS:    120,
		SmoothWindow:     3,
		PeakMinPromPct:   0.05,
	}
}

// Fingerprint 计算参数指纹（稳定字符串）。
func (p Params) Fingerprint() string {
	return fmt.Sprintf("delay=%g;lag=%g;gap=%g;win=%d;prom=%g",
		p.AtmosphereDelayS, p.TempLagS, p.GapThresholdS, p.SmoothWindow, p.PeakMinPromPct)
}
