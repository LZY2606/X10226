package domain

// 输入层种类：原始采样点、天平漂移校正、炉程序、气体阀事件、空白曲线、样品初重。
const (
	InputRawPoints   = "raw_points"
	InputDrift       = "drift_correction"
	InputFurnace     = "furnace_program"
	InputGasEvents   = "gas_events"
	InputBlank       = "blank_curve"
	InputInitialMass = "initial_mass"
)

// InputKinds 按规范顺序列出全部分层输入。
var InputKinds = []string{
	InputRawPoints, InputDrift, InputFurnace, InputGasEvents, InputBlank, InputInitialMass,
}

// RawPoint 是一行原始采样点，保留导入时的原始单位与基准。
type RawPoint struct {
	Row  int      `json:"row"`
	Time Quantity `json:"time"`
	Mass Quantity `json:"mass"`
	Temp Quantity `json:"temp"`
}

// RawPointsDoc 是 raw_points 输入的解析结果。
type RawPointsDoc struct {
	Points      []RawPoint   `json:"points"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

// DriftCorrection 是天平漂移校正模型（线性漂移）。
type DriftCorrection struct {
	Model     string   `json:"model"` // "linear"
	Slope     Quantity `json:"slope"`
	Intercept Quantity `json:"intercept"`
}

// FurnaceSegment 是炉程序的一段（线性升温或恒温）。
type FurnaceSegment struct {
	StartTime Quantity `json:"start_time"`
	EndTime   Quantity `json:"end_time"`
	StartTemp Quantity `json:"start_temp"`
	EndTemp   Quantity `json:"end_temp"`
}

type FurnaceProgram struct {
	Segments []FurnaceSegment `json:"segments"`
}

// GasEvent 是一次气体阀切换事件（阀时间，不等于样品反应时间）。
type GasEvent struct {
	Time    Quantity `json:"time"`
	FromGas string   `json:"from_gas"`
	ToGas   string   `json:"to_gas"`
	Flow    Quantity `json:"flow"`
}

type GasEventsDoc struct {
	Events []GasEvent `json:"events"`
}

// BlankPoint 是空白（浮力/基线）曲线的一个点。
type BlankPoint struct {
	Time Quantity `json:"time"`
	Mass Quantity `json:"mass"`
}

type BlankCurve struct {
	Points []BlankPoint `json:"points"`
}

// InitialMass 是样品初重，携带称量基准（如 as-received / dry）。
type InitialMass struct {
	Mass  Quantity `json:"mass"`
	Basis string   `json:"basis"`
}

// Judgment 是机器可读取的判定事件载荷。
type Judgment struct {
	Code    string         `json:"code"`
	Subject string         `json:"subject"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// 判定码。
const (
	JAtmosphereDelay   = "atmosphere_delay"
	JStageOverlap      = "stage_overlap"
	JSumNotForced      = "sum_not_forced_100"
	JGapNoInterp       = "gap_no_interpolation"
	JUnknownCandidate  = "unknown_candidate"
	JDriftApplied      = "drift_applied"
	JNoiseTolerated    = "nonmonotonic_noise"
	JPartialImport     = "partial_import"
	JUnitUnconvertible = "unit_unconvertible"
)
