package domain

import "encoding/json"

const (
	EventProjectCreated      = "project.created"
	EventImportBatch         = "import.batch"
	EventParameterRevision   = "parameter.revision"
	EventAnalysisRun         = "analysis.run"
	EventDecision            = "human.decision"
	EventPrincipleAsserted   = "principle.asserted"
	EventCounterexampleSaved = "counterexample.saved"
	EventNote                = "review.note"
)

type Event struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"project_id"`
	Seq           int64           `json:"seq"`
	Type          string          `json:"type"`
	OccurredAt    string          `json:"occurred_at"`
	Actor         string          `json:"actor"`
	ParentEventID string          `json:"parent_event_id,omitempty"`
	DirectSources []string        `json:"direct_sources"`
	Payload       json.RawMessage `json:"payload"`
}

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CasePackID  string `json:"case_pack_id,omitempty"`
	CreatedAt   string `json:"created_at"`
	Description string `json:"description,omitempty"`
}

type ValueRef struct {
	ID              string   `json:"id"`
	Value           *float64 `json:"value,omitempty"`
	Unit            string   `json:"unit,omitempty"`
	Original        string   `json:"original,omitempty"`
	CoordinateBasis string   `json:"coordinate_basis,omitempty"`
	Status          string   `json:"status"`
	Diagnostics     []string `json:"diagnostics,omitempty"`
}

type SamplePoint struct {
	ID            string   `json:"id"`
	BatchID       string   `json:"batch_id"`
	Index         int      `json:"index"`
	Time          ValueRef `json:"time"`
	FurnaceTemp   ValueRef `json:"furnace_temp"`
	SampleTemp    ValueRef `json:"sample_temp"`
	Mass          ValueRef `json:"mass"`
	BlankMass     ValueRef `json:"blank_mass"`
	CorrectedMass ValueRef `json:"corrected_mass"`
	Role          string   `json:"role"`
	Diagnostics   []string `json:"diagnostics,omitempty"`
}

type InitialMass struct {
	ID          string   `json:"id"`
	BatchID     string   `json:"batch_id"`
	Mass        ValueRef `json:"mass"`
	Basis       string   `json:"basis"`
	RevisesID   string   `json:"revises_id,omitempty"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

type FurnaceSegment struct {
	ID          string   `json:"id"`
	BatchID     string   `json:"batch_id"`
	StartTime   ValueRef `json:"start_time"`
	EndTime     ValueRef `json:"end_time"`
	TargetTemp  ValueRef `json:"target_temp"`
	HeatingRate ValueRef `json:"heating_rate"`
}

type GasValveEvent struct {
	ID          string   `json:"id"`
	BatchID     string   `json:"batch_id"`
	ValveTime   ValueRef `json:"valve_time"`
	Gas         string   `json:"gas"`
	Action      string   `json:"action"`
	EffectiveAt ValueRef `json:"effective_at"`
	Note        string   `json:"note,omitempty"`
}

type BlankPoint struct {
	ID      string   `json:"id"`
	BatchID string   `json:"batch_id"`
	Index   int      `json:"index"`
	Time    ValueRef `json:"time"`
	Mass    ValueRef `json:"mass"`
}

