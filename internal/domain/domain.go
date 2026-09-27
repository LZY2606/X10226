// Package domain 定义热重分析(TGA)阶段分解/气氛事件系统的核心模型。
// 分层约定:
//   Raw    —— 导入时逐字符保留的原始表示, 永不规范化、永不覆盖;
//   Parsed —— 带单位/坐标基准的结构化解析值, 无法转换的值留在原位并附诊断;
//   Derived—— 规范化后的派生层(TG/DTG/阶段候选/残留率), 带算法版本、参数指纹、直接来源。
package domain

import "encoding/json"

// AlgVersion 是派生算法版本, 任何算法/判定规则变化都必须提升版本。
const AlgVersion = "tga-derive-1.0.0"

// Kind 标识分层来源材料的类型。
type Kind string

const (
	KindSamples    Kind = "samples"
	KindDrift      Kind = "drift_correction"
	KindFurnace    Kind = "furnace_program"
	KindValves     Kind = "gas_valves"
	KindBlank      Kind = "blank_curve"
	KindInitial    Kind = "initial_mass"
	KindParams     Kind = "parameters"
)

// Diagnostic 记录无法转换/可疑数据的诊断, 数据不会因诊断被删除。
type Diagnostic struct {
	Level   string `json:"level"`             // error | warning
	Code    string `json:"code"`              // 机器可读码
	Message string `json:"message"`
	Section string `json:"section,omitempty"`
	Row     int    `json:"row,omitempty"`
	Field   string `json:"field,omitempty"`
}

// Q 是带单位的领域量; Raw 保留导入原始表示, Unit 是声明单位。
// Valid=false 表示无法转换到规范单位, 值留在 Raw, 绝不丢弃。
type Q struct {
	Num   float64 `json:"num"`
	Unit  string  `json:"unit"`
	Raw   string  `json:"raw"`
	Valid bool    `json:"valid"`
}

