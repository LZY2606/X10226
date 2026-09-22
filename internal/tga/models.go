package tga

import (
	"time"

	"tga-review/internal/quantity"
)

// Layer names correspond to the six stratified raw stores that must be
// checked before any review starts.
const (
	LayerRawPoint  = "raw_points"   // mass, temperature, furnace time samples
	LayerDrift     = "balance_drift" // balance drift correction
	LayerFurnace   = "furnace_program"
	LayerValve     = "gas_valve"
	LayerBlank     = "blank_curve"
	LayerInitMass  = "initial_mass"
)

// Layers is the canonical review order (核对顺序).
var Layers = []string{
	LayerRawPoint,
	LayerDrift,
	LayerFurnace,
	LayerValve,
	LayerBlank,
	LayerInitMass,
}

// LayerTitle is the human readable Chinese name shown in the review UI.
var LayerTitle = map[string]string{
	LayerRawPoint: "原始采样点",
	LayerDrift:    "天平漂移校正",
	LayerFurnace:  "炉程序",
	LayerValve:    "气体阀事件",
	LayerBlank:    "空白曲线",
	LayerInitMass: "样品初重分层",
}

// EventType enumerates the append-only state machine transitions.
type EventType string

const (
	EvProjectCreated EventType = "project_created"
	EvIngested       EventType = "ingested"
	EvParamsRevised  EventType = "params_revised"
	EvDecision       EventType = "decision"
	EvReverted       EventType = "reverted" // 撤销/冲突解决的一部分
)

// Event is an immutable log record. Cancelled marks events removed from the
// current view by a later EvReverted; the records themselves stay forever.
type Event struct {
	ID         string          `json:"id"`
	ProjectID  string          `json:"project_id"`
	Seq        int64           `json:"seq"`
	Type       EventType       `json:"type"`
	At         time.Time       `json:"at"`
	PrevID     string          `json:"prev_id"`
	Payload    []byte          `json:"payload"`
	Cancelled  bool            `json:"cancelled,omitempty"`
	CancelNote string          `json:"cancel_note,omitempty"`
}

// Project is created exactly once.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Job records one import attempt, including partial failures.
type Job struct {
	ID        string    `json:"id"`
	EventID   string    `json:"event_id"`
	Layer     string    `json:"layer"`
	Label     string    `json:"label"`
	Status    string    `json:"status"` // ok | partial | failed
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	TotalRows int       `json:"total_rows"`
	GoodRows  int       `json:"good_rows"`
	BadRows   int       `json:"bad_rows"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

// Diagnostic keeps an unusable value at its original position.
type Diagnostic struct {
	Row     int    `json:"row"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	RawLine string `json:"raw_line,omitempty"`
}

// RawRecord is one row exactly as imported (never overwritten).
type RawRecord struct {
	ID       string             `json:"id"`
	JobID    string             `json:"job_id"`
	Layer    string             `json:"layer"`
	RowNo    int                `json:"row_no"`
	Line     string             `json:"raw_line"`
	Fields   map[string]string  `json:"fields"`
	Quant    map[string]quantity.Quantity `json:"quantities,omitempty"`
	Accepted bool               `json:"accepted"`
	Diagnostic string           `json:"diagnostic,omitempty"`
}

// SourceVersion is a committed, immutable slice of one raw layer.
type SourceVersion struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Layer     string    `json:"layer"`
	Seq       int64     `json:"seq"`
	ParentID  string    `json:"parent_id,omitempty"`
	JobID     string    `json:"job_id"`
	EventID   string    `json:"event_id"`
	RecordIDs []string  `json:"record_ids"`
	ContentHash string  `json:"content_hash"`
	CreatedAt time.Time `json:"created_at"`
	Note      string    `json:"note,omitempty"`
}

// Params are the analysis parameters. A revision creates a new version with a
// parent link and a fingerprint of values plus algorithm versions.
type Params struct {
	VersionID string  `json:"version_id"`
	ParentID  string  `json:"parent_id,omitempty"`
	Seq       int64   `json:"seq"`

	DriftApply       bool    `json:"drift_apply"`
	BlankApply       bool    `json:"blank_apply"`
	GapThresholdSec  float64 `json:"gap_threshold_s"`
	SmoothWindowSec  float64 `json:"smooth_window_s"`
	PeakPromPctPerMin float64 `json:"peak_prom_pct_per_min"`
	MinStageLossPct  float64 `json:"min_stage_loss_pct"`
	ValveLagMinSec   float64 `json:"valve_lag_min_s"`
	ValveLagMaxSec   float64 `json:"valve_lag_max_s"`
	TempLagMaxSec    float64 `json:"temp_lag_max_s"`
	TailWindowSec    float64 `json:"tail_window_s"`
	OverlapTolSec    float64 `json:"overlap_tol_s"`

	Fingerprint string `json:"fingerprint"`
}

// ArtifactKinds enumerates derived layers.
const (
	ArtCorrected  = "corrected_series"
	ArtTG         = "tg_series"
	ArtDTG        = "dtg_series"
	ArtFurnaceTimeline = "furnace_timeline"
	ArtGasTimeline     = "gas_timeline"
	ArtStages          = "stage_candidates"
	ArtResidue         = "residue_evidence"
)

// Artifact is one immutable derived result with full provenance.
type Artifact struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Algorithm   string   `json:"algorithm_version"`
	ParamsID    string   `json:"params_id"`
	ParamsFP    string   `json:"params_fp"`
	Sources     map[string]string `json:"sources"`
	EventID     string   `json:"event_id"`
	Seq         int64    `json:"seq"`
	Payload     []byte   `json:"payload"`
	SupersededBy string  `json:"superseded_by,omitempty"`
	Unknown     bool     `json:"unknown,omitempty"`
}

// DecisionAction values.
const (
	ActAccept        = "accept"
	ActReject        = "reject"
	ActAdjustParams  = "adjust_params"
	ActResolvePick   = "resolve_pick"
	ActReopen        = "reopen"
)

// Decision is a human verdict attached to a target (stage candidate id,
// residue chain id, ...).
type Decision struct {
	ID        string    `json:"id"`
	EventID   string    `json:"event_id"`
	Seq       int64     `json:"seq"`
	Target    string    `json:"target"`
	TargetKind string   `json:"target_kind"`
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
	Overrides string    `json:"overrides,omitempty"` // decision id conflicted with
	ArtifactID string   `json:"artifact_id,omitempty"` // view provenance
	ParamsID  string    `json:"params_id,omitempty"`
}

// Affected describes the blast radius of an import or revision.
type Affected struct {
	ChangedLayer string   `json:"changed_layer"`
	InvalidatedArtifacts []string `json:"invalidated_artifacts"`
	KeptArtifacts []string `json:"kept_artifacts"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

// Val is a display value with stable identity, units and coordinate basis.
type Val struct {
	ID       string             `json:"id"`
	V        *float64           `json:"v,omitempty"`
	Raw      string             `json:"raw,omitempty"`
	Unit     string             `json:"unit"`
	Basis    string             `json:"basis,omitempty"`
	Unknown  bool               `json:"unknown,omitempty"`
	Diag     string             `json:"diag,omitempty"`
}

// Point with value id.
type CPoint struct {
	VID   string  `json:"vid"`
	T     float64 `json:"t"` // furnace time, s
	V     float64 `json:"v"`
}

// Diagnostic note helper used by payloads.
type Note struct {
	Level   string `json:"level"` // info | warn | error
	Code    string `json:"code"`
	Message string `json:"message"`
}
