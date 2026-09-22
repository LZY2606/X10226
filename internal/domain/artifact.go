package domain

import "encoding/json"

// Artifact kinds produced by the derivation layers.
const (
	ArtValidated   = "validated_series"
	ArtSegments    = "segments"
	ArtCorrected   = "corrected_series"
	ArtDTG         = "dtg_series"
	ArtFurnace     = "furnace_program"
	ArtGasLine     = "gas_timeline"
	ArtProposal    = "stage_proposal"
	ArtCandSet     = "candidate_set"
	ArtResidual    = "residual"
	ArtBalance     = "mass_balance"
	ArtMassBalance = "mass_balance"
	ArtArchive     = "archive_report"
)

// Artifact is an immutable derived value. The same logical result keeps the
// same stable ID across recomputation and export/import round trips.
type Artifact struct {
	ID          string          `json:"id"` // stable, family+content based
	ProjectID   string          `json:"project_id"`
	Kind        string          `json:"kind"`
	Family      string          `json:"family"` // logical identity (e.g. "residual")
	Label       string          `json:"label"`
	AlgoVersion string          `json:"algo_version"`
	ParamFP     string          `json:"param_fp"`
	SourceIDs   []string        `json:"source_ids"`
	DependsOn   []string        `json:"depends_on"`
	CreatedSeq  int64           `json:"created_seq"` // event seq at birth
	Payload     json.RawMessage `json:"payload"`
}

// Sample is one validated sample in the SI-normalized frame. Raw token text
// of every coordinate is preserved for traceability.
type Sample struct {
	Index    int     `json:"index"`
	LineNo   int     `json:"line_no"`
	TimeS    float64 `json:"time_s"`
	TempC    float64 `json:"temp_c"`
	MassMg   float64 `json:"mass_mg"`
	RawTime  string  `json:"raw_time"`
	RawTemp  string  `json:"raw_temp"`
	RawMass  string  `json:"raw_mass"`
	Excluded bool    `json:"excluded,omitempty"`
	Reason   string  `json:"reason,omitempty"`
}

type ValidatedSeries struct {
	TimeUnit string   `json:"time_unit"`
	TempUnit string   `json:"temp_unit"`
	MassUnit string   `json:"mass_unit"`
	Samples  []Sample `json:"samples"`
}

// Segment is a monotonic-time run split at sampling gaps. Gaps are never
// bridged by interpolation.
type Segment struct {
	ID         string  `json:"id"`
	StartIndex int     `json:"start_index"`
	EndIndex   int     `json:"end_index"`
	StartTimeS float64 `json:"start_time_s"`
	EndTimeS   float64 `json:"end_time_s"`
	GapAfterS  float64 `json:"gap_after_s,omitempty"`
}

type CorrectedSample struct {
	Index       int     `json:"index"`
	TimeS       float64 `json:"time_s"`
	TempC       float64 `json:"temp_c"`
	ObservedMg  float64 `json:"observed_mg"`
	DriftMg     float64 `json:"drift_mg"`
	BlankMg     float64 `json:"blank_mg"`
	CorrectedMg float64 `json:"corrected_mg"`
}

type CorrectedSeries struct {
	Samples    []CorrectedSample `json:"samples"`
	DriftModel string            `json:"drift_model"`
}

type DTGPoint struct {
	Index       int     `json:"index"`
	TimeS       float64 `json:"time_s"`
	TempC       float64 `json:"temp_c"`
	DTGMgPerMin float64 `json:"dtg_mg_per_min"`
	DTGPerMin   float64 `json:"dtg_per_min"` // normalized 1/min
	InGap       bool    `json:"in_gap,omitempty"`
}

type DTGSeries struct {
	Points []DTGPoint `json:"points"`
}

type FurnaceStep struct {
	Index       int     `json:"index"`
	FromS       float64 `json:"from_s"`
	ToS         float64 `json:"to_s"`
	StartC      float64 `json:"start_c"`
	EndC        float64 `json:"end_c"`
	RateCPerMin float64 `json:"rate_c_per_min"`
	Hold        bool    `json:"hold"`
	RawText     string  `json:"raw_text"`
}

type FurnaceProgram struct {
	Steps []FurnaceStep `json:"steps"`
}

// GasEvent is a valve event plus the delayed/hysteretic instant at which the
// atmosphere actually reaches the sample.
type GasEvent struct {
	Index       int     `json:"index"`
	AtS         float64 `json:"at_s"` // valve switch time
	FromGas     string  `json:"from_gas"`
	ToGas       string  `json:"to_gas"`
	FlowMLMin   float64 `json:"flow_ml_min"`
	DelayS      float64 `json:"delay_s"`       // atmosphere delay
	ReactionAtS float64 `json:"reaction_at_s"` // effective reaction time
	TempLagC    float64 `json:"temp_lag_c"`    // sample-vs-program temperature lag
	RawText     string  `json:"raw_text"`
}

type GasTimeline struct {
	Events []GasEvent `json:"events"`
}
