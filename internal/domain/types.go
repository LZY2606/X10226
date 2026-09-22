// Package domain holds the TGA (thermogravimetric analysis) review core:
// immutable source submissions, pure analysis algorithms, stage candidates,
// residues with computation chains, and provenance for every derived value.
package domain

import "time"

// Layer identifies one of the separately preserved source strata.
type Layer string

const (
	LayerRawPoints   Layer = "raw_points"
	LayerDrift       Layer = "balance_drift"
	LayerFurnace     Layer = "furnace_program"
	LayerGasValve    Layer = "gas_valve"
	LayerBlankCurve  Layer = "blank_curve"
	LayerInitialMass Layer = "initial_mass"
	LayerParams      Layer = "params"
)

// AllLayers in the canonical stratified order shown to reviewers.
var AllLayers = []Layer{
	LayerRawPoints, LayerDrift, LayerFurnace,
	LayerGasValve, LayerBlankCurve, LayerInitialMass, LayerParams,
}

func (l Layer) Label() string {
	switch l {
	case LayerRawPoints:
		return "原始采样点"
	case LayerDrift:
		return "天平漂移校正"
	case LayerFurnace:
		return "炉程序"
	case LayerGasValve:
		return "气体阀事件"
	case LayerBlankCurve:
		return "空白曲线"
	case LayerInitialMass:
		return "样品初重"
	case LayerParams:
		return "分析参数"
	}
	return string(l)
}

// RawValue preserves the original textual representation of an imported value.
// Normalisation to Value/Unit happens only in derived layers; unconvertible
// values stay in place (OK=false) with diagnostics instead of being dropped.
type RawValue struct {
	Text  string   `json:"text"`
	Value float64  `json:"value,omitempty"`
	Unit  string   `json:"unit"`
	OK    bool     `json:"ok"`
	Diags []string `json:"diags,omitempty"`
}

// Diag is a machine-readable diagnostic attached to a submission/derivation.
type Diag struct {
	Level   string `json:"level"` // error | warning
	Code    string `json:"code"`
	Layer   string `json:"layer,omitempty"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
	Raw     string `json:"raw,omitempty"`
}

// RawPoint is one imported TG sample row in its original representation.
type RawPoint struct {
	Line int      `json:"line"`
	Time RawValue `json:"time"`
	Temp RawValue `json:"temp"`
	Mass RawValue `json:"mass"`
	Raw  string   `json:"raw,omitempty"`
}

func (p RawPoint) Valid() bool { return p.Time.OK && p.Temp.OK && p.Mass.OK }

// DriftSpec is the balance baseline-drift correction record (linear model
// fitted to the blank run, stored as an imported/revised stratum).
type DriftSpec struct {
	Line        int      `json:"line"`
	Model       string   `json:"model"` // blank_linear
	SlopeMgPerS RawValue `json:"slope_mg_per_s"`
	InterceptMg RawValue `json:"intercept_mg"`
	Note        string   `json:"note,omitempty"`
}

// FurnaceSegment is one programmed furnace ramp/hold step.
type FurnaceSegment struct {
	Line       int      `json:"line"`
	StartS     RawValue `json:"start_s"`
	EndS       RawValue `json:"end_s"`
	StartTempC RawValue `json:"start_temp_c"`
	EndTempC   RawValue `json:"end_temp_c"`
	Kind       string   `json:"kind"` // ramp | hold
}

// ValveEvent is a gas valve switch as recorded by the instrument.
type ValveEvent struct {
	Line     int      `json:"line"`
	AtS      RawValue `json:"at_s"`
	FromGas  string   `json:"from_gas"`
	ToGas    string   `json:"to_gas"`
	FlowSCCM RawValue `json:"flow_sccm,omitempty"`
}

// BlankPoint is one blank/baseline curve point.
type BlankPoint struct {
	Line int      `json:"line"`
	Time RawValue `json:"time"`
	Mass RawValue `json:"mass"`
}

// InitialMass is the stratified sample initial weighing record.
type InitialMass struct {
	Line  int      `json:"line"`
	Mass  RawValue `json:"mass"`
	Basis string   `json:"basis,omitempty"` // tared | revised
}

// Params are the versioned analysis parameters (algorithm tga-v1).
type Params struct {
	Algorithm       string  `json:"algorithm"`
	GasDelayS       float64 `json:"gas_delay_s"`         // valve -> sample reaction delay
	TempLagS        float64 `json:"temp_lag_s"`          // temperature sensor lag
	DTGHalfWidth    int     `json:"dtg_half_width"`      // central-difference half width (points)
	GapMaxS         float64 `json:"gap_max_s"`           // larger time gaps: no interpolation across
	NoiseSigma      float64 `json:"noise_sigma"`         // non-monotonic noise threshold (mg)
	PeakPromPct     float64 `json:"peak_prominence_pct"` // DTG peak prominence, %/min
	MinStageS       float64 `json:"min_stage_s"`         // minimum stage width, s
	OverlapTouchS   float64 `json:"overlap_touch_s"`     // intervals within this count as overlapping
	BlankDriftModel string  `json:"blank_drift_model"`   // blank_linear
}

// DefaultParams returns the tga-v1 default parameter set.
func DefaultParams() Params {
	return Params{
		Algorithm:       "tga-v1",
		GasDelayS:       0,
		TempLagS:        0,
		DTGHalfWidth:    2,
		GapMaxS:         45,
		NoiseSigma:      0.06,
		PeakPromPct:     0.04,
		MinStageS:       20,
		OverlapTouchS:   6,
		BlankDriftModel: "blank_linear",
	}
}

// Submission is an immutable, append-only source record. Revisions are new
// submissions with ParentID set; the original is never overwritten.
type Submission struct {
	ID          string           `json:"id"`
	ProjectID   string           `json:"project_id"`
	Seq         int              `json:"seq"`
	Layer       Layer            `json:"layer"`
	Kind        string           `json:"kind"` // import | revision | import_partial
	ParentID    string           `json:"parent_id,omitempty"`
	SourceName  string           `json:"source_name"`
	PayloadHash string           `json:"payload_hash"`
	Partial     bool             `json:"partial,omitempty"`
	Mutation    string           `json:"mutation,omitempty"` // counterexample mutation tag
	Note        string           `json:"note,omitempty"`
	Diags       []Diag           `json:"diags,omitempty"`
	Points      []RawPoint       `json:"points,omitempty"`
	Drift       *DriftSpec       `json:"drift,omitempty"`
	Furnace     []FurnaceSegment `json:"furnace,omitempty"`
	Valves      []ValveEvent     `json:"valves,omitempty"`
	Blank       []BlankPoint     `json:"blank,omitempty"`
	InitialMass *InitialMass     `json:"initial_mass,omitempty"`
	Params      *Params          `json:"params,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
}

