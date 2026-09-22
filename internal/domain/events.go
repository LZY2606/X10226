package domain

import "encoding/json"

// Event type codes.
const (
	EvtProjectCreated = "project_created"
	EvtBatchImported  = "batch_imported"
	EvtRunProduced    = "run_produced"
	EvtParamsRevised  = "params_revised"
	EvtDecision       = "decision_made"
	EvtConflict       = "conflict_resolved"
	EvtUndo           = "undone"
	EvtExported       = "exported"
)

// Decision actions on candidates/residue.
const (
	DecisionAccept    = "ACCEPT"
	DecisionReject    = "REJECT"
	DecisionAdjust    = "ADJUST"
	DecisionUncertain = "UNCERTAIN"
)

// Event is an append-only ledger entry. It is the only thing that mutates
// project state: replaying events rebuilds every view at any point in time.
type Event struct {
	ID          string          `json:"id"`
	Seq         int64           `json:"seq"`
	ProjectID   string          `json:"project_id"`
	Type        string          `json:"type"`
	OccurredAt  string          `json:"occurred_at"`
	Reviewer    string          `json:"reviewer"`
	Undoes      string          `json:"undoes,omitempty"` // id of reversed event
	ConflictOf  string          `json:"conflict_of,omitempty"`
	Payload     json.RawMessage `json:"payload"`
}

// ParamsRevisedPayload records parent parameter fingerprint and reason.
type ParamsRevisedPayload struct {
	ParentFingerprint string          `json:"parent_fingerprint"`
	NewFingerprint    string          `json:"new_fingerprint"`
	Params           Params          `json:"params"`
	Reason           string          `json:"reason"`
}

// DecisionPayload is a human action on a result. Adjust carries new bounds.
type DecisionPayload struct {
	TargetType string   `json:"target_type"` // stage | residue
	TargetID   string   `json:"target_id"`
	Action     string   `json:"action"`
	Reason     string   `json:"reason"`
	NewOnsetT  *float64 `json:"new_onset_t,omitempty"`
	NewEndT    *float64 `json:"new_end_t,omitempty"`
	RunID      string   `json:"run_id"`
}

// RunProducedPayload stores the full derived set so a replay at an old event
// restores the exact conclusion seen then, not a recomputed newest one.
type RunProducedPayload struct {
	Derived DerivedSet `json:"derived"`
}

// ConflictResolvedPayload shows both positions and the winning one.
type ConflictResolvedPayload struct {
	ObjectID       string `json:"object_id"`
	FirstDecision  string `json:"first_decision_id"`
	SecondDecision string `json:"second_decision_id"`
	Winner         string `json:"winner"` // first | second
	Resolution     string `json:"resolution"`
}

// UndoPayload names the event reversed and why.
type UndoPayload struct {
	ReversedEventID string `json:"reversed_event_id"`
	Reason          string `json:"reason"`
}
