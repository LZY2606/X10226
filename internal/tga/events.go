package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// 事件类型（append-only）。
const (
	EvProjectCreated    = "project.created"
	EvSubmission        = "submission.imported"
	EvParamsRevised     = "params.revised"
	EvCandidateAccepted = "candidate.accepted"
	EvCandidateRejected = "candidate.rejected"
	EvDecisionUndone    = "decision.undone"
	EvConflictResolved  = "conflict.resolved"
	EvCounterexample    = "counterexample.checked" // 反例档案判定（机器可读）
	EvReplayNote        = "replay.note"
)

// AnalysisParams 是派生计算使用的参数集；指纹即其规范化 JSON 的哈希。
type AnalysisParams struct {
	GasDelayS           float64 `json:"gas_delay_s"`            // 气氛延迟（阀→样品）
	TempLagS            float64 `json:"temp_lag_s"`             // 温度滞后（炉温→样品）
	SmoothWindowS       float64 `json:"smooth_window_s"`        // DTG 平滑窗（秒）
	PeakProminenceMgMin float64 `json:"peak_prominence_mg_min"` // 峰显著性下限（mg/min）
	OverlapWindowS      float64 `json:"overlap_window_s"`       // 重叠峰归组窗（秒）
	BlankDriftMethod    string  `json:"blank_drift_method"`     // none | blank | linear
	GapFactor           float64 `json:"gap_factor"`             // 缺口判定：间隔 > 中位数 * 因子
	GapMinPoints        int     `json:"gap_min_points"`
}

// DefaultParams 是案例包与新项目的初始规则版本。
func DefaultParams() AnalysisParams {
	return AnalysisParams{
		GasDelayS:           45,
		TempLagS:            30,
		SmoothWindowS:       30,
		PeakProminenceMgMin: 0.02,
		OverlapWindowS:      90,
		BlankDriftMethod:    "blank",
		GapFactor:           3,
		GapMinPoints:        8,
	}
}

// AlgorithmVersion 派生算法版本（随代码升级显式变更）。
const AlgorithmVersion = "tga-derive-1.0.0"

// Fingerprint 返回参数指纹（稳定 JSON，键序确定）。
func (p AnalysisParams) Fingerprint() string {
	return "pfx-" + shaJSON(p)
}

func shaJSON(v any) string {
	b, _ := json.Marshal(canonical(v))
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// canonical 将任意值转换为键排序后的形态，保证 JSON 确定。
func canonical(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([][2]any, 0, len(keys))
		for _, k := range keys {
			out = append(out, [2]any{k, canonical(t[k])})
		}
		return out
	case []any:
		for i := range t {
			t[i] = canonical(t[i])
		}
		return t
	default:
		return v
	}
}

// Event 是 append-only 历史中的一条事件。
type Event struct {
	ID        string          `json:"id"` // E#<seq>#<hash7>
	ProjectID string          `json:"project_id"`
	Seq       int             `json:"seq"`
	Type      string          `json:"type"`
	AtMs      int64           `json:"at_ms"` // 虚拟时间（UTC 毫秒）
	Actor     string          `json:"actor"` // reviewer 或 system
	Payload   json.RawMessage `json:"payload"`
}

// 各事件负载。
type SubmissionPayload struct {
	SubmissionID string `json:"submission_id"`
	Seq          int    `json:"seq"`
	ParentSeq    int    `json:"parent_seq,omitempty"`
	Origin       string `json:"origin"`
	Status       string `json:"status"`
	Layers       []string `json:"layers"`
	Note         string `json:"note,omitempty"`
}

type ParamsPayload struct {
	ParentEventSeq int             `json:"parent_event_seq,omitempty"`
	From           AnalysisParams  `json:"from"`
	To             AnalysisParams  `json:"to"`
	Fingerprint    string          `json:"fingerprint"`
	Reason         string          `json:"reason,omitempty"`
}

type DecisionPayload struct {
	CandidateID string `json:"candidate_id"`
	Kind        string `json:"kind"` // stage_decomposition | residue | peak
	Decision    string `json:"decision"` // accepted | rejected
	Reason      string `json:"reason,omitempty"`
	ConflictsWith []string `json:"conflicts_with,omitempty"`
}

type UndoPayload struct {
	DecisionEventSeq int    `json:"decision_event_seq"`
	CandidateID      string `json:"candidate_id"`
}

type ConflictPayload struct {
	KeptEventSeq    int      `json:"kept_event_seq"`
	KeptCandidateID string   `json:"kept_candidate_id"`
	VoidedEventSeqs []int    `json:"voided_event_seqs"`
	Reason          string   `json:"reason"`
}

// CounterexamplePayload 是机器可读的反例判定事件。
// Expectation 的每条判断都可被测试与界面直接消费。
type CounterexamplePayload struct {
	CaseID      string              `json:"case_id"`
	Title       string              `json:"title"`
	PerturbOnly string              `json:"perturb_only"` // 唯一被改动的直接来源
	Changed     []string            `json:"changed"`      // 预期（且实际）过期/重算的对象
	Unchanged   []string            `json:"unchanged"`    // 预期保持的对象
	Pass        bool                `json:"pass"`
	Expectation map[string]any      `json:"expectation"` // 机器可读判断依据
	Findings    []string            `json:"findings,omitempty"`
}