// SourceRef binds a derived input to the exact submission it came from.
type SourceRef struct {
	Layer Layer  `json:"layer"`
	SubID string `json:"submission_id"`
	Hash  string `json:"payload_hash"`
}

// ChainStep is one expandable step in a residue/result computation chain.
type ChainStep struct {
	Label      string `json:"label"`
	Value      string `json:"value"`       // display string carrying unit/coordinate basis
	SourceKind string `json:"source_kind"` // raw | submission | derived | decision | param
	Ref        string `json:"ref"`         // stable identity of the upstream value
	Formula    string `json:"formula,omitempty"`
}

// SeriesPoint is one normalised TG/DTG series point.
type SeriesPoint struct {
	ID           string  `json:"id"`
	Line         int     `json:"line"`
	TimeS        float64 `json:"time_s"`
	SensorTempC  float64 `json:"sensor_temp_c"`
	ReactTempC   float64 `json:"reaction_temp_c"` // temp shifted by sensor lag
	MassMgCorr   float64 `json:"mass_mg_corr"`
	MassPct      float64 `json:"mass_pct"`
	DTGPctPerMin float64 `json:"dtg_pct_per_min"`
	GapAfterS    float64 `json:"gap_after_s,omitempty"`
	NoAcrossGap  bool    `json:"no_across_gap,omitempty"`
	NonMono      bool    `json:"non_mono,omitempty"`
	Excluded     bool    `json:"excluded,omitempty"`
}

// Gap marks a sampling gap that must not be interpolated across.
type Gap struct {
	FromLine int     `json:"from_line"`
	ToLine   int     `json:"to_line"`
	FromS    float64 `json:"from_s"`
	ToS      float64 `json:"to_s"`
}

