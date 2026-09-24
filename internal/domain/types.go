// Package domain holds the vocabulary shared by every layer of the
// thermogravimetric review system: measured quantities (always carrying a
// unit and coordinate basis), immutable source documents, append-only events,
// derived results and human decisions.
package domain

// Measure is a physical quantity with an explicit unit and coordinate basis.
// Raw is the original representation as imported; Value is the normalized
// number used by derived layers. A value that cannot be converted stays at
// its source with a diagnostic instead of being silently dropped.
type Measure struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Basis string  `json:"basis,omitempty"`
	Raw   string  `json:"raw,omitempty"`
}

// Point is one sampled furnace instant. Break marks a sampling gap: the
// surrounding points are never interpolated across a break.
type Point struct {
	T     Measure `json:"t"`
	Temp  Measure `json:"temp"`
	Mass  Measure `json:"mass"`
	Break bool    `json:"break,omitempty"`
}

// SampleDoc holds the raw sample channel.
type SampleDoc struct {
	Instrument string  `json:"instrument,omitempty"`
	Points     []Point `json:"points"`
}

// BlankPoint and BlankDoc are the empty-pan baseline curve.
type BlankPoint struct {
	T    Measure `json:"t"`
	Mass Measure `json:"mass"`
}
type BlankDoc struct {
	Points []BlankPoint `json:"points"`
}

// DriftDoc is the balance baseline drift correction (piecewise linear in time).
type DriftDoc struct {
	Method string    `json:"method,omitempty"`
	Knots  []DriftPt `json:"knots"`
}
type DriftPt struct {
	T     Measure `json:"t"`
	Delta Measure `json:"delta"`
}

// FurnaceStep is one ramp/hold segment of the furnace program.
type FurnaceStep struct {
	Kind  string  `json:"kind"` // ramp | hold
	FromT Measure `json:"from_t"`
	ToT   Measure `json:"to_t"`
	Start Measure `json:"start"`
	End   Measure `json:"end"`
}
type FurnaceDoc struct {
	Steps []FurnaceStep `json:"steps"`
}

// ValveEvent is a gas valve switch on the furnace timeline.
type ValveEvent struct {
	At    Measure `json:"at"`
	Gas   string  `json:"gas"`
	Flow  Measure `json:"flow,omitempty"`
	Note  string  `json:"note,omitempty"`
}
type ValveDoc struct {
	Events []ValveEvent `json:"events"`
}

// M0Doc is the sample initial mass. Revisions arrive as new versions
// carrying ParentDoc.
type M0Doc struct {
	M0 Measure `json:"m0"`
}

// ParamsDoc holds tunable analysis parameters; fingerprints let every
// derived value name the exact rule version it used.
type ParamsDoc struct {
	AtmDelayS       float64 `json:"atm_delay_s"`
	TempLagS        float64 `json:"temp_lag_s"`
	NoiseWindow     int     `json:"noise_window"`
	MonotonicStrict bool    `json:"monotonic_strict"`
	PeakPromFrac    float64 `json:"peak_prom_frac"`
	StageMinDTG     float64 `json:"stage_min_dtg"`
}

// Diag is a machine readable diagnostic attached to an import or derivation.
type Diag struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // error | warning | info
	Message  string `json:"message"`
	DocKind  string `json:"doc_kind,omitempty"`
	Index    int    `json:"index,omitempty"`
	Ref      string `json:"ref,omitempty"`
}

// RawDoc is an immutable source document layer.
type RawDoc struct {
	ID        string                 `json:"id"`
	Project   string                 `json:"project"`
	Kind      string                 `json:"kind"`
	Layer     string                 `json:"layer"`
	Version   int                    `json:"version"`
	ParentDoc string                 `json:"parent_doc,omitempty"`
	Origin    string                 `json:"origin"`
	Committed int64                  `json:"committed_at"`
	RawText   string                 `json:"raw_text,omitempty"`
	Content   map[string]interface{} `json:"content"`
}

// Event kinds.
const (
	EvProjectCreated = "project_created"
	EvDocCommitted   = "doc_committed"
	EvBatchOpened    = "batch_opened"
	EvBatchClosed    = "batch_closed"
	EvDecided        = "candidate_decided"
	EvUndone         = "decision_undone"
	EvResolved       = "conflict_resolved"
	EvAsserted       = "judgement_asserted"
)

