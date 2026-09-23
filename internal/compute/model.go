// Package compute is versioned analysis logic (algorithm "alg1"). Every
// emitted value is content-addressed from (algorithm version, parameter
// fingerprint, direct source row ids), so changing one direct source only
// changes the values that actually depend on it.
package compute

// AlgoVersion pins the behaviour of this engine. Any change to the math must
// bump this, otherwise historical replays would silently change.
const AlgoVersion = "alg1"

// normalized units used by every derived value
const (
	UnitTime = "s"
	UnitTemp = "degC"
	UnitMass = "mg"
)

// Ref points at a preserved raw row or another node.
type Ref struct {
	Kind string `json:"kind"` // "raw" | "node" | "param" | "rule"
	ID   string `json:"id"`
	Role string `json:"role,omitempty"`
}

// Node is one calculation-chain value with a stable identity and provenance.
type Node struct {
	ID       string  `json:"id"`
	Kind     string  `json:"kind"`
	Label    string  `json:"label"`
	Value    float64 `json:"value,omitempty"`
	Unit     string  `json:"unit,omitempty"`
	Status   string  `json:"status"` // ok | unknown | noisy | gap_uncertain
	Formula  string  `json:"formula,omitempty"`
	Sources  []Ref   `json:"sources,omitempty"`
	Note     string  `json:"note,omitempty"`
}

// CurvePoint is one normalized TG sample point.
type CurvePoint struct {
	Node        Node    `json:"node"`
	T           float64 `json:"t"` // furnace/elapsed seconds (normalized)
	TRawUnit    string  `json:"t_raw_unit,omitempty"`
	SampleTemp  float64 `json:"sample_temp"`
	TempStatus  string  `json:"temp_status"`
	MRaw        float64 `json:"m_raw"`
	MCorr       float64 `json:"m_corr"`
	CorrStatus  string  `json:"corr_status"` // ok | blank_missing | unconvertible
	InGap       bool    `json:"in_gap"`
	Segment     int     `json:"segment"`
	Noise       bool    `json:"noise"`
	RawID       string  `json:"raw_id"`
}

// Gap is a sampling hole. Nothing is interpolated across it.
type Gap struct {
	Node       Node    `json:"node"`
	TBefore    float64 `json:"t_before"`
	TAfter     float64 `json:"t_after"`
	MBefore    float64 `json:"m_before"`
	MAfter     float64 `json:"m_after"`
	SpanS      float64 `json:"span_s"`
	CrossDelta float64 `json:"cross_delta_mg"`
}

// DTGPoint is one derivative sample (smoothed), per segment.
type DTGPoint struct {
	Node      Node    `json:"node"`
	T         float64 `json:"t"`
	Rate      float64 `json:"rate"` // mg/s (negative for loss)
	Segment   int     `json:"segment"`
	AcrossGap bool    `json:"across_gap"`
}

// Peak is a detected DTG extremum.
type Peak struct {
	Node      Node    `json:"node"`
	T         float64 `json:"t"`
	Rate      float64 `json:"rate"`
	Segment   int     `json:"segment"`
	Prominence float64 `json:"prominence"`
}

// GasSpan is a valve event projected onto sample-visible atmosphere time.
type GasSpan struct {
	Node        Node    `json:"node"`
	ValveT      float64 `json:"valve_t"`
	EffectiveT  float64 `json:"effective_t"`
	EndT        float64 `json:"end_t"` // effective start of next gas or +inf
	Gas         string  `json:"gas"`
	RawID       string  `json:"raw_id"`
}

// StageCandidate is one legal (possibly overlapping) mass-loss stage.
type StageCandidate struct {
	Node       Node     `json:"node"`
	ID         string   `json:"id"`
	Group      string   `json:"group"` // overlapping alternatives share a group
	TStart     float64  `json:"t_start"`
	TEnd       float64  `json:"t_end"`
	PeakT      float64  `json:"peak_t,omitempty"`
	DeltaMMg   float64  `json:"delta_m_mg,omitempty"`
	DeltaRange [2]float64 `json:"delta_range_mg,omitempty"` // when not uniquely decidable
	PercentOfM0 float64 `json:"percent_of_m0,omitempty"`
	PercentUnknown bool  `json:"percent_unknown"`
	Gas        string   `json:"gas,omitempty"`
	Status     string   `json:"status"` // ok | gap_uncertain | overlap_alternative
	Sources    []Ref    `json:"sources"`
	Rejects    []string `json:"rejects,omitempty"` // rejected micro-peaks etc.
}

// Residue is the final residual fraction with an expandable chain.
type Residue struct {
	Node         Node    `json:"node"`
	Percent      float64 `json:"percent"`
	Status       string  `json:"status"`
	TailStart    float64 `json:"tail_start"`
	TailEnd      float64 `json:"tail_end"`
	TailMeanMg   float64 `json:"tail_mean_mg"`
	M0Mg         float64 `json:"m0_mg"`
	PercentUnknown bool  `json:"percent_unknown"`
	Chain        []Node  `json:"chain"`
}

// Judgment is a machine-readable domain rule event.
type Judgment struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Rule     string   `json:"rule"`
	Refs     []Ref    `json:"refs"`
	Detail   map[string]float64 `json:"detail,omitempty"`
}

// Result is the full derived artifact set for one state.
type Result struct {
	AlgoVersion  string          `json:"algo_version"`
	ParamFP      string          `json:"param_fp"`
	Curve        []CurvePoint    `json:"curve"`
	Gaps         []Gap           `json:"gaps"`
	DTG          []DTGPoint      `json:"dtg"`
	Peaks        []Peak          `json:"peaks"`
	GasSpans     []GasSpan       `json:"gas_spans"`
	Program      []ProgPoint     `json:"program"`
	Stages       []StageCandidate `json:"stages"`
	Residues     []Residue       `json:"residues"`
	Judgments    []Judgment      `json:"judgments"`
	Diags        []string        `json:"diags"`
	RawRefs      []string        `json:"raw_refs"`
}

// ProgPoint is one normalized furnace-program point (sample-time shifted).
type ProgPoint struct {
	Node       Node    `json:"node"`
	TProg      float64 `json:"t_prog"`
	SampleT    float64 `json:"sample_t"` // furnace time at which sample sees it
	Temp       float64 `json:"temp"`
	RawID      string  `json:"raw_id"`
}

func (r Residue) ResiduesSortDummy() {}
