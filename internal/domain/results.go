package domain

import "time"

// StageCandidate is one proposed decomposition stage.
type StageCandidate struct {
	ID           string   `json:"id"`
	Index        int      `json:"index"`
	StartT       float64  `json:"start_t"`
	EndT         float64  `json:"end_t"`
	PeakT        float64  `json:"peak_t"`
	ReactionT    float64  `json:"reaction_t,omitempty"` // valve time corrected for atm delay/lag
	AtmGas       string   `json:"atm_gas,omitempty"`
	LossPct      float64  `json:"loss_pct"`             // % of declared initial mass
	LossPctErr   float64  `json:"loss_pct_err,omitempty"`
	OverlapsWith []string `json:"overlaps_with,omitempty"`
	Ambiguous    bool     `json:"ambiguous,omitempty"`
	Basis        string   `json:"basis"`
	RejectReason string   `json:"reject_reason,omitempty"`
	// AlternativeIDs lists other legal candidates kept when not unique.
	AlternativeIDs []string `json:"alternatives,omitempty"`
}

// Residue is a residue-rate value with a full calculation chain.
type Residue struct {
	ID        string       `json:"id"`
	AtT       float64      `json:"at_t"`
	Label     string       `json:"label"`
	Pct       float64      `json:"pct"`
	PctStatus string       `json:"status"` // ok|unknown|error
	Chain     []ChainStep  `json:"chain"`
	Issues    []Issue      `json:"issues,omitempty"`
}

// ChainStep is one auditable calculation step.
type ChainStep struct {
	Order   int               `json:"order"`
	Op      string            `json:"op"`
	Detail  string            `json:"detail"`
	Inputs  map[string]string `json:"inputs,omitempty"`
	Output  string            `json:"output"`
	Algo    string            `json:"algo,omitempty"`
	SourceRefs []string       `json:"source_refs,omitempty"`
}

// Finding is a machine-readable judgement about an observed phenomenon
// (atmosphere delay, gap, non-monotonic noise, overlap ...).
type Finding struct {
	Code      string            `json:"code"`
	Severity  string            `json:"severity"`
	Message   string            `json:"message"`
	StartT    *float64          `json:"start_t,omitempty"`
	EndT      *float64          `json:"end_t,omitempty"`
	Data      map[string]any    `json:"data,omitempty"`
	SourceRefs []string         `json:"source_refs,omitempty"`
}

// Result bundles one derivation output snapshot.
type Result struct {
	ID           string           `json:"id"`
	ProjectID    string           `json:"project_id"`
	AlgoVersion  string           `json:"algo_version"`
	ParamID      string           `json:"param_id"`
	ParamFingerprint string       `json:"param_fingerprint"`
	DirectSources  []SourceRef    `json:"direct_sources"`
	Samples      []SamplePoint    `json:"samples"`
	TG           []TGPoint        `json:"tg"`
	DTG          []DTGPoint       `json:"dtg"`
	Furnace      []FurnacePoint   `json:"furnace"`
	AtmSegments  []AtmSegment     `json:"atm"`
	Stages       []StageCandidate `json:"stages"`
	Residues     []Residue        `json:"residues"`
	Findings     []Finding        `json:"findings"`
	Gaps         []Gap            `json:"gaps"`
	Issues       []Issue          `json:"issues"`
	Stale        bool             `json:"stale"`
	CreatedAt    time.Time        `json:"created_at"`
}

// SourceRef is a direct pointer to an immutable input document/version.
type SourceRef struct {
	DocID   string `json:"doc_id"`
	Kind    string `json:"kind"`
	Version int    `json:"version"`
}

type TGPoint struct {
	T       float64 `json:"t"`
	Pct     float64 `json:"pct"`
	Mass    float64 `json:"mass"`     // corrected mass in mg
	InGap   bool    `json:"in_gap,omitempty"`
	Sparse  bool    `json:"sparse,omitempty"`
}

type DTGPoint struct {
	T       float64  `json:"t"`
	Value   float64  `json:"value"` // %/min, negative for losses
	PeakID  string   `json:"peak_id,omitempty"`
	InGap   bool     `json:"in_gap,omitempty"`
}

type FurnacePoint struct {
	T        float64 `json:"t"`
	Setpoint float64 `json:"setpoint"`
	Sample   float64 `json:"sample"` // lagged sample-side temperature
	Lagged   bool    `json:"lagged"`
}

type AtmSegment struct {
	StartT      float64 `json:"start_t"`
	EndT        float64 `json:"end_t"`
	Gas         string  `json:"gas"`
	ValveT      float64 `json:"valve_t"`
	ReactionT   float64 `json:"reaction_t"`
	DelayS      float64 `json:"delay_s"`
}

type Gap struct {
	StartT float64 `json:"start_t"`
	EndT   float64 `json:"end_t"`
	Reason string  `json:"reason"`
}

// Decision is a human accept/reject/adjust action recorded as an event.
type Decision struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Action      string    `json:"action"` // accept|reject|adjust|resolve_conflict|undo
	TargetType  string    `json:"target_type"` // stage|residue|result|finding
	TargetID    string    `json:"target_id"`
	Comment     string    `json:"comment,omitempty"`
	Adjustments map[string]float64 `json:"adjustments,omitempty"`
	BasedOn     []string  `json:"based_on"` // result/doc ids used
	Undoes      string    `json:"undoes,omitempty"`
	ConflictKey string    `json:"conflict_key,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}
