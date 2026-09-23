// Package model holds the domain types for the TGA evidence-chain system.
//
// Every domain quantity carries its unit/coordinate reference. Imported
// representations are preserved verbatim; normalisation happens only in the
// derived layer.
package model

import "time"

// Qty is a domain quantity. Raw is the representation as imported (kept for
// audit), Value/Unit are the normalised form. When conversion is impossible
// Valid is false and Diagnostic explains why; the value stays in place and is
// never dropped just to make plotting possible.
type Qty struct {
	Raw        string  `json:"raw"`
	Value      float64 `json:"value,omitempty"`
	Unit       string  `json:"unit,omitempty"`
	Valid      bool    `json:"valid"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

// BatchKind enumerates the layered raw inputs.
type BatchKind string

const (
	KindSample    BatchKind = "sample"
	KindFurnace   BatchKind = "furnace"
	KindGas       BatchKind = "gas"
	KindBlank     BatchKind = "blank"
	KindDrift     BatchKind = "drift"
	KindInitial   BatchKind = "initial_mass"
	KindParams    BatchKind = "parameters"
	KindUnknown   BatchKind = "unknown"
)

// Status of an imported batch.
type BatchStatus string

const (
	BatchImported BatchStatus = "imported"        // fully parsed, all rows convertible
	BatchPartial  BatchStatus = "partial"         // partially imported (half-failure)
	BatchQuarantine BatchStatus = "quarantined"   // unparseable, held aside
	BatchSuperseded BatchStatus = "superseded"    // newer same-layer revision exists
)

// Diagnostic is a non-fatal or fatal problem recorded against an import,
// always attached to the affected object and never silently swallowed.
type Diagnostic struct {
	Severity string `json:"severity"` // error | warning
	Section  string `json:"section"`
	Line     int    `json:"line,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

// SamplePoint is one raw sampling row.
type SamplePoint struct {
	Index   int    `json:"index"`
	TimeRaw string `json:"time_raw"`
	TempRaw string `json:"temp_raw"`
	MassRaw string `json:"mass_raw"`
	Time    Qty    `json:"time"` // base unit: s
	Temp    Qty    `json:"temp"` // base unit: °C
	Mass    Qty    `json:"mass"` // base unit: mg
	TimeF   float64 `json:"-"`
	TempF   float64 `json:"-"`
	MassF   float64 `json:"-"`
	Useable bool    `json:"-"`
}

// FurnaceStep is a programmed furnace segment (ramp/hold) in furnace time.
type FurnaceStep struct {
	Index      int    `json:"index"`
	Kind       string `json:"kind"` // ramp | hold
	FromRaw    string `json:"from_raw"`
	ToRaw      string `json:"to_raw"`
	RateRaw    string `json:"rate_raw"`
	HoldRaw    string `json:"hold_raw"`
	StartRaw   string `json:"start_raw"`
	From       Qty    `json:"from"`
	To         Qty    `json:"to"`
	Rate       Qty    `json:"rate"` // °C/min
	Hold       Qty    `json:"hold"` // s
	Start      Qty    `json:"start"` // s (furnace time offset)
}

// GasEvent is a valve event in raw valve time.
type GasEvent struct {
	Index    int    `json:"index"`
	TimeRaw  string `json:"time_raw"`
	AtmFrom  string `json:"atm_from"`
	AtmTo    string `json:"atm_to"`
	FlowRaw  string `json:"flow_raw"`
	Time     Qty    `json:"time"`
	Flow     Qty    `json:"flow"` // mL/min
}

// DriftParam describes the balance baseline drift model.
type DriftParam struct {
	InterceptRaw string `json:"intercept_raw"`
	SlopeRaw     string `json:"slope_raw"`
	Intercept    Qty    `json:"intercept"` // mg
	Slope        Qty    `json:"slope"`     // mg/min
}

// InitialMass is one layered statement of the sample initial mass.
type InitialMass struct {
	Layer    string `json:"layer"` // balance_tare | pre_run | reweigh
	MassRaw  string `json:"mass_raw"`
	AtTRaw   string `json:"at_t_raw"`
	Mass     Qty    `json:"mass"`
	AtT      Qty    `json:"at_t"`
}

// Params are the analysis parameters (algorithm tuning + lag models).
type Params struct {
	Version         int     `json:"version"`
	SmoothWindowS   float64 `json:"smooth_window_s"`
	PeakMinDepthPct float64 `json:"peak_min_depth_pct"`
	PeakNoisePct    float64 `json:"peak_noise_pct"`
	GasDelayS       float64 `gas_delay_s`
	GasDelaySJSON   float64 `json:"gas_delay_s"`     // transport delay valve->sample
	TempLagS        float64 `json:"temp_lag_s"`      // furnace->sample temperature lag
	OverlapWindowS  float64 `json:"overlap_window_s"`
	GapThresholdS   float64 `json:"gap_threshold_s"`
	DriftApplied    bool    `json:"drift_applied"`
	BlankApplied    bool    `json:"blank_applied"`
}

// Batch is one immutable imported layer.
type Batch struct {
	ID         string         `json:"id"`
	Kind       BatchKind      `json:"kind"`
	Filename   string         `json:"filename"`
	RawText    string         `json:"raw_text"`
	Status     BatchStatus    `json:"status"`
	Diagnostics []Diagnostic  `json:"diagnostics"`
	SubmittedAt time.Time     `json:"submitted_at"`
	ParentID   string         `json:"parent_id,omitempty"`
	// parsed payloads (only what was parseable)
	Points    []SamplePoint `json:"points,omitempty"`
	Steps     []FurnaceStep `json:"steps,omitempty"`
	Gas       []GasEvent    `json:"gas,omitempty"`
	Blank     []SamplePoint `json:"blank,omitempty"`
	Drift     *DriftParam   `json:"drift,omitempty"`
	Initial   []InitialMass `json:"initial_mass,omitempty"`
	Params    *Params       `json:"params,omitempty"`
}

// Event kinds.
type EventKind string

const (
	EvImported  EventKind = "batch.imported"
	EvSuperseded EventKind = "batch.superseded"
	EvAdjudicate EventKind = "candidate.adjudicated"
	EvParamRev   EventKind = "parameters.revised"
	EvUndo       EventKind = "decision.undone"
	EvResolve    EventKind = "conflict.resolved"
	EvJudgment   EventKind = "engine.judgment"
	EvProject    EventKind = "project.created"
	EvCase       EventKind = "case.loaded"
)

// Event is an append-only fact. Sequence is project-scoped and stable.
type Event struct {
	Seq       int64           `json:"seq"`
	Kind      EventKind       `json:"kind"`
	At        time.Time       `json:"at"`
	Actor     string          `json:"actor"`
	BatchID   string          `json:"batch_id,omitempty"`
	TargetID  string          `json:"target_id,omitempty"`
	Summary   string          `json:"summary"`
	Payload   map[string]any  `json:"payload,omitempty"`
}

// CandidateState is the lifecycle of a stage/peak candidate.
type CandidateState string

const (
	CandProposed   CandidateState = "proposed"
	CandAccepted   CandidateState = "accepted"
	CandRejected   CandidateState = "rejected"
	CandConflicted CandidateState = "conflicted"
)

// Candidate is a proposed decomposition stage. Overlapping stages are legal;
// mass losses are NOT forced to sum to 100%.
type Candidate struct {
	ID            string         `json:"id"`
	StageNo       int            `json:"stage_no"`
	PeakT         float64        `json:"peak_t_s"`
	OnsetT        float64        `json:"onset_t_s"`
	EndT          float64        `json:"end_t_s"`
	PeakTempC     float64        `json:"peak_temp_c"`
	OnsetTempC    float64        `json:"onset_temp_c"`
	EndTempC      float64        `json:"end_temp_c"`
	PeakRate      float64        `json:"peak_rate_pct_per_min"`
	MassLossPct   float64        `json:"mass_loss_pct"`
	MassLossMg    float64        `json:"mass_loss_mg"`
	Overlaps      []int          `json:"overlaps"`
	Unknown       bool           `json:"unknown"`
	UnknownReason string         `json:"unknown_reason,omitempty"`
	RejectReasons []string       `json:"reject_reasons,omitempty"`
	State         CandidateState `json:"state"`
	SourceNodeID  string         `json:"source_node_id"`
	AtmAtPeak     string         `json:"atm_at_peak"`
	ValveRef      *ValveRef      `json:"valve_ref,omitempty"`
}

// ValveRef records the non-equality of valve switch time and sample reaction
// time (atmosphere delay / temperature lag).
type ValveRef struct {
	ValveSeq      int     `json:"valve_seq"`
	ValveTimeS    float64 `json:"valve_time_s"`
	ReactionTimeS float64 `json:"reaction_time_s"`
	GasDelayS     float64 `json:"gas_delay_s"`
	TempLagS      float64 `json:"temp_lag_s"`
	Note          string  `json:"note"`
}

// ChainStep is one step in an expandable residue/candidate calculation chain.
type ChainStep struct {
	Order   int      `json:"order"`
	Label   string   `json:"label"`
	Formula string   `json:"formula"`
	Inputs  []string `json:"inputs"`
	Value   string   `json:"value"`
	Unit    string   `json:"unit"`
	NodeID  string   `json:"node_id"`
	ValueID string   `json:"value_id"`
}

// Residue is the final residual fraction with full calculation chain.
type Residue struct {
	ID          string      `json:"id"`
	FinalPct    float64     `json:"final_pct"`
	FinalMg     float64     `json:"final_mg"`
	FinalTempC  float64     `json:"final_temp_c"`
	Unknown     bool        `json:"unknown"`
	UnknownReason string    `json:"unknown_reason,omitempty"`
	Chain       []ChainStep `json:"chain"`
	SourceNodeID string     `json:"source_node_id"`
}

// DerivedNode is one content-addressed derived result.
type DerivedNode struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	AlgoVersion string         `json:"algo_version"`
	ParamFP     string         `json:"param_fp"`
	Sources     []string       `json:"sources"`
	InputFP     string         `json:"input_fp"`
	Active      bool           `json:"active"`
	ProducedAt  time.Time      `json:"produced_at"`
	Payload     map[string]any `json:"payload"`
}

// Decision is a human adjudication at a point in event history.
type Decision struct {
	Seq        int64          `json:"seq"`
	At         time.Time      `json:"at"`
	CandidateID string        `json:"candidate_id"`
	Action     CandidateState `json:"action"`
	Actor      string         `json:"actor"`
	Note       string         `json:"note"`
	Undone     bool           `json:"undone"`
	UndoSeq    int64          `json:"undo_seq,omitempty"`
}
