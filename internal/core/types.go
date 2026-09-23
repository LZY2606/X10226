// Package core 定义热重分析复核系统的核心领域类型：
// 原始文档（不可覆盖、版本化）、事件（溯源日志）、派生结果
// （带算法版本/参数指纹/直接来源）与判定。
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// DocKind 是原始输入文档的种类。
type DocKind string

const (
	DocRawSamples      DocKind = "raw_samples"      // 原始采样点
	DocDriftCorrection DocKind = "drift_correction" // 天平漂移校正
	DocFurnaceProgram  DocKind = "furnace_program"  // 炉程序
	DocGasEvents       DocKind = "gas_events"       // 气体阀事件
	DocBlankCurve      DocKind = "blank_curve"      // 空白曲线
	DocInitialWeight   DocKind = "initial_weight"   // 样品初重
)

// AllDocKinds 列出全部文档种类（导入顺序固定）。
var AllDocKinds = []DocKind{
	DocRawSamples, DocDriftCorrection, DocFurnaceProgram,
	DocGasEvents, DocBlankCurve, DocInitialWeight,
}

// Diagnostic 是一条机器可读的诊断。
type Diagnostic struct {
	Severity string `json:"severity"` // info | warning | error
	Code     string `json:"code"`
	Message  string `json:"message"`
	Line     int    `json:"line,omitempty"`
}

// Document 是一份原始输入文档的某个版本。一经提交不可覆盖；
// 修订以新版本（ParentID 指向前版）表达。
type Document struct {
	ID          string       `json:"id"`
	LineageID   string       `json:"lineage_id"` // 同一文档谱系的根 ID
	Kind        DocKind      `json:"kind"`
	Version     int          `json:"version"`
	ParentID    string       `json:"parent_id,omitempty"`
	Content     string       `json:"content"` // 原始文本，逐字保留
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	SubmittedAt string       `json:"submitted_at"`
	Retracted   bool         `json:"retracted,omitempty"` // 被撤销（undo）的版本
}

// ContentID 计算内容寻址 ID：同内容同谱系位置必得同 ID，
// 因此导出/删除/再导入后身份保持不变。
func ContentID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}

// DocID 计算文档版本 ID。
func DocID(lineageID string, kind DocKind, version int, content string) string {
	return ContentID("doc", lineageID, string(kind), fmt.Sprint(version), content)
}

// 事件类型。
const (
	EvImportSubmit  = "import.submit"    // 导入提交（可为半失败）
	EvImportRevise  = "import.revise"    // 文档修订（新版本+父版本）
	EvParamsRevise  = "params.revise"    // 参数修订
	EvComputeRun    = "compute.run"      // 一次派生计算（含结果）
	EvDecisionMake  = "decision.make"    // 人工裁定
	EvDecisionSolve = "decision.resolve" // 冲突解决
	EvUndo          = "event.undo"       // 撤销某事件
)

// Event 是溯源日志中的一条记录。ID 由 (项目, 前序事件, 类型, 负载, 逻辑时间)
// 哈希得到，形成哈希链；重放与再导入后身份不变。
type Event struct {
	ID        string          `json:"id"`
	Seq       int64           `json:"seq"`
	ProjectID string          `json:"project_id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Actor     string          `json:"actor"`
	At        string          `json:"at"` // 虚拟时间（ISO8601）
	PrevID    string          `json:"prev_id"`
}

// EventID 计算事件 ID（哈希链）。
func EventID(projectID, prevID, typ string, payload []byte, at string) string {
	return ContentID("evt", projectID, prevID, typ, string(payload), at)
}

// ResultStatus 是派生结果的状态。
type ResultStatus string

const (
	StatusCurrent    ResultStatus = "current"    // 当前有效
	StatusStale      ResultStatus = "stale"      // 依赖已变化，过期但仍可查看
	StatusSuperseded ResultStatus = "superseded" // 已被重算结果取代
)

// Result 是一条派生结果。任何派生值都带算法版本、参数指纹与直接来源。
type Result struct {
	ID               string          `json:"id"`
	Kind             string          `json:"kind"` // corrected_series | dtg | gap | noise_flag | atmosphere_event | stage_candidate | stage_sum | residue
	Stage            string          `json:"stage"`
	AlgoVersion      string          `json:"algo_version"`
	ParamFingerprint string          `json:"param_fingerprint"`
	SourceIDs        []string        `json:"source_ids"` // 直接来源（文档版本 ID）
	Payload          json.RawMessage `json:"payload"`
	Status           ResultStatus    `json:"status"`
	CreatedSeq       int64           `json:"created_seq"`
	InputFingerprint string          `json:"input_fingerprint"` // 输入指纹，用于局部失效判定
}

// ResultID 计算派生结果 ID：由种类、输入指纹与序号决定，重算同输入得同 ID。
func ResultID(kind, inputFingerprint string, ordinal int) string {
	return ContentID("res", kind, inputFingerprint, fmt.Sprint(ordinal))
}

// Verdict 是人工裁定的结论。
type Verdict string

const (
	VerdictAccept Verdict = "accept"
	VerdictReject Verdict = "reject"
	VerdictAdjust Verdict = "adjust"
)

// Decision 是一次人工裁定。
type Decision struct {
	ID          string   `json:"id"`
	EventSeq    int64    `json:"event_seq"`
	CandidateID string   `json:"candidate_id"`
	Verdict     Verdict  `json:"verdict"`
	Reason      string   `json:"reason"`
	Actor       string   `json:"actor"`
	BaseSeq     int64    `json:"base_seq"` // 作出决定时看到的日志位置（用于冲突检测）
	Active      bool     `json:"active"`
	Supersedes  []string `json:"supersedes,omitempty"`
}

// Conflict 是两个不兼容裁定之间的冲突。
type Conflict struct {
	ID          string   `json:"id"`
	CandidateID string   `json:"candidate_id"`
	DecisionIDs []string `json:"decision_ids"`
	Resolved    bool     `json:"resolved"`
	ChosenID    string   `json:"chosen_id,omitempty"`
	Reason      string   `json:"reason,omitempty"`
}