// Event is the only thing that mutates project state. Events are append-only
// and hash-chained so exports prove their own history.
type Event struct {
	Seq       int64                  `json:"seq"`
	Project   string                 `json:"project"`
	Kind      string                 `json:"kind"`
	OccurredAt int64                 `json:"occurred_at"`
	Payload   map[string]interface{} `json:"payload"`
	ClientID  string                 `json:"client_id,omitempty"`
	Hash      string                 `json:"hash,omitempty"`
	PrevHash  string                 `json:"prev_hash,omitempty"`
}

// Document layers and kinds.
const (
	LayerSample  = "sample"
	LayerBlank   = "blank"
	LayerDrift   = "drift"
	LayerFurnace = "furnace"
	LayerValve   = "valve"
	LayerM0      = "m0"
	LayerParams  = "params"
)

const (
	KindSample  = "sample_points"
	KindBlank   = "blank_curve"
	KindDrift   = "drift_correction"
	KindFurnace = "furnace_program"
	KindValve   = "valve_events"
	KindM0      = "initial_mass"
	KindParams  = "analysis_params"
)

// Derived result types.
const (
	ResSeries   = "corrected_series"
	ResTG       = "tg_curve"
	ResDTG      = "dtg_curve"
	ResStage    = "stage_candidate"
	ResResidue  = "final_residue"
	ResAtmos    = "atmosphere_marker"
	ResOverview = "run_overview"
)

// Result statuses: candidates/diagnostics are first class.
const (
	StatusOK      = "ok"
	StatusRejected = "rejected"
	StatusError   = "error"
	StatusUnknown = "unknown"
)

// Candidate review states.
const (
	ReviewProposed  = "proposed"
	ReviewAccepted  = "accepted"
	ReviewRejected  = "rejected"
	ReviewAdjusted  = "adjusted"
	ReviewSuperseded = "superseded"
)

// Result is one derived artifact. Old input versions produce stale results
// that stay readable but never pose as current.
type Result struct {
	ID         string                 `json:"id"`
	Project    string                 `json:"project"`
	Type       string                 `json:"type"`
	Status     string                 `json:"status"`
	AlgoVer    string                 `json:"algo_ver"`
	ParamsFP   string                 `json:"params_fp"`
	InputSet   map[string]string      `json:"input_set"`
	Current    bool                   `json:"current"`
	CreatedSeq int64                  `json:"created_seq"`
	Body       map[string]interface{} `json:"body"`
}

// Decision records a human act on a candidate.
type Decision struct {
	ID          string                 `json:"id"`
	Project     string                 `json:"project"`
	Seq         int64                  `json:"seq"`
	Kind        string                 `json:"kind"` // accept | reject | adjust
	CandidateID string                 `json:"candidate_id"`
	ResultID    string                 `json:"result_id"`
	Reason      string                 `json:"reason,omitempty"`
	Params      map[string]interface{} `json:"params,omitempty"`
	Undone      bool                   `json:"undone"`
	UndoSeq     int64                  `json:"undo_seq,omitempty"`
	OccurredAt  int64                  `json:"occurred_at"`
}

// Conflict is a contradictory pair of active human decisions.
type Conflict struct {
	ID          string   `json:"id"`
	Project     string   `json:"project"`
	Between     []string `json:"between"`
	Kind        string   `json:"kind"`
	Description string   `json:"description"`
	ResolvedSeq int64    `json:"resolved_seq,omitempty"`
	Resolution  string   `json:"resolution,omitempty"`
}

// ImportItem is one element of an import batch.
type ImportItem struct {
	Kind      string                 `json:"kind"`
	Origin    string                 `json:"origin,omitempty"`
	RawText   string                 `json:"raw_text,omitempty"`
	Content   map[string]interface{} `json:"content,omitempty"`
	ClientID  string                 `json:"client_id,omitempty"`
}

// ImportBatchResult reports per-item fate after an import attempt.
type ImportBatchResult struct {
	BatchID  string             `json:"batch_id"`
	Status   string             `json:"status"` // committed | partial | failed
	Accepted []AcceptedDoc      `json:"accepted"`
	Rejected []RejectedItem     `json:"rejected"`
}
type AcceptedDoc struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Version int    `json:"version"`
	Item    int    `json:"item"`
}
type RejectedItem struct {
	Item   int      `json:"item"`
	Kind   string   `json:"kind"`
	Diags  []Diag   `json:"diags"`
	Ref    string   `json:"ref,omitempty"`
}
