package domain

// EventType enumerates every append-only event.
type EventType string

const (
	EvProjectCreated EventType = "project_created"
	EvSourceImported EventType = "source_imported"
	EvImportRejected EventType = "import_rejected"
	EvDerived        EventType = "derived"
	EvDecision       EventType = "decision"
	EvConflict       EventType = "conflict"
	EvJudgement      EventType = "judgement" // machine-readable domain judgement
	EvParamRevised   EventType = "param_revised"
	EvUndo           EventType = "undo"
	EvExported       EventType = "exported"
	EvImportedBundle EventType = "imported_bundle"
	EvNote           EventType = "note"
)

// Event is the immutable audit unit. Payload is type-specific JSON.
type Event struct {
	Seq       int64           `json:"seq"`
	ProjectID string          `json:"project_id"`
	Type      EventType       `json:"type"`
	Clock     string          `json:"clock"` // virtual/real ISO timestamp
	VClock    int64           `json:"vclock"`
	Actor     string          `json:"actor"`
	Payload   map[string]any  `json:"payload"`
	Refs      []string        `json:"refs,omitempty"`
}

// Decision is a human action on a candidate/value.
type Decision struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	EventSeq   int64  `json:"event_seq"`
	TargetType string `json:"target_type"` // "stage" | "residue" | "derivation"
	TargetID   string `json:"target_id"`
	Action     string `json:"action"`      // accept | reject | adjust | comment
	PrevStatus string `json:"prev_status,omitempty"`
	NewStatus  string `json:"new_status,omitempty"`
	Parameter  map[string]any `json:"parameter,omitempty"`
	Reason     string `json:"reason"`
	Actor      string `json:"actor"`
	SupersededBy int64 `json:"superseded_by,omitempty"` // set when undone/conflicted
	Active     bool   `json:"active"`
}

// Judgement is a machine-readable statement of domain reasoning. These are the
// persisted "judgement basis" entries the review UI and replay rely on.
type Judgement struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	StartS    float64        `json:"start_s,omitempty"`
	EndS      float64        `json:"end_s,omitempty"`
	Subjects  []string       `json:"subjects,omitempty"`
	Facts     map[string]any `json:"facts,omitempty"`
	EventSeq  int64          `json:"event_seq"`
}

// Project is the top-level aggregate.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedVClock int64 `json:"created_vclock"`
	HeadVClock int64 `json:"head_vclock"`
}
