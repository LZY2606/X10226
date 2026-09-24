package domain

// EventType enumerates append-only project events.
type EventType string

const (
	EvProjectCreated EventType = "project_created"
	EvLayerCommitted EventType = "layer_committed"
	EvBuildSucceeded EventType = "build_succeeded"
	EvDecisionMade   EventType = "decision_made"
	EvDecisionUndone EventType = "decision_undone"
	EvConflictResolved EventType = "conflict_resolved"
	EvParamsRevised  EventType = "params_revised"
	EvArchiveImported EventType = "archive_imported"
)

// LayerCommitPayload creates a new immutable layer version.
type LayerCommitPayload struct {
	LayerID string    `json:"layer_id"`
	Kind    LayerKind `json:"kind"`
	Version int       `json:"version"`
	ParentID string   `json:"parent_id,omitempty"`
	SHA     string    `json:"sha"`
	Origin  string    `json:"origin"`
	AcceptedCount int  `json:"accepted_count"`
	RejectedCount int  `json:"rejected_count"`
	Truncated bool    `json:"truncated"`
}

// ParamsPayload is a full named parameter set. Revision uses parent linkage.
type ParamsPayload struct {
	Name       string             `json:"name"`
	Params     map[string]float64 `json:"params"`
	ParentName string             `json:"parent_name,omitempty"`
	Reason     string             `json:"reason,omitempty"`
}

// DecisionPayload is the full adjudication record content.
type DecisionPayload struct {
	SubjectID string             `json:"subject_id"`
	Kind      string             `json:"kind"`
	Verdict   string             `json:"verdict"`
	Reason    string             `json:"reason"`
	Params    map[string]float64 `json:"params,omitempty"`
	SupersedesEventID string     `json:"supersedes_event_id,omitempty"`
	ConflictWithEvent string      `json:"conflict_with_event,omitempty"`
}

// BuildPayload records a derived computation generation.
type BuildPayload struct {
	BuildID      string         `json:"build_id"`
	Algorithm    string         `json:"algorithm"`
	AlgoVersion  string         `json:"algo_version"`
	ParamsName   string         `json:"params_name"`
	ParamsFP     string         `json:"params_fp"`
	Inputs       map[string]string `json:"inputs"`    // layer kind -> layer id (or "")
	InputVersions map[string]int   `json:"input_versions"`
	NodeIDs      []string       `json:"node_ids"`
	Stale        bool           `json:"stale"`
	Cause        string         `json:"cause,omitempty"`
	ParentBuildID string        `json:"parent_build_id,omitempty"`
}

// ArchivePayload records provenance of an imported archive.
type ArchivePayload struct {
	SHA         string `json:"sha"`
	EventCount  int    `json:"event_count"`
	NodeCount   int    `json:"node_count"`
	OriginalProjectID string `json:"original_project_id,omitempty"`
}

// Event is one immutable append-only record, hash-chained to its predecessor.
type Event struct {
	Seq       int64           `json:"seq"`
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Type      EventType       `json:"type"`
	At        string          `json:"at"` // RFC3339 virtual time
	Actor     string          `json:"actor"`
	ParentID  string          `json:"parent_id"` // hash chain
	Payload   any             `json:"payload"`
}

// NodeKind enumerates content-addressed derived values.
type NodeKind string

const (
	NodeCorrectedSeries NodeKind = "corrected_series" // after drift + blank
	NodeFurnaceTrace    NodeKind = "furnace_trace"    // programmed T(t) aligned
	NodeTGNormalized    NodeKind = "tg_normalized"    // mass fraction vs m0
	NodeGapMask         NodeKind = "gap_mask"
	NodeDTG             NodeKind = "dtg"
	NodePeakSet         NodeKind = "peak_set"
	NodeReactionWindows NodeKind = "reaction_windows"
	NodeStageCandidates NodeKind = "stage_candidates"
	NodeResidues        NodeKind = "residues"
	NodeNoiseReport     NodeKind = "noise_report"
)

// Node is an immutable derived artifact with direct sources and fingerprints.
type Node struct {
	ID         string   `json:"id"`
	Kind       NodeKind `json:"kind"`
	AlgoVersion string  `json:"algo_version"`
	ParamsFP   string   `json:"params_fp"`
	DirectSources []SourceRef `json:"direct_sources"`
	// JSON payload (Series, []Peak, []StageCandidate, []Residue, ...).
	Body       string   `json:"body"`
	Status     string   `json:"status"` // ok | unknown
	Notes      []string `json:"notes,omitempty"`
}

// SourceRef points at a direct input to a node: a layer id (+version),
// another node id, a params fingerprint or a specific event.
type SourceRef struct {
	Kind   string `json:"kind"` // layer | node | params | event
	ID     string `json:"id"`
	Version int   `json:"version,omitempty"`
	Detail string `json:"detail,omitempty"`
}
