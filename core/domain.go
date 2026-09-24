package core

import "encoding/json"

const (
	KindSamples     = "samples"
	KindDrift       = "drift"
	KindFurnace     = "furnace"
	KindValves      = "valves"
	KindBlank       = "blank"
	KindInitialMass = "initial_mass"
	KindParameters  = "parameters"
)

type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Subject  string `json:"subject,omitempty"`
}

type Quantity struct {
	Value        float64 `json:"value,omitempty"`
	Unit         string  `json:"unit"`
	Original     string  `json:"original"`
	OK           bool    `json:"ok"`
	Coordinate   string  `json:"coordinate,omitempty"`
	DiagnosticID string  `json:"diagnostic_id,omitempty"`
}

type SamplePoint struct {
	SourceID    string   `json:"source_id"`
	Seq         int      `json:"seq"`
	Time        Quantity `json:"time"`
	Temperature Quantity `json:"temperature"`
	Mass        Quantity `json:"mass"`
	RawRow      string   `json:"raw_row,omitempty"`
}

type DriftCorrection struct {
	SourceID     string       `json:"source_id"`
	ParentID     string       `json:"parent_id,omitempty"`
	CorrectionID string       `json:"correction_id"`
	Model        string       `json:"model"`
	Start        Quantity     `json:"start"`
	End          Quantity     `json:"end"`
	Rate         Quantity     `json:"rate"`
	BlankCurveID string       `json:"blank_curve_id,omitempty"`
	Note         string       `json:"note,omitempty"`
	Algorithm    string       `json:"algorithm"`
	Fingerprint  string       `json:"fingerprint"`
	Diagnostics  []Diagnostic `json:"diagnostics,omitempty"`
}

type FurnacePoint struct {
	SourceID string   `json:"source_id"`
	Seq      int      `json:"seq"`
	Time     Quantity `json:"time"`
	Setpoint Quantity `json:"setpoint"`
	Measured Quantity `json:"measured"`
}

type GasValveEvent struct {
	SourceID      string   `json:"source_id"`
	EventID       string   `json:"event_id"`
	Time          Quantity `json:"time"`
	FromGas       string   `json:"from_gas"`
	ToGas         string   `json:"to_gas"`
	Delay         Quantity `json:"delay"`
	ThermalLag    Quantity `json:"thermal_lag"`
	ReactionStart Quantity `json:"reaction_start,omitempty"`
	Note          string   `json:"note,omitempty"`
}

type BlankPoint struct {
	SourceID     string   `json:"source_id"`
	BlankCurveID string   `json:"blank_curve_id"`
	Seq          int      `json:"seq"`
	Time         Quantity `json:"time"`
	BaselineMass Quantity `json:"baseline_mass"`
}

type InitialMass struct {
	SourceID   string   `json:"source_id"`
	ParentID   string   `json:"parent_id,omitempty"`
	RevisionID string   `json:"revision_id"`
	Mass       Quantity `json:"mass"`
	Basis      string   `json:"basis"`
	Note       string   `json:"note,omitempty"`
}

type Parameters struct {
	SourceID               string  `json:"source_id"`
	ParentID               string  `json:"parent_id,omitempty"`
	AlgorithmVersion       string  `json:"algorithm_version"`
	SmoothingWindowSeconds float64 `json:"smoothing_window_s"`
	DTGThresholdMgPerMin   float64 `json:"dtg_threshold_mg_per_min"`
	OverlapShoulderRatio   float64 `json:"overlap_shoulder_ratio"`
	MinimumStageSeconds    float64 `json:"minimum_stage_s"`
	Fingerprint            string  `json:"fingerprint"`
}

type SourceBundle struct {
	ProjectID string          `json:"project_id"`
	BatchID   string          `json:"batch_id"`
	ParentID  string          `json:"parent_id,omitempty"`
	Label     string          `json:"label"`
	Actor     string          `json:"actor"`
	Sections  []SourceSection `json:"sections"`
}

type SourceSection struct {
	Kind      string `json:"kind"`
	MediaType string `json:"media_type"`
	Filename  string `json:"filename"`
	Content   string `json:"content"`
}

type RawImport struct {
	BatchID       string          `json:"batch_id"`
	ParentID      string          `json:"parent_id,omitempty"`
	Label         string          `json:"label"`
	Actor         string          `json:"actor"`
	RawSHA256     string          `json:"raw_sha256"`
	AcceptedKinds []string        `json:"accepted_kinds"`
	RejectedKinds []string        `json:"rejected_kinds,omitempty"`
	Diagnostics   []Diagnostic    `json:"diagnostics"`
	Sections      []SourceSection `json:"sections"`
	Partial       bool            `json:"partial"`
}

