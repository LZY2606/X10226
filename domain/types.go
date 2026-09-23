// Package domain 定义热重分析(TGA)证据链系统的领域模型。
//
// 设计约束:
//   - 所有领域量携带单位或坐标基准(Coord); 原始表示(RawText)在导入层原样保留,
//     派生层才规范化; 无法转换的值留在原位置并附诊断。
//   - 原始输入一经提交不可覆盖; 修订以新版本+父版本关系表达。
//   - 所有派生值携带算法版本、参数指纹与直接来源。
package domain

import "time"

// Coord 是时间/温度坐标的基准说明。
// 炉时间全部以“炉程序起点为零的分钟数”(furnace minutes, [min@furnace]) 为
// 规范化坐标; 原始导入若提供绝对时间戳则原样保留。
const CoordFurnaceMinute = "furnace_minute"

// QuantStatus 是一个量化值的可信度状态。
type QuantStatus string

const (
	StatusOK      QuantStatus = "ok"      // 已成功规范化
	StatusUnknown QuantStatus = "unknown" // 缺失/未知, 但结构合法
	StatusInvalid QuantStatus = "invalid" // 无法转换; 保留原始表示与诊断
)

// Quant 是带单位、原始表示与坐标基准的标量领域量。
type Quant struct {
	Value      float64     `json:"value"`
	Unit       string      `json:"unit"`    // 规范化单位, 如 mg / degC / min
	RawText    string      `json:"rawText"` // 导入时的原始表示, 永不丢弃
	RawUnit    string      `json:"rawUnit,omitempty"`
	Status     QuantStatus `json:"status"`
	Diagnostic string      `json:"diagnostic,omitempty"`
}

// QTime 是一个带坐标基准的时间点。
type QTime struct {
	Minutes    float64 `json:"minutes"` // 规范化: 自炉程序起点的分钟数
	Coord      string  `json:"coord"`   // 通常为 CoordFurnaceMinute
	RawText    string  `json:"rawText"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

// RawSample 是原始采样点; 质量/温度可能无效, 时间也可能缺失(采样缺口)。
type RawSample struct {
	Seq  int   `json:"seq"`
	Line int   `json:"line"`
	Time QTime `json:"time"`
	Mass Quant `json:"mass"`
	Temp Quant `json:"temp"`
}

// BlankPoint 是空白曲线(空坩埚)采样点。
type BlankPoint struct {
	Seq  int   `json:"seq"`
	Line int   `json:"line"`
	Time QTime `json:"time"`
	Mass Quant `json:"mass"`
}

// DriftAnchor 是天平漂移校正锚点(恒温段天平读数偏移观测)。
type DriftAnchor struct {
	Seq    int    `json:"seq"`
	Line   int    `json:"line"`
	Time   QTime  `json:"time"`
	Offset Quant  `json:"offset"` // 观测到的偏移(带符号), 规范化 mg
	Note   string `json:"note,omitempty"`
}

// FurnaceSeg 是炉程序的一段: 在 [Start,End] 内温度由 From 线性变化到 To。
type FurnaceSeg struct {
	Seq   int    `json:"seq"`
	Line  int    `json:"line"`
	Kind  string `json:"kind"` // ramp
	Start QTime  `json:"start"`
	End   QTime  `json:"end"`
	From  Quant  `json:"from"`
	To    Quant  `json:"to"`
}

// GasEvent 是气体阀事件。阀切换时间 != 样品反应时间(气氛延迟+温度滞后)。
type GasEvent struct {
	Seq     int    `json:"seq"`
	Line    int    `json:"line"`
	Time    QTime  `json:"time"`
	FromGas string `json:"fromGas"`
	ToGas   string `json:"toGas"`
	RawText string `json:"rawText,omitempty"`
}

// InitialMass 是样品初重分层保存的一次声明; 修订产生新版本(父版本链)。
type InitialMass struct {
	Seq   int    `json:"seq"`
	Line  int    `json:"line"`
	Mass  Quant  `json:"mass"`
	Basis string `json:"basis"` // balance_reading / corrected / revised
	Note  string `json:"note,omitempty"`
}

// Layer 标识一次提交内的六种原始分层之一。
type Layer string

const (
	LayerSamples     Layer = "samples"      // 原始采样点
	LayerDrift       Layer = "drift"        // 天平漂移校正
	LayerFurnace     Layer = "furnace"      // 炉程序
	LayerGas         Layer = "gas"          // 气体阀事件
	LayerBlank       Layer = "blank"        // 空白曲线
	LayerInitialMass Layer = "initial_mass" // 样品初重
)

// Diagnostic 是导入/计算过程中附着在对象上的诊断信息(一等数据)。
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	Layer   string `json:"layer,omitempty"`
}

// LayerData 以分层形式保存一次提交解析出的全部内容。
type LayerData struct {
	Samples       []RawSample   `json:"samples"`
	DriftAnchors  []DriftAnchor `json:"driftAnchors"`
	FurnaceSegs   []FurnaceSeg  `json:"furnaceSegs"`
	GasEvents     []GasEvent    `json:"gasEvents"`
	Blank         []BlankPoint  `json:"blank"`
	InitialMasses []InitialMass `json:"initialMasses"`
	Params        []RawParam    `json:"params,omitempty"`
}

// RawParam 是参数段中的原始键值声明(原样保留, 派生层才解释)。
type RawParam struct {
	Line    int    `json:"line"`
	Key     string `json:"key"`
	RawText string `json:"rawText"`
	Note    string `json:"note,omitempty"`
}

func (l LayerData) Empty() bool {
	return len(l.Samples)+len(l.DriftAnchors)+len(l.FurnaceSegs)+
		len(l.GasEvents)+len(l.Blank)+len(l.InitialMasses) == 0
}

// Submission 是一次不可变导入。
type Submission struct {
	ID          string         `json:"id"`
	Seq         int            `json:"seq"`
	ParentID    string         `json:"parentId,omitempty"` // 修订链
	Kind        string         `json:"kind"`               // initial / revision / partial
	Title       string         `json:"title"`
	RawText     string         `json:"rawText"` // 原始文本逐字保存
	Layers      LayerData      `json:"layers"`
	Present     map[Layer]bool `json:"present"` // 本次出现了哪些分层
	Status      string         `json:"status"`  // accepted / partial / rejected
	Diagnostics []Diagnostic   `json:"diagnostics"`
	CreatedAt   time.Time      `json:"createdAt"`
}

// Clock 抽象时间, 测试使用虚拟时间。
type Clock func() time.Time

// RealClock 是系统墙钟。
func RealClock() time.Time { return time.Now().UTC() }
