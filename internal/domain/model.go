package domain

import "encoding/json"

const AlgorithmVersion = "tga-stage-evidence/2026-09-23"

type Event struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"project_id"`
	Sequence      int64           `json:"sequence"`
	ParentEventID string          `json:"parent_event_id"`
	Type          string          `json:"type"`
	OccurredAt    string          `json:"occurred_at"`
	Actor         string          `json:"actor"`
	Payload       json.RawMessage `json:"payload"`
}

type Source struct {
	ID              string          `json:"id"`
	Layer           string          `json:"layer"`
	Version         int             `json:"version"`
	ParentID        string          `json:"parent_id,omitempty"`
	CommitEventID   string          `json:"commit_event_id"`
	ContentType     string          `json:"content_type"`
	Raw             json.RawMessage `json:"raw_representation"`
	CoordinateBasis string          `json:"coordinate_basis,omitempty"`
	Diagnostics     []Diagnostic    `json:"diagnostics,omitempty"`
	OccurredAt      string          `json:"occurred_at"`
}

type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Message  string `json:"message"`
	Raw      any    `json:"raw,omitempty"`
}

type Quantity struct {
	ID     string   `json:"id"`
	Label  string   `json:"label,omitempty"`
	Value  *float64 `json:"value"`
	Raw    any      `json:"raw,omitempty"`
	Unit   string   `json:"unit,omitempty"`
	Basis  string   `json:"basis,omitempty"`
	Status string   `json:"status"`
}

type SourceRef struct {
	SourceID string `json:"source_id"`
	Layer    string `json:"layer"`
	Version  int    `json:"version"`
	Path     string `json:"path"`
}

type ValueNode struct {
	ID               string       `json:"id"`
	Key              string       `json:"key"`
	Kind             string       `json:"kind"`
	Label            string       `json:"label"`
	Value            any          `json:"value"`
	Unit             string       `json:"unit,omitempty"`
	Basis            string       `json:"basis,omitempty"`
	Status           string       `json:"status"`
	DirectSources    []SourceRef  `json:"direct_sources"`
	DependsOnNodes   []string     `json:"depends_on_nodes,omitempty"`
	Formula          string       `json:"formula,omitempty"`
	AlgorithmVersion string       `json:"algorithm_version"`
	ParameterHash    string       `json:"parameter_hash"`
	DerivationID     string       `json:"derivation_id"`
	Diagnostics      []Diagnostic `json:"diagnostics,omitempty"`
}

type Graph struct {
	DerivationID string            `json:"derivation_id"`
	Nodes        []ValueNode       `json:"nodes"`
	Bindings     map[string]string `json:"bindings"`
}

type Decision struct {
	ID            string   `json:"id"`
	EventID       string   `json:"event_id"`
	SubjectID     string   `json:"subject_id"`
	Verdict       string   `json:"verdict"`
	Rationale     string   `json:"rationale"`
	Actor         string   `json:"actor"`
	OccurredAt    string   `json:"occurred_at"`
	ConflictsWith string   `json:"conflicts_with,omitempty"`
	UndoneEventID string   `json:"undone_event_id,omitempty"`
	UndoneAt      string   `json:"undone_at,omitempty"`
	BasisEventIDs []string `json:"basis_event_ids"`
	ParameterHash string   `json:"parameter_hash"`
}

type ImportAttempt struct {
	EventID     string       `json:"event_id"`
	BatchID     string       `json:"batch_id"`
	Status      string       `json:"status"`
	OccurredAt  string       `json:"occurred_at"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	FragmentIDs []string     `json:"fragment_ids"`
}

type Project struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	Events         []Event               `json:"events"`
	Sources        map[string]*Source    `json:"sources"`
	CurrentSources map[string]*Source    `json:"current_sources"`
	Nodes          map[string]*ValueNode `json:"nodes"`
	CurrentNodes   map[string]string     `json:"current_nodes"`
	Decisions      map[string]*Decision  `json:"decisions"`
	DecisionLog    []Decision            `json:"decision_log"`
	ImportAttempts []ImportAttempt       `json:"import_attempts"`
	Graphs         []Graph               `json:"graphs"`
}

type RawPoint struct {
	Time        Quantity `json:"time"`
	Temperature Quantity `json:"temperature,omitempty"`
	Mass        Quantity `json:"mass"`
}

type RawSamplesContent struct {
	CoordinateBasis string     `json:"coordinate_basis"`
	Rows            []RawPoint `json:"rows"`
}

type BlankPoint struct {
	Time Quantity `json:"time"`
	Mass Quantity `json:"mass"`
}

type BlankCurveContent struct {
	CoordinateBasis string       `json:"coordinate_basis"`
	Rows            []BlankPoint `json:"rows"`
}

type DriftContent struct {
	Slope     Quantity `json:"slope"`
	Intercept Quantity `json:"intercept"`
	Model     string   `json:"model"`
}

type FurnaceSegment struct {
	Start     Quantity `json:"start"`
	End       Quantity `json:"end"`
	StartTemp Quantity `json:"start_temp"`
	EndTemp   Quantity `json:"end_temp"`
	Kind      string   `json:"kind"`
}

type FurnaceContent struct {
	CoordinateBasis string           `json:"coordinate_basis"`
	Segments        []FurnaceSegment `json:"segments"`
}

type GasEvent struct {
	ID               string   `json:"id"`
	ValveAt          Quantity `json:"valve_at"`
	FromAtmosphere   string   `json:"from_atmosphere"`
	ToAtmosphere     string   `json:"to_atmosphere"`
	AtmosphereDelay  Quantity `json:"atmosphere_delay"`
	TemperatureLag   Quantity `json:"temperature_lag"`
	WindowEndValveAt Quantity `json:"window_end_valve_at"`
}

type GasEventsContent struct {
	CoordinateBasis string     `json:"coordinate_basis"`
	Events          []GasEvent `json:"events"`
}

type InitialMassContent struct {
	Mass   Quantity `json:"mass"`
	Method string   `json:"method"`
}

type AnalysisParameters struct {
	CoordinateBasis    string   `json:"coordinate_basis"`
	SampleGapThreshold Quantity `json:"sample_gap_threshold"`
	PeakThreshold      Quantity `json:"peak_threshold"`
	OverlapStrategy    string   `json:"overlap_strategy"`
	BlankGapThreshold  Quantity `json:"blank_gap_threshold"`
	NoiseThreshold     Quantity `json:"noise_threshold"`
}

const (
	LayerRawSamples         = "raw_samples"
	LayerDriftCorrection    = "balance_drift_correction"
	LayerFurnaceProgram     = "furnace_program"
	LayerGasValveEvents     = "gas_valve_events"
	LayerBlankCurve         = "blank_curve"
	LayerInitialMass        = "sample_initial_mass"
	LayerAnalysisParameters = "analysis_parameters"
)

func NewProject(id, name string) *Project {
	return &Project{
		ID:             id,
		Name:           name,
		Sources:        map[string]*Source{},
		CurrentSources: map[string]*Source{},
		Nodes:          map[string]*ValueNode{},
		CurrentNodes:   map[string]string{},
		Decisions:      map[string]*Decision{},
	}
}

func Layers() []string {
	return []string{
		LayerRawSamples,
		LayerDriftCorrection,
		LayerFurnaceProgram,
		LayerGasValveEvents,
		LayerBlankCurve,
		LayerInitialMass,
		LayerAnalysisParameters,
	}
}
