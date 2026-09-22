package model

import "time"

type EventType string

const (
	EvProjectCreated EventType = "project_created"
	EvLayerSubmitted EventType = "layer_submitted"
	EvParamsRevised  EventType = "params_revised"
	EvDecision       EventType = "decision"
	EvUndo           EventType = "undo"
	EvImported       EventType = "imported"
)

type DecisionKind string

const (
	// Decision on a phase candidate.
	DecAccept   DecisionKind = "accept"
	DecReject   DecisionKind = "reject"
	DecAdjust   DecisionKind = "adjust"
	DecConflict DecisionKind = "conflict" // recorded but not applied
)

// Event is the append-only history record. Events are immutable. Decisions,
// conflict resolution and undo are themselves events, so any old state can be
// reconstructed by replaying a prefix of the log.
type Event struct {
	Seq       int64          `json:"seq"`
	ID        string         `json:"id"`
	Type      EventType      `json:"type"`
	At        time.Time      `json:"at"`
	Actor     string         `json:"actor"`
	ProjectID string         `json:"projectId"`
	Layer     *Layer         `json:"layer,omitempty"`
	Params    *Params        `json:"params,omitempty"`
	Decision  *Decision      `json:"decision,omitempty"`
	UndoSeq   int64          `json:"undoSeq,omitempty"`
	ImportRef string         `json:"importRef,omitempty"`
	Note      string         `json:"note,omitempty"`
	Diags     []Diagnostic   `json:"diags,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// Decision is an adjudication on a stage candidate or residue.
type Decision struct {
	Kind       DecisionKind `json:"kind"`
	TargetID   string       `json:"targetId"` // candidate/residue stable id
	Reason     string       `json:"reason"`
	OnsetS     *float64     `json:"onsetS,omitempty"`     // for adjust
	OffsetS    *float64     `json:"offsetS,omitempty"`    // for adjust
	ConflictsW string       `json:"conflictsWith,omitempty"` // for conflict
	Supersedes string       `json:"supersedes,omitempty"` // seq/id of overridden decision
}
