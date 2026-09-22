package domain

import (
	"encoding/json"
	"time"
)

// Event types. The log is append-only; undo and conflict resolution are
// themselves events so every visible conclusion can be restored at any seq.
const (
	EvProjectCreated   = "project_created"
	EvImportAccepted   = "import_accepted"
	EvImportQuarantine = "import_quarantined"
	EvParamsRevised    = "params_revised"
	EvDecision         = "decision" // accept | reject | adjust
	EvConflict         = "conflict"
	EvConflictResolved = "conflict_resolved"
	EvUndo             = "undo"
	EvArchiveBuilt     = "archive_built"
)

type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	SampleID  string    `json:"sample_id"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type Event struct {
	Seq       int64           `json:"seq"`
	ProjectID string          `json:"project_id"`
	Type      string          `json:"type"`
	At        time.Time       `json:"at"`
	Actor     string          `json:"actor"`
	Payload   json.RawMessage `json:"payload"`
}

// DecisionPayload covers accept/reject/adjust decisions.
type DecisionPayload struct {
	Action     string         `json:"action"` // accept | reject | adjust
	ProposalID string         `json:"proposal_id"`
	RegionID   string         `json:"region_id"`
	Rationale  string         `json:"rationale,omitempty"`
	Adjusted   *StageProposal `json:"adjusted,omitempty"`
	Undoes     int64          `json:"undoes_seq,omitempty"`
}

type ParamsPayload struct {
	AtmosDelayS  *float64 `json:"atmos_delay_s,omitempty"`
	TempLagC     *float64 `json:"temp_lag_c,omitempty"`
	DriftRateMgH *float64 `json:"drift_rate_mg_h,omitempty"`
	DriftKind    string   `json:"drift_kind,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	ParentSeq    int64    `json:"parent_seq,omitempty"`
}

type ConflictPayload struct {
	RegionID    string   `json:"region_id"`
	ProposalIDs []string `json:"proposal_ids"`
	Resolution  string   `json:"resolution,omitempty"` // kept | chosen
	ChosenID    string   `json:"chosen_id,omitempty"`
	Note        string   `json:"note,omitempty"`
	ResolvedSeq int64    `json:"resolved_seq,omitempty"`
}

// Params is the current revision of analysis parameters.
type Params struct {
	AtmosDelayS  float64
	TempLagC     float64
	DriftKind    string
	DriftRateMgH float64
	Seq          int64 // event seq of latest revision (0 = import defaults)
}

// ArchiveRun is one branch of the counterexample dossier.
type ArchiveRun struct {
	ID                string          `json:"id"`
	BaselineProjectID string          `json:"baseline_project_id"`
	Perturbation      string          `json:"perturbation"`
	ChangedSource     string          `json:"changed_source"` // the single direct source
	ProjectID         string          `json:"project_id"`
	Report            json.RawMessage `json:"report,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
}
