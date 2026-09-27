package tga

import "encoding/json"

type Value struct {
	Raw           string   `json:"raw"`
	Value         float64  `json:"value,omitempty"`
	Unit          string   `json:"unit,omitempty"`
	Basis         string   `json:"basis,omitempty"`
	Valid         bool     `json:"valid"`
	Diagnostics   []string `json:"diagnostics,omitempty"`
	OriginalValue string   `json:"original_value,omitempty"`
}

type SamplePoint struct {
	ID            string `json:"id"`
	Time          Value  `json:"time"`
	FurnaceTime   Value  `json:"furnace_time"`
	Temperature   Value  `json:"temperature"`
	Mass          Value  `json:"mass"`
	Quality       string `json:"quality,omitempty"`
	ImportStatus  string `json:"import_status,omitempty"`
	SourceVersion string `json:"source_version,omitempty"`
}

type BlankPoint struct {
	ID            string `json:"id"`
	Time          Value  `json:"time"`
	Mass          Value  `json:"mass"`
	SourceVersion string `json:"source_version,omitempty"`
}

type FurnaceSegment struct {
	ID            string `json:"id"`
	StartTime     Value  `json:"start_time"`
	EndTime       Value  `json:"end_time"`
	StartTemp     Value  `json:"start_temperature"`
	EndTemp       Value  `json:"end_temperature"`
	Rate          Value  `json:"rate"`
	Mode          string `json:"mode"`
	ImportStatus  string `json:"import_status,omitempty"`
	SourceVersion string `json:"source_version,omitempty"`
}

type GasEvent struct {
	ID            string `json:"id"`
	Time          Value  `json:"time"`
	Valve         string `json:"valve"`
	Gas           string `json:"gas"`
	Action        string `json:"action"`
	SourceVersion string `json:"source_version,omitempty"`
}

