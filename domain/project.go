// Package domain: project 是事件溯源聚合, 负责命令校验、派生重放、
// 局部过期、人工裁定与冲突/撤销, 以及 as-of 历史回放。
package domain

import (
	"fmt"
	"time"
)

// ReactionTempC 是 O2 下氧化反应的特征温度(°C); 作为反应窗口的温度依据。
const ReactionTempC = 520.0

// Decision 记录一次人工裁定。
type Decision struct {
	ID          string `json:"id"`
	EventID     string `json:"eventId"`
	CandidateID string `json:"candidateId"`
	Action      string `json:"action"` // accept / reject / adjust
	Rationale   string `json:"rationale"`
	// 裁定所基于的候选快照与参数版本(回到旧事件时恢复这些)
	Candidate    StageCandidate `json:"candidate"`
	ParamFinger  string         `json:"paramFingerprint"`
	ResidueID    string         `json:"residueId"`
	Active       bool           `json:"active"`
	UndoneBy     string         `json:"undoneBy,omitempty"`
	SupersededBy string         `json:"supersededBy,omitempty"` // 冲突解决后被取代
	Time         time.Time      `json:"time"`
}

// Conflict 描述互斥候选被同时接受。
type Conflict struct {
	ID           string    `json:"id"`
	CandidateIDs []string  `json:"candidateIds"`
	Message      string    `json:"message"`
	ResolvedBy   string    `json:"resolvedBy,omitempty"`
	Winner       string    `json:"winner,omitempty"`
	Open         bool      `json:"open"`
	Time         time.Time `json:"time"`
}

// ArtifactStatus 描述一个派生结果相对当前输入的时效。
type ArtifactStatus struct {
	ID            string `json:"id"`
	Kind          string `json:"kind"` // corrected / dtg / stages / residue
	Current       bool   `json:"current"`
	Stale         bool   `json:"stale"`
	StaleReason   string `json:"staleReason,omitempty"`
	ProducedAtSeq int    `json:"producedAtSeq"`
	Viewable      bool   `json:"viewable"` // 过期结果仍可查看, 但不冒充当前
}

// Project 是内存中的聚合状态。
type Project struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	CreatedAt   time.Time    `json:"createdAt"`
	Events      []Event      `json:"events"`
	Submissions []Submission `json:"submissions"`
	Decisions   []Decision   `json:"decisions"`
	Conflicts   []Conflict   `json:"conflicts"`

	// 当前投影(随事件推进)
	layers        LayerData
	layerSource   map[string]string // 数据层 -> 最新提交 id
	params        Params
	paramVerID    string
	parentParamID string
	frame         DerivedFrame
	clock         Clock
	artifactLog   []ArtifactStatus
	// 用于记录最近一次命令的额外诊断(如半失败)
	lastDiags []Diagnostic
}

// NewProject 创建空项目。
func NewProject(id, name string, clock Clock) *Project {
	if clock == nil {
		clock = RealClock
	}
	p := &Project{
		ID:          id,
		Name:        name,
		CreatedAt:   clock(),
		layerSource: map[string]string{},
		params:      DefaultParams(),
		clock:       clock,
	}
	p.params.Raw = map[string]string{}
	p.frame.Params = p.params
	p.frame.ParamFingerprint = FingerprintParams(p.params)
	p.frame.ParamVersionID = stableID("paramv", "default", p.frame.ParamFingerprint)
	p.paramVerID = p.frame.ParamVersionID
	return p
}

// LatestFrame 返回当前派生帧。
func (p *Project) LatestFrame() DerivedFrame { return p.frame }

// ArtifactStatuses 返回派生结果时效表。
func (p *Project) ArtifactStatuses() []ArtifactStatus { return p.artifactLog }

// LastDiagnostics 返回最近命令诊断。
func (p *Project) LastDiagnostics() []Diagnostic { return p.lastDiags }

// Apply 校验并应用一条命令, 返回新事件。
func (p *Project) Apply(cmd Command) (Event, error) {
	ev := Event{
		Seq:     len(p.Events) + 1,
		ID:      stableID("evt", p.ID, itoa(len(p.Events)+1)),
		Time:    p.clock(),
		Payload: map[string]any{"command": cmd},
	}
	switch cmd.Kind {
	case "import":
		if err := p.applyImport(&ev, cmd); err != nil {
			return Event{}, err
		}
	case "apply_params":
		if err := p.applyParams(&ev, cmd); err != nil {
			return Event{}, err
		}
	case "decide":
		if err := p.applyDecision(&ev, cmd); err != nil {
			return Event{}, err
		}
	case "resolve_conflict":
		if err := p.applyResolve(&ev, cmd); err != nil {
			return Event{}, err
		}
	case "undo":
		if err := p.applyUndo(&ev, cmd); err != nil {
			return Event{}, err
		}
	default:
		return Event{}, fmt.Errorf("未知命令类型 %q", cmd.Kind)
	}
	p.Events = append(p.Events, ev)
	return ev, nil
}
