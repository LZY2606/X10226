// Package domain defines the core types of the TGA stage-decomposition and
// atmosphere-event review system: stable identities, layered raw inputs,
// derived values with provenance, human decisions and machine-readable events.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// LayerKind enumerates the raw input layers that are stored immutably.
type LayerKind string

const (
	LayerRawPoints   LayerKind = "raw_points"   // 原始采样点 time/temp/mass
	LayerDrift       LayerKind = "drift"        // 天平漂移校正
	LayerFurnace     LayerKind = "furnace"      // 炉程序
	LayerGasEvents   LayerKind = "gas_events"   // 气体阀事件
	LayerBlank       LayerKind = "blank"        // 空白曲线
	LayerInitialMass LayerKind = "initial_mass" // 样品初重
)

// LayerKinds lists all layer kinds in canonical review order.
var LayerKinds = []LayerKind{
	LayerRawPoints, LayerDrift, LayerFurnace, LayerGasEvents, LayerBlank, LayerInitialMass,
}

// Layer is one immutable version of a raw input layer. Raw text is preserved
// exactly as submitted; revisions create a new Layer with Parent set.
type Layer struct {
	ID          string    `json:"id"`
	Project     string    `json:"project"`
	Kind        LayerKind `json:"kind"`
	Version     int       `json:"version"`
	Parent      string    `json:"parent,omitempty"`
	Raw         string    `json:"raw"`
	SubmittedAt time.Time `json:"submitted_at"`
	Seq         int64     `json:"seq"` // event seq that introduced it
	// Diagnostics records values that could not be normalized; the raw
	// representation stays in place and is never silently dropped.
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Diagnostic describes a value kept in raw form because it cannot be
// converted (unknown unit, unparsable number, ...).
type Diagnostic struct {
	Code    string `json:"code"`    // e.g. "unknown_unit", "bad_number", "gap"
	Where   string `json:"where"`   // line/field reference
	Message string `json:"message"` // human readable
}

// DerivedKind enumerates computed artifacts.
type DerivedKind string

const (
	DerivedNormalized DerivedKind = "normalized" // unit-normalized sample points
	DerivedCorrected  DerivedKind = "corrected"  // drift + blank corrected curve
	DerivedMassPct    DerivedKind = "mass_pct"   // mass % vs initial mass
	DerivedDTG        DerivedKind = "dtg"        // derivative curve
	DerivedAtmosphere DerivedKind = "atmosphere" // effective atmosphere at sample
	DerivedStages     DerivedKind = "stages"     // stage decomposition candidates
	DerivedResidual   DerivedKind = "residual"   // final residue rate
)

// Status of a derived value or candidate.
type Status string

const (
	StatusCurrent Status = "current"
	StatusStale   Status = "stale" // invalidated by a newer source version
	StatusError   Status = "error"
	StatusUnknown Status = "unknown" // e.g. beyond a data gap
)

// Derived is a computed value carrying algorithm version, parameter
// fingerprint and the direct sources it was computed from.
type Derived struct {
	ID          string          `json:"id"`
	Project     string          `json:"project"`
	Kind        DerivedKind     `json:"kind"`
	Algo        string          `json:"algo"`
	AlgoVersion string          `json:"algo_version"`
	ParamFP     string          `json:"param_fp"`
	Sources     []string        `json:"sources"` // direct source ids (layers or derived)
	Payload     json.RawMessage `json:"payload"`
	Status      Status          `json:"status"`
	Err         string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	Seq         int64           `json:"seq"`
}

// Decision is a human ruling on a stage candidate (or a parameter
// adjustment). History is append-only: undo and conflict resolution are
// themselves decisions referencing earlier ones.
type Decision struct {
	ID         string    `json:"id"`
	Project    string    `json:"project"`
	TargetID   string    `json:"target_id"` // candidate id
	Action     string    `json:"action"`    // accept | reject | adjust | undo
	Reason     string    `json:"reason"`
	Params     string    `json:"params,omitempty"`
	Supersedes string    `json:"supersedes,omitempty"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
	Seq        int64     `json:"seq"`
}

// Event types recorded in the append-only log.
const (
	EvLayerSubmitted     = "layer_submitted"
	EvLayerRevised       = "layer_revised"
	EvImportPartial      = "import_partial_failure"
	EvPipelineRun        = "pipeline_run"
	EvCandidateGenerated = "candidate_generated"
	EvStaleMarked        = "staleness_marked"
	EvDecision           = "decision_recorded"
	EvDecisionConflict   = "decision_conflict"
	EvDecisionUndone     = "decision_undone"
	// Machine-readable judgment events required by the review protocol.
	EvJudgeAtmosphereLag = "judgment_atmosphere_lag" // 阀切换时间 ≠ 样品反应时间
	EvJudgeOverlap       = "judgment_stage_overlap"  // 阶段可重叠，不强制总和 100%
	EvJudgeGap           = "judgment_gap_no_interp"  // 数据缺口不跨越插值
	EvJudgeNonMonotonic  = "judgment_nonmonotonic"   // 非单调噪声
	EvParamsRevised      = "params_revised"
)

// Event is one append-only log entry. IDs are hash-chained so that identity
// and order survive export / delete / re-import.
type Event struct {
	Seq       int64           `json:"seq"`
	ID        string          `json:"id"`
	Project   string          `json:"project"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	PrevHash  string          `json:"prev_hash"`
	CreatedAt time.Time       `json:"created_at"`
}

// HashID builds a stable content-addressed identity.
func HashID(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// Fingerprint hashes a canonical JSON encoding of params.
func Fingerprint(v any) string {
	b, _ := json.Marshal(v)
	return HashID(string(b))
}

// SortedIDs returns a sorted copy for deterministic hashing.
func SortedIDs(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}

// DerivedID computes the deterministic identity of a derived value.
func DerivedID(kind DerivedKind, algo, algoVer, paramFP string, sources []string) string {
	return HashID("derived", string(kind), algo, algoVer, paramFP, strings.Join(SortedIDs(sources), ","))
}
