package domain

import "time"

const AlgorithmVersion = "tga-stage-decomposition/v1.0.0"

type Value struct {
	Raw         string  `json:"raw"`
	Value       float64 `json:"value,omitempty"`
	Unit        string  `json:"unit,omitempty"`
	Basis       string  `json:"basis,omitempty"`
	Convertible bool    `json:"convertible"`
	Diagnostic  string  `json:"diagnostic,omitempty"`
}

type MeasurementSample struct {
	ID          string   `json:"id"`
	Time        Value    `json:"time"`
	Temperature Value    `json:"temperature"`
	Mass        Value    `json:"mass"`
	SegmentID   string   `json:"segment_id,omitempty"`
	GapAfterMin *float64 `json:"gap_after_min,omitempty"`
}

type DriftCorrection struct {
	ID            string `json:"id"`
	Start         Value  `json:"start"`
	ReferenceTime Value  `json:"reference_time"`
	Slope         Value  `json:"slope"`
	BaselineMass  Value  `json:"baseline_mass"`
	Method        string `json:"method"`
}

type FurnaceSegment struct {
	ID        string `json:"id"`
	Start     Value  `json:"start"`
	End       Value  `json:"end"`
	Setpoint  Value  `json:"setpoint"`
	Rate      Value  `json:"rate"`
	ProgramID string `json:"program_id"`
}

type GasValveEvent struct {
	ID             string `json:"id"`
	ValveTime      Value  `json:"valve_time"`
	FromGas        string `json:"from_gas"`
	ToGas          string `json:"to_gas"`
	ObservedDelay  Value  `json:"observed_delay"`
	TemperatureLag Value  `json:"temperature_lag"`
	ReactionTime   Value  `json:"reaction_time"`
}

type BlankPoint struct {
	ID   string `json:"id"`
	Time Value  `json:"time"`
	Mass Value  `json:"mass"`
}

