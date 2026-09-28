package domain

type PointState string

const (
	PointObserved PointState = "observed"
	PointGap      PointState = "gap"
	PointUnknown  PointState = "unknown"
)

type CurvePoint struct {
	Seq             int        `json:"seq"`
	TimeMin         float64    `json:"time_min"`
	FurnaceTimeMin  float64    `json:"furnace_time_min"`
	TemperatureC    *float64   `json:"temperature_c,omitempty"`
	RawMassMg       *float64   `json:"raw_mass_mg,omitempty"`
	DriftCorrectedMg *float64  `json:"drift_corrected_mg,omitempty"`
	BlankCorrectedMg *float64  `json:"blank_corrected_mg,omitempty"`
	SampleMassMg    *float64   `json:"sample_mass_mg,omitempty"`
	NormalizedPct   *float64   `json:"normalized_pct,omitempty"`
	DTGMgPerMin     *float64   `json:"dtg_mg_per_min,omitempty"`
	State           PointState `json:"state"`
	Diagnostics     []string   `json:"diagnostics,omitempty"`
}

type DTGPeak struct {
	ID              string   `json:"id"`
	TimeMin         float64  `json:"time_min"`
	TemperatureC    *float64 `json:"temperature_c,omitempty"`
	DTGMgPerMin     float64  `json:"dtg_mg_per_min"`
	OverlapPeakIDs  []string `json:"overlap_peak_ids,omitempty"`
}

type AtmosphereWindow struct {
	Gas           string  `json:"gas"`
	ValveStartMin float64 `json:"valve_start_min"`
	ValveEndMin   float64 `json:"valve_end_min"`
	ReactionStart float64 `json:"reaction_start_min"`
	ReactionEnd   float64 `json:"reaction_end_min"`
	SourceEventID string  `json:"source_event_id,omitempty"`
}

type GapSegment struct {
	StartTimeMin float64  `json:"start_time_min"`
	EndTimeMin   float64  `json:"end_time_min"`
	MassLossMg   *float64 `json:"mass_loss_mg,omitempty"`
	BoundedOnly  bool     `json:"bounded_only"`
}

type Candidate struct {
	ID                string         `json:"id"`
	Code              string         `json:"code"`
	Kind              string         `json:"kind"`
	Label             string         `json:"label"`
	StartTimeMin      float64        `json:"start_time_min"`
	EndTimeMin        float64        `json:"end_time_min"`
	StartTemperatureC *float64       `json:"start_temperature_c,omitempty"`
	EndTemperatureC   *float64       `json:"end_temperature_c,omitempty"`
	PeakTimeMin       *float64       `json:"peak_time_min,omitempty"`
	Atmosphere        string         `json:"atmosphere,omitempty"`
	MassLossMg        *float64       `json:"mass_loss_mg,omitempty"`
	LossPct           *float64       `json:"loss_pct,omitempty"`
	Uncertainty       string         `json:"uncertainty,omitempty"`
	Status            string         `json:"status"`
	Legality          string         `json:"legality"`
	RejectionReasons  []string       `json:"rejection_reasons,omitempty"`
	Overlaps          []string       `json:"overlaps,omitempty"`
	Evidence          map[string]any `json:"evidence,omitempty"`
}

type DisplayValue struct {
	StableID    string   `json:"stable_id"`
	Kind        string   `json:"kind"`
	Label       string   `json:"label"`
	Value       string   `json:"value"`
	Unit        string   `json:"unit,omitempty"`
	Basis       string   `json:"basis,omitempty"`
	Status      string   `json:"status"`
	DirectInputs []string `json:"direct_inputs,omitempty"`
	Formula      string   `json:"formula,omitempty"`
	Steps        []CalcStep `json:"steps,omitempty"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

type CalcStep struct {
	Order int      `json:"order"`
	Text  string   `json:"text"`
	Value string   `json:"value,omitempty"`
	Unit  string   `json:"unit,omitempty"`
	Refs  []string `json:"refs,omitempty"`
}

type AnalysisRun struct {
	ID                 string              `json:"id"`
	ProjectID          string              `json:"project_id"`
	TriggerEventID     string              `json:"trigger_event_id"`
	AlgorithmVersion   string              `json:"algorithm_version"`
	ParameterFingerprint string            `json:"parameter_fingerprint"`
	Parameters         Parameters          `json:"parameters"`
	DirectSources      map[string]string   `json:"direct_sources"`
	CreatedAt          string              `json:"created_at"`
	Points             []CurvePoint        `json:"points"`
	Peaks              []DTGPeak           `json:"peaks"`
	AtmosphereWindows  []AtmosphereWindow  `json:"atmosphere_windows"`
	Gaps               []GapSegment        `json:"gaps"`
	Candidates         []Candidate         `json:"candidates"`
	DisplayValues      []DisplayValue      `json:"display_values"`
	Assertions         []MachineAssertion  `json:"assertions"`
	Diagnostics        []string            `json:"diagnostics"`
	Current            bool                `json:"current"`
}

type MachineAssertion struct {
	Code      string         `json:"code"`
	Status    string         `json:"status"`
	Statement string         `json:"statement"`
	Evidence  map[string]any `json:"evidence,omitempty"`
}

const (
	CandidateLegalAccepted = "legal"
	CandidateRejected      = "rejected"
	CandidateAmbiguous     = "ambiguous"
)
