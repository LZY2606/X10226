// Package domain defines the domain model of the TGA review system:
// layered raw sources (immutable, versioned), derived values with
// provenance, machine-readable events and human decisions.
package domain

// Source kinds. Raw inputs are never overwritten: a revision creates a new
// version linked to its parent version.
const (
	SrcRawPoints   = "raw_points"       // 原始采样点
	SrcDrift       = "drift_correction" // 天平漂移校正
	SrcFurnace     = "furnace_program"  // 炉程序
	SrcGasEvents   = "gas_events"       // 气体阀事件
	SrcBlank       = "blank_curve"      // 空白曲线
	SrcInitialMass = "initial_mass"     // 样品初重
)

// SourceKinds lists all layered source kinds in canonical order.
var SourceKinds = []string{SrcRawPoints, SrcDrift, SrcFurnace, SrcGasEvents, SrcBlank, SrcInitialMass}

// Quantity keeps the original textual representation and its unit. The raw
// layer is preserved verbatim; only the derived layer normalizes. Values
// that cannot be converted stay in place and carry a diagnostic.
type Quantity struct {
	Raw  string `json:"raw"`
	Unit string `json:"unit"`
}

// Diagnostic explains why a value could not be normalized or used.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Ref     string `json:"ref"` // location of the offending value, e.g. "points[12].temp"
}

// ---- Source payloads (raw layer) ----

type RawPoint struct {
	Time Quantity `json:"time"`
	Temp Quantity `json:"temp"`
	Mass Quantity `json:"mass"`
}

type RawPointsPayload struct {
	SampleID string     `json:"sample_id"`
	Points   []RawPoint `json:"points"`
}

// DriftPayload models balance baseline drift as a linear rate around t0.
type DriftPayload struct {
	Rate Quantity `json:"rate"` // e.g. 0.02 mg/min
	T0   Quantity `json:"t0"`   // reference time
}

type FurnaceSegment struct {
	StartTime Quantity `json:"start_time"`
	EndTime   Quantity `json:"end_time"`
	StartTemp Quantity `json:"start_temp"`
	EndTemp   Quantity `json:"end_temp"`
}

type FurnacePayload struct {
	Segments []FurnaceSegment `json:"segments"`
}

type GasEvent struct {
	Time Quantity `json:"time"`
	Gas  string   `json:"gas"`
	Flow Quantity `json:"flow"`
}

type GasPayload struct {
	Events []GasEvent `json:"events"`
}

type BlankPoint struct {
	Temp Quantity `json:"temp"`
	Mass Quantity `json:"mass"`
}

type BlankPayload struct {
	Points []BlankPoint `json:"points"`
}

type InitialMassPayload struct {
	Mass Quantity `json:"mass"`
}

// ---- Derived payloads (derived layer, SI-normalized: s, C, mg) ----

// CurvePoint is one normalized, corrected TG point. SegmentID changes at
// sampling gaps: no interpolation ever crosses a gap.
type CurvePoint struct {
	Index     int      `json:"index"`
	SegmentID int      `json:"segment_id"`
	TimeS     float64  `json:"time_s"`
	TempC     float64  `json:"temp_c"`
	MassMg    float64  `json:"mass_mg"` // drift + blank corrected
	RawMassMg float64  `json:"raw_mass_mg"`
	BlankMg   float64  `json:"blank_mg"`
	DriftMg   float64  `json:"drift_mg"`
	Flags     []string `json:"flags,omitempty"` // e.g. "blank-extrapolated","gap-before"
}

type DTGPoint struct {
	TimeS     float64 `json:"time_s"`
	TempC     float64 `json:"temp_c"`
	DtgMgPerS float64 `json:"dtg_mg_per_s"`
	SegmentID int     `json:"segment_id"`
}

type CurvePayload struct {
	Points      []CurvePoint `json:"points"`
	DTG         []DTGPoint   `json:"dtg"`
	Gaps        []Gap        `json:"gaps"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

// Gap marks a sampling gap. Data is never interpolated across it.
type Gap struct {
	AfterIndex int     `json:"after_index"`
	FromS      float64 `json:"from_s"`
	ToS        float64 `json:"to_s"`
}

// AtmosphereEvent maps a valve switch to the effective reaction time at the
// sample: atmosphere delay + thermal lag mean valve time != reaction time.
type AtmosphereEvent struct {
	Gas           string  `json:"gas"`
	ValveTimeS    float64 `json:"valve_time_s"`
	ReactionTimeS float64 `json:"reaction_time_s"`
	DelayS        float64 `json:"delay_s"`
	FlowMlPerMin  float64 `json:"flow_ml_per_min"`
}

type AtmospherePayload struct {
	Events []AtmosphereEvent `json:"events"`
	Rule   string            `json:"rule"` // "atmosphere-lag"
}

// StageCandidate is one proposed decomposition stage. Stages may overlap and
// their mass losses are NOT required to sum to 100%.
type StageCandidate struct {
	CandidateID string   `json:"candidate_id"`
	SegmentID   int      `json:"segment_id"`
	StartS      float64  `json:"start_s"`
	EndS        float64  `json:"end_s"`
	StartTempC  float64  `json:"start_temp_c"`
	EndTempC    float64  `json:"end_temp_c"`
	PeakTempC   float64  `json:"peak_temp_c"`
	MassLossMg  float64  `json:"mass_loss_mg"`
	MassLossPct float64  `json:"mass_loss_pct"`        // of initial mass
	Overlaps    []string `json:"overlaps,omitempty"`   // ids of overlapping candidates
	Atmosphere  string   `json:"atmosphere,omitempty"` // gas effective during the stage
	Status      string   `json:"status"`               // "proposed","accepted","rejected","unknown"
	Reason      string   `json:"reason,omitempty"`     // rejection / unknown reason
}

type StagesPayload struct {
	Candidates []StageCandidate `json:"candidates"`
	Rejected   []StageCandidate `json:"rejected"`
	Unknowns   []StageCandidate `json:"unknowns"`
	Note       string           `json:"note"` // stages may overlap; sum not forced to 100%
}

// CalcStep is one node of a residue-rate calculation chain. Every displayed
// value carries a stable identity: <derived-id>#<step>.
type CalcStep struct {
	Step     string  `json:"step"` // e.g. "initial_mass","drift_correction","ratio"
	SourceID string  `json:"source_id,omitempty"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Detail   string  `json:"detail"`
}

type ResiduePayload struct {
	ResiduePct float64    `json:"residue_pct"`
	ResidueMg  float64    `json:"residue_mg"`
	Chain      []CalcStep `json:"chain"`
}

// ---- Decisions ----

const (
	DecideAccept = "accept"
	DecideReject = "reject"
	DecideAdjust = "adjust"
)
