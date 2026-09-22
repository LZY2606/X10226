package engine

// Basis maps each raw layer kind to the exact revision id used as input.
type Basis map[string]string

// Point is one normalized (but traceable) series value.
type Point struct {
	Index        int     `json:"index"`
	TimeS        float64 `json:"time_s"`
	TFurnaceC    float64 `json:"t_furnace_c"`
	TSampleC     float64 `json:"t_sample_c"`
	TEstimated   bool    `json:"t_estimated"` // lag extrapolated across a gap
	MRawMg       float64 `json:"m_raw_mg"`
	BlankMg      float64 `json:"blank_mg"`
	DriftMg      float64 `json:"drift_mg"`
	MCorrMg      float64 `json:"m_corr_mg"`
	Pct          float64 `json:"pct"` // % of current initial mass, basis T_sample not implied
	DTGPctPerMin float64 `json:"dtg_pct_per_min"`
	IntervalID   string  `json:"interval_id"`
	NonMonotonic bool    `json:"non_monotonic"`
	MissingInput bool    `json:"missing_input"` // source row retained but a field unconvertible
}

// Gap records a break in acquisition. Interpolation is forbidden across it.
type Gap struct {
	AfterIndex int     `json:"after_index"`
	T1S        float64 `json:"t1_s"`
	T2S        float64 `json:"t2_s"`
	DtS        float64 `json:"dt_s"`
}

// Interval is a contiguous stretch of valid samples between gaps.
type Interval struct {
	ID        string  `json:"id"`
	FromIndex int     `json:"from_index"`
	ToIndex   int     `json:"to_index"`
	StartS    float64 `json:"start_s"`
	EndS      float64 `json:"end_s"`
}

// RejectedFeature keeps a feature that was considered and refused, with reason.
type RejectedFeature struct {
	Kind    string   `json:"kind"` // peak | stage
	TimeS   float64  `json:"time_s"`
	Reasons []string `json:"reasons"`
}

// StageCandidate is one legal decomposition option. Overlapping peaks yield
// several candidates for the same window; nothing is forced to sum to 100%.
type StageCandidate struct {
	ID             string   `json:"id"`
	GroupID        string   `json:"group_id"`
	Option         string   `json:"option"` // merged | split_a | split_b
	StartS         float64  `json:"start_s"`
	EndS           float64  `json:"end_s"`
	PeakS          float64  `json:"peak_s"`
	PeakTSampleC   float64  `json:"peak_t_sample_c"`
	ActiveGas      string   `json:"active_gas_at_sample"`
	ValveSwitchS   *float64 `json:"valve_switch_s"`   // furnace clock
	ReactionOnsetS *float64 `json:"reaction_onset_s"` // valve + atmosphere delay
	LossPct        *float64 `json:"loss_pct"`
	LossMg         *float64 `json:"loss_mg"`
	ResidueEndPct  *float64 `json:"residue_end_pct"`
	SpansGap       bool     `json:"spans_gap"`
	Status         string   `json:"status"` // proposed | unknown_gap | rejected
	Reasons        []string `json:"reasons,omitempty"`
	OverlapsWith   []string `json:"overlaps_with,omitempty"`
}

// ChainNode is one step of an expandable residue calculation chain.
type ChainNode struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Value       *float64 `json:"value"`
	Unit        string   `json:"unit"`
	Basis       string   `json:"basis"`
	Status      string   `json:"status"` // ok | unknown
	SourceKind  string   `json:"source_kind,omitempty"`
	SourceRev   string   `json:"source_rev,omitempty"`
	Formula     string   `json:"formula,omitempty"`
	Diagnostics []string `json:"diagnostics,omitempty"`

	m0Mg *float64 // unexported: current initial mass used while building
}

// ResidueChain traces final residue back to every input it depends on.
type ResidueChain struct {
	ID         string      `json:"id"`
	ResiduePct *float64    `json:"residue_pct"`
	Status     string      `json:"status"`
	Nodes      []ChainNode `json:"nodes"`
}

// RuleJudgement is a machine-readable statement of a domain rule.
type RuleJudgement struct {
	Code    string   `json:"code"`
	Status  string   `json:"status"` // applies | pass | info
	Message string   `json:"message"`
	Refs    []string `json:"refs"`
}

// Artifact is the full deterministic derived result for one basis.
type Artifact struct {
	ID                string            `json:"id"`
	AlgorithmVersion  string            `json:"algorithm_version"`
	ParamsFingerprint string            `json:"params_fingerprint"`
	Params            Params            `json:"params"`
	Basis             Basis             `json:"basis"`
	Points            []Point           `json:"points"`
	Gaps              []Gap             `json:"gaps"`
	Intervals         []Interval        `json:"intervals"`
	Candidates        []StageCandidate  `json:"candidates"`
	Rejected          []RejectedFeature `json:"rejected_features"`
	Residue           *ResidueChain     `json:"residue_chain"`
	Judgements        []RuleJudgement   `json:"judgements"`
	Valves            []ValveMark       `json:"valves"`
	Diagnostics       []string          `json:"diagnostics"`
	m0Mg              *float64          // unexported: current initial mass used while building
}

// ValveMark is a gas valve switch on the furnace clock.
type ValveMark struct {
	TimeS     float64 `json:"time_s"`
	FromGas   string  `json:"from_gas"`
	ToGas     string  `json:"to_gas"`
	ReactionS float64 `json:"reaction_time_s"` // switch + atmosphere delay
}