type InitialMass struct {
	ID       string `json:"id"`
	Mass     Value  `json:"mass"`
	Method   string `json:"method"`
	ParentID string `json:"parent_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type ParameterSet struct {
	ID          string           `json:"id"`
	ParentID    string           `json:"parent_id,omitempty"`
	Reason      string           `json:"reason,omitempty"`
	Values      map[string]Value `json:"values"`
	Fingerprint string           `json:"fingerprint"`
}

type SourceRef struct {
	Kind           string `json:"kind"`
	ID             string `json:"id"`
	VersionEventID string `json:"version_event_id"`
	Role           string `json:"role,omitempty"`
}

type CalculationStep struct {
	ID      string      `json:"id"`
	Label   string      `json:"label"`
	Formula string      `json:"formula"`
	Inputs  []SourceRef `json:"inputs"`
	Output  Value       `json:"output"`
	Note    string      `json:"note,omitempty"`
}

type RuleEvaluation struct {
	RuleID   string   `json:"rule_id"`
	Status   string   `json:"status"`
	Message  string   `json:"message"`
	Affected []string `json:"affected"`
}

type StageCandidate struct {
	ID            string            `json:"id"`
	Label         string            `json:"label"`
	Start         Value             `json:"start"`
	End           Value             `json:"end"`
	PeakTimes     []Value           `json:"peak_times"`
	StartMass     Value             `json:"start_mass"`
	EndMass       Value             `json:"end_mass"`
	MassLoss      Value             `json:"mass_loss"`
	LossOfStart   Value             `json:"loss_of_start"`
	LossOfInitial Value             `json:"loss_of_initial"`
	Overlaps      bool              `json:"overlaps"`
	NonAdditive   bool              `json:"non_additive"`
	Validity      string            `json:"validity"`
	RejectReason  string            `json:"reject_reason,omitempty"`
	UnknownReason string            `json:"unknown_reason,omitempty"`
	Atmosphere    *AtmosphereLink   `json:"atmosphere,omitempty"`
	SourceRefs    []SourceRef       `json:"source_refs"`
	Steps         []CalculationStep `json:"steps"`
	Rules         []RuleEvaluation  `json:"rules"`
}

type AtmosphereLink struct {
	ValveEventID      string `json:"valve_event_id"`
	ValveTime         Value  `json:"valve_time"`
	ReactionTime      Value  `json:"reaction_time"`
	WindowStart       Value  `json:"window_start"`
	WindowEnd         Value  `json:"window_end"`
	AttributionStatus string `json:"attribution_status"`
	Reason            string `json:"reason"`
}

type ResidueResult struct {
	ID             string            `json:"id"`
	FinalTime      Value             `json:"final_time"`
	FinalMass      Value             `json:"final_mass"`
	InitialMass    Value             `json:"initial_mass"`
	ResiduePercent Value             `json:"residue_percent"`
	Validity       string            `json:"validity"`
	UnknownReason  string            `json:"unknown_reason,omitempty"`
	SourceRefs     []SourceRef       `json:"source_refs"`
	Steps          []CalculationStep `json:"steps"`
	Rules          []RuleEvaluation  `json:"rules"`
}

type DTGPoint struct {
	ID           string      `json:"id"`
	Time         Value       `json:"time"`
	Temperature  Value       `json:"temperature"`
	Mass         Value       `json:"mass"`
	Rate         Value       `json:"rate"`
	SegmentID    string      `json:"segment_id"`
	Interpolated bool        `json:"interpolated"`
	SourceRefs   []SourceRef `json:"source_refs"`
}

type AnalysisResult struct {
	ID                   string           `json:"id"`
	AlgorithmVersion     string           `json:"fingerprint"`
	ParameterFingerprint string           `json:"parameter_fingerprint"`
	DirectSources        []SourceRef      `json:"direct_sources"`
	Status               string           `json:"status"`
	DTG                  []DTGPoint       `json:"dtg"`
	Stages               []StageCandidate `json:"stages"`
	Rejected             []StageCandidate `json:"rejected"`
	Unknown              []StageCandidate `json:"unknown"`
	Residues             []ResidueResult  `json:"residues"`
	Diagnostics          []string         `json:"diagnostics"`
	Rules                []RuleEvaluation `json:"rules"`
	CreatedEventID       string           `json:"created_event_id,omitempty"`
	CreatedSeq           int64            `json:"created_seq"`
	StaleSeq             int64            `json:"stale_seq,omitempty"`
}

type Decision struct {
	ID                 string           `json:"id"`
	SubjectKind        string           `json:"subject_kind"`
	SubjectID          string           `json:"subject_id"`
	Action             string           `json:"action"`
	Rationale          string           `json:"rationale"`
	ParameterOverrides map[string]Value `json:"parameter_overrides,omitempty"`
	EventID            string           `json:"event_id"`
	Seq                int64            `json:"seq"`
	Reverted           bool             `json:"reverted"`
	RevertEventID      string           `json:"revert_event_id,omitempty"`
	ParentDecisionID   string           `json:"parent_decision_id,omitempty"`
}

type Conflict struct {
	ID          string   `json:"id"`
	SubjectKind string   `json:"subject_kind"`
	SubjectID   string   `json:"subject_id"`
	DecisionIDs []string `json:"decision_ids"`
	Status      string   `json:"status"`
	ResolvedBy  string   `json:"resolved_by,omitempty"`
	Resolution  string   `json:"resolution,omitempty"`
	Seq         int64    `json:"seq"`
}

type Counterexample struct {
	ID                  string      `json:"id"`
	Name                string      `json:"name"`
	DirectSourceChanged string      `json:"direct_source_changed"`
	ExpectedScope       []string    `json:"expected_scope"`
	ExpectedNotAffected []string    `json:"expected_not_affected"`
	MachineRule         string      `json:"machine_rule"`
	Status              string      `json:"status"`
	Evidence            []SourceRef `json:"evidence"`
}

type Event struct {
	Seq        int64     `json:"seq"`
	EventID    string    `json:"event_id"`
	ProjectID  string    `json:"project_id"`
	Type       string    `json:"type"`
	Actor      string    `json:"actor"`
	OccurredAt time.Time `json:"occurred_at"`
	Payload    []byte    `json:"payload"`
}

type ProjectSnapshot struct {
	ProjectID       string              `json:"project_id"`
	Name            string              `json:"name"`
	AsOfSeq         int64               `json:"as_of_seq"`
	CurrentSeq      int64               `json:"current_seq"`
	Measurements    []MeasurementSample `json:"measurements"`
	Drift           []DriftCorrection   `json:"drift"`
	Furnace         []FurnaceSegment    `json:"furnace"`
	Valves          []GasValveEvent     `json:"valves"`
	Blank           []BlankPoint        `json:"blank"`
	InitialMasses   []InitialMass       `json:"initial_masses"`
	Parameters      []ParameterSet      `json:"parameters"`
	CurrentAnalysis *AnalysisResult     `json:"current_analysis"`
	StaleAnalyses   []AnalysisResult    `json:"stale_analyses"`
	Decisions       []Decision          `json:"decisions"`
	Conflicts       []Conflict          `json:"conflicts"`
	Counterexamples []Counterexample    `json:"counterexamples"`
	ImportBatches   []ImportBatch       `json:"import_batches"`
	Events          []Event             `json:"events"`
}

type ImportBatch struct {
	ID            string         `json:"id"`
	ParentBatchID string         `json:"parent_batch_id,omitempty"`
	Status        string         `json:"status"`
	FragmentName  string         `json:"fragment_name"`
	AcceptedKinds []string       `json:"accepted_kinds"`
	Diagnostics   []string       `json:"diagnostics"`
	RawManifest   map[string]any `json:"raw_manifest"`
	Seq           int64          `json:"seq"`
}
