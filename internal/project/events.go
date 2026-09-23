package project

import (
	"encoding/json"

	"tga-evidence/internal/domain"
)

// EventType enumerates every state transition recognized by the system.
type EventType string

const (
	EvProjectCreated  EventType = "project_created"
	EvSourcesImported EventType = "sources_imported" // one bundle, possibly partial
	EvParamsRevised   EventType = "params_revised"
	EvAnalysisRun     EventType = "analysis_run"
	EvDecision        EventType = "decision"          // accept/reject/adjust a candidate
	EvConflict        EventType = "conflict_resolved" // two decisions collide
	EvRollback        EventType = "rollback"          // restore an old view; history retained
	EvNote            EventType = "note"
)

// ImportedFile is one file inside an import bundle.
type ImportedFile struct {
	Type     domain.SourceType `json:"type"`
	Filename string            `json:"filename"`
	Encoding string            `json:"encoding"` // utf-8 / latin-1 / ...
	Content  string            `json:"content"`
	Note     string            `json:"note,omitempty"`
}

// SourcesImportedPayload records exactly what entered the system.
type SourcesImportedPayload struct {
	BundleLabel string         `json:"bundle_label"`
	Files       []ImportedFile `json:"files"`
	Strict      bool           `json:"strict"` // strict: any bad row aborts the whole file
	Partial     bool           `json:"partial"`
	CaseID      string         `json:"case_id,omitempty"`
}

// ParamsRevisedPayload introduces a new parameter version with a parent link.
type ParamsRevisedPayload struct {
	ParentVersion int    `json:"parent_version"`
	Reason        string `json:"reason"`
	ParamsJSON    string `json:"params_json"`
}

// AnalysisRunPayload stores a full derived analysis (large JSON blob).
type AnalysisRunPayload struct {
	AnalysisID string `json:"analysis_id"`
	JSON       string `json:"analysis_json"`
}

// DecisionPayload is an adjudicator verdict on one candidate/residue.
type DecisionPayload struct {
	TargetID    string `json:"target_id"`
	TargetKind  string `json:"target_kind"` // stage_candidate | residue | analysis
	AnalysisID  string `json:"analysis_id"`
	Verdict     string `json:"verdict"`     // accepted | rejected | adjusted
	Reason      string `json:"reason"`
	Adjustment  string `json:"adjustment_json,omitempty"`
}

// ConflictPayload resolves a contradiction between decisions.
type ConflictPayload struct {
	TargetID       string `json:"target_id"`
	WonDecisionID  string `json:"won_decision_id"`
	LostDecisionID string `json:"lost_decision_id"`
	Resolution     string `json:"resolution"`
}

// RollbackPayload restores the effective view at event seq RestoreSeq.
// Events after it remain in the log (visible history) but are superseded; new
// events are appended on top, never rewriting the past.
type RollbackPayload struct {
	RestoreSeq int    `json:"restore_seq"`
	Reason     string `json:"reason"`
}

// NotePayload is a free machine-readable annotation.
type NotePayload struct {
	AboutID string `json:"about_id,omitempty"`
	Text    string `json:"text"`
}

// Event is one immutable log record.
type Event struct {
	Seq       int            `json:"seq"`
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Type      EventType      `json:"type"`
	At        string         `json:"at"`
	Actor     string         `json:"actor"`
	Payload   map[string]any `json:"payload"`
	PrevHash  string         `json:"prev_hash"`
	Hash      string         `json:"hash"`
}

func payloadOf(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}