type InitialMass struct {
	ID            string `json:"id"`
	Version       int    `json:"version"`
	Mass          Value  `json:"mass"`
	Method        string `json:"method"`
	ParentVersion int    `json:"parent_version,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type DriftCorrection struct {
	ID            string  `json:"id"`
	Version       int     `json:"version"`
	InterceptMg   float64 `json:"intercept_mg"`
	RateMgPerMin  float64 `json:"rate_mg_per_min"`
	Method        string  `json:"method"`
	ParentVersion int     `json:"parent_version,omitempty"`
	Reason        string  `json:"reason,omitempty"`
}

type InputBundle struct {
	Samples          []SamplePoint     `json:"sample_points"`
	Blank            []BlankPoint      `json:"blank_curve"`
	Furnace          []FurnaceSegment  `json:"furnace_program"`
	GasEvents        []GasEvent        `json:"gas_valve_events"`
	InitialMasses    []InitialMass     `json:"initial_mass_layers"`
	DriftCorrections []DriftCorrection `json:"drift_corrections"`
}

type Parameters struct {
	AlgorithmVersion      string  `json:"algorithm_version"`
	AtmosphereDelayS      float64 `json:"atmosphere_delay_s"`
	TemperatureLagS       float64 `json:"temperature_lag_s"`
	DtgThresholdPctPerMin float64 `json:"dtg_threshold_pct_per_min"`
	DtgZeroPctPerMin      float64 `json:"dtg_zero_pct_per_min"`
	NoiseToleranceMg      float64 `json:"noise_tolerance_mg"`
	GapMultiple           float64 `json:"gap_multiple"`
	OverlapSeparationS    float64 `json:"overlap_separation_s"`
}

func DefaultParameters() Parameters {
	return Parameters{
		AlgorithmVersion:      "tga-stage-decomp/1.0.0",
		AtmosphereDelayS:      22,
		TemperatureLagS:       12,
		DtgThresholdPctPerMin: -0.12,
		DtgZeroPctPerMin:      -0.025,
		NoiseToleranceMg:      0.015,
		GapMultiple:           2.2,
		OverlapSeparationS:    95,
	}
}

type SourceRef struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int    `json:"version"`
	Role    string `json:"role"`
}

type Node struct {
	ID      string         `json:"id"`
	Kind    string         `json:"kind"`
	Label   string         `json:"label"`
	Value   string         `json:"value,omitempty"`
	Unit    string         `json:"unit,omitempty"`
	Status  string         `json:"status,omitempty"`
	Sources []SourceRef    `json:"sources,omitempty"`
	Meta    map[string]any `json:"meta,omitempty"`
}

type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Role string `json:"role"`
}

type GraphPoint struct {
	TimeS           float64  `json:"time_s"`
	TemperatureC    float64  `json:"temperature_c,omitempty"`
	RawMassMg       *float64 `json:"raw_mass_mg,omitempty"`
	BlankMassMg     *float64 `json:"blank_mass_mg,omitempty"`
	CorrectedMassMg *float64 `json:"corrected_mass_mg,omitempty"`
	ResidualPct     *float64 `json:"residual_pct,omitempty"`
	DTGPctPerMin    *float64 `json:"dtg_pct_per_min,omitempty"`
	DTGMgPerMin     *float64 `json:"dtg_mg_per_min,omitempty"`
	GapAfter        bool     `json:"gap_after,omitempty"`
	NonMonotonic    bool     `json:"non_monotonic,omitempty"`
	Quality         string   `json:"quality,omitempty"`
	SourcePointID   string   `json:"source_point_id,omitempty"`
	Status          string   `json:"status,omitempty"`
	Diagnostics     []string `json:"diagnostics,omitempty"`
}

type Peak struct {
	ID           string   `json:"id"`
	TimeS        float64  `json:"time_s"`
	TemperatureC float64  `json:"temperature_c"`
	DTGPctPerMin float64  `json:"dtg_pct_per_min"`
	WindowStartS float64  `json:"window_start_s"`
	WindowEndS   float64  `json:"window_end_s"`
	Prominence   float64  `json:"prominence"`
	Status       string   `json:"status,omitempty"`
	Diagnostics  []string `json:"diagnostics,omitempty"`
}

type StageCandidate struct {
	ID                  string   `json:"id"`
	Index               int      `json:"index"`
	Label               string   `json:"label"`
	StartS              float64  `json:"start_s"`
	EndS                float64  `json:"end_s"`
	StartTemperatureC   float64  `json:"start_temperature_c"`
	EndTemperatureC     float64  `json:"end_temperature_c"`
	StartResidualPct    float64  `json:"start_residual_pct"`
	EndResidualPct      float64  `json:"end_residual_pct"`
	MassLossPct         float64  `json:"mass_loss_pct"`
	PeakIDs             []string `json:"peak_ids"`
	AtmosphereGas       string   `json:"atmosphere_gas,omitempty"`
	ValveTimeS          *float64 `json:"valve_time_s,omitempty"`
	EffectiveTimeS      *float64 `json:"effective_atmosphere_time_s,omitempty"`
	ReactionDelayS      *float64 `json:"reaction_delay_s,omitempty"`
	ProgramTemperatureC *float64 `json:"program_temperature_c,omitempty"`
	TemperatureLagS     *float64 `json:"temperature_lag_s,omitempty"`
	Status              string   `json:"status"`
	RejectedReasons     []string `json:"rejected_reasons,omitempty"`
	Observations        []string `json:"observations,omitempty"`
	OverlapsWith        []string `json:"overlaps_with,omitempty"`
	ReviewStatus        string   `json:"review_status,omitempty"`
	NodeIDs             []string `json:"node_ids,omitempty"`
}

type Residual struct {
	ID              string   `json:"id"`
	Label           string   `json:"label"`
	TimeS           float64  `json:"time_s"`
	TemperatureC    float64  `json:"temperature_c"`
	CorrectedMassMg float64  `json:"corrected_mass_mg"`
	InitialMassMg   float64  `json:"initial_mass_mg"`
	Percent         float64  `json:"percent"`
	Unit            string   `json:"unit"`
	Status          string   `json:"status"`
	NodeIDs         []string `json:"node_ids,omitempty"`
	Diagnostics     []string `json:"diagnostics,omitempty"`
}

type Gap struct {
	StartS float64 `json:"start_s"`
	EndS   float64 `json:"end_s"`
	Reason string  `json:"reason"`
}

type Observation struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	TimeS    float64 `json:"time_s,omitempty"`
	ObjectID string  `json:"object_id,omitempty"`
	Message  string  `json:"message"`
	Status   string  `json:"status"`
}

type Analysis struct {
	ID                   string                 `json:"id"`
	Sequence             int64                  `json:"sequence"`
	AlgorithmVersion     string                 `json:"algorithm_version"`
	ParameterFingerprint string                 `json:"parameter_fingerprint"`
	Parameters           Parameters             `json:"parameters"`
	Sources              []SourceRef            `json:"sources"`
	Points               []GraphPoint           `json:"points"`
	Peaks                []Peak                 `json:"peaks"`
	Stages               []StageCandidate       `json:"stages"`
	Residuals            []Residual             `json:"residuals"`
	Gaps                 []Gap                  `json:"gaps"`
	Observations         []Observation          `json:"observations"`
	Nodes                []Node                 `json:"nodes"`
	Edges                []Edge                 `json:"edges"`
	Status               string                 `json:"status"`
	AffectedObjects      map[string][]SourceRef `json:"affected_objects,omitempty"`
	Diagnostics          []string               `json:"diagnostics,omitempty"`
}

type Event struct {
	ProjectID string          `json:"project_id"`
	Sequence  int64           `json:"sequence"`
	ID        string          `json:"id"`
	ParentID  string          `json:"parent_id,omitempty"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt string          `json:"created_at"`
}

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	ParentID    string `json:"parent_id,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type Decision struct {
	ID             string   `json:"id"`
	TargetID       string   `json:"target_id"`
	TargetKind     string   `json:"target_kind"`
	Action         string   `json:"action"`
	PreviousAction string   `json:"previous_action,omitempty"`
	Rationale      string   `json:"rationale"`
	AdjustedValue  *float64 `json:"adjusted_value,omitempty"`
	Unit           string   `json:"unit,omitempty"`
	ConflictsWith  []string `json:"conflicts_with,omitempty"`
	Supersedes     string   `json:"supersedes,omitempty"`
}

type ImportDiagnostic struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	Raw     any    `json:"raw,omitempty"`
}

type ImportAttempt struct {
	ID              string             `json:"id"`
	AcceptedKinds   []string           `json:"accepted_kinds"`
	RefusedKinds    []string           `json:"refused_kinds"`
	Status          string             `json:"status"`
	Diagnostics     []ImportDiagnostic `json:"diagnostics"`
	AffectedObjects []string           `json:"affected_objects"`
	Fragment        json.RawMessage    `json:"fragment"`
}