type SourceRecord struct {
	BatchID     string          `json:"batch_id"`
	Kind        string          `json:"kind"`
	RecordID    string          `json:"record_id"`
	ParentID    string          `json:"parent_id,omitempty"`
	Data        json.RawMessage `json:"data"`
	Diagnostics []Diagnostic    `json:"diagnostics,omitempty"`
}

type Provenance struct {
	AlgorithmVersion string   `json:"algorithm_version"`
	ParameterSource  string   `json:"parameter_source"`
	ParameterHash    string   `json:"parameter_hash"`
	DirectSources    []string `json:"direct_sources"`
	InputVersion     int      `json:"input_version"`
}

type CalculationStep struct {
	ID     string `json:"id"`
	Op     string `json:"op"`
	Detail string `json:"detail"`
	Value  string `json:"value,omitempty"`
	Unit   string `json:"unit,omitempty"`
}

type StageCandidate struct {
	ID             string            `json:"id"`
	RevisionID     string            `json:"revision_id"`
	Kind           string            `json:"kind"`
	Status         string            `json:"status"`
	Title          string            `json:"title"`
	Start          Quantity          `json:"start"`
	End            Quantity          `json:"end"`
	Peak           Quantity          `json:"peak"`
	MassLoss       Quantity          `json:"mass_loss"`
	MassLossPct    Quantity          `json:"mass_loss_pct"`
	ResiduePct     Quantity          `json:"residue_pct"`
	ResidueMass    Quantity          `json:"residue_mass"`
	Overlaps       []string          `json:"overlaps,omitempty"`
	RejectionCodes []string          `json:"rejection_codes,omitempty"`
	Diagnostics    []Diagnostic      `json:"diagnostics,omitempty"`
	Provenance     Provenance        `json:"provenance"`
	Chain          []CalculationStep `json:"chain"`
	Current        bool              `json:"current"`
}

type Decision struct {
	ID           string             `json:"id"`
	CandidateID  string             `json:"candidate_id"`
	RevisionID   string             `json:"revision_id"`
	Action       string             `json:"action"`
	Actor        string             `json:"actor"`
	Reason       string             `json:"reason"`
	Parameters   map[string]float64 `json:"parameters,omitempty"`
	Supersedes   string             `json:"supersedes,omitempty"`
	Undone       bool               `json:"undone"`
	UndoneReason string             `json:"undone_reason,omitempty"`
	ConflictWith []string           `json:"conflict_with,omitempty"`
}

type Event struct {
	Seq        int             `json:"seq"`
	ID         string          `json:"id"`
	ProjectID  string          `json:"project_id"`
	Type       string          `json:"type"`
	Actor      string          `json:"actor"`
	OccurredAt string          `json:"occurred_at"`
	Payload    json.RawMessage `json:"payload"`
	PrevHash   string          `json:"prev_hash"`
	Hash       string          `json:"hash"`
}

type PlotPoint struct {
	Seq         int     `json:"seq"`
	TimeS       float64 `json:"time_s"`
	TempC       float64 `json:"temp_c"`
	RawMassMg   float64 `json:"raw_mass_mg"`
	CorrMassMg  float64 `json:"corr_mass_mg"`
	DTGMgPerMin float64 `json:"dtg_mg_per_min,omitempty"`
	Segment     int     `json:"segment"`
	GapBefore   bool    `json:"gap_before"`
	NonMono     bool    `json:"non_mono"`
	Unplottable bool    `json:"unplottable,omitempty"`
}

type ValveView struct {
	ID            string  `json:"id"`
	TimeS         float64 `json:"time_s"`
	ReactionTimeS float64 `json:"reaction_time_s"`
	FromGas       string  `json:"from_gas"`
	ToGas         string  `json:"to_gas"`
	DelayS        float64 `json:"delay_s"`
	LagS          float64 `json:"lag_s"`
}

type State struct {
	ProjectID     string            `json:"project_id"`
	Version       int               `json:"version"`
	Samples       []SamplePoint     `json:"samples"`
	Drifts        []DriftCorrection `json:"drifts"`
	Furnace       []FurnacePoint    `json:"furnace"`
	Valves        []GasValveEvent   `json:"valves"`
	Blank         []BlankPoint      `json:"blank"`
	InitialMasses []InitialMass     `json:"initial_masses"`
	Parameters    Parameters        `json:"parameters"`
	Imports       []RawImport       `json:"imports"`
	Sources       []SourceRecord    `json:"sources"`
	Diagnostics   []Diagnostic      `json:"diagnostics"`
	Plot          []PlotPoint       `json:"plot"`
	ValveViews    []ValveView       `json:"valve_views"`
	Candidates    []StageCandidate  `json:"candidates"`
	Decisions     []Decision        `json:"decisions"`
	Events        []Event           `json:"events"`
	Current       bool              `json:"current"`
}
