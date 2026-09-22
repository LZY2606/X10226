// Package core implements the TGA (thermogravimetric analysis) review system:
// layered immutable imports, provenance-bearing derived values, a machine
// readable decision/event history with time travel, and export/import that
// preserves stable identities.
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Layer kinds — the six stratums of source material.
const (
	KindRawSamples       = "raw_samples"       // 原始采样点 (time, temperature, mass)
	KindDriftCorrection  = "drift_correction"  // 天平漂移校正
	KindFurnaceProgram   = "furnace_program"   // 炉程序
	KindGasEvents        = "gas_events"        // 气体阀事件
	KindBlankCurve       = "blank_curve"       // 空白曲线
	KindInitialMass      = "initial_mass"      // 样品初重
)

var LayerKinds = []string{
	KindRawSamples, KindDriftCorrection, KindFurnaceProgram,
	KindGasEvents, KindBlankCurve, KindInitialMass,
}

// Derived kinds.
const (
	DerTG       = "tg_curve"        // TG 曲线 (质量百分比)
	DerDTG      = "dtg"             // DTG 曲线
	DerStages   = "stage_candidates"
	DerResidue  = "residue"         // 最终残留率
)

// Derived/import statuses.
const (
	StatusCurrent  = "current"  // 当前结果
	StatusStale    = "stale"    // 过期结果:仍可查看,不得冒充当前
	StatusOK       = "ok"
	StatusPartial  = "partial"  // 导入半失败:部分层有效
	StatusRejected = "rejected" // 整层拒绝
	StatusError    = "error"    // 派生无法给出值
	StatusUnknown  = "unknown"  // 未知/无法唯一决定
)

// Event types — every state change is an append-only event.
const (
	EvProject       = "project"
	EvImport        = "import_layer"     // 单层(成功或拒绝)
	EvImportBatch   = "import_batch"     // 一次提交的汇总(可半失败)
	EvDerive        = "derive"           // 派生批次
	EvRuleFinding   = "rule_finding"     // 机器可读的判断依据
	EvParamsRevise  = "params_revise"    // 参数修订
	EvDecide        = "decide"           // 人工裁定
	EvConflict      = "conflict"         // 冲突的人工决定
	EvResolve       = "resolve_conflict"
	EvUndo          = "undo_decision"
	EvExport        = "export"
	EvImportBundle  = "import_bundle"
	EvCaseReplay    = "case_replay"
)

// Rule codes — 判断依据,作为机器可读事件持久化。
const (
	RuleAtmosphereDelay    = "ATMOSPHERE_DELAY"      // 阀切换时间 != 样品反应时间
	RuleTempLag            = "TEMPERATURE_LAG"       // 炉温 != 样品温度
	RuleStageOverlap       = "STAGE_OVERLAP_ALLOWED" // 阶段可重叠
	RuleNoSumToHundred     = "NO_SUM_TO_100"         // 不强制总和为 100%
	RuleGapNoInterpolation = "GAP_NO_INTERPOLATION"  // 数据缺口不跨越插值
	RuleAmbiguousCandidates= "AMBIGUOUS_CANDIDATES"  // 无法唯一决定,保留全部合法候选
	RuleUnconvertibleValue = "UNCONVERTIBLE_VALUE"   // 无法转换的值留在原位置
)

// Clock is injectable so tests run on virtual time.
type Clock interface{ Now() int64 }

// Quantity carries the original representation through the import layer.
// Normalisation happens only in the derived layer.
type Quantity struct {
	Raw     string  `json:"raw"`              // 导入时的原始表示,原样保留
	UnitRaw string  `json:"unit_raw"`         // 原始单位标记
	Value   float64 `json:"value,omitempty"`  // 规范化数值
	Unit    string  `json:"unit,omitempty"`   // 规范单位
	OK      bool    `json:"ok"`               // 是否成功转换
	Diag    string  `json:"diag,omitempty"`   // 无法转换时的诊断
}

// Coordinate base labels for time/temperature/mass axes.
const (
	BaseTime   = "time:s"
	BaseTemp   = "temperature:degC"
	BaseMass   = "mass:mg"
	BasePct    = "mass_percent:%"
	BaseRate   = "dm/dt:%/s"
	BaseFlow   = "flow:mL/min"
)

// Diagnostic records a local, non-fatal problem with the material.
type Diagnostic struct {
	Code    string `json:"code"`
	Where   string `json:"where"`
	Message string `json:"message"`
}