// StageCandidate is one legal stage decomposition candidate. When stages
// cannot be uniquely decided (overlapping peaks), every legal alternative is
// kept; Group ties alternatives of the same ambiguity.
type StageCandidate struct {
	ID               string      `json:"id"`
	Key              string      `json:"key"` // stable G{n}-A{m}
	Group            int         `json:"group"`
	Alternative      int         `json:"alternative"`
	Label            string      `json:"label"`
	OnsetS           float64     `json:"onset_s"`
	PeakS            float64     `json:"peak_s"`
	EndS             float64     `json:"end_s"`
	OnsetTempC       float64     `json:"onset_temp_c"`
	PeakTempC        float64     `json:"peak_temp_c"`
	EndTempC         float64     `json:"end_temp_c"`
	MassLossPct      float64     `json:"mass_loss_pct"`
	MassLossMg       float64     `json:"mass_loss_mg"`
	DTGPeakPctPerMin float64     `json:"dtg_peak_pct_per_min"`
	Gas              string      `json:"gas"`
	GasAttribution   string      `json:"gas_attribution"` // unique | delayed | unknown
	ValveS           float64     `json:"valve_s,omitempty"`
	ReactionS        float64     `json:"reaction_s,omitempty"`
	Overlap          bool        `json:"overlap"`
	RuleRejected     bool        `json:"rule_rejected,omitempty"`
	RejectReason     string      `json:"reject_reason,omitempty"`
	DependsOn        []Layer     `json:"depends_on"`
	Basis            []ChainStep `json:"basis,omitempty"`
	Status           string      `json:"status,omitempty"` // pending_review | accepted | rejected | carried | expired
	DecisionID       string      `json:"decision_id,omitempty"`
}

// Residue is a residual-mass percentage with its full computation chain.
type Residue struct {
	ID         string      `json:"id"`
	Key        string      `json:"key"`
	Label      string      `json:"label"`
	AtS        float64     `json:"at_s"`
	AtTempC    float64     `json:"at_temp_c"`
	Pct        float64     `json:"pct"`
	Mg         float64     `json:"mg"`
	Unknown    bool        `json:"unknown,omitempty"`
	ErrorPct   float64     `json:"error_pct,omitempty"`
	Status     string      `json:"status,omitempty"`
	DecisionID string      `json:"decision_id,omitempty"`
	Chain      []ChainStep `json:"chain"`
}

// Derivation is one immutable computed version of the analysis.
type Derivation struct {
	ID               string           `json:"id"`
	Seq              int              `json:"seq"`
	ProjectID        string           `json:"project_id"`
	Algorithm        string           `json:"algorithm"`
	ParamFingerprint string           `json:"param_fingerprint"`
	ParamsHash       string           `json:"params_hash"`
	Sources          []SourceRef      `json:"sources"`
	EventID          string           `json:"event_id"`
	CreatedAt        time.Time        `json:"created_at"`
	Series           []SeriesPoint    `json:"series"`
	Gaps             []Gap            `json:"gaps,omitempty"`
	Stages           []StageCandidate `json:"stages"`
	Residues         []Residue        `json:"residues"`
	Diags            []Diag           `json:"diags,omitempty"`
	// AttributionRules records the machine-readable judgement basis.
	AttributionRules []MachineRule `json:"attribution_rules,omitempty"`
}

// MachineRule is a machine-readable judgement/event basis.
type MachineRule struct {
	Code      string            `json:"code"`
	Statement string            `json:"statement"`
	Evidence  map[string]string `json:"evidence,omitempty"`
}

// Event is an append-only project history entry.
type Event struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Seq       int            `json:"seq"`
	Type      string         `json:"type"`
	At        time.Time      `json:"at"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// Decision is a human adjudication (accept/reject/adjust/resolve/undo).
type Decision struct {
	ID           string         `json:"id"`
	Seq          int            `json:"seq"`
	ProjectID    string         `json:"project_id"`
	EventID      string         `json:"event_id"`
	TargetKind   string         `json:"target_kind"` // candidate | residue
	TargetKey    string         `json:"target_key"`
	DerivationID string         `json:"derivation_id"`
	Action       string         `json:"action"` // accept | reject | adjust | resolve_conflict | undo
	Adjustments  map[string]any `json:"adjustments,omitempty"`
	Reason       string         `json:"reason"`
	SupersedesID string         `json:"supersedes_id,omitempty"`
	Active       bool           `json:"active"`
	CreatedAt    time.Time      `json:"created_at"`
}

// Project is the review container.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Material  string    `json:"material"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
