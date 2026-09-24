package tga

import "time"

const (
	KindSamples        = "samples"
	KindDrift          = "balance_drift"
	KindFurnace        = "furnace_program"
	KindGas            = "gas_valve_events"
	KindBlank          = "blank_curve"
	KindInitialMass    = "initial_mass"
	AlgorithmCandidates = "tga-stage-decomposition/v1"
)

type Clock func() time.Time

type Quantity struct {
	Raw      string  `json:"raw"`
	Value    float64 `json:"value,omitempty"`
	Unit     string  `json:"unit,omitempty"`
	Valid    bool    `json:"valid"`
	Diagnosis string `json:"diagnosis,omitempty"`
}

type Source struct {
	ID        string                 `json:"id"`
	Kind      string                 `json:"kind"`
	Version   int                    `json:"version"`
	ParentID  string                 `json:"parent_id,omitempty"`
	Label     string                 `json:"label"`
	Raw       string                 `json:"raw"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
	Order     int                    `json:"order"`
	CreatedAt time.Time              `json:"created_at"`
	Status    string                 `json:"status,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

type RawSample struct {
	Index       int      `json:"index"`
	FurnaceTime Quantity `json:"furnace_time"`
	Temperature Quantity `json:"temperature"`
	Mass        Quantity `json:"mass"`
	Present     bool     `json:"present"`
}

type CorrectedSample struct {
	Index         int     `json:"index"`
	TimeMin       float64 `json:"time_min"`
	TemperatureC  float64 `json:"temperature_c"`
	ObservedMg    float64 `json:"observed_mg"`
	CorrectedMg   float64 `json:"corrected_mg"`
	BlankMg       float64 `json:"blank_mg"`
	NetMg         float64 `json:"net_mg"`
	TGPercent     float64 `json:"tg_percent"`
	DTGPercentMin float64 `json:"dtg_percent_min,omitempty"`
	DTGValid      bool    `json:"dtg_valid"`
	GapBefore     bool    `json:"gap_before"`
	NonMonotonic  bool    `json:"non_monotonic"`
	SourceSample  string  `json:"source_sample"`
}

type FurnaceSegment struct {
	StartMin     float64 `json:"start_min"`
	EndMin       float64 `json:"end_min"`
	StartC       float64 `json:"start_c"`
	EndC         float64 `json:"end_c"`
	RateCPerMin  float64 `json:"rate_c_per_min"`
	SourceEventID string `json:"source_event_id"`
}

type GasInterval struct {
	ValveTimeMin       float64 `json:"valve_time_min"`
	ReactionTimeMin    float64 `json:"reaction_time_min"`
	EndReactionTimeMin float64 `json:"end_reaction_time_min"`
	Gas                string  `json:"gas"`
	DelayS             float64 `json:"delay_s"`
	SourceEventID      string  `json:"source_event_id"`
}

type Candidate struct {
	ID                 string   `json:"id"`
	Stage              int      `json:"stage"`
	Alternative        int      `json:"alternative"`
	Label              string   `json:"label"`
	Gas                string   `json:"gas"`
	StartMin           float64 `json:"start_min"`
	PeakMin            float64 `json:"peak_min"`
	EndMin             float64 `json:"end_min"`
	TemperatureStartC  float64 `json:"temperature_start_c"`
	TemperaturePeakC   float64 `json:"temperature_peak_c"`
	TemperatureEndC    float64 `json:"temperature_end_c"`
	MassLossMg         float64 `json:"mass_loss_mg"`
	MassLossPercent    float64 `json:"mass_loss_percent"`
	ResidueAtEndPct    float64 `json:"residue_at_end_percent"`
	OverlapsWith       []string `json:"overlaps_with,omitempty"`
	Unknown            bool     `json:"unknown"`
	RejectedReason     string   `json:"rejected_reason,omitempty"`
	CalculationChain   []string `json:"calculation_chain"`
	DirectSources      []string `json:"direct_sources"`
}

type Node struct {
	Key             string             `json:"key"`
	Kind            string             `json:"kind"`
	Version         int                `json:"version"`
	DisplayID       string             `json:"display_id"`
	Status          string             `json:"status"`
	Label           string             `json:"label"`
	Value           string             `json:"value,omitempty"`
	Unit            string             `json:"unit,omitempty"`
	CoordinateBasis string             `json:"coordinate_basis,omitempty"`
	Unknown         bool               `json:"unknown"`
	Error           string             `json:"error,omitempty"`
	Algorithm       string             `json:"algorithm"`
	ParameterHash   string             `json:"parameter_hash"`
	DirectSources   []string           `json:"direct_sources"`
	Chain           []string           `json:"calculation_chain"`
	RawSamples      []RawSample        `json:"raw_samples,omitempty"`
	Samples         []CorrectedSample  `json:"samples,omitempty"`
	Furnace         []FurnaceSegment   `json:"furnace,omitempty"`
	GasTimeline     []GasInterval      `json:"gas_timeline,omitempty"`
	Candidates       []Candidate        `json:"candidates,omitempty"`
	Diagnoses       []Judgment         `json:"diagnoses,omitempty"`
}

type Judgment struct {
	Code        string   `json:"code"`
	Severity    string   `json:"severity"`
	Message     string   `json:"message"`
	ObjectKeys  []string `json:"object_keys"`
	SourceIDs   []string `json:"source_ids"`
	RuleVersion string   `json:"rule_version"`
}

type Decision struct {
	ID            string    `json:"id"`
	EventID       int64     `json:"event_id"`
	CandidateKey  string    `json:"candidate_key"`
	Stage         int       `json:"stage"`
	Alternative   int       `json:"alternative"`
	Action        string    `json:"action"`
	Reason        string    `json:"reason"`
	Parameters    map[string]float64 `json:"parameters,omitempty"`
	ConflictWith  []string  `json:"conflict_with,omitempty"`
	SupersededBy  string    `json:"superseded_by,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type InputBundle struct {
	Sources        []Source
	CurrentByKind  map[string]Source
	FailedByKind   map[string]Source
	Params         Params
}

type Params struct {
	GasDelayS          float64 `json:"gas_delay_s"`
	ThermalLagS        float64 `json:"thermal_lag_s"`
	DriftOffsetStartUg float64 `json:"drift_offset_start_ug"`
	DriftOffsetEndUg   float64 `json:"drift_offset_end_ug"`
	PeakThresholdPctMin float64 `json:"peak_threshold_pct_min"`
	OverlapWindowMin   float64 `json:"overlap_window_min"`
	GapLimitSec        float64 `json:"gap_limit_sec"`
}

type Result struct {
	Nodes     []Node
	Judgments []Judgment
}
