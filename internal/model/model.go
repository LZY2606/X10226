// Package model defines the domain types for the TGA stage-decomposition
// and atmosphere-event review system. Every value that crosses a layer
// boundary carries a stable identity so that export / re-import and
// as-of replay preserve identity, order and provenance.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// SourceKind enumerates the six layered inputs of a TGA run.
type SourceKind string

const (
	SrcRawPoints   SourceKind = "raw_points"        // 原始采样点
	SrcDrift       SourceKind = "drift_correction"  // 天平漂移校正
	SrcFurnace     SourceKind = "furnace_program"   // 炉程序
	SrcGas         SourceKind = "gas_events"        // 气体阀事件
	SrcBlank       SourceKind = "blank_curve"       // 空白曲线
	SrcInitialMass SourceKind = "initial_mass"      // 样品初重（分层）
)

// SourceOrder fixes the deterministic display / replay order of layers.
var SourceOrder = []SourceKind{
	SrcRawPoints, SrcDrift, SrcFurnace, SrcGas, SrcBlank, SrcInitialMass,
}

// Diagnostic is attached to values that could not be normalized; the raw
// representation stays in place and the diagnostic explains why.
type Diagnostic struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Location string `json:"location,omitempty"`
}

// Source is one immutable version of a layered input. Raw keeps the
// original representation verbatim; revisions create a new Source with
// ParentID pointing at the superseded version.
type Source struct {
	ID          string       `json:"id"`
	Kind        SourceKind   `json:"kind"`
	Version     int          `json:"version"`
	ParentID    string       `json:"parent_id,omitempty"`
	Raw         string       `json:"raw"`
	Format      string       `json:"format"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	CreatedSeq  int64        `json:"created_seq"`
	CreatedAt   time.Time    `json:"created_at"`
}

// DerivedStatus marks whether a derived value reflects current inputs.
type DerivedStatus string

const (
	StatusCurrent   DerivedStatus = "current"
	StatusStale     DerivedStatus = "stale"   // viewable, must not pose as current
	StatusCandidate DerivedStatus = "candidate"
	StatusAccepted  DerivedStatus = "accepted"
	StatusRejected  DerivedStatus = "rejected"
	StatusUnknown   DerivedStatus = "unknown"
)

// Derived is a computed node: TG/DTG curves, stage candidates, residues.
// It always carries the algorithm version, a parameter fingerprint and the
// direct sources it was computed from.
type Derived struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"` // norm_points|tg_curve|dtg_curve|stage_set|stage_candidate|residue
	AlgoVersion string          `json:"algo_version"`
	ParamFP     string          `json:"param_fingerprint"`
	Deps        []string        `json:"deps"` // ids of sources / derived nodes
	Value       json.RawMessage `json:"value"`
	Status      DerivedStatus   `json:"status"`
	Reason      string          `json:"reason,omitempty"`
	CreatedSeq  int64           `json:"created_seq"`
	CreatedAt   time.Time       `json:"created_at"`
}

// Event types. The event log is the single source of truth for decisions.
const (
	EvSourceImported  = "source.imported"
	EvSourceRevised   = "source.revised"
	EvDerivedComputed = "derived.computed"
	EvMarkedStale     = "derived.marked_stale"
	EvDecision        = "decision.made"
	EvConflict        = "decision.conflicted"
	EvResolved        = "conflict.resolved"
	EvUndone          = "event.undone"
	EvJudgment        = "judgment.recorded"
	EvPartialImport   = "import.partial_failure"
	EvExported        = "archive.exported"
	EvImported        = "archive.imported"
)

// Event is an append-only record. UndoneBy holds the seq of the undo event.
type Event struct {
	Seq       int64           `json:"seq"`
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Actor     string          `json:"actor"`
	Payload   json.RawMessage `json:"payload"`
	At        time.Time       `json:"at"`
	UndoneBy  int64           `json:"undone_by,omitempty"`
}

// Judgment is the machine-readable form of a review judgment, e.g.
// "valve switch time != sample reaction time".
type Judgment struct {
	Code      string         `json:"code"`
	Statement string         `json:"statement"`
	Detail    map[string]any `json:"detail,omitempty"`
}

// DecisionPayload is stored inside decision / conflict / resolve events.
type DecisionPayload struct {
	ObjectID string `json:"object_id"`
	Verdict  string `json:"verdict"` // accept|reject|adjust
	Reason   string `json:"reason"`
	Adjust   string `json:"adjust,omitempty"`
}

// StableID derives a deterministic identity from kind and content so that
// export -> delete -> import preserves identity.
func StableID(prefix string, parts ...any) string {
	h := sha256.New()
	h.Write([]byte(prefix))
	for _, p := range parts {
		b, _ := json.Marshal(p)
		h.Write(b)
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(h.Sum(nil))[:16])
}

// Fingerprint hashes an arbitrary JSON-able parameter set.
func Fingerprint(parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		b, _ := json.Marshal(p)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