// Source 是不可变的分层来源版本。同 Kind 的后续提交以父版本链表达。
type Source struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Kind      Kind            `json:"kind"`
	Version   int             `json:"version"`
	ParentID  string          `json:"parent_id,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	Raw       string          `json:"raw"` // 原始表示, 逐字符保存
	Parsed    json.RawMessage `json:"parsed,omitempty"`
	Diags     []Diagnostic    `json:"diagnostics,omitempty"`
	Accepted  bool            `json:"accepted"` // 该 section 是否被采纳进项目
	CreatedAt string          `json:"created_at"`
}

// ---- Parsed 层结构(全部带单位/坐标基准) ----

type SamplePoint struct {
	Time Q       `json:"time"` // 基准: 炉时间 (furnace clock, min)
	Temp Q       `json:"temp"` // 坐标基准: 炉温 (degC)
	Mass Q       `json:"mass"` // mg, 未校正示值
	Seq  int     `json:"seq"`
	Note string  `json:"note,omitempty"`
}

type FurnaceSegment struct {
	StartMin Q `json:"start_min"`
	EndMin   Q `json:"end_min"`
	StartTC  Q `json:"start_temp_c"`
	EndTC    Q `json:"end_temp_c"`
}

type GasValveEvent struct {
	Time   Q      `json:"time"`
	Gas    string `json:"gas"`
	Action string `json:"action"`
}

type BlankPoint struct {
	Time Q `json:"time"`
	Mass Q `json:"mass"`
}

type DriftCorrection struct {
	Method     string `json:"method"`
	RefTimeMin Q      `json:"ref_time_min"`
}

type InitialMass struct {
	MassMg Q      `json:"mass_mg"`
	Basis  string `json:"basis"`
	Note   string `json:"note,omitempty"`
}

// Params 是派生参数的命名快照, 修订以新版本保存。
type Params struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Version      int                `json:"version"`
	ParentID     string             `json:"parent_id,omitempty"`
	Values       map[string]float64 `json:"values"`
	CreatedAt    string             `json:"created_at"`
	Fingerprint  string             `json:"fingerprint"`
	SessionID    string             `json:"session_id,omitempty"`
}

// ---- Derived 层 ----

type Point struct {
	Seq       int     `json:"seq"`
	TimeMin   float64 `json:"time_min"`
	FurnaceTC float64 `json:"furnace_c"`
	SampleTC  float64 `json:"sample_c,omitempty"` // 炉程序插值
	MassRaw   float64 `json:"mass_raw_mg"`
	BlankMg   float64 `json:"blank_mg"`
	MassCorr  float64 `json:"mass_corr_mg"`
	TGPct     float64 `json:"tg_pct"`
	DTG       float64 `json:"dtg_pct_min,omitempty"`
	DTGValid  bool    `json:"dtg_valid"`
	Segment   int     `json:"segment"` // 缺口分段编号
	Noisy     bool    `json:"noisy"`
	Refs      []Ref   `json:"refs"`
}

// Ref 是直接来源引用: 每个显示值都能沿 Ref 回到具体来源版本/行/参数。
type Ref struct {
	Kind    Kind   `json:"kind"`
	SourceID string `json:"source_id"`
	Version int    `json:"version"`
	Row     int    `json:"row,omitempty"`
}

// StageCandidate 是阶段候选。阶段可重叠, 失重百分比不强制总和为 100%。
type StageCandidate struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	StartMin       float64  `json:"start_min"`
	EndMin         float64  `json:"end_min"`
	StartMin2      float64  `json:"start_min_alt,omitempty"` // 无法唯一决定时的全部合法候选
	AmbiguousStart bool     `json:"ambiguous_start"`
	SpansGap       bool     `json:"spans_gap"`                // 数据缺口不跨越插值
	PeakTimes      []float64 `json:"peak_times"`
	DTGMin         float64  `json:"dtg_min"`
	MassLossMg     float64  `json:"mass_loss_mg"`
	MassLossPct    float64  `json:"mass_loss_pct"`
	Gas            string   `json:"gas"`            // 按样品反应窗口归属(含气氛延迟)
	GasAmbiguous   bool     `json:"gas_ambiguous"`
	OverlapsWith   []string `json:"overlaps_with,omitempty"`
	Refs           []Ref    `json:"refs"`
	RejectReason   string   `json:"reject_reason,omitempty"`
}

// AtmosphereWindow 是考虑阀延迟后的有效气氛窗口。
type AtmosphereWindow struct {
	Gas          string  `json:"gas"`
	ValveTimeMin float64 `json:"valve_time_min"`
	StartMin     float64 `json:"reaction_start_min"`
	EndMin       float64 `json:"end_min"`
}

type Step struct {
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Detail string  `json:"detail"`
	Refs   []Ref   `json:"refs"`
}

// Residue 是残留率及其可展开计算链。
type Residue struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	AtMin    float64 `json:"at_min"`
	ValuePct float64 `json:"value_pct"`
	Status   string  `json:"status"` // ok | unknown | degraded
	Steps    []Step  `json:"steps"`
	Refs     []Ref   `json:"refs"`
}

// Run 是一次完整派生计算(不可变), 由当时采纳的来源版本+参数指纹确定。
type Run struct {
	ID             string             `json:"id"`
	ProjectID      string             `json:"project_id"`
	AlgVersion     string             `json:"alg_version"`
	ParamID        string             `json:"param_id"`
	InputVersions  map[Kind]string    `json:"input_versions"` // kind -> source/param ID
	Points         []Point            `json:"points"`
	Stages         []StageCandidate   `json:"stages"`
	Residues       []Residue          `json:"residues"`
	Windows        []AtmosphereWindow `json:"atmosphere_windows"`
	Diags          []Diagnostic       `json:"diagnostics,omitempty"`
	CreatedAt      string             `json:"created_at"`
}

// Stale 标记单个派生对象相对当前输入是否过期; 过期仍可查看但不能冒充当前。
type StaleInfo struct {
	IsStale bool   `json:"is_stale"`
	Reason  string `json:"reason,omitempty"` // 哪些直接来源发生了新版本
}

// Decision 是人工裁定(接受/否定/参数调整)。撤销以补偿事件表达, 不删除记录。
type Decision struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	EventID    string `json:"event_id"`
	TargetID   string `json:"target_id"` // stage id / residue id / run id
	Action     string `json:"action"`    // accept | reject | adjust
	PrevAction string `json:"prev_action,omitempty"`
	Reason     string `json:"reason"`
	Payload    string `json:"payload,omitempty"` // 调整参数 JSON
	UndoneBy   string `json:"undone_by,omitempty"`
	ConflictOf string `json:"conflict_of,omitempty"` // 冲突裁定: 上一条被覆盖的决定
	CreatedAt  string `json:"created_at"`
}

// Event 是只追加历史; 所有输入提交、派生、判定、撤销、冲突解决都是事件。
type Event struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt string          `json:"created_at"`
}

// ImportSession 记录一次导入(允许半失败): 每个片段的判定状态与受影响对象。
type ImportSession struct {
	ID         string          `json:"id"`
	ProjectID  string          `json:"project_id"`
	Status     string          `json:"status"` // ok | partial | failed
	Fragments  []FragResult    `json:"fragments"`
	Diags      []Diagnostic    `json:"diagnostics"`
	Raw        string          `json:"raw"`
	CreatedAt  string          `json:"created_at"`
}

type FragResult struct {
	Section   Kind     `json:"section"`
	Status    string   `json:"status"` // committed | rejected
	SourceID  string   `json:"source_id,omitempty"`
	Diags     []Diagnostic `json:"diagnostics,omitempty"`
	Affects   []string `json:"affects,omitempty"` // 受影响对象的描述
}

// Project 是复查工作区。
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}
