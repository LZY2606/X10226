package domain

import "encoding/json"

const (
	AlgorithmVersion = "tga-stage-decomp-v1"

	StatusUnknown  = "unknown"
	StatusAccepted = "accepted"
	StatusRejected = "rejected"
	StatusAdjusted = "adjusted"

	DependencyCurrent = "current"
	DependencyStale   = "stale"

	ConflictUnresolved = "unresolved"
	ConflictSuperseded = "superseded"
	ConflictCoexist    = "coexist"
)

type Clock interface {
	Now() string
}

type Event struct {
	Seq          int64           `json:"seq"`
	EventID      string          `json:"event_id"`
	ParentSeq    int64           `json:"parent_seq"`
	Kind         string          `json:"kind"`
	OccurredAt   string          `json:"occurred_at"`
	Actor        string          `json:"actor"`
	Payload      json.RawMessage `json:"payload"`
	SupersededBy int64           `json:"superseded_by,omitempty"`
	Undone       bool            `json:"undone,omitempty"`
}

type LayerRef struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int    `json:"version"`
}

type SourceRecord struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Version   int    `json:"version"`
	ParentID  string `json:"parent_id,omitempty"`
	EventSeq  int64  `json:"event_seq"`
	ImportID  string `json:"import_id"`
	ProjectID string `json:"project_id"`
	Logical   string `json:"logical"`
	Label     string `json:"label"`
	Unit      string `json:"unit,omitempty"`
	Ref       string `json:"ref,omitempty"`
	Raw       string `json:"raw"`
	Value     any    `json:"value,omitempty"`
	Valid     bool   `json:"valid"`
	Diagnostic string `json:"diagnostic,omitempty"`
	Retracted bool   `json:"retracted,omitempty"`
	Replaces  string `json:"replaces,omitempty"`
}

