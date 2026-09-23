package domain

// Derived-node payloads.

type CleanPoint struct {
	Index    int     `json:"index"`
	TimeS    float64 `json:"time_s"`
	TempC    float64 `json:"temp_c"`
	MassRaw  float64 `json:"mass_raw_mg"`
	MassCor  float64 `json:"mass_corr_mg"`
	Interval int     `json:"interval"`
}

type GapInfo struct {
	AfterIndex int     `json:"after_index"`
	BeforeT    float64 `json:"before_t_s"`
	AfterT     float64 `json:"after_t_s"`
	WidthS     float64 `json:"width_s"`
}

type CleanedSeriesPayload struct {
	TimeBasis      string       `json:"time_basis"`
	Points         []CleanPoint `json:"points"`
	Gaps           []GapInfo    `json:"gaps"`
	Intervals      [][2]int     `json:"intervals"` // index ranges [lo,hi] of point indices
	NonMonotonic   []int        `json:"nonmonotonic_indices"`
	DriftMethod    string       `json:"drift_method"`
	Diagnostics    []string     `json:"diagnostics,omitempty"`
	DroppedRawRows []int        `json:"dropped_raw_rows,omitempty"`
}

type DTGPoint struct {
	Index     int     `json:"index"`
	TimeS     float64 `json:"time_s"`
	MgPerMin  float64 `json:"dtg_mg_per_min"`
	PctPerMin float64 `json:"dtg_pct_per_min,omitempty"`
	Interval  int     `json:"interval"`
}

type DTGPayload struct {
	Points       []DTGPoint `json:"points"`
	Gaps         []GapInfo  `json:"gaps"`
	Smoothing    string     `json:"smoothing"` // e.g. moving_average_k3
	PeakNoiseStd float64    `json:"peak_noise_std_mg_per_min"`
}

type StageLoss struct {
	ValuePct float64 `json:"value_pct,omitempty"`
	ValueMg  float64 `json:"value_mg,omitempty"`
	Status   string  `json:"status"` // ok | unknown
	Reason   string  `json:"reason,omitempty"`
}

type SubPeak struct {
	TimeS float64 `json:"time_s"`
	Value float64 `json:"value_mg_per_min"`
}

type StageCandidatePayload struct {
	Key           string    `json:"key"`
	Origin        string    `json:"origin"` // dtg_peak | atmosphere_window | manual_adjustment
	Gas           string    `json:"gas,omitempty"`
	ValveID       string    `json:"valve_id,omitempty"`
	StartS        float64   `json:"start_s"`
	EndS          float64   `json:"end_s"`
	PeakS         float64   `json:"peak_s,omitempty"`
	StartC        float64   `json:"start_c,omitempty"`
	EndC          float64   `json:"end_c,omitempty"`
	Loss          StageLoss `json:"loss"`
	OverlapsWith  []string  `json:"overlaps_with,omitempty"`
	SubPeaks      []SubPeak `json:"sub_peaks,omitempty"`
	Reasons       []string  `json:"reasons,omitempty"`
	RejectReasons []string  `json:"reject_reasons,omitempty"`
	Unknown       bool      `json:"unknown,omitempty"`
	AdjustedFrom  string    `json:"adjusted_from,omitempty"`
}

type ResidueChainStep struct {
	Ref      Ref     `json:"ref"`
	Role     string  `json:"role"`
	ValueMg  float64 `json:"value_mg,omitempty"`
	ValuePct float64 `json:"value_pct,omitempty"`
	Status   string  `json:"status"`
	Note     string  `json:"note,omitempty"`
}

type FinalResiduePayload struct {
	FinalMassMg float64            `json:"final_mass_mg"`
	InitialMg   float64            `json:"initial_mass_mg"`
	ResiduePct  StageLoss          `json:"residue_pct"`
	AtTimeS     float64            `json:"at_time_s"`
	Chain       []ResidueChainStep `json:"chain"`
}

// Judgment is a machine-readable statement of domain reasoning.
type Judgment struct {
	Type      string                 `json:"type"`
	Seq       int64                  `json:"seq,omitempty"`
	BornSeq   int64                  `json:"born_seq,omitempty"`
	VirtualTS string                 `json:"virtual_ts,omitempty"`
	Props     map[string]interface{} `json:"props"`
	Basis     []Ref                  `json:"basis,omitempty"`
}

// Node kinds.
const (
	NodeCleanedSeries = "cleaned_series"
	NodeDTG           = "dtg_series"
	NodeStage         = "stage_candidate"
	NodeAtmoStage     = "atmosphere_stage"
	NodeResidue       = "final_residue"
)
