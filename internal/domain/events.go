package domain

import "encoding/json"

// Event kinds of the append-only review log.
const (
	EvProjectCreated = "project_created"
	EvImport         = "source_imported" // half-failed imports still land here (per fragment accepted/rejected)
	EvDecision       = "decision"        // accept / reject / adjust a candidate or residue
	EvConflict       = "conflict"        // two incompatible human decisions recorded
	EvConflictResolved = "conflict_resolved"
	EvUndone         = "undone"          // soft undo: earlier event is marked reverted
)

// Judgment principles are machine-readable evaluation rules. Each concrete
// finding emitted by review carries one of these codes, so the reasons behind
// the counterexample archive stay readable by software as well as people.
const (
	PrincipleValveNotReaction = "P_VALVE_NOT_REACTION" // 气氛延迟/温滞：阀切换时刻 != 样品反应时刻
	PrincipleStagesOverlap    = "P_STAGES_OVERLAP"     // 阶段可重叠
	PrincipleNoTotal100       = "P_NO_FORCED_100"       // 不强制各段失重之和为 100%
	PrincipleGapNoInterp      = "P_GAP_NO_INTERPOLATION" // 数据缺口不跨越插值
	PrincipleAmbiguousKept    = "P_AMBIGUOUS_CANDIDATES"  // 无法唯一决定时保留全部合法候选
	PrincipleLocalInvalidate  = "P_LOCAL_INVALIDATION"    // 局部输入变化只使直接/间接依赖过期
	PrincipleRawImmutable     = "P_RAW_IMMUTABLE"         // 原始输入不可覆盖，以版本+父版本表达修订
	PrincipleHalfFailure      = "P_IMPORT_HALF_FAILURE"   // 导入半失败：可用片段落库，错误片段拒绝
	PrincipleUnitKept         = "P_UNIT_KEPT"             // 无法转换的值留在原位并附诊断
)

// Principle describes one machine-readable judgment rule.
type Principle struct {
	Code      string `json:"code"`
	Title     string `json:"title"`
	Statement string `json:"statement"`
}

// Principles is the fixed registry rendered into the UI and exported bundles.
var Principles = []Principle{
	{PrincipleValveNotReaction, "阀切换时刻不等于反应时刻",
		"气体管路延迟与炉体/样品温度滞后意味着样品实际经历的气氛变化晚于阀切换；阶段归属使用样品侧到达时刻而非阀时间。"},
	{PrincipleStagesOverlap, "阶段允许重叠",
		"重叠 DTG 峰可产生时间上重叠的阶段候选，系统不把它们强行切成互斥区间。"},
	{PrincipleNoTotal100, "不强制失重总和为百分之百",
		"各阶段失重率按各自初重基准计算，允许重叠且不做归一化，因此其和不等于 100% 是正常结果。"},
	{PrincipleGapNoInterp, "缺口不插值",
		"采样缺口处不进行跨越插值；DTG 与峰检测在缺口处分段，跨缺口的候选标记为 unknown。"},
	{PrincipleAmbiguousKept, "保留全部合法候选",
		"无法唯一决定时（如重叠峰不可分），保留全部合法候选及拒绝理由，由人工裁定。"},
	{PrincipleLocalInvalidate, "局部依赖失效",
		"源数据改动只使依赖该层的派生结果过期，其余结果保持当前；过期结果可见但不可冒充当前。"},
	{PrincipleRawImmutable, "原始输入不可覆盖",
		"提交后的原始文本不可修改；修订以新版本与父版本关系表达，派生值携带算法版本与参数指纹。"},
	{PrincipleHalfFailure, "导入半失败",
		"一批输入中合法片段分层落库，非法片段被拒绝并附诊断，整批不以全有/全无方式回滚。"},
	{PrincipleUnitKept, "单位/原始表示保留",
		"所有领域量携带单位或坐标基准；无法转换的值留在原位置并附诊断，不为绘图而丢弃。"},
}

// Event is one append-only record of the review history.
type Event struct {
	Seq        int             `json:"seq"`
	ID         string          `json:"id"`
	ProjectID  string          `json:"projectId"`
	Kind       string          `json:"kind"`
	AtMS       int64           `json:"atMs"`
	Actor      string          `json:"actor"`
	RevertedOf string          `json:"revertedOf,omitempty"` // for EvUndone
	Payload    json.RawMessage `json:"payload"`
}

// ImportPayload records an import batch and every fragment it created.
type ImportPayload struct {
	BatchID    string          `json:"batchId"`
	Reason     string          `json:"reason,omitempty"` // scenario name / note
	Fragments  []ImportFragment `json:"fragments"`
	Principles []string        `json:"principles,omitempty"`
}

// ImportFragment is one layer attempt inside an import batch.
type ImportFragment struct {
	SourceID   string       `json:"sourceId"`
	Kind       string       `json:"kind"`
	Name       string       `json:"name"`
	Accepted   bool         `json:"accepted"`
	Version    int          `json:"version,omitempty"`
	ParentID   string       `json:"parentId,omitempty"`
	Diagnostic *Diagnostic  `json:"diagnostic,omitempty"`
}

// DecisionPayload covers accept / reject / parameter adjustments.
type DecisionPayload struct {
	TargetType  string   `json:"targetType"` // peak_candidate | stage | residue
	TargetID    string   `json:"targetId"`
	Action      string   `json:"action"`     // accept | reject | adjust
	Note        string   `json:"note,omitempty"`
	Principles  []string `json:"principles,omitempty"`
	// Adjustment fields (action=adjust).
	ParamKey    string   `json:"paramKey,omitempty"`
	OldValue    string   `json:"oldValue,omitempty"`
	NewValue    string   `json:"newValue,omitempty"`
	// Conflict bookkeeping.
	ConflictsWith string `json:"conflictsWith,omitempty"`
}

// ConflictPayload records mutually incompatible human decisions.
type ConflictPayload struct {
	DecisionA  string `json:"decisionA"`
	DecisionB  string `json:"decisionB"`
	TargetID   string `json:"targetId"`
	Message    string `json:"message"`
	Resolved   bool   `json:"resolved"`
	Resolution string `json:"resolution,omitempty"`
}

// ResolvePayload resolves a conflict.
type ResolvePayload struct {
	ConflictEventID string `json:"conflictEventId"`
	WinnerEventID   string `json:"winnerEventId"`
	Note            string `json:"note,omitempty"`
}

// Project metadata.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Note      string `json:"note,omitempty"`
	CreatedAt int64  `json:"createdAtMs"`
}
