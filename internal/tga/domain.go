package tga

// 领域基础类型：所有物理量以“原始表示 + 规范化值 + 单位基准”三层保存。
// 原始层永远保留用户导入时的字符串；无法转换的值留在原位并附诊断，
// 仅在派生层（normalized != nil）参与计算与绘图。

// Layer 名称：六层来源必须分层保存。
const (
	LayerSamples    = "samples"    // 原始采样点  time_s, temp_c, mass_mg
	LayerCorrection = "correction" // 天平漂移校正声明 method, a, b
	LayerFurnace    = "furnace"    // 炉程序      time_s, target_temp_c
	LayerGas        = "gas"        // 气体阀事件  time_s, valve, gas
	LayerBlank      = "blank"      // 空白曲线    time_s, blank_mass_mg
	LayerInitial    = "initial"    // 样品初重    mass_mg, basis, note
)

// LayerOrder 是导入与显示的规范顺序。
var LayerOrder = []string{
	LayerSamples, LayerCorrection, LayerFurnace, LayerGas, LayerBlank, LayerInitial,
}

// LayerTitles 是面向审阅界面的中文标题。
var LayerTitles = map[string]string{
	LayerSamples:    "原始采样点",
	LayerCorrection: "天平漂移校正",
	LayerFurnace:    "炉程序",
	LayerGas:        "气体阀事件",
	LayerBlank:      "空白曲线",
	LayerInitial:    "样品初重",
}

// Status 是层/提交/候选的判定状态。
const (
	StatusOK       = "ok"       // 可用
	StatusPartial  = "partial"  // 局部失败（半失败）：可用片段与诊断并存
	StatusFailed   = "failed"   // 结构性失败，整层不参与派生
	StatusUnknown  = "unknown"  // 无法唯一决定（缺口、证据不足）
	StatusAccepted = "accepted" // 人工接受
	StatusRejected = "rejected" // 人工否定
)

// Severity 诊断等级。
const (
	SevError   = "error"
	SevWarning = "warning"
	SevInfo    = "info"
)

// Diagnostic 记录一条单元格/层级诊断。
type Diagnostic struct {
	Severity string `json:"severity"`
	Layer    string `json:"layer"`
	Row      int    `json:"row"`
	Column   string `json:"column"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Original string `json:"original,omitempty"` // 原始表示，无法转换时保留
}

// Q 是带单位基准的领域量。Original 是导入原文；Unit 是声明单位；
// Normalized 为规范化后的值（质量 mg、温度 °C、时间 s），无法转换时为 nil。
type Q struct {
	Original   string   `json:"original"`
	Unit       string   `json:"unit"`
	Normalized *float64 `json:"normalized"`
}

// Cell 是一个带身份的单元格：值 + 诊断。
type Cell struct {
	Q     Q           `json:"q"`
	Diags []Diagnostic `json:"diags,omitempty"`
}

// Row 是一层中的一行；Header 为列名（固定），Cells 与 Header 对齐。
type Row struct {
	ID    string   `json:"id"` // 稳定身份，如 P#samples#3
	Cells map[string]Cell `json:"cells"`
}

// Layer 是一次提交中一层的原始保存。
type Layer struct {
	Name    string       `json:"name"`
	Status  string       `json:"status"` // ok | partial | failed
	Header  []string     `json:"header"`
	Rows    []Row        `json:"rows"`
	Diags   []Diagnostic `json:"diags,omitempty"`
	Present bool         `json:"present"` // 该层是否在包中出现
}

// Submission 是一次不可覆盖的原始提交。修订通过新 Submission + ParentID 表达。
type Submission struct {
	ID        string              `json:"id"`
	ProjectID string              `json:"project_id"`
	Seq       int                 `json:"seq"`
	ParentID  string              `json:"parent_id,omitempty"`
	AtEventID string              `json:"at_event_id"` // 触发该提交的事件
	Origin    string              `json:"origin"`      // import | revision | half_failure
	Note      string              `json:"note,omitempty"`
	Layers    map[string]*Layer   `json:"layers"`
	Diags     []Diagnostic        `json:"diags,omitempty"`
	Status    string              `json:"status"` // ok | partial | failed
}

// SourceRef 指向某个派生结果的直接来源（原始单元格或另一个派生结果）。
type SourceRef struct {
	Kind     string `json:"kind"` // cell | artifact | params | decision
	SubmissionSeq int `json:"submission_seq,omitempty"`
	Layer    string `json:"layer,omitempty"`
	RowID    string `json:"row_id,omitempty"`
	Column   string `json:"column,omitempty"`
	ArtifactID string `json:"artifact_id,omitempty"`
	Note     string `json:"note,omitempty"`
}
