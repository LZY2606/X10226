package domain

import "time"

// Source kinds — the six stratified raw layers.
const (
	SrcSamplePoints = "sample_points" // raw TG samples: furnace time, temperature, apparent mass
	SrcDrift        = "drift_corr"    // balance drift correction table
	SrcFurnace      = "furnace_prog"  // furnace program segments
	SrcGasValve     = "gas_valve"     // gas valve events
	SrcBlank        = "blank_curve"   // blank/baseline curve
	SrcInitialMass  = "initial_mass"  // stratified sample initial mass record
)

// SourceKinds lists all six raw layers in canonical order.
var SourceKinds = []string{
	SrcSamplePoints, SrcDrift, SrcFurnace, SrcGasValve, SrcBlank, SrcInitialMass,
}

// SourceLabels maps kinds to human labels used in the timeline and UI.
var SourceLabels = map[string]string{
	SrcSamplePoints: "原始采样点",
	SrcDrift:        "天平漂移校正",
	SrcFurnace:      "炉程序",
	SrcGasValve:     "气体阀事件",
	SrcBlank:        "空白曲线",
	SrcInitialMass:  "样品初重(分层保存)",
}

// Row is one preserved raw row. On partial-import failures, Rejected rows keep
// their raw text and reason instead of aborting the whole import.
type Row struct {
	Line       int               `json:"line"`
	Raw        map[string]string `json:"raw"`
	Cells      map[string]Q      `json:"cells,omitempty"`
	Accepted   bool              `json:"accepted"`
	Diagnostic string            `json:"diagnostic,omitempty"`
}

// Source is one committed version of a raw layer. Committed sources are
// immutable; revisions arrive as new versions linked by ParentVersion.
type Source struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"project_id"`
	Kind          string         `json:"kind"`
	Version       int            `json:"version"`
	ParentVersion int            `json:"parent_version"`
	Filename      string         `json:"filename"`
	Header        []string       `json:"header"`
	UnitDecl      map[string]string `json:"unit_decl"` // column -> declared unit
	Rows          []Row          `json:"rows"`
	Accepted      int            `json:"accepted"`
	Rejected      int            `json:"rejected"`
	CommitEventID string         `json:"commit_event_id"`
	CreatedAt     time.Time      `json:"created_at"`
}

// Event types written to the append-only journal.
const (
	EvProjectCreated  = "project_created"
	EvSourceCommitted = "source_committed"
	EvParamsRevised   = "params_revised"
	EvDerivationRun   = "derivation_run"
	EvDecision        = "decision"          // accept/reject/adjust a candidate
	EvDecisionRevoked = "decision_revoked"  // undo: never deletes history
	EvConflict        = "conflict"          // conflicting human decision detected
	EvConflictResolved= "conflict_resolved"
	EvNote            = "note"
)

// Event is one journal entry. ID chains through the parent event so the order
// and provenance are themselves tamper-evident and exportable.
type Event struct {
	Seq       int64           `json:"seq"`
	ID        string          `json:"id"`
	ParentID  string          `json:"parent_id"`
	ProjectID string          `json:"project_id"`
	Type      string          `json:"type"`
	At        time.Time       `json:"at"`
	Actor     string          `json:"actor"`
	Summary   string          `json:"summary"`
	Payload   map[string]any  `json:"payload"`
}

// Artifact kinds produced by the derivation layer.
const (
	ArtCorrectedTG  = "corrected_tg"   // normalized, drift/blank corrected points + gaps
	ArtDTG          = "dtg"            // DTG series, never interpolated across gaps
	ArtAtmoTimeline = "atmo_timeline"  // valve timeline mapped to reaction time
	ArtCandidates   = "stage_candidates"
	ArtResidue      = "residue"
)

// Status values for candidates/decisions.
const (
	CandProposed = "proposed"
	CandAccepted = "accepted"
	CandRejected = "rejected"
	CandAdjusted = "adjusted" // human-adjusted parameters supersede an accepted one
)

// Candidate is a stage (or residue) proposal. Candidates are first-class data:
// rejected ones and rejection reasons persist; when attribution is not unique
// every legal candidate is retained.
type Candidate struct {
	ID         string            `json:"id"`
	Kind       string            `json:"kind"` // "stage" or "residue"
	Name       string            `json:"name"`
	StartT     float64           `json:"start_t"`     // reaction-time basis, s
	EndT       float64           `json:"end_t"`
	StartTemp  float64           `json:"start_temp"`
	EndTemp    float64           `json:"end_temp"`
	MassLossPct *float64         `json:"mass_loss_pct,omitempty"`
	ResiduePct *float64          `json:"residue_pct,omitempty"`
	Overlaps   bool              `json:"overlaps"`
	GapBounded bool              `json:"gap_bounded"`
	Status     string            `json:"status"`
	RejectReason string          `json:"reject_reason,omitempty"`
	Notes      map[string]string `json:"notes,omitempty"`
	Chain      []ChainNode       `json:"chain"`
}

// ChainNode is one step of a fully expandable calculation chain.
type ChainNode struct {
	Step   string  `json:"step"`
	Detail string  `json:"detail"`
	Value  string  `json:"value,omitempty"`
}

// Artifact is a persisted derivation output with algorithm version, parameter
// fingerprint and the exact direct source IDs it was computed from.
type Artifact struct {
	ID          string        `json:"id"`
	ProjectID   string        `json:"project_id"`
	Kind        string        `json:"kind"`
	AlgoVersion string        `json:"algo_version"`
	ParamFP     string        `json:"param_fp"`
	Inputs      []string      `json:"inputs"`
	DerivEvent  string        `json:"deriv_event"`
	Seq         int64         `json:"seq"`
	Data        any           `json:"data"`
	Candidates  []Candidate   `json:"candidates,omitempty"`
	CreatedAt   time.Time     `json:"created_at"`
}

// Project bundles sources, events and artifacts.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}
