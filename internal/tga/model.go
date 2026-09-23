package tga

// Layer identifiers for the six stratified source layers.
const (
	LayerSamples   = "samples"
	LayerDrift     = "drift"
	LayerProgram   = "program"
	LayerGas       = "gas"
	LayerBlank     = "blank"
	LayerInitialMg = "initial_mass"
)

// Layers in canonical review order.
var Layers = []string{
	LayerSamples, LayerDrift, LayerProgram, LayerGas, LayerBlank, LayerInitialMg,
}

// LayerTitle maps a layer id to a human readable name.
var LayerTitle = map[string]string{
	LayerSamples:   "原始采样点",
	LayerDrift:     "天平漂移校正",
	LayerProgram:   "炉程序",
	LayerGas:       "气体阀事件",
	LayerBlank:     "空白曲线",
	LayerInitialMg: "样品初重",
}

// Batch is one committed import layer. Raw input is append-only: a batch can
// never be overwritten, later revisions are expressed as new batches linked
// through supersedes.
type Batch struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	Seq        int64  `json:"seq"`
	Layer      string `json:"layer"`
	Parent     string `json:"parent_batch,omitempty"`
	Supersedes string `json:"supersedes,omitempty"`
	Origin     string `json:"origin"` // text | csv
	Kind       string `json:"kind"`   // layer | partial
	Note       string `json:"note,omitempty"`
	CommittedAtMS int64 `json:"committed_at_ms"`
	Items      []RawItem `json:"items"`
}

// RawItem is one raw source record. Raw fields are preserved exactly as
// imported; normalization happens only in the derived layer.
type RawItem struct {
	ID      string            `json:"id"`
	BatchID string            `json:"batch_id"`
	Idx     int               `json:"idx"`
	Kind    string            `json:"kind"`
	Raw     map[string]string `json:"raw"`
	Valid   bool              `json:"valid"`
	Diags   []Diagnostic      `json:"diags,omitempty"`
}

// Diagnostic is attached to a raw item when a value cannot be converted. The
// value stays in place (raw) rather than being dropped for plotting.
type Diagnostic struct {
	Code    string `json:"code"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// ParamVersion is one revision of analysis parameters.
type ParamVersion struct {
	ID         string `json:"id"`
	Seq        int64  `json:"seq"`
	Parent     string `json:"parent,omitempty"`
	Note       string `json:"note,omitempty"`
	CommittedAtMS int64 `json:"committed_at_ms"`
	Params     Params `json:"params"`
}

// Params are the algorithm parameters. Fingerprint is derived from the JSON
// canonical form of this struct.
type Params struct {
	AlgorithmVersion string  `json:"algorithm_version"`
	DriftModel       string  `json:"drift_model"` // "blank" uses blank curve, "linear" uses raw drift points
	AtmDelayS        float64 `json:"atm_delay_s"`
	FurnaceLagS      float64 `json:"furnace_lag_s"`
	OverlapWindowS   float64 `json:"overlap_window_s"`
	DtgSmoothWinN    int     `json:"dtg_smooth_n"`
	PeakMinPromPct   float64 `json:"peak_min_prom_pct"`
	NoiseBandPct     float64 `json:"noise_band_pct"`
}

// DefaultParams returns algorithm v1 defaults.
func DefaultParams() Params {
	return Params{
		AlgorithmVersion: CalcVersion,
		DriftModel:       "blank",
		AtmDelayS:        0,
		FurnaceLagS:      0,
		OverlapWindowS:  120,
		DtgSmoothWinN:    5,
		PeakMinPromPct:  0.02,
		NoiseBandPct:    0.15,
	}
}

// Decision states.
const (
	DecisionStateActive   = "active"
	DecisionStateResolved = "resolved"
	DecisionStateUndone   = "undone"
)

// DecisionSubject kinds.
const (
	SubjectCandidate = "candidate"
	SubjectResidue   = "residue"
)

// Verdict values.
const (
	VerdictAccepted = "accept"
	VerdictRejected = "reject"
	VerdictAdjusted = "adjust"
)

// Decision is a human adjudication attached to a derived subject.
type Decision struct {
	ID          string  `json:"id"`
	Seq         int64   `json:"seq"`
	SubjectKind string  `json:"subject_kind"`
	SubjectID   string  `json:"subject_id"`
	Verdict     string  `json:"verdict"`
	Comment     string  `json:"comment,omitempty"`
	StartS      float64 `json:"start_s,omitempty"`
	EndS        float64 `json:"end_s,omitempty"`
	AttributedS string  `json:"attributed_switch_id,omitempty"`
	State       string  `json:"state"`
	SupersededBy string `json:"superseded_by,omitempty"`
	Resolves     string `json:"resolves,omitempty"` // conflict id this resolution closes
	CommittedAtMS int64 `json:"committed_at_ms"`
}

// Conflict between two active human decisions on the same subject.
type Conflict struct {
	ID         string `json:"id"`
	Seq        int64  `json:"seq"`
	SubjectKind string `json:"subject_kind"`
	SubjectID  string `json:"subject_id"`
	FirstID    string `json:"first_id"`
	SecondID   string `json:"second_id"`
	Reason     string `json:"reason"`
	ResolvedBy string `json:"resolved_by,omitempty"`
	Resolution string `json:"resolution,omitempty"` // keep_first | keep_second
	Open       bool   `json:"open"`
	AtMS       int64  `json:"at_ms"`
}

// Provenance is the direct-source link carried by every derived value.
type Provenance struct {
	NodeID         string   `json:"node_id"`
	AlgorithmVersion string `json:"algorithm_version"`
	ParamVersionID string   `json:"param_version_id"`
	ParamFingerprint string `json:"param_fingerprint"`
	SourceBatchIDs []string `json:"source_batch_ids"`
	InputItemIDs   []string `json:"input_item_ids"`
	UpstreamIDs    []string `json:"upstream_ids"`
	Step           string   `json:"step"`
}

// Event kinds appended to the project log.
const (
	EvProjectCreated = "project_created"
	EvBatchCommitted = "batch_committed"
	EvImportRejected = "import_rejected"
	EvParamsCommitted = "params_committed"
	EvDecisionRecorded = "decision_recorded"
	EvConflictRecorded = "conflict_recorded"
	EvConflictResolved = "conflict_resolved"
	EvDecisionUndone = "decision_undone"
)

// Event is one immutable append-only log entry.
type Event struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"project_id"`
	Seq         int64           `json:"seq"`
	Kind        string          `json:"kind"`
	AtMS        int64           `json:"at_ms"`
	Payload     []byte          `json:"payload"`
}

// RejectedImport records a failed / partial import attempt (input fragment,
// status and affected objects are all retained).
type RejectedImport struct {
	ID           string       `json:"id"`
	Seq          int64        `json:"seq"`
	AtMS         int64        `json:"at_ms"`
	Origin       string       `json:"origin"`
	Fragment     string       `json:"fragment"`
	Status       string       `json:"status"` // rejected | partial
	RequestedLayer string    `json:"requested_layer"`
	Diags        []Diagnostic `json:"diags"`
	AcceptedBatchID string    `json:"accepted_batch_id,omitempty"`
}
