package model

// EventType enumerates the entries of the append-only project log.
type EventType string

const (
	EvProjectCreated EventType = "project_created"
	EvImported       EventType = "imported"        // a Submission of one layer
	EvParams         EventType = "params_revised"  // analysis parameter revision
	EvDecision       EventType = "decision"        // accept / reject / adjust
	EvReverted       EventType = "reverted"        // project-level undo
	EvDerived        EventType = "derivation_run"  // provenance of a calculation run
)

// Event is one immutable log entry. Payload holds the typed entity JSON.
type Event struct {
	ID         string    `json:"id"`
	Seq        int       `json:"seq"`
	Type       EventType `json:"type"`
	OccurredAt string    `json:"occurred_at"`
	ParentID   string    `json:"parent_id,omitempty"`
	Actor      string    `json:"actor,omitempty"`
	Note       string    `json:"note,omitempty"`

	Submission *Submission      `json:"submission,omitempty"`
	Params     *AnalysisParams  `json:"params,omitempty"`
	Decision   *Decision        `json:"decision,omitempty"`
	Reverted   *RevertRecord    `json:"revert,omitempty"`
	Derived    *DerivationRecord `json:"derived,omitempty"`

	CreateName string `json:"create_name,omitempty"`
}

// DecisionVerdict enumerates human resolutions.
type DecisionVerdict string

const (
	VerdictAccept DecisionVerdict = "accepted"
	VerdictReject DecisionVerdict = "rejected"
	VerdictAdjust DecisionVerdict = "adjusted"
)

// Decision is a human judgement on one candidate or residue. Conflicting
// decisions are kept: the latest non-reverted one that targets the same
// target ID wins, and the loser is preserved with the conflict reference.
type Decision struct {
	TargetID    string          `json:"target_id"`
	TargetKind  string          `json:"target_kind"` // "stage_candidate" | "residue"
	Verdict     DecisionVerdict `json:"verdict"`
	Reason      string          `json:"reason"`
	ParamsPatch *AnalysisParams `json:"params_patch,omitempty"`
	NewBounds   *Bounds         `json:"new_bounds,omitempty"`
	ConflictsWith string        `json:"conflicts_with,omitempty"`
}

// Bounds is a human-adjusted stage window (minutes on the sample clock).
type Bounds struct {
	TStart float64 `json:"t_start_min"`
	TEnd   float64 `json:"t_end_min"`
}

// RevertRecord undoes every event after TargetSeq; reverted events stay in
// the log and their conclusions remain reachable via as-of views.
type RevertRecord struct {
	TargetSeq int `json:"target_seq"`
	RevertedCount int `json:"reverted_count"`
}

// AnalysisParams is the versioned set of calculation parameters.
type AnalysisParams struct {
	Revision        int     `json:"revision"`
	ParentID        string  `json:"parent_id,omitempty"`
	DTGWindowPoints int     `json:"dtg_window_points"`
	PeakProminence  float64 `json:"peak_prominence_pct_per_min"`
	GapThresholdMin float64 `json:"gap_threshold_min"`
	MinStageLossPct float64 `json:"min_stage_loss_pct"`
	SmoothingPoints int     `json:"smoothing_points"`
	ResidueWindowMin float64 `json:"residue_window_min"`
	AlgorithmVersion string `json:"algorithm_version"`
	Fingerprint      string `json:"fingerprint"`
}

// ChainNode is one step of the expandable calculation chain.
type ChainNode struct {
	Step      string  `json:"step"`
	Formula   string  `json:"formula"`
	Value     string  `json:"value"`
	Unit      Unit    `json:"unit"`
	SourceIDs []string `json:"source_ids,omitempty"`
}

// Gap is a sampling break; it splits the series and is never interpolated.
type Gap struct {
	TBefore float64 `json:"t_before_min"`
	TAfter  float64 `json:"t_after_min"`
	Kind    string  `json:"kind"` // "missing_rows" | "unparseable_rows"
	N       int     `json:"n"`
}

// StageCandidate is one legal decomposition stage (possibly overlapping
// another). Stages are not constrained to sum to 100%.
type StageCandidate struct {
	ID             string    `json:"id"`
	Label          string    `json:"label"`
	PeakT          float64   `json:"peak_t_min"`
	PeakDTR        float64   `json:"peak_dtg_pct_per_min"`
	TStart         float64   `json:"t_start_min"`
	TEnd           float64   `json:"t_end_min"`
	TOnset         float64   `json:"t_onset_min"`
	TOffset        float64   `json:"t_offset_min"`
	MassLossPct    float64   `json:"mass_loss_pct"`
	MassLossMG     float64   `json:"mass_loss_mg"`
	GasAtPeak      string    `json:"gas_at_peak"`
	ValveRef       string    `json:"valve_ref,omitempty"`
	DelayMin       *float64  `json:"delay_min,omitempty"`
	OverlapGroup   int       `json:"overlap_group,omitempty"`
	Ambiguity      string    `json:"ambiguity,omitempty"`
	Alternatives   []Bounds  `json:"alternatives,omitempty"`
	RejectedReason string    `json:"rejected_reason,omitempty"`
	Status         string    `json:"status"` // "candidate" | "accepted" | "rejected" | "adjusted" | "stale"
	SegmentIndex   int       `json:"segment_index"`
	Chain          []ChainNode `json:"chain,omitempty"`
	Sources        []string    `json:"sources"`
	RunID          string      `json:"run_id"`
}

// Residue is the final residual fraction with an expandable chain.
type Residue struct {
	ID           string      `json:"id"`
	Percent      float64     `json:"percent"`
	MassMG       float64     `json:"mass_mg"`
	WindowStart  float64     `json:"window_start_min"`
	WindowEnd    float64     `json:"window_end_min"`
	Status       string      `json:"status"`
	Unknown      bool        `json:"unknown,omitempty"`
	UnknownReason string     `json:"unknown_reason,omitempty"`
	Chain        []ChainNode `json:"chain"`
	Sources      []string    `json:"sources"`
	RunID        string      `json:"run_id"`
}

// DerivationRecord captures one full calculation run.
type DerivationRecord struct {
	RunID           string         `json:"run_id"`
	HeadSeq         int            `json:"head_seq"`
	ParamRevision   int            `json:"param_revision"`
	AlgorithmVersion string        `json:"algorithm_version"`
	ParamFingerprint string        `json:"param_fingerprint"`
	DirectSources   []string       `json:"direct_sources"`
	Fingerprint     string         `json:"fingerprint"`
	Stages          []StageCandidate `json:"stages"`
	Residue         Residue        `json:"residue"`
	Gaps            []Gap          `json:"gaps"`
	Diagnostics     []string       `json:"diagnostics"`
}

// PlotPoint is one normalised curve sample used for TG/DTG rendering.
type PlotPoint struct {
	TMin   float64 `json:"t"`
	TGPct  float64 `json:"tg"`
	Dtg    float64 `json:"dtg"`
	TempC  float64 `json:"temp"`
	Segment int    `json:"segment"`
}
