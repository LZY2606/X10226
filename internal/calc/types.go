package calc

import "tga-review/internal/domain"

const (
	AlgoCorrected = "corrected_series/v1"
	AlgoFurnace   = "furnace_trace/v1"
	AlgoGas       = "gas_timeline/v1"
	AlgoDTG       = "dtg/v1"
	AlgoStages    = "stages/v1"
	AlgoResidue   = "residue_chain/v1"
)

// Inputs gathers the current revisions of all six layers.
// Layers may be nil when they have not been imported yet.
type Inputs struct {
	Samples  *domain.Source
	Drift    *domain.Source
	Furnace  *domain.Source
	Valves   *domain.Source
	Blank    *domain.Source
	InitMass *domain.Source
	Params   domain.Params
}

type Gap struct {
	FromS float64 `json:"from_s"`
	ToS   float64 `json:"to_s"`
	Rows  [2]int  `json:"rows"`
}

type CorrectedPoint struct {
	TS        float64 `json:"t_s"`
	Row       int     `json:"row"`
	RawMg     float64 `json:"raw_mg"`
	BlankMg   float64 `json:"blank_mg"`
	DriftMg   float64 `json:"drift_mg"`
	CorrMg    float64 `json:"corr_mg"`
	Usable    bool    `json:"usable"`
	Region    int     `json:"region"`
	Quarantine string `json:"quarantine,omitempty"` // nonmonotonic | unconvertible
	GapBefore bool    `json:"gap_before,omitempty"`
}

type Corrected struct {
	Points       []CorrectedPoint `json:"points"`
	Gaps         []Gap            `json:"gaps"`
	Nonmonotonic []float64        `json:"nonmonotonic_t_s"`
	InterceptMg  float64          `json:"intercept_mg"`
	SlopeMgS     float64          `json:"slope_mg_per_s"`
}

type TracePoint struct {
	TS         float64 `json:"t_s"`
	SetpointC  float64 `json:"setpoint_c"`
	EffectiveC float64 `json:"effective_c"`
}

type FurnaceTrace struct {
	Points          []TracePoint `json:"points"`
	PeakSetpointC   float64      `json:"peak_setpoint_c"`
	PeakEffectiveC  float64      `json:"peak_effective_c"`
	AtValves        []AtValve    `json:"at_valves"`
	TotalS          float64      `json:"total_s"`
}

type AtValve struct {
	ValveTS        float64 `json:"valve_t_s"`
	Gas            string  `json:"gas"`
	SetpointC      float64 `json:"setpoint_c"`
	EffectiveC     float64 `json:"effective_c"`
}

type GasSegment struct {
	FromS        float64 `json:"from_s"`
	ToS          float64 `json:"to_s"`
	ValveGas     string  `json:"valve_gas"`
	EffectiveGas string  `json:"effective_gas"`
	DelayS       float64 `json:"delay_s"`
	Transition   bool    `json:"transition"`
}

type Switch struct {
	ValveTS     float64 `json:"valve_t_s"`
	EffectiveTS float64 `json:"effective_t_s"`
	Gas         string  `json:"gas"`
	DelayS      float64 `json:"delay_s"`
}

type GasTimeline struct {
	Segments []GasSegment `json:"segments"`
	Switches []Switch     `json:"switches"`
}

type DTGPoint struct {
	TS            float64 `json:"t_s"`
	Region        int     `json:"region"`
	DmDtMgS       float64 `json:"dmdt_mg_s"`
	SmoothPctMin  float64 `json:"smooth_pct_min"`
	DmDtMgK       float64 `json:"dmdt_mg_k"`
	EffTempC      float64 `json:"eff_temp_c"`
	Valid         bool    `json:"valid"`
}

type DTG struct {
	Points       []DTGPoint `json:"points"`
	M0Mg         float64    `json:"m0_mg"`
	ThresholdPct float64    `json:"threshold_pct_min"`
}

type StageCandidate struct {
	Key            string  `json:"key"`
	Proposal       string  `json:"proposal"` // single | merged | part_a | part_b
	Cluster        int     `json:"cluster"`
	StartS         float64 `json:"start_s"`
	PeakS          float64 `json:"peak_s"`
	EndS           float64 `json:"end_s"`
	PeakPctMin     float64 `json:"peak_pct_min"`
	PeakTempC      float64 `json:"peak_temp_c"`
	AreaMg         float64 `json:"area_mg"`
	AreaPct        float64 `json:"area_pct"`
	Atmosphere     string  `json:"atmosphere"`
	AtmosphereNote string  `json:"atmosphere_note,omitempty"`
	BoundedByGap   bool    `json:"bounded_by_gap"`
	Status         string  `json:"status"` // ok | unknown
	RejectReason   string  `json:"reject_reason,omitempty"`
}

type Stages struct {
	Candidates []StageCandidate `json:"candidates"`
}

type ChainStep struct {
	Label   string             `json:"label"`
	Formula string             `json:"formula"`
	Inputs  []string           `json:"inputs"`
	Value   *domain.Q          `json:"value,omitempty"`
	Detail  map[string]float64 `json:"detail,omitempty"`
}

type StageLoss struct {
	Key     string  `json:"key"`
	AreaPct float64 `json:"area_pct"`
	Overlap bool    `json:"overlap"`
}

type ResidueChain struct {
	Steps         []ChainStep `json:"steps"`
	ResiduePct    float64     `json:"residue_pct"`
	FinalMg       float64     `json:"final_mg"`
	M0Mg          float64     `json:"m0_mg"`
	AcceptedLoss  []StageLoss `json:"accepted_stage_loss_pct"`
	SumAccepted   float64     `json:"sum_accepted_pct"`
	BalanceNote   string      `json:"balance_note"`
}
