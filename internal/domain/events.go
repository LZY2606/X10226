package domain

import "encoding/json"

type EventType string

const (
	EvProjectCreated EventType = "project.created"
	// Import: one atomic event per committed layer version. A half-failed
	// import leaves only the layers that parsed; the rest stay at their
	// previous versions with diagnostics.
	EvLayerCommitted  EventType = "layer.committed"
	EvParamsRevised   EventType = "params.revised"
	EvDerivedPublished EventType = "derived.published"
	EvCandidatesGenerated EventType = "candidates.generated"
	EvDecisionMade    EventType = "decision.made"
	EvConflictMarked  EventType = "conflict.marked"
	EvConflictResolved EventType = "conflict.resolved"
	EvReviewFinalized EventType = "review.finalized"
	EvReviewReopened  EventType = "review.reopened"
	EvUndo            EventType = "undo"
	EvNoteAdded       EventType = "note.added"
)

// Event is the append-only history unit. Payload carries the exact data
// captured at that moment; replay reconstructs every past projection.
type Event struct {
	Seq      int64           `json:"seq"`
	Project  string          `json:"project"`
	Type     EventType       `json:"type"`
	VirtualTS string         `json:"virtual_ts"`
	Actor    string          `json:"actor,omitempty"`
	UndoOf   int64           `json:"undo_of,omitempty"`
	Payload  json.RawMessage `json:"payload"`
}

type LayerPayload struct {
	Layer LayerVersion `json:"layer"`
	// PartialFailure lists layers the importer rejected in the same attempt.
	PartialFailure []Diagnostic `json:"partial_failure,omitempty"`
	AttemptID string `json:"attempt_id,omitempty"`
}

type ParamsPayload struct {
	Params    Params `json:"params"`
	ParentVer int    `json:"parent_version"`
	Reason    string `json:"reason"`
}

type DerivedPayload struct {
	Node DerivedNode `json:"node"`
}

type DecisionPayload struct {
	NodeSeq      int64  `json:"node_seq"`
	NodeVersion  int64  `json:"node_version"`
	GroupingIdx  int    `json:"grouping_idx"` // -1 for residual-only / whole batch decisions
	StageCID     string `json:"stage_cid,omitempty"`
	ResidualOnly bool   `json:"residual_only,omitempty"`
	Verdict      string `json:"verdict"` // accepted | rejected | adjusted
	Adjusted     *Stage `json:"adjusted,omitempty"`
	ResidualOverride *float64 `json:"residual_override_pct,omitempty"`
	Reason       string `json:"reason"`
	DecisionID   string `json:"decision_id"`
	Supersedes   string `json:"supersedes,omitempty"`
}

type ConflictPayload struct {
	DecisionIDs []string `json:"decision_ids"`
	NodeSeq     int64    `json:"node_seq"`
	Reason      string   `json:"reason"`
	Winner      string   `json:"winner,omitempty"`
}

type FinalizePayload struct {
	ResidualDecisionID string `json:"residual_decision_id"`
	Note               string `json:"note,omitempty"`
}

type NotePayload struct {
	Target string `json:"target"` // stable id of any displayed value/object
	Text   string `json:"text"`
}
