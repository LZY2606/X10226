package domain

import "encoding/json"

// Event is the unit of the append-only log. Nothing mutable exists outside the
// log: current state is a fold over events, and any past event can be revisited
// by folding up to and including it.
type Event struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Seq       int64           `json:"seq"` // 1-based, per project, gapless
	Type      string          `json:"type"`
	At        string          `json:"at"` // RFC3339 from the active clock (virtual in tests)
	Actor     string          `json:"actor"`
	ParentID  string          `json:"parent_id"` // immediately preceding event (chain)
	Payload   json.RawMessage `json:"payload"`
	// Undo: events can be undone by an UndoEvent referencing them. The undo is
	// itself an event, so history remains complete.
	UndoneBy  string          `json:"undone_by,omitempty"`
}

const (
	EvProjectCreated   = "project.created"
	EvImportBegan      = "import.began"
	EvLayerCommitted   = "import.layer_committed"
	EvImportConcluded  = "import.concluded" // success | partial | failed
	EvParamsRevised    = "params.revised"
	EvDerivedProduced  = "derived.produced"
	EvDecision         = "review.decision"
	EvConflictRaised   = "review.conflict_raised"
	EvConflictResolved = "review.conflict_resolved"
	EvUndo             = "history.undo"
	EvProjectImported  = "project.imported"
)

type ProjectCreated struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type ImportBegan struct {
	SessionID string   `json:"session_id"`
	Layers    []string `json:"layers"`
	Correlate string   `json:"correlate,omitempty"` // client correlation token
}

type LayerCommitted struct {
	VersionID string `json:"version_id"`
	Layer     string `json:"layer"`
	Version   int    `json:"version"`
	Parent    string `json:"parent,omitempty"`
	SHA       string `json:"sha"`
	RowsOK    int    `json:"rows_ok"`
	RowsBad   int    `json:"rows_bad"`
}

type ImportConcluded struct {
	SessionID string       `json:"session_id"`
	Status    ImportStatus `json:"status"` // success | partial | failed
	LayersOK  []string     `json:"layers_ok"`
	LayersBad []string     `json:"layers_bad"`
	Summary   string       `json:"summary,omitempty"`
}

type ParamsRevised struct {
	ParamsID    string `json:"params_id"`
	Parent      string `json:"parent,omitempty"`
	Name        string `json:"name"`
	Fingerprint Fingerprint
	Reason      string `json:"reason,omitempty"`
}

type DerivedProduced struct {
	BatchID    string      `json:"batch_id"`
	Algorithm  string      `json:"algorithm"`
	Version    string      `json:"version"`
	Fingerprint Fingerprint
	NodeIDs    []string    `json:"node_ids"`
	Supersedes []string    `json:"supersedes,omitempty"` // prior derived nodes now stale
	Note       string      `json:"note,omitempty"`
}

type Decision struct {
	TargetType string  `json:"target_type"` // candidate | stage | residue
	TargetID   string  `json:"target_id"`
	Action     string  `json:"action"`      // accept | reject | adjust
	ParamsID   string  `json:"params_id,omitempty"`
	Reason     string  `json:"reason"`
	BasisEvent string  `json:"basis_event,omitempty"` // event seq context in which target was current
}

type ConflictRaised struct {
	TargetID string   `json:"target_id"`
	DecisionIDs []string `json:"decision_ids"`
	Reason   string   `json:"reason"`
}

type ConflictResolved struct {
	ConflictEventID string `json:"conflict_event_id"`
	TargetID        string `json:"target_id"`
	WinnerDecision  string `json:"winner_decision_id"`
	Note            string `json:"note"`
}

type UndoPayload struct {
	UndoneEventID string `json:"undone_event_id"`
	Reason        string `json:"reason"`
}

type ProjectImported struct {
	OriginProjectID string `json:"origin_project_id"`
	ExportSHA       string `json:"export_sha"`
}

// ImportStatus is the state machine outcome of an import session.
type ImportStatus string

const (
	ImportBeganState   ImportStatus = "began"
	ImportSuccess      ImportStatus = "success"
	ImportPartial      ImportStatus = "partial" // some layers/rows failed; good layers committed
	ImportFailed       ImportStatus = "failed"  // nothing committed
)
