package domain

// EventType enumerates the auditable event-sourced transitions.
type EventType string

const (
	EvProjectCreated   EventType = "project_created"
	EvImportCommitted  EventType = "import_committed"
	EvParamRevised     EventType = "param_revised"
	EvSourceRevised    EventType = "source_revised"
	EvDecision         EventType = "decision"
	EvConflictResolved EventType = "conflict_resolved"
	EvRevoked          EventType = "revoked"
)

// Event is one append-only state transition. Payload is typed JSON depending on
// Kind; Revokes points at an earlier event for undo/reversal.
type Event struct {
	Seq       int64     `json:"seq"`
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Kind      EventType `json:"kind"`
	Payload   string    `json:"payload"`
	Revokes   string    `json:"revokes,omitempty"`
	Actor     string    `json:"actor,omitempty"`
	Note      string    `json:"note,omitempty"`
	TS        string    `json:"ts"`
}

// ImportCommitPayload attaches every layer from one batch.
type ImportCommitPayload struct {
	BatchID string   `json:"batch_id"`
	DocIDs  []string `json:"doc_ids"`
	Partial bool     `json:"partial"`
	Accepted []string `json:"accepted_layers,omitempty"`
	Rejected []string `json:"rejected_layers,omitempty"`
	Scenario string  `json:"scenario,omitempty"`
}

// ParamRevisePayload is a new parameter version with its parent.
type ParamRevisePayload struct {
	ParentID string `json:"parent_id,omitempty"`
	Params   Params `json:"params"`
}

// SourceRevisePayload replaces one direct source layer. Only results that
// directly depend on that layer may expire.
type SourceRevisePayload struct {
	Kind     DocKind `json:"kind"`
	OldDocID string  `json:"old_doc_id"`
	NewDocID string  `json:"new_doc_id"`
}

// DecisionPayload is an accept/reject/adjust action on one candidate or residue.
type DecisionPayload struct {
	SubjectType string `json:"subject_type"` // "stage" | "residue"
	SubjectID   string `json:"subject_id"`
	Verdict     string `json:"verdict"` // "accepted" | "rejected"
	Adjustment  *Params `json:"adjustment,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// ConflictResolvePayload forces a winning human decision when two legal
// decisions disagree; the losing one remains recorded.
type ConflictResolvePayload struct {
	AgainstIDs []string `json:"against_ids"`
	WinnerID   string   `json:"winner_id"`
	Reason     string   `json:"reason,omitempty"`
}

// RevokedPayload explains what was reversed.
type RevokedPayload struct {
	TargetSeq int64  `json:"target_seq"`
	TargetID  string `json:"target_id"`
	Reason    string `json:"reason,omitempty"`
}
