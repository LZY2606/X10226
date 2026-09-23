// Package domain: events 是事件溯源的命令与事件定义。
//
// 不可变原则: 原始提交只能追加, 不能覆盖; 修订通过新提交 + parent 链表达。
// 派生结果(corrected/dtg/stages/residue)由事件重放确定性地重新计算,
// 每个派生结果带算法版本/参数指纹/直接来源; 局部源改动只使依赖它的结果过期。
package domain

import "time"

type EventType string

const (
	EvProjectCreated   EventType = "project_created"
	EvImported         EventType = "imported"       // 新原始提交(可 partial)
	EvParamsApplied    EventType = "params_applied" // 参数修订(新版本, 父=旧参数版本)
	EvComputed         EventType = "computed"       // 一次派生重算快照的版本标记
	EvDecision         EventType = "decision"       // 接受/否定/调整
	EvConflictResolved EventType = "conflict_resolved"
	EvUndone           EventType = "undone" // 撤销某决定
)

// Command 是可追加到项目上的意图。
type Command struct {
	Kind     string `json:"kind"` // import / apply_params / decide / resolve_conflict / undo
	Text     string `json:"text,omitempty"`
	Title    string `json:"title,omitempty"`
	ParentID string `json:"parentId,omitempty"`
	// decide
	CandidateID string `json:"candidateId,omitempty"`
	Decision    string `json:"decision,omitempty"` // accept / reject / adjust
	Rationale   string `json:"rationale,omitempty"`
	AdjustKey   string `json:"adjustKey,omitempty"`
	AdjustRaw   string `json:"adjustRaw,omitempty"`
	// resolve_conflict
	ConflictID  string `json:"conflictId,omitempty"`
	Winner      string `json:"winner,omitempty"` // accepted_vs_id
	UndoEventID string `json:"undoEventId,omitempty"`
}

// Event 是持久化的不可变事实。
type Event struct {
	Seq      int            `json:"seq"`
	ID       string         `json:"id"`
	Type     EventType      `json:"type"`
	Time     time.Time      `json:"time"`
	ParentID string         `json:"parentId,omitempty"`
	Payload  map[string]any `json:"payload"`
	// Replay 计算结果(由引擎填充, 与事件一起存储以便历史快照)
	Derived *DerivedFrame `json:"derived,omitempty"`
}

// DerivedFrame 是某事件之后的派生结果快照; 过期标记是相对“当前”状态的,
// 历史帧本身永不变质。
type DerivedFrame struct {
	Params           Params          `json:"params"`
	ParamFingerprint string          `json:"paramFingerprint"`
	ParamVersionID   string          `json:"paramVersionId"`
	CorrectedID      string          `json:"correctedId"`
	DTGID            string          `json:"dtgId"`
	Corrected        CorrectedSeries `json:"corrected"`
	DTG              DTGSeries       `json:"dtg"`
	Stages           StageSet        `json:"stages"`
	Residue          Residue         `json:"residue"`
	Furnace          FurnaceCurve    `json:"furnace"`
	Diagnostics      []Diagnostic    `json:"diagnostics"`
	SubmissionSeq    int             `json:"submissionSeq"`
}
