// Package domain defines the core types of the TGA stage-decomposition
// review system: immutable layered raw inputs, derived results with
// provenance, and the machine-readable event log.
package domain

// Input layer kinds. Raw inputs are immutable once submitted; a revision is
// a new version linked to its parent version.
const (
	KindSamplePoints    = "sample_points"    // 原始采样点
	KindDriftCorrection = "drift_correction" // 天平漂移校正
	KindFurnaceProgram  = "furnace_program"  // 炉程序
	KindGasEvents       = "gas_events"       // 气体阀事件
	KindBlankCurve      = "blank_curve"      // 空白曲线
	KindInitialMass     = "initial_mass"     // 样品初重
)

// LayerKinds is the canonical import order of the layered baseline.
var LayerKinds = []string{
	KindSamplePoints,
	KindDriftCorrection,
	KindFurnaceProgram,
	KindGasEvents,
	KindBlankCurve,
	KindInitialMass,
}

// Derived result kinds.
const (
	DKindCorrectedCurve  = "corrected_curve"
	DKindDTG             = "dtg"
	DKindAtmosphere      = "atmosphere"
	DKindStageCandidates = "stage_candidates"
	DKindResidue         = "residue"
)

// Derived status values.
const (
	StatusCurrent = "current"
	StatusStale   = "stale" // viewable, but must not masquerade as current
)

// Event types written to the machine-readable history.
const (
	EvImport          = "import"
	EvImportRejected  = "import_rejected"
	EvImportPartial   = "import_partial_failure"
	EvRevise          = "revise"
	EvCompute         = "compute"
	EvInvalidate      = "invalidate"
	EvDecision        = "decision"
	EvDecisionUndo    = "decision_undo"
	EvConflict        = "conflict"
	EvAtmosphereDelay = "atmosphere_delay_note"
	EvDiagnostic      = "diagnostic"
)

// Decision actions.
const (
	ActAccept = "accept"
	ActReject = "reject"
	ActAdjust = "adjust"
)

// Input is one immutable version of a raw layer. Raw keeps the original
// representation byte-for-byte; normalization happens only in derived layers.
type Input struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Version    int    `json:"version"`
	ParentID   string `json:"parent_id,omitempty"`
	Raw        string `json:"raw"`
	Note       string `json:"note,omitempty"`
	CreatedSeq int64  `json:"created_seq"`
}

// Derived is a computed result. Every derived value carries the algorithm
// version, a fingerprint of the parameters, and its direct sources.
type Derived struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	AlgoVersion string   `json:"algo_version"`
	ParamFP     string   `json:"param_fp"`
	SourceIDs   []string `json:"source_ids"`
	Status      string   `json:"status"`
	Payload     []byte   `json:"payload"`
	CreatedSeq  int64    `json:"created_seq"`
}

// Event is one entry of the append-only history. Accepts, rejections,
// parameter adjustments, conflict resolution and undo are all events.
type Event struct {
	Seq     int64  `json:"seq"`
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload []byte `json:"payload"`
	At      int64  `json:"at"` // virtual clock tick
}

// Diagnostic is attached to values that could not be normalized; the
// original value stays in place and is never dropped for plotting.
type Diagnostic struct {
	Layer  string `json:"layer"`
	Where  string `json:"where"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}
