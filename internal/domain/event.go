package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

// Event types cover the whole narrative: create project, immutable imports,
// parameter revisions, reviewer decisions, machine-readable judgment rules,
// conflict resolution, and undo.
const (
	EvCreate    = "PROJECT_CREATED"
	EvImport    = "IMPORT_COMMITTED"
	EvParams    = "PARAMS_REVISED"
	EvDecision  = "DECISION_MADE"
	EvConflict  = "CONFLICT_RESOLVED"
	EvUndo      = "DECISION_UNDONE"
	EvRule      = "JUDGMENT_RULE"
)

// CreatePayload creates a project with its narrative label.
type CreatePayload struct {
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
}

// ImportPayload references the batch and the raw sources it added.
type ImportPayload struct {
	BatchID string     `json:"batch_id"`
	Status  string     `json:"status"`
	Added   []Ref      `json:"added"`
	Rejected []Reject  `json:"rejected,omitempty"`
	Binds    []Bind     `json:"binds,omitempty"`
	Label   string     `json:"label,omitempty"`
}

// Bind records that a derived slot pointed at a node version after an event.
type Bind struct {
	Slot   string `json:"slot"`
	NodeID string `json:"node_id"`
	Reason string `json:"reason,omitempty"`
}

// ParamsPayload stores the revision with parent params id and reason.
type ParamsPayload struct {
	Params   Params `json:"params"`
	ParamsID string `json:"params_id"`
	ParentID string `json:"parent_params_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Binds    []Bind `json:"binds,omitempty"`
}

// DecisionPayload is one accept/reject/adjust action on a candidate or residue.
type DecisionPayload struct {
	TargetKind string  `json:"target_kind"` // stage | residue
	NodeID     string  `json:"node_id"`     // pin to a derived node version
	CID        string  `json:"cid"`         // candidate/result stable id
	Action     string  `json:"action"`      // accept | reject | adjust
	Comment    string  `json:"comment,omitempty"`
	Adjust     *Params `json:"adjust_params,omitempty"`
	Snapshot   string  `json:"snapshot,omitempty"` // key displayed values at decision time
	Reviewer   string  `json:"reviewer,omitempty"`
}

// ConflictPayload records resolution between conflicting human decisions.
type ConflictPayload struct {
	Group       string   `json:"group"`
	WonCID      string   `json:"won_cid"`
	LostCID     string   `json:"lost_cid"`
	Reason      string   `json:"reason"`
	Superseded  []string `json:"superseded_event_ids,omitempty"`
}

// UndoPayload inverts a prior decision/conflict event by id.
type UndoPayload struct {
	UndoneEventSeq int    `json:"undone_event_seq"`
	UndoneEventID  string `json:"undone_event_id"`
	Reason         string `json:"reason,omitempty"`
}

// RulePayload is a machine-readable judgment/basis event (counterexample
// archive judgments live here too).
type RulePayload struct {
	Code     string         `json:"code"`
	Title    string         `json:"title"`
	Subject  string         `json:"subject,omitempty"`
	Basis    string         `json:"basis"`
	Holds    bool           `json:"holds"`
	Evidence map[string]any `json:"evidence,omitempty"`
}

// Event is append-only. Seq is per-project insertion order; ID is stable
// content hash so export/reimport preserves identity.
type Event struct {
	Seq       int            `json:"seq"`
	ID        string         `json:"id"`
	Project   string         `json:"project"`
	Type      string         `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Actor     string         `json:"actor,omitempty"`
	Created   time.Time      `json:"created"`
}

// CanonicalJSON serializes with sorted object keys and no extra whitespace.
func CanonicalJSON(v any) []byte {
	b, err := json.Marshal(canon(v))
	if err != nil {
		panic(err)
}
	return b
}

func canon(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([][2]any, 0, len(keys))
		for _, k := range keys {
			out = append(out, [2]any{k, canon(t[k])})
		}
		return out
	case []any:
		for i := range t {
			t[i] = canon(t[i])
		}
		return t
	default:
		return v
	}
}

// HashID returns a stable prefixed id from canonical content.
func HashID(prefix string, v any) string {
	sum := sha256.Sum256(CanonicalJSON(v))
	return prefix + hex.EncodeToString(sum[:])[:16]
}

// HashBytes hashes raw canonical bytes.
func HashBytes(prefix string, b []byte) string {
	sum := sha256.Sum256(bytes.TrimSpace(b))
	return prefix + hex.EncodeToString(sum[:])[:16]
}

// MustJSON encodes payloads; used where payloads are statically typed.
func MustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
