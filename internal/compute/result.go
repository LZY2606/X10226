package compute

// Provenance is attached to every derived output.
type Provenance struct {
	AlgorithmVersion string   `json:"algorithm_version"`
	ParamFingerprint string   `json:"param_fingerprint"`
	DirectSources    []string `json:"direct_sources"`
	InputHash        string   `json:"input_hash"`
}

// ChainStep is one arithmetic or reasoning step in a calculation chain.
type ChainStep struct {
	Order   int     `json:"order"`
	Op      string  `json:"op"`
	Detail  string  `json:"detail"`
	Value   float64 `json:"value,omitempty"`
	Unit    string  `json:"unit,omitempty"`
	Ref     string  `json:"ref,omitempty"`
	Unknown bool    `json:"unknown,omitempty"`
}

// Point is a normalised trace point inside one segment.
type Point struct {
	T            float64 `json:"t_min"`
	Tk           float64 `json:"T_K"`
	MassRaw      float64 `json:"mass_raw_mg"`
	MassCorrect  float64 `json:"mass_corrected_mg"`
	TG           float64 `json:"tg_pct"`
	DTG          float64 `json:"dtg_pct_per_min"`
	NonMonotonic bool    `json:"non_monotonic,omitempty"`
}

// Gap is a declared data hole. Curves never interpolate across it.
type Gap struct {
	ID      string  `json:"id"`
	FromT   float64 `json:"from_t_min"`
	ToT     float64 `json:"to_t_min"`
	Missing int     `json:"missing_estimate"`
	Reason  string  `json:"reason"`
}

// Segment is a contiguous, gap-free run of sample points.
type Segment struct {
	ID       string  `json:"id"`
	Index    int     `json:"index"`
	TStart   float64 `json:"t_start_min"`
	TEnd     float64 `json:"t_end_min"`
	TStartK  float64 `json:"T_start_K"`
	TEndK    float64 `json:"T_end_K"`
	Points   []Point `json:"points"`
}

// PeakCandidate is one DTG feature; overlaps are retained as alternatives.
type PeakCandidate struct {
	ID            string   `json:"id"`
	SegmentID     string   `json:"segment_id"`
	AtT           float64  `json:"at_t_min"`
	AtTk          float64  `json:"at_T_K"`
	DTG           float64  `json:"dtg_pct_per_min"`
	ProminenceOK  bool     `json:"prominence_ok"`
	OverlapGroup  string   `json:"overlap_group,omitempty"`
	AlternativeTo string   `json:"alternative_to,omitempty"`
	Status        string   `json:"status"` // proposed | rejected | ambiguous
	RejectReason  string   `json:"reject_reason,omitempty"`
	SupportsStage string   `json:"supports_stage,omitempty"`
	Sources       []string `json:"sources"`
}

// AtmosphereWindow is the reaction-time mapping of a valve event.
type AtmosphereWindow struct {
	ID              string  `json:"id"`
	ValveSource     string  `json:"valve_source"`
	ValveT          float64 `json:"valve_t_min"`
	ReactionT       float64 `json:"reaction_t_min"`
	FromGas         string  `json:"from_gas"`
	ToGas           string  `json:"to_gas"`
	DelayMin        float64 `json:"delay_min"`
	Note            string  `json:"note"`
}

// StageCandidate is a proposed decomposition interval.
type StageCandidate struct {
	ID          string   `json:"id"`
	SegmentID   string   `json:"segment_id"`
	StartT      float64  `json:"start_t_min"`
	EndT        float64  `json:"end_t_min"`
	StartTk     float64  `json:"start_T_K"`
	EndTk       float64  `json:"end_T_K"`
	MassStart   float64  `json:"mass_start_mg"`
	MassEnd     float64  `json:"mass_end_mg"`
	LossPct     float64  `json:"loss_pct_of_initial"`
	PeakID      string   `json:"peak_id,omitempty"`
	GasAtStart  string   `json:"gas_at_start,omitempty"`
	GasAtEnd    string   `json:"gas_at_end,omitempty"`
	OverlapsWith []string `json:"overlaps_with,omitempty"`
	Status      string   `json:"status"` // proposed | accepted | rejected | ambiguous
	RejectReason string  `json:"reject_reason,omitempty"`
	// Adjudication is filled by the state machine, never by the engine.
	Adjudication *DecisionRef `json:"adjudication,omitempty"`
	Sources      []string     `json:"sources"`
	Chain        []ChainStep  `json:"chain"`
}

// DecisionRef links a candidate to an adjudication event.
type DecisionRef struct {
	EventID  string `json:"event_id"`
	Kind     string `json:"kind"`
	AtSeq    int64  `json:"at_seq"`
	Basis    string `json:"basis"`
	Supersedes string `json:"supersedes,omitempty"`
}

// Residue is one reported residual mass ratio with a full calculation chain.
type Residue struct {
	ID        string      `json:"id"`
	SegmentID string      `json:"segment_id"`
	AtT       float64     `json:"at_t_min"`
	AtTk      float64     `json:"at_T_K"`
	ValuePct  float64     `json:"value_pct"`
	Unknown   bool        `json:"unknown"`
	Status    string      `json:"status"` // computed | unknown
	Reason    string      `json:"reason,omitempty"`
	Sources   []string    `json:"sources"`
	Chain     []ChainStep `json:"chain"`
}

// Diagnostic describes a non-fatal input problem observed during derivation.
type Diagnostic struct {
	Scope   string `json:"scope"`
	Message string `json:"message"`
	Severity string `json:"severity"` // info | warning | error
}

// Result is the full deterministic derivation output.
type Result struct {
	AlgorithmVersion string             `json:"algorithm_version"`
	ParamFingerprint string             `json:"param_fingerprint"`
	Params           Params             `json:"params"`
	InputHash        string             `json:"input_hash"`
	InitialMassMG    float64            `json:"initial_mass_mg"`
	InitialMassKnown bool               `json:"initial_mass_known"`
	Segments         []Segment          `json:"segments"`
	Gaps             []Gap              `json:"gaps"`
	Peaks            []PeakCandidate    `json:"peaks"`
	Atmospheres      []AtmosphereWindow `json:"atmospheres"`
	Stages           []StageCandidate   `json:"stages"`
	Residues         []Residue          `json:"residues"`
	Diagnostics      []Diagnostic       `json:"diagnostics"`
	Provenance       Provenance         `json:"provenance"`
}
