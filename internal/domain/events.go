package domain

import "encoding/json"

type ProjectCreated struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type CommittedSource struct {
	ID          string          `json:"id"`
	Layer       string          `json:"layer"`
	Version     int             `json:"version"`
	ParentID    string          `json:"parent_id,omitempty"`
	ContentType string          `json:"content_type"`
	Content     json.RawMessage `json:"content"`
}

type SourcesCommitted struct {
	BatchID     string            `json:"batch_id"`
	ParentBatch string            `json:"parent_batch,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	Scope       string            `json:"scope,omitempty"`
	Status      string            `json:"status,omitempty"`
	Sources     []CommittedSource `json:"sources"`
	Diagnostics []Diagnostic      `json:"diagnostics,omitempty"`
	Fragments   []json.RawMessage `json:"fragments,omitempty"`
}

type DerivationRecorded struct {
	DerivationID string          `json:"derivation_id"`
	Graph        json.RawMessage `json:"graph"`
}

type DecisionMade struct {
	DecisionID    string   `json:"decision_id"`
	SubjectID     string   `json:"subject_id"`
	Verdict       string   `json:"verdict"`
	Rationale     string   `json:"rationale"`
	BasisEventIDs []string `json:"basis_event_ids"`
	ParameterHash string   `json:"parameter_hash"`
	ConflictsWith string   `json:"conflicts_with,omitempty"`
}

type DecisionUndone struct {
	DecisionID string `json:"decision_id"`
	Reason     string `json:"reason,omitempty"`
}

type ProjectImported struct {
	OriginalProjectID string `json:"original_project_id"`
	EventCount        int    `json:"event_count"`
}

const (
	EventProjectCreated   = "project_created"
	EventSourcesCommitted = "sources_committed"
	EventDerivation       = "derivation_recorded"
	EventDecision         = "decision_made"
	EventDecisionUndone   = "decision_undone"
	EventProjectImported  = "project_imported"
)
