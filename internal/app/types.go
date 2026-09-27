package app

import "encoding/json"

type Clock func() string

type Project struct {
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	MaterialID  string `json:"material_id"`
	CreatedAt   string `json:"created_at"`
	ClockFamily string `json:"clock_family"`
}

type Event struct {
	ProjectID    string          `json:"project_id"`
	Seq          int64           `json:"seq"`
	EventID      string          `json:"event_id"`
	EventType    string          `json:"event_type"`
	Actor        string          `json:"actor"`
	OccurredAt   string          `json:"occurred_at"`
	Payload      json.RawMessage `json:"payload"`
	PreviousHash string          `json:"previous_hash"`
	EventHash    string          `json:"event_hash"`
}

type RawRecord struct {
	ProjectID    string            `json:"project_id"`
	RecordID     string            `json:"record_id"`
	Version      int               `json:"version"`
	ParentVersion *int             `json:"parent_version,omitempty"`
	SourceType   string            `json:"source_type"`
	MaterialID   string            `json:"material_id"`
	SubmittedAt  string            `json:"submitted_at"`
	OriginalText string            `json:"original_text"`
	Normalized   json.RawMessage  `json:"normalized"`
	Status       string            `json:"status"`
	Diagnostics  []json.RawMessage `json:"diagnostics"`
	EventID      string            `json:"event_id"`
}

type ImportReport struct {
	Status            string      `json:"status"`
	Accepted          int         `json:"accepted"`
	Rejected          int         `json:"rejected"`
	LastSeq           int64       `json:"last_seq"`
	AcceptedRecordIDs []string    `json:"accepted_record_ids"`
	Diagnostics       []Diagnostic `json:"diagnostics"`
}

type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Source   string `json:"source,omitempty"`
	Index    int    `json:"index,omitempty"`
	Raw      string `json:"raw,omitempty"`
}

type DecisionInput struct {
	NodeID         string             `json:"node_id"`
	Action         string             `json:"action"`
	Reason         string             `json:"reason"`
	ParameterPatch map[string]float64 `json:"parameter_patch,omitempty"`
}

type State struct {
	Project           Project                    `json:"project"`
	Seq               int64                      `json:"seq"`
	Records           []RawRecord                `json:"records"`
	Active            map[string]RawRecord       `json:"active"`
	Derived           []DerivedNode              `json:"derived"`
	Decisions         map[string]DecisionPayload `json:"decisions"`
	LastHash          string                     `json:"last_hash"`
}

type DerivedNode struct {
	Seq                   int64           `json:"seq"`
	NodeID                string          `json:"node_id"`
	NodeKind              string          `json:"node_kind"`
	AlgorithmVersion      string          `json:"algorithm_version"`
	ParameterFingerprint  string          `json:"parameter_fingerprint"`
	DependencyFingerprint string          `json:"dependency_fingerprint"`
	CreatedAt             string          `json:"created_at"`
	Payload               json.RawMessage `json:"payload"`
}

type DecisionPayload struct {
	DecisionID     string             `json:"decision_id"`
	NodeID         string             `json:"node_id"`
	Action         string             `json:"action"`
	Reason         string             `json:"reason"`
	ParameterPatch map[string]float64 `json:"parameter_patch,omitempty"`
	Seq            int64              `json:"seq"`
	OccurredAt     string             `json:"occurred_at"`
	Actor          string             `json:"actor"`
}

type ExportBundle struct {
	Format  string          `json:"format"`
	Version string          `json:"version"`
	Project Project         `json:"project"`
	Events  []Event         `json:"events"`
}
