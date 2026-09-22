package domain

import "encoding/json"

// EventType enumerates every machine-readable action in the evidence chain.
// These strings are a stable API: replays and exported archives rely on them.
type EventType string

const (
	EvProjectCreated   EventType = "project.created"
	EvImported         EventType = "raw.imported"          // one layer committed
	EvImportPartial    EventType = "raw.import_partial"    // some sections/rows failed
	EvImportRejected   EventType = "raw.import_rejected"   // whole layer rejected (format error)
	EvParamsRevised    EventType = "params.revised"
	EvDerived          EventType = "derived.computed"
	EvDecision         EventType = "decision.recorded"
	EvConflictResolved EventType = "decision.conflict_resolved"
	EvUndo             EventType = "decision.undone"
	EvCheckout         EventType = "history.checkout"
	EvExported         EventType = "archive.exported"
	EvArchiveImported  EventType = "archive.imported"
)

// Event is an append-only ledger entry. Payload holds type-specific data;
// RawRefs lists committed submission IDs; DerivedRefs lists derived result IDs.
type Event struct {
	Seq       int64           `json:"seq"`
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Type      EventType       `json:"type"`
	At        string          `json:"at"` // RFC3339 virtual time
	Actor     string          `json:"actor"`
	Summary   string          `json:"summary"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	RawRefs    []string        `json:"raw_refs,omitempty"`
	DerivedRefs []string      `json:"derived_refs,omitempty"`
	UndoesSeq int64           `json:"undoes_seq,omitempty"`
	Active    bool            `json:"active"`
}

// ImportPayload is the machine-readable body of an import-related event.
type ImportPayload struct {
	Layer          Kind         `json:"layer"`
	SubmissionID   string       `json:"submission_id"`
	ParentVersion  string       `json:"parent_version,omitempty"`
	Version        int          `json:"version"`
	AcceptedRows   int          `json:"accepted_rows"`
	RejectedRows   int          `json:"rejected_rows"`
	RejectedSections []string   `json:"rejected_sections,omitempty"`
	Diagnostics    []Diagnostic `json:"diagnostics,omitempty"`
	DirectOnly     bool         `json:"direct_only"` // change is confined to direct dependents
}

// ParamsPayload records a parameter revision and its fingerprint chain.
type ParamsPayload struct {
	SubmissionID      string `json:"submission_id"`
	Version           int    `json:"version"`
	Fingerprint       string `json:"fingerprint"`
	ParentFingerprint string `json:"parent_fingerprint,omitempty"`
	Reason            string `json:"reason,omitempty"`
}

// DerivedPayload records one content-addressed derived computation.
type DerivedPayload struct {
	ResultID   string   `json:"result_id"`
	Kind       Kind     `json:"kind"`
	Algo       string   `json:"algo_version"`
	ParamsFP   string   `json:"params_fingerprint"`
	Sources    []Source `json:"sources"`
	Status     Status   `json:"status"`
	ReplacesID string   `json:"replaces_id,omitempty"`
}

// Source is a provenance edge from a derived result to one direct source.
type Source struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	Role string `json:"role"`
}

// DecisionPayload records an accept / reject / adjust decision.
type DecisionPayload struct {
	DecisionID string `json:"decision_id"`
	SubjectID  string `json:"subject_id"`  // stage set / stage candidate / residual
	SubjectKind Kind  `json:"subject_kind"`
	Verdict    string `json:"verdict"`     // accept | reject | adjust
	ParamsID   string `json:"params_id,omitempty"`
	Rationale  string `json:"rationale"`
	// Conflict resolution: verdict of the overridden decision.
	OverridesDecision string `json:"overrides_decision,omitempty"`
}