type Diagnostic struct {
	Code     string   `json:"code"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Subject  string   `json:"subject,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

type ImportBatch struct {
	ID             string            `json:"id"`
	Kind           string            `json:"kind"`
	Layer          string            `json:"layer"`
	Origin         string            `json:"origin"`
	Status         string            `json:"status"`
	ParentBatchID  string            `json:"parent_batch_id,omitempty"`
	SamplePoints   []SamplePoint     `json:"sample_points,omitempty"`
	InitialMasses  []InitialMass     `json:"initial_masses,omitempty"`
	FurnaceProgram []FurnaceSegment  `json:"furnace_program,omitempty"`
	GasValveEvents []GasValveEvent   `json:"gas_valve_events,omitempty"`
	BlankPoints    []BlankPoint      `json:"blank_points,omitempty"`
	Diagnostics    []Diagnostic      `json:"diagnostics,omitempty"`
	RawFragments   []json.RawMessage `json:"raw_fragments,omitempty"`
	DirectSources  []string          `json:"direct_sources,omitempty"`
}

type Parameters struct {
	ID              string   `json:"id"`
	AlgorithmVersion string  `json:"algorithm_version"`
	ParentID        string   `json:"parent_id,omitempty"`
	GasDelay        ValueRef `json:"gas_delay"`
	TemperatureLag  ValueRef `json:"temperature_lag"`
	NoiseThreshold  ValueRef `json:"noise_threshold"`
	MinProminence   ValueRef `json:"min_prominence"`
	Fingerprint     string   `json:"fingerprint"`
	DirectSources   []string `json:"direct_sources"`
}

type Interval struct {
	ID       string    `json:"id"`
	Start    ValueRef  `json:"start"`
	End      ValueRef  `json:"end"`
	Boundary []string  `json:"boundary,omitempty"`
	Status   string    `json:"status"`
}

type Candidate struct {
	ID             string   `json:"id"`
	Key            string   `json:"key"`
	Label          string   `json:"label"`
	PeakTime       ValueRef `json:"peak_time"`
	PeakSampleTemp ValueRef `json:"peak_sample_temp"`
	ReactionWindow Interval `json:"reaction_window"`
	ValveWindow    Interval `json:"valve_window"`
	MassLoss       ValueRef `json:"mass_loss"`
	MassLossPct    ValueRef `json:"mass_loss_pct"`
	Status         string   `json:"status"`
	Rejection      []string `json:"rejection_reasons,omitempty"`
	DirectSources  []string `json:"direct_sources"`
	Overlaps       []string `json:"overlaps,omitempty"`
}

type ChainStep struct {
	ID       string   `json:"id"`
	Order    int      `json:"order"`
	Label    string   `json:"label"`
	Value    ValueRef `json:"value"`
	Formula  string   `json:"formula,omitempty"`
	SourceID string   `json:"source_id,omitempty"`
}

type Residue struct {
	ID            string      `json:"id"`
	Label         string      `json:"label"`
	AtTime        ValueRef    `json:"at_time"`
	Mass          ValueRef    `json:"mass"`
	BasisMass     ValueRef    `json:"basis_mass"`
	Percent       ValueRef    `json:"percent"`
	Status        string      `json:"status"`
	Uncertainty   ValueRef    `json:"uncertainty"`
	Chain         []ChainStep `json:"chain"`
	DirectSources []string    `json:"direct_sources"`
}

type DataGap struct {
	ID     string   `json:"id"`
	Start  ValueRef `json:"start"`
	End    ValueRef `json:"end"`
	Reason string   `json:"reason"`
}

type AnalysisRun struct {
	ID            string        `json:"id"`
	ParametersID  string        `json:"parameters_id"`
	Status        string        `json:"status"`
	Points        []SamplePoint `json:"points"`
	Candidates    []Candidate   `json:"candidates"`
	Residues      []Residue     `json:"residues"`
	Gaps          []DataGap     `json:"gaps"`
	Diagnostics   []Diagnostic  `json:"diagnostics,omitempty"`
	DirectSources []string      `json:"direct_sources"`
	Fingerprint   string        `json:"fingerprint"`
}

type Decision struct {
	ID                 string   `json:"id"`
	ParentDecisionID   string   `json:"parent_decision_id,omitempty"`
	TargetType         string   `json:"target_type"`
	TargetID           string   `json:"target_id"`
	RunID              string   `json:"run_id"`
	Action             string   `json:"action"`
	Rationale          string   `json:"rationale"`
	AdjustedParameters []string `json:"adjusted_parameters,omitempty"`
	ConflictWith       []string `json:"conflict_with,omitempty"`
	DirectSources      []string `json:"direct_sources"`
}

type Principle struct {
	Code        string   `json:"code"`
	Statement   string   `json:"statement"`
	Basis       string   `json:"basis"`
	AppliesTo   []string `json:"applies_to"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type Counterexample struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	ChangedDirectSource string `json:"changed_direct_source"`
	ExpectedScope     []string `json:"expected_affected_scope"`
	ProtectedScope    []string `json:"protected_scope"`
	AcceptedResults   []string `json:"accepted_results"`
	RejectedResults   []string `json:"rejected_results"`
	Status            string   `json:"status"`
	Findings          []string `json:"findings"`
	ProjectID         string   `json:"project_id,omitempty"`
}
