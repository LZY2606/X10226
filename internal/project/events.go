// Package project implements the event-sourced application service: importing
// immutable layer revisions, recomputing deterministic artifacts, recording
// human decisions and reconstructing any historical view. Local input changes
// only invalidate derived results that directly depend on them.
package project

import (
	"encoding/json"

	"tgareview/internal/bundle"
	"tgareview/internal/domain"
	"tgareview/internal/engine"
)

type EventType string

const (
	EvProjectCreated   EventType = "project_created"
	EvImportSubmitted  EventType = "import_submitted"
	EvLayerCommitted   EventType = "layer_committed"
	EvParamsRevised    EventType = "params_revised"
	EvDecisionRecorded EventType = "decision_recorded"
	EvDecisionUndone   EventType = "decision_undone"
)

type CreatedPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type LayerCommitInfo struct {
	Kind       domain.LayerKind `json:"kind"`
	RevID      string           `json:"rev_id"`
	ParentRev  string           `json:"parent_rev"`
	SourceFile string           `json:"source_file"`
}

type ImportPayload struct {
	ImportID    string                      `json:"import_id"`
	Status      string                      `json:"status"` // committed | partial | rejected
	Accepted    []LayerCommitInfo           `json:"accepted"`
	Diagnostics []bundle.FragmentDiagnostic `json:"diagnostics"`
	Note        string                      `json:"note,omitempty"`
}

type ParamsPayload struct {
	ParamsRevID string        `json:"params_rev_id"`
	ParentRevID string        `json:"parent_rev_id"`
	Params      engine.Params `json:"params"`
	Reason      string        `json:"reason"`
}

type DecisionPayload struct {
	DecisionID string `json:"decision_id"`
	ArtifactID string `json:"artifact_id"`
	TargetID   string `json:"target_id"` // candidate id or residue id
	Kind       string `json:"kind"`      // accept | reject | parameter
	Value      string `json:"value"`
	Rationale  string `json:"rationale"`
	Supersedes string `json:"supersedes"`
	ConflictOf string `json:"conflict_of,omitempty"`
}

type UndoPayload struct {
	DecisionID string `json:"decision_id"`
}

func marshalPayload(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
