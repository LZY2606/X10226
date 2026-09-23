package domain

import "time"

// EventType enumerates the append-only event log entries.
type EventType string

const (
	EvProjectCreated   EventType = "project_created"
	EvDocumentAccepted EventType = "document_accepted"
	EvImportFailed     EventType = "import_failed"
	EvParamsRevised    EventType = "params_revised"
	EvDecision         EventType = "decision"
	EvConflictResolved EventType = "conflict_resolved"
	EvUndo             EventType = "undo"
	EvCaseApplied      EventType = "case_applied"
	EvCaseCheck        EventType = "case_check"
)

// Event is the unit of the append-only history. Everything needed to restore a
// historical view is recoverable by folding events from zero.
type Event struct {
	Seq       int64          `json:"seq"`
	ID        string         `json:"id"` // stable across export/import
	ProjectID string         `json:"project_id"`
	Type      EventType      `json:"type"`
	At        time.Time      `json:"at"` // virtual clock
	Actor     string         `json:"actor"`
	Payload   map[string]any `json:"payload"`
}

// Decision payload fields (stored inside Event.Payload):
//
//	target_type  stage_candidate | residue | artifact
//	target_id    stable id of the judged object
//	verdict      accepted | rejected | adjusted
//	reason       free text
//	adjustment   map (e.g. {"onset_s":..,"offset_s":..}) optional
//	conflicts_with []event ids of the contradictory decision(s)
//	undone       bool (set by undo fold)
type DecisionTarget string

const (
	TargetStage    DecisionTarget = "stage_candidate"
	TargetResidue  DecisionTarget = "residue"
	TargetArtifact DecisionTarget = "artifact"
)

// ArtifactType enumerates derived objects. Each artifact is content-addressed
// so recomputation under identical inputs yields an identical stable id.
type ArtifactType string

const (
	ArtCorrectedSeries ArtifactType = "corrected_series"
	ArtTGSeries        ArtifactType = "tg_series"
	ArtDTGSeries       ArtifactType = "dtg_series"
	ArtGasTimeline     ArtifactType = "gas_timeline"
	ArtFurnaceTimeline ArtifactType = "furnace_timeline"
	ArtStageCandidates ArtifactType = "stage_candidates"
	ArtResidue         ArtifactType = "residue"
	ArtBlankProfile    ArtifactType = "blank_profile"
)

// Provenance is the direct-source edge of every derived value.
type Provenance struct {
	DocIDs     []string `json:"doc_ids"`
	DocVersions map[string]int `json:"doc_versions,omitempty"`
	ParamsFP   string   `json:"params_fp"`
	AlgoVersion string  `json:"algo_version"`
	DependsOn  []string `json:"depends_on"` // artifact ids
	InputsHash string   `json:"inputs_hash"`
}

// Artifact is an immutable derived result record.
type Artifact struct {
	ID        string                 `json:"id"`
	ProjectID string                 `json:"project_id"`
	Type      ArtifactType           `json:"type"`
	Version   int                    `json:"version"`
	AtEventSeq int64                 `json:"at_event_seq"` // seq of the event that produced this run
	Current   bool                   `json:"current"`
	Stale     bool                   `json:"stale"`
	Status    string                 `json:"status"` // ok | partial | unknown
	Data      map[string]any         `json:"data"`
	Prov      Provenance             `json:"prov"`
	Diagnostics []Diagnostic         `json:"diagnostics,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

// Point is a normalized derived series point. Gap flags prevent interpolation.
type Point struct {
	T          float64 `json:"t_s"`
	TempC      float64 `json:"temp_c"`
	MassMg     float64 `json:"mass_mg"`
	MassPct    float64 `json:"mass_pct"`
	DTGMgPerMin float64 `json:"dtg_mg_per_min"`
	GapBefore  bool    `json:"gap_before,omitempty"`
}

// GasBand is a valve interval projected onto sample reaction time.
type GasBand struct {
	FromS      float64 `json:"from_s"` // sample-reaction time basis
	ToS        float64 `json:"to_s"`
	ValveAtS   float64 `json:"valve_at_s"` // raw valve switch time
	Atmosphere string  `json:"atmosphere"`
	DelayS     float64 `json:"delay_s"`
}

// FurnaceSegment is one ramp/hold of the program, in sample coordinates.
type FurnaceSegment struct {
	FromS   float64 `json:"from_s"`
	ToS     float64 `json:"to_s"`
	FromC   float64 `json:"from_c"`
	ToC     float64 `json:"to_c"`
	RateCPerMin float64 `json:"rate_c_per_min"`
	Kind    string  `json:"kind"` // ramp|hold
}

// StageCandidate is one legal decomposition interpretation. When data cannot
// uniquely decide boundaries, every legal candidate is retained.
type StageCandidate struct {
	ID          string   `json:"id"`
	Index       int      `json:"index"`
	OnsetS      float64  `json:"onset_s"`
	OffsetS     float64  `json:"offset_s"`
	PeakS       float64  `json:"peak_s"`
	PeakMgPerMin float64 `json:"peak_mg_per_min"`
	OnsetTempC  float64  `json:"onset_temp_c"`
	PeakTempC   float64  `json:"peak_temp_c"`
	OffsetTempC float64  `json:"offset_temp_c"`
	LossMg      float64  `json:"loss_mg"`
	LossPct     float64  `json:"loss_pct"` // of initial mass; stages need not sum to 100
	Atmosphere  string   `json:"atmosphere"`
	OverlapGroup int     `json:"overlap_group,omitempty"`
	// Alternatives links mutually exclusive candidates for the same overlap.
	Alternatives []string `json:"alternatives,omitempty"`
	RejectedReasons []string `json:"rejected_reasons,omitempty"` // why *other* readings were refused
	SpanGap      bool     `json:"span_gap,omitempty"`
	UncertaintyS float64 `json:"uncertainty_s,omitempty"`
}

// ChainNode is one step of an expandable residue calculation chain.
type ChainNode struct {
	ValueID    string `json:"value_id"` // stable display-value identity
	Label      string `json:"label"`
	Value      string `json:"value"` // rendered with unit
	Formula    string `json:"formula,omitempty"`
	SourceID   string `json:"source_id,omitempty"`
	SourceKind string `json:"source_kind,omitempty"`
	Status     string `json:"status"` // ok | partial | unknown
	Note       string `json:"note,omitempty"`
}
