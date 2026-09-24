package domain

import "time"

const AlgorithmVersion = "tga-stage-decomposition@1.0.0"

type Quantity struct {
	Raw         string  `json:"raw"`
	Value       float64 `json:"value,omitempty"`
	Unit        string  `json:"unit"`
	Basis       string  `json:"basis,omitempty"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

type Record struct {
	ID         string            `json:"id"`
	Layer      string            `json:"layer"`
	Time       Quantity          `json:"time"`
	Temperature Quantity          `json:"temperature,omitempty"`
	Mass       Quantity          `json:"mass,omitempty"`
	InitialMass Quantity         `json:"initial_mass,omitempty"`
	Drift      Quantity          `json:"drift,omitempty"`
	Gas        string            `json:"gas,omitempty"`
	Segment    string            `json:"segment,omitempty"`
	TargetTemp Quantity          `json:"target_temperature,omitempty"`
	RampRate   Quantity          `json:"ramp_rate,omitempty"`
	Note       string            `json:"note,omitempty"`
	Valid      bool              `json:"valid"`
	Diagnostic string            `json:"diagnostic,omitempty"`
	RawText    string            `json:"raw_text"`
	Extras     map[string]string `json:"extras,omitempty"`
	Supersedes string            `json:"supersedes,omitempty"`
}

type Batch struct {
	Records       []Record `json:"records"`
	AcceptedCount int      `json:"accepted_count"`
	RejectedCount int      `json:"rejected_count"`
	RawText       string   `json:"raw_text"`
	Fragment      string   `json:"fragment,omitempty"`
	Diagnostics   []string `json:"diagnostics"`
}

type Params struct {
	Version             int     `json:"version"`
	ParentVersion       int     `json:"parent_version,omitempty"`
	GasDelaySeconds     float64 `json:"gas_delay_seconds"`
	ThermalLagSeconds   float64 `json:"thermal_lag_seconds"`
	SmoothWindow        int     `json:"smooth_window"`
	PeakThresholdMgPerMin float64 `json:"peak_threshold_mg_per_min"`
	GapThresholdSeconds float64 `json:"gap_threshold_seconds"`
	Fingerprint         string  `json:"fingerprint"`
}

func DefaultParams() Params {
	p := Params{
		GasDelaySeconds:     12,
		ThermalLagSeconds:   8,
		SmoothWindow:        3,
		PeakThresholdMgPerMin: 0.01,
		GapThresholdSeconds: 15,
	}
	p.Fingerprint = FingerprintParams(p)
	return p
}

type ImportCommitted struct {
	Batch Batch `json:"batch"`
}

type ImportPartial struct {
	Batch Batch `json:"batch"`
}

type ParamsRevised struct {
	Params Params `json:"params"`
	Reason string `json:"reason"`
}

type SourceRevised struct {
	Record Record `json:"record"`
	Reason string `json:"reason"`
}

type ManualDecision struct {
	DecisionID string `json:"decision_id"`
	TargetID   string `json:"target_id"`
	Action     string `json:"action"`
	Reason     string `json:"reason"`
	ParamsPatch *Params `json:"params_patch,omitempty"`
	Undoes      string `json:"undoes,omitempty"`
}

type Event struct {
	Seq         int64          `json:"seq"`
	ID          string         `json:"id"`
	ProjectID   string         `json:"project_id"`
	Type        string         `json:"type"`
	CreatedAt   time.Time      `json:"created_at"`
	Operator    string         `json:"operator"`
	PayloadJSON string         `json:"payload_json"`
	Payload     any            `json:"payload,omitempty"`
}

type Value struct {
	ID         string   `json:"id"`
	Label      string   `json:"label"`
	Raw        string   `json:"raw,omitempty"`
	Numeric    *float64 `json:"numeric,omitempty"`
	Unit       string   `json:"unit"`
	Basis      string   `json:"basis,omitempty"`
	Status     string   `json:"status"`
	Unknown    bool     `json:"unknown,omitempty"`
	Formula    string   `json:"formula,omitempty"`
	SourceIDs  []string `json:"source_ids"`
	Diagnostic string   `json:"diagnostic,omitempty"`
}

type CalcChain struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Value  Value   `json:"value"`
	Steps  []Value `json:"steps"`
}

type StageCandidate struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	Status         string   `json:"status"`
	StartSecond    float64  `json:"start_second"`
	EndSecond      float64  `json:"end_second"`
	StartCelsius   *float64 `json:"start_celsius,omitempty"`
	EndCelsius     *float64 `json:"end_celsius,omitempty"`
	Gas            string   `json:"gas,omitempty"`
	PeakSeconds    []float64 `json:"peak_seconds,omitempty"`
	PeakCelsius    []float64 `json:"peak_celsius,omitempty"`
	MassLossPercent *float64 `json:"mass_loss_percent,omitempty"`
	OverlapWith    []string `json:"overlap_with,omitempty"`
	RejectReasons  []string `json:"reject_reasons,omitempty"`
	SourceIDs      []string `json:"source_ids"`
	DecisionID     string   `json:"decision_id,omitempty"`
	DecisionAction string   `json:"decision_action,omitempty"`
	Chain          CalcChain `json:"chain"`
}

type TimelineEvent struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Second    float64 `json:"second"`
	Celsius   *float64 `json:"celsius,omitempty"`
	Label     string  `json:"label"`
	Gas       string  `json:"gas,omitempty"`
	SourceIDs []string `json:"source_ids"`
	Note      string  `json:"note,omitempty"`
}

type Residue struct {
	ID     string    `json:"id"`
	Second float64   `json:"second"`
	Value  Value     `json:"value"`
	Chain  CalcChain `json:"chain"`
}

type Analysis struct {
	ID                string           `json:"id"`
	ProjectID         string           `json:"project_id"`
	AlgorithmVersion  string           `json:"algorithm_version"`
	ParameterVersion  int              `json:"parameter_version"`
	ParameterFingerprint string         `json:"parameter_fingerprint"`
	SourceSetHash     string           `json:"source_set_hash"`
	Current           bool             `json:"current"`
	ObsoleteReason    string           `json:"obsolete_reason,omitempty"`
	CreatedFromSeq    int64            `json:"created_from_seq"`
	Stages            []StageCandidate `json:"stages"`
	RejectedStages    []StageCandidate `json:"rejected_stages"`
	Residues          []Residue        `json:"residues"`
	Timeline          []TimelineEvent  `json:"timeline"`
	MachineJudgments  []MachineJudgment `json:"machine_judgments"`
	Diagnostics       []string         `json:"diagnostics"`
}

type MachineJudgment struct {
	ID       string   `json:"id"`
	Code     string   `json:"code"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	StartSecond *float64 `json:"start_second,omitempty"`
	EndSecond *float64 `json:"end_second,omitempty"`
	SourceIDs []string `json:"source_ids"`
}
