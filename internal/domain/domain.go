package domain

// Field 保留导入时的原始表示与单位；Num 仅在可规范化时填充。
// 无法转换的原始值留在原位并附诊断，派生层不得将其丢弃。
type Field struct {
	Raw         string   `json:"raw"`
	Unit        string   `json:"unit,omitempty"`
	Num         *float64 `json:"num,omitempty"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

const (
	KindSamples = "samples" // 原始采样点：炉时间 / 质量 / 温度
	KindDrift   = "drift"   // 天平漂移校正点
	KindFurnace = "furnace" // 炉程序段
	KindGas     = "gas"     // 气体阀事件
	KindBlank   = "blank"   // 空白曲线
	KindMass    = "mass"    // 样品初重（分层：坩埚皮重/毛重/净重）
)

func ValidKind(k string) bool {
	switch k {
	case KindSamples, KindDrift, KindFurnace, KindGas, KindBlank, KindMass:
		return true
	}
	return false
}

const (
	DocStatusCommitted = "committed" // 通过校验并提交
	DocStatusRejected  = "rejected"  // 格式错误，未提交，仅留诊断
)

// RawDoc 是一次导入中某一来源层的不可变文档，修订以新版本 + ParentID 表达。
type RawDoc struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	ImportID   string `json:"import_id"`
	Kind       string `json:"kind"`
	Version    int    `json:"version"`
	ParentID   string `json:"parent_id,omitempty"`
	Status     string `json:"status"`
	Filename   string `json:"filename,omitempty"`
	Note       string `json:"note,omitempty"`
	CreatedSeq int    `json:"created_seq"`
}

type SamplePoint struct {
	Idx  int   `json:"idx"`
	T    Field `json:"t"`
	M    Field `json:"m"`
	Temp Field `json:"temp"`
}

type DriftPoint struct {
	Idx    int   `json:"idx"`
	T      Field `json:"t"`
	Offset Field `json:"offset"`
}

type FurnaceSeg struct {
	Idx    int   `json:"idx"`
	Start  Field `json:"start"`
	End    Field `json:"end"`
	Rate   Field `json:"rate"`
	Target Field `json:"target"`
}

type GasEventRec struct {
	Idx   int    `json:"idx"`
	T     Field  `json:"t"`
	Valve string `json:"valve"`
	Gas   string `json:"gas"`
	Action string `json:"action"`
}

const (
	MassTierTare  = "tare"  // 坩埚皮重
	MassTierGross = "gross" // 坩埚+样品毛重
	MassTierNet   = "net"   // 样品净重（残留率分母）
)

type MassRec struct {
	Idx   int    `json:"idx"`
	Tier  string `json:"tier"`
	Basis string `json:"basis"` // wet / dry 等基准
	Value Field  `json:"value"`
}

// Params 是派生算法参数，每次修订产生新版本与参数指纹。
type Params struct {
	Version             int     `json:"version"`
	Note                string  `json:"note,omitempty"`
	AtmosphereDelayS    float64 `json:"atmosphere_delay_s"`
	ThermalLagS         float64 `json:"thermal_lag_s"`
	DTGSmoothWindow     int     `json:"dtg_smooth_window"`
	PeakProminenceMgPerS float64 `json:"peak_prominence_mg_per_s"`
	PeakThresholdMgPerS float64 `json:"peak_threshold_mg_per_s"`
	GapThresholdS       float64 `json:"gap_threshold_s"`
	BlankDriftTolMg     float64 `json:"blank_drift_tolerance_mg"`
}

func DefaultParams() Params {
	return Params{
		Version:              1,
		Note:                 "default",
		AtmosphereDelayS:     15,
		ThermalLagS:          20,
		DTGSmoothWindow:      5,
		PeakProminenceMgPerS: 0.002,
		PeakThresholdMgPerS:  0.005,
		GapThresholdS:        60,
		BlankDriftTolMg:      0.05,
	}
}

// 派生节点类型（DAG）。
const (
	NodeNormalized   = "normalized_curve"  // 规范化采样曲线
	NodeDriftCorrect = "drift_corrected"   // 天平漂移校正后曲线
	NodeBlankCheck   = "blank_check"       // 空白曲线比对
	NodeDTG          = "dtg_series"        // DTG 序列
	NodeGapSet       = "gap_set"           // 采样缺口集合（不插值）
	NodeFurnaceTL    = "furnace_timeline"  // 炉程序时间线
	NodeAtmosTL      = "atmosphere_timeline" // 气氛时间线（阀时间+延迟=反应时间）
	NodePeakSet      = "peak_set"          // DTG 峰集合
	NodeStage        = "stage_candidate"   // 单个阶段候选
	NodeResidual     = "residual_rate"     // 残留率（含逐阶段与最终）
)

const (
	NodeStatusCurrent = "current"
	NodeStatusStale   = "stale"
)

type SourceRef struct {
	Kind    string `json:"kind"` // document | node | params
	ID      string `json:"id"`
	Version int    `json:"version,omitempty"`
	Label   string `json:"label,omitempty"`
}

type Judgment struct {
	Rule     string   `json:"rule"`
	Status   string   `json:"status"` // satisfied | violated | unknown
	Target   string   `json:"target"`
	Message  string   `json:"message"`
	Evidence []string `json:"evidence,omitempty"`
}

const (
	JudgmentSatisfied = "satisfied"
	JudgmentViolated  = "violated"
	JudgmentUnknown   = "unknown"
)

type Node struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"project_id"`
	BranchID   string     `json:"branch_id"`
	Kind       string     `json:"kind"`
	OrderKey   int        `json:"order_key"`
	AlgoVersion string    `json:"algo_version"`
	ParamFP    string     `json:"param_fp"`
	Sources    []SourceRef `json:"sources"`
	FromSeq    int        `json:"from_seq"`
	ToSeq      int        `json:"to_seq,omitempty"`
	CreatedSeq int        `json:"created_seq"`
	Detail     []byte     `json:"detail"`
	Judgments  []Judgment `json:"judgments"`
}
