package domain

// Core domain model for the TGA review / stage-decomposition / atmosphere-event system.
//
// Design rules enforced throughout this file:
//   - every domain quantity carries a unit (and, where relevant, a coordinate basis);
//   - original textual representations are preserved on the raw/import layer,
//     normalization happens only on derived layers;
//   - candidates, rejection reasons, errors and unknown states are first-class.

import "encoding/json"

// AlgoVersion is the version of the deterministic decomposition algorithm.
const AlgoVersion = "tga-decomp/v1.0.0"

// Stratum kinds (the six separately-versioned raw layers).
const (
	KindSamplePoints    = "sample_points"
	KindDriftCorrection = "drift_correction"
	KindFurnaceProgram  = "furnace_program"
	KindGasValve        = "gas_valve"
	KindBlankCurve      = "blank_curve"
	KindInitialMass     = "initial_mass"
)

// StratumKinds lists every raw layer in review order.
var StratumKinds = []string{
	KindSamplePoints,
	KindDriftCorrection,
	KindFurnaceProgram,
	KindGasValve,
	KindBlankCurve,
	KindInitialMass,
}

// Q is a quantity that remembers its original representation.
// Value is nil when the value could not be converted; Raw and Diagnostic then
// stay in place instead of the point being dropped for plotting convenience.
type Q struct {
	Raw        string   `json:"raw"`
	Value      *float64 `json:"value,omitempty"`
	Unit       string   `json:"unit"`
	Basis      string   `json:"basis,omitempty"`
	Diagnostic string   `json:"diagnostic,omitempty"`
}

// Valid reports whether the quantity was convertible.
func (q Q) Valid() bool { return q.Value != nil && q.Diagnostic == "" }

// RawPoint is one imported sampling row, exactly as received.
type RawPoint struct {
	Index        int      `json:"index"`
	TimeS        Q        `json:"time_s"`
	TempC        Q        `json:"temp_c"`
	MassMg       Q        `json:"mass_mg"`
	GapAfter     bool     `json:"gap_after,omitempty"`
	Diagnostics  []string `json:"diagnostics,omitempty"`
	OriginalLine string   `json:"original_line,omitempty"`
}

// SamplePointsPayload is the immutable sample_points stratum payload.
type SamplePointsPayload struct {
	CoordinateBasis string     `json:"coordinate_basis"`
	Points          []RawPoint `json:"points"`
	Note            string     `json:"note,omitempty"`
}

// DriftPayload describes the balance baseline-drift correction.
type DriftPayload struct {
	Method       string  `json:"method"` // "linear" | "blank_linked"
	RateMgPerMin float64 `json:"rate_mg_per_min,omitempty"`
	TimeBasis    string  `json:"time_basis"`
	BlankCurveID string  `json:"blank_curve_id,omitempty"`
	Note         string  `json:"note,omitempty"`
}

// FurnaceSegment is one furnace-program segment.
type FurnaceSegment struct {
	Index     int     `json:"index"`
	StartS    float64 `json:"start_s"`
	EndS      float64 `json:"end_s"`
	StartC    float64 `json:"start_c"`
	EndC      float64 `json:"end_c"`
	Kind      string  `json:"kind"` // ramp | hold
	SetpointC float64 `json:"setpoint_c,omitempty"`
}

// FurnacePayload is the furnace-program stratum.
type FurnacePayload struct {
	TimeBasis string           `json:"time_basis"`
	Segments  []FurnaceSegment `json:"segments"`
}

// ValveEvent is one gas-valve switch. DelayS models the atmosphere transport
// delay: valve switching time is NOT the sample reaction time.
type ValveEvent struct {
	ID      string  `json:"id"`
	TimeS   float64 `json:"time_s"`
	FromGas string  `json:"from_gas"`
	ToGas   string  `json:"to_gas"`
	DelayS  float64 `json:"delay_s"`
	Note    string  `json:"note,omitempty"`
}

// GasValvePayload is the gas-valve event stratum.
type GasValvePayload struct {
	TimeBasis string       `json:"time_basis"`
	Events    []ValveEvent `json:"events"`
}

// BlankPoint is one blank-curve sample.
type BlankPoint struct {
	Index  int     `json:"index"`
	TimeS  float64 `json:"time_s"`
	MassMg float64 `json:"mass_mg"`
}

// BlankCurvePayload is the blank-curve stratum.
type BlankCurvePayload struct {
	TimeBasis string       `json:"time_basis"`
	Points    []BlankPoint `json:"points"`
}

// InitialMassPayload is the separately versioned sample initial mass.
type InitialMassPayload struct {
	MassMg      float64 `json:"mass_mg"`
	Unit        string  `json:"unit"`
	Basis       string  `json:"basis"`
	RevisedFrom string  `json:"revised_from,omitempty"`
	Reason      string  `json:"reason,omitempty"`
}

// Source is one committed, immutable version of a raw stratum.
type Source struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Kind      string          `json:"kind"`
	Version   int             `json:"version"`
	ParentID  string          `json:"parent_id,omitempty"`
	BornSeq   int64           `json:"born_seq"`
	VirtualTS string          `json:"virtual_ts"`
	Payload   json.RawMessage `json:"payload"`
}

// Ref points at a direct dependency (source, node or event) at an exact version.
type Ref struct {
	Kind    string `json:"kind"` // source | node | event
	ID      string `json:"id"`
	Version string `json:"version,omitempty"`
	Role    string `json:"role,omitempty"`
}

// Node is one persisted derived result with full provenance.
type Node struct {
	ID                string          `json:"id"`
	ProjectID         string          `json:"project_id"`
	Kind              string          `json:"kind"`
	GroupKey          string          `json:"group_key"`
	AlgoVersion       string          `json:"algo_version"`
	ParamsFingerprint string          `json:"params_fingerprint"`
	DirectSources     []Ref           `json:"direct_sources"`
	BornSeq           int64           `json:"born_seq"`
	Payload           json.RawMessage `json:"payload"`
}

// Node view statuses (computed at read time).
const (
	StatusCurrent = "current"
	StatusStale   = "stale"
)

// Decision is a human adjudication attached to a node.
type Decision struct {
	EventID    string                 `json:"event_id"`
	Seq        int64                  `json:"seq"`
	Actor      string                 `json:"actor"`
	Verdict    string                 `json:"verdict"` // accepted | rejected | adjusted
	TargetNode string                 `json:"target_node"`
	TargetSeq  int64                  `json:"target_born_seq"`
	Params     map[string]float64     `json:"params,omitempty"`
	Rationale  string                 `json:"rationale,omitempty"`
	Undone     bool                   `json:"undone,omitempty"`
	UndoneBy   string                 `json:"undone_by,omitempty"`
	VirtualTS  string                 `json:"virtual_ts"`
	Extra      map[string]interface{} `json:"extra,omitempty"`
}