type ImportRecord struct {
	ID          string `json:"id"`
	EventSeq    int64  `json:"event_seq"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Accepted    int    `json:"accepted_count"`
	Rejected    int    `json:"rejected_count"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type Diagnostic struct {
	Path     string `json:"path"`
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Raw      string `json:"raw,omitempty"`
}

type Parameters struct {
	GapThresholdS          float64 `json:"gap_threshold_s"`
	AtmosphereDelayS       float64 `json:"atmosphere_delay_s"`
	TemperatureLagC        float64 `json:"temperature_lag_c"`
	PeakProminencePctPerS  float64 `json:"peak_prominence_pct_per_s"`
	NoiseWindowPts         int     `json:"noise_window_pts"`
	BlankToleranceMg       float64 `json:"blank_tolerance_mg"`
}

func DefaultParameters() Parameters {
	return Parameters{
		GapThresholdS:         35,
		AtmosphereDelayS:      0,
		TemperatureLagC:       0,
		PeakProminencePctPerS: 0.015,
		NoiseWindowPts:        5,
		BlankToleranceMg:      0.05,
	}
}

type Run struct {
	ID             string          `json:"id"`
	Version        int             `json:"version"`
	EventSeq       int64           `json:"event_seq"`
	ProjectID      string          `json:"project_id"`
	Algorithm      string          `json:"algorithm"`
	Parameters     Parameters      `json:"parameters"`
	ParameterHash  string          `json:"parameter_hash"`
	Inputs         []LayerRef      `json:"inputs"`
	InputFingerprint string        `json:"input_fingerprint"`
	Current        bool            `json:"current"`
	Payload        json.RawMessage `json:"payload"`
}

type Point struct {
	T              float64 `json:"t_s"`
	FurnaceC       float64 `json:"furnace_c"`
	SampleC        float64 `json:"sample_c"`
	MassMg         float64 `json:"mass_mg"`
	CorrectedMg    float64 `json:"corrected_mg"`
	DTGPctPerS     float64 `json:"dtg_pct_per_s"`
	GapBefore      bool    `json:"gap_before"`
	NoInterp       bool    `json:"no_interp"`
	NonMonotonic   bool    `json:"non_monotonic"`
	DirectSourceID string  `json:"direct_source_id"`
	BlankSourceID  string  `json:"blank_source_id,omitempty"`
	RawRepresentations []RawRef `json:"raw_representations"`
}

type RawRef struct {
	SourceID string `json:"source_id"`
	Kind     string `json:"kind"`
	Raw      string `json:"raw"`
	Unit     string `json:"unit"`
}

type FurnaceSegment struct {
	StartS     float64 `json:"start_s"`
	EndS       float64 `json:"end_s"`
	StartC     float64 `json:"start_c"`
	EndC       float64 `json:"end_c"`
	RateCPerS  float64 `json:"rate_c_per_s"`
	SourceID   string  `json:"source_id"`
}

type GasEvent struct {
	ValveS     float64 `json:"valve_s"`
	ReactionS  float64 `json:"reaction_s"`
	From       string  `json:"from_gas"`
	To         string  `json:"to_gas"`
	SourceID   string  `json:"source_id"`
	Note       string  `json:"note,omitempty"`
}

type Gap struct {
	StartS float64 `json:"start_s"`
	EndS   float64 `json:"end_s"`
	Reason string  `json:"reason"`
}

type StageCandidate struct {
	ID             string   `json:"id"`
	Key            string   `json:"key"`
	Index          int      `json:"index"`
	StartS         float64  `json:"start_s"`
	EndS           float64  `json:"end_s"`
	StartC         float64  `json:"start_c"`
	EndC           float64  `json:"end_c"`
	PeakS          *float64 `json:"peak_s,omitempty"`
	PeakC          *float64 `json:"peak_c,omitempty"`
	MassLossMg     float64  `json:"mass_loss_mg"`
	MassLossPct    float64  `json:"mass_loss_pct"`
	Atmosphere     string   `json:"atmosphere"`
	Status         string   `json:"status"`
	Ambiguous      bool     `json:"ambiguous"`
	RejectedReason string   `json:"rejected_reason,omitempty"`
	DirectSources  []string `json:"direct_sources"`
	Overlaps       []string `json:"overlaps"`
}

type Residue struct {
	ID            string   `json:"id"`
	AtS           float64  `json:"at_s"`
	TemperatureC  float64  `json:"temperature_c"`
	MassMg        float64  `json:"mass_mg"`
	InitialMassMg float64  `json:"initial_mass_mg"`
	Percent       float64  `json:"residue_pct"`
	Status        string   `json:"status"`
	Unknown       bool     `json:"unknown"`
	Chain         []ChainStep `json:"chain"`
	DirectSources []string `json:"direct_sources"`
}

type ChainStep struct {
	Step       string `json:"step"`
	Value      string `json:"value"`
	Unit       string `json:"unit,omitempty"`
	SourceID   string `json:"source_id,omitempty"`
	Basis      string `json:"basis,omitempty"`
	Formula    string `json:"formula,omitempty"`
	Status     string `json:"status,omitempty"`
}

type Finding struct {
	Code    string   `json:"code"`
	Message string   `json:"message"`
	Severity string  `json:"severity"`
	ObjectIDs []string `json:"object_ids,omitempty"`
}

type RunResult struct {
	ProjectID       string           `json:"project_id"`
	InitialMassMg   float64          `json:"initial_mass_mg"`
	InitialMassRaw  string           `json:"initial_mass_raw,omitempty"`
	InitialMassSourceID string       `json:"initial_mass_source_id,omitempty"`
	Points          []Point          `json:"points"`
	FurnaceSegments []FurnaceSegment `json:"furnace_segments"`
	GasEvents       []GasEvent        `json:"gas_events"`
	Gaps            []Gap             `json:"gaps"`
	Stages          []StageCandidate  `json:"stages"`
	Residues        []Residue         `json:"residues"`
	Findings        []Finding         `json:"findings"`
	Diagnostics     []Diagnostic      `json:"diagnostics"`
}

type Decision struct {
	ID            string `json:"id"`
	EventSeq      int64  `json:"event_seq"`
	ProjectID     string `json:"project_id"`
	RunID         string `json:"run_id"`
	TargetKind    string `json:"target_kind"`
	TargetID      string `json:"target_id"`
	Action        string `json:"action"`
	Reason        string `json:"reason"`
	Actor         string `json:"actor"`
	ConflictState string `json:"conflict_state"`
	ConflictsWith []string `json:"conflicts_with,omitempty"`
	Parameters    *Parameters `json:"parameters,omitempty"`
	Adjustment    *StageCandidate `json:"adjustment,omitempty"`
	Undone        bool `json:"undone"`
	UndoneBy      string `json:"undone_by,omitempty"`
}