// Layer is one immutable submission of one source stratum.
type Layer struct {
	ID          string       `json:"id"`           // 内容寻址,跨导出/导入稳定
	ProjectID   string       `json:"project_id"`
	Kind        string       `json:"kind"`
	Version     int          `json:"version"`      // 同类层的第几版
	ParentID    string       `json:"parent_id,omitempty"` // 父版本
	Raw         string       `json:"raw"`          // 原始输入原文
	Parsed      any          `json:"parsed"`       // 含每值 raw 表示与诊断
	Status      string       `json:"status"`       // ok|partial|rejected
	Diagnostics []Diagnostic `json:"diagnostics"`
	SubmittedAt int64        `json:"submitted_at"`
	Seq         int64        `json:"seq"`
}

// Derived is an immutable provenance-bearing computed result.
type Derived struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"project_id"`
	Kind          string          `json:"kind"`
	AlgoVersion   string          `json:"algo_version"`
	ParamsFP      string          `json:"params_fp"`
	SourceIDs     []string        `json:"source_ids"` // 直接来源(层或派生)
	Payload       json.RawMessage `json:"payload"`
	Status        string          `json:"status"` // current|stale|error
	StaleSinceSeq int64           `json:"stale_since_seq,omitempty"`
	CreatedSeq    int64           `json:"created_seq"`
}

// Decision is a human accept/reject/adjust on a derived result.
type Decision struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	TargetID      string `json:"target_id"`
	Action        string `json:"action"` // accept|reject|adjust
	Params        string `json:"params,omitempty"`
	Reviewer      string `json:"reviewer"`
	Rationale     string `json:"rationale"`
	Seq           int64  `json:"seq"`
	UndoneBySeq   int64  `json:"undone_by_seq,omitempty"`
	SupersededBySeq int64 `json:"superseded_by_seq,omitempty"` // 冲突解决后被取代
}

// Event is one row of the append-only machine readable history.
type Event struct {
	Seq       int64           `json:"seq"`
	ProjectID string          `json:"project_id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	At        int64           `json:"at"`
}

// Params is the versioned derivation parameter set.
type Params struct {
	Version           int     `json:"version"`
	ParentVersion     int     `json:"parent_version,omitempty"`
	AtmosphereDelayS  float64 `json:"atmosphere_delay_s"` // 气氛延迟:阀切换 -> 样品反应
	TemperatureLagC   float64 `json:"temperature_lag_c"`  // 温度滞后:炉温 -> 样温
	SmoothingWindow   int     `json:"smoothing_window"`   // DTG 平滑窗口(点)
	PeakThresholdRate float64 `json:"peak_threshold_rate"`// DTG 峰阈值 %/s
	SeparationRatio   float64 `json:"separation_ratio"`   // 重叠峰谷/峰比
	GapFactor         float64 `json:"gap_factor"`         // 缺口 = dt > gapFactor * 中位 dt
	NoiseSigma        float64 `json:"noise_sigma"`        // 非单调噪声带
	Seq               int64   `json:"seq,omitempty"`
}

func DefaultParams() Params {
	return Params{
		Version: 1, AtmosphereDelayS: 0, TemperatureLagC: 0,
		SmoothingWindow: 5, PeakThresholdRate: 0.002,
		SeparationRatio: 0.5, GapFactor: 5, NoiseSigma: 0.05,
	}
}

// Stable hashing helpers.
func hashParts(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			fmt.Fprint(h, "|")
		}
		fmt.Fprint(h, p)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// LayerID is content addressed; parent included so identical revisions differ.
func LayerID(kind, parentID, raw string) string {
	return "lay:" + hashParts(kind, parentID, raw)
}

// DerivedID fingerprints algorithm, parameters and direct sources.
func DerivedID(kind, algo, paramsFP string, sourceIDs []string) string {
	src := append([]string(nil), sourceIDs...)
	return "der:" + hashParts(kind, algo, paramsFP, joinSorted(src))
}

// DecisionID binds the decision to its event position; stable across export.
func DecisionID(targetID, action, reviewer, rationale string, seq int64) string {
	return "dec:" + hashParts(targetID, action, reviewer, rationale, fmt.Sprint(seq))
}

// ParamsFP fingerprints a parameter set.
func (p Params) Fingerprint() string {
	b, _ := canonicalJSON(p.withoutSeq())
	return "pf:" + hashParts(string(b))
}

func (p Params) withoutSeq() Params {
	q := p
	q.Seq = 0
	return q
}

// canonicalJSON sorts map keys so fingerprints are stable.
func canonicalJSON(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var x any
	if err := json.Unmarshal(b, &x); err != nil {
		return nil, err
	}
	return json.Marshal(x)
}

func joinSorted(s []string) string {
	cp := append([]string(nil), s...)
	// simple insertion sort; slices are small
	for i := 1; i < len(cp); i++ {
		for j := i; j > 0 && cp[j-1] > cp[j]; j-- {
			cp[j-1], cp[j] = cp[j], cp[j-1]
		}
	}
	out := ""
	for i, v := range cp {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}
