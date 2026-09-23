// Package domain defines the immutable source layer, the append-only event
// log and the derived layer of the TGA review system.
//
// Design rules the rest of the codebase must preserve:
//   - Raw inputs are never overwritten. Every revision is a new source
//     version linked to its parent.
//   - Every derived value carries an algorithm version, a parameter
//     fingerprint and direct dependencies.
//   - Candidates, rejection reasons, errors and unknown states are first
//     class data. Expired versions stay readable but are never "current".
//   - Valve switch times are not reaction times (atmosphere delay and
//     temperature hysteresis). Stages may overlap; mass losses are not
//     forced to sum to 100%. Gaps are never interpolated across.
package domain

import (
	"encoding/json"
)

// Layer names identify the separately versioned source layers.
const (
	LayerInitialMass = "initial_mass"
	LayerSample      = "sample_points"
	LayerDrift       = "drift_correction"
	LayerBlank       = "blank_curve"
	LayerFurnace     = "furnace_program"
	LayerGas         = "gas_events"
	LayerParams      = "parameters"
)

// Parse/derived statuses.
const (
	StatusOK      = "ok"
	StatusPartial = "partial"
	StatusError   = "error"
	StatusUnknown = "unknown"
)

// Decision kinds.
const (
	DecisionAccepted = "accepted"
	DecisionRejected = "rejected"
	DecisionAdjusted = "adjusted"
)

// Diagnostic stays attached to the exact source location/object that caused it.
type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Ref      string `json:"ref"`
}

// Quantity preserves the original textual representation. Normalisation only
// happens in derived layers; an unconvertible value stays in place as unknown.
type Quantity struct {
	Raw      string   `json:"raw"`
	Value    *float64 `json:"value"`
	Unit     string   `json:"unit"`
	RawUnit  string   `json:"raw_unit"`
	Basis    string   `json:"basis,omitempty"`
	DiagCode string   `json:"diag_code,omitempty"`
	DiagMsg  string   `json:"diag_msg,omitempty"`
}

func Q(v float64, unit string) Quantity {
	return Quantity{Value: &v, Unit: unit, RawUnit: unit}
}

func BadQ(raw, rawUnit, code, msg string) Quantity {
	return Quantity{Raw: raw, RawUnit: rawUnit, DiagCode: code, DiagMsg: msg}
}

func (q Quantity) Known() bool { return q.Value != nil }

// SamplePoint is one raw acquisition row; each field keeps its own status.
type SamplePoint struct {
	Index int      `json:"index"`
	Time  Quantity `json:"time"`
	Temp  Quantity `json:"temp"`
	Mass  Quantity `json:"mass"`
}

// MassRecord is one weighing evidence row for the initial mass layer.
type MassRecord struct {
	Index int      `json:"index"`
	Label string   `json:"label"`
	Mass  Quantity `json:"mass"`
	Note  string   `json:"note,omitempty"`
}

// DriftPoint describes empty-pan balance drift: raw reading at time.
type DriftPoint struct {
	Index int      `json:"index"`
	Time  Quantity `json:"time"`
	Mass  Quantity `json:"mass"`
}

// BlankPoint is one blank-curve row used for blank subtraction.
type BlankPoint struct {
	Index int      `json:"index"`
	Time  Quantity `json:"time"`
	Mass  Quantity `json:"mass"`
}

// FurnaceStep is one programmed temperature segment.
type FurnaceStep struct {
	Index     int      `json:"index"`
	Kind      string   `json:"kind"`
	StartTime Quantity `json:"start_time"`
	EndTime   Quantity `json:"end_time"`
	StartTemp Quantity `json:"start_temp"`
	EndTemp   Quantity `json:"end_temp"`
	Rate      Quantity `json:"rate,omitempty"`
}

// GasEvent is a valve switch. The reaction window is the atmosphere delay:
// the sample reacts only within a parameterised later interval.
type GasEvent struct {
	Index          int      `json:"index"`
	ValveAt        Quantity `json:"valve_at"`
	FromGas        string   `json:"from_gas"`
	ToGas          string   `json:"to_gas"`
	Flow           Quantity `json:"flow,omitempty"`
	Note           string   `json:"note,omitempty"`
	ReactionStartS *float64 `json:"reaction_start_s,omitempty"`
	ReactionEndS   *float64 `json:"reaction_end_s,omitempty"`
}

// Params are fingerprinted and versioned; durations are in seconds.
type Params struct {
	AtmosDelayMinS     float64 `json:"atmos_delay_min_s"`
	AtmosDelayMaxS     float64 `json:"atmos_delay_max_s"`
	TempHysteresisC    float64 `json:"temp_hysteresis_c"`
	NoiseMonotoneTolMg float64 `json:"noise_monotone_tol_mg"`
	GapMaxS            float64 `json:"gap_max_s"`
	OverlapWindowS     float64 `json:"overlap_window_s"`
	DtgWindowPoints    int     `json:"dtg_window_points"`
	PeakProminenceMg   float64 `json:"peak_prominence_mg"`
}

// ParsedLayers is the typed result of parsing one source bundle.
type ParsedLayers struct {
	ProjectName   string        `json:"project_name,omitempty"`
	Sample        []SamplePoint `json:"sample_points,omitempty"`
	InitialMass   []MassRecord  `json:"initial_mass,omitempty"`
	Drift         []DriftPoint  `json:"drift_correction,omitempty"`
	Blank         []BlankPoint  `json:"blank_curve,omitempty"`
	Furnace       []FurnaceStep `json:"furnace_program,omitempty"`
	Gas           []GasEvent    `json:"gas_events,omitempty"`
	Params        *Params       `json:"parameters,omitempty"`
	PresentLayers []string      `json:"present_layers"`
	Diagnostics   []Diagnostic  `json:"diagnostics,omitempty"`
}

// SourceVersion is an immutable, hash-identified revision of one layer.
type SourceVersion struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"project_id"`
	Layer       string       `json:"layer"`
	Version     int          `json:"version"`
	ParentID    string       `json:"parent_id,omitempty"`
	Name        string       `json:"name,omitempty"`
	RawText     string       `json:"raw_text"`
	RawSha256   string       `json:"raw_sha256"`
	ParseState  string       `json:"parse_state"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	CreatedSeq  int64        `json:"created_seq"`
}

// DepRef points at the exact version an output was computed from.
type DepRef struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	VersionID string `json:"version_id"`
	Layer     string `json:"layer,omitempty"`
}

// Node kinds.
const (
	NodeCorrected = "corrected_series"
	NodeTG        = "tg_series"
	NodeDTG       = "dtg_series"
	NodeStage     = "stage_candidate"
	NodeResidue   = "residue"
	NodeFinal     = "final_residue"
)

// CorrectedPoint is the derived per-point row after drift/blank correction.
type CorrectedPoint struct {
	Index       int      `json:"index"`
	TimeS       *float64 `json:"time_s"`
	TempC       *float64 `json:"temp_c"`
	RawMg       *float64 `json:"raw_mg"`
	DriftCorrMg *float64 `json:"drift_corr_mg"`
	BlankCorrMg *float64 `json:"blank_corr_mg"`
	GapAfter    bool     `json:"gap_after"`
	Unknown     bool     `json:"unknown"`
	Reasons     []string `json:"reasons,omitempty"`
}

// TGPoint / DTGPoint are normalised derived rows; DTG across a gap is nil.
type TGPoint struct {
	Index   int      `json:"index"`
	TimeS   *float64 `json:"time_s"`
	TempC   *float64 `json:"temp_c"`
	MassPct *float64 `json:"mass_pct"`
	Unknown bool     `json:"unknown"`
}

type DTGPoint struct {
	Index     int      `json:"index"`
	TimeS     *float64 `json:"time_s"`
	TempC     *float64 `json:"temp_c"`
	DtgMgMin  *float64 `json:"dtg_mg_per_min"`
	DtgPctMin *float64 `json:"dtg_pct_per_min"`
	Gap       bool     `json:"gap"`
}

// StageCandidate is one decomposition candidate. Ambiguous splits produce
// several candidates that share the same cluster.
type StageCandidate struct {
	Cluster     int      `json:"cluster"`
	Ordinal     int      `json:"ordinal"`
	Label       string   `json:"label"`
	GasEventID  string   `json:"gas_event_id,omitempty"`
	PeakTimeS   *float64 `json:"peak_time_s"`
	OnsetTimeS  *float64 `json:"onset_time_s"`
	EndsetTimeS *float64 `json:"endset_time_s"`
	ReactionLoS *float64 `json:"reaction_lo_s"`
	ReactionHiS *float64 `json:"reaction_hi_s"`
	ValveTimeS  *float64 `json:"valve_time_s"`
	MassLossMg  Quantity `json:"mass_loss_mg"`
	MassLossPct Quantity `json:"mass_loss_pct"`
	Overlaps    bool     `json:"overlaps"`
	SpansGap    bool     `json:"spans_gap"`
	NonMonotone bool     `json:"non_monotone"`
	Ambiguous   bool     `json:"ambiguous"`
	Range       [2]int   `json:"range"`
	Rejected    []string `json:"rejected_reasons,omitempty"`
	SplitTag    string   `json:"split_tag,omitempty"`
}

// ChainStep is one line of an expandable residue calculation chain.
type ChainStep struct {
	Order      int      `json:"order"`
	Text       string   `json:"text"`
	ValueMg    *float64 `json:"value_mg,omitempty"`
	ValuePct   *float64 `json:"value_pct,omitempty"`
	RefKind    string   `json:"ref_kind,omitempty"`
	RefID      string   `json:"ref_id,omitempty"`
	RefVersion string   `json:"ref_version,omitempty"`
	Unknown    bool     `json:"unknown,omitempty"`
	Reason     string   `json:"reason,omitempty"`
}

// Residue is a residual-ratio node with a full provenance chain.
type Residue struct {
	AtTimeS *float64    `json:"at_time_s,omitempty"`
	Label   string      `json:"label"`
	MassMg  Quantity    `json:"mass_mg"`
	Percent Quantity    `json:"percent"`
	Unknown bool        `json:"unknown"`
	Chain   []ChainStep `json:"chain"`
}

// NodeVersion is one immutable computed version of a stable node identity.
type NodeVersion struct {
	NodeID           string          `json:"node_id"`
	VersionID        string          `json:"version_id"`
	Kind             string          `json:"kind"`
	Version          int             `json:"version"`
	Status           string          `json:"status"`
	AlgoVersion      string          `json:"algo_version"`
	ParamFingerprint string          `json:"param_fingerprint"`
	Deps             []DepRef        `json:"deps"`
	Data             json.RawMessage `json:"data"`
	Diagnostics      []Diagnostic    `json:"diagnostics,omitempty"`
	CreatedSeq       int64           `json:"created_seq"`
}

// Decision records a human verdict on a candidate cluster.
type Decision struct {
	ID          string  `json:"id"`
	Cluster     int     `json:"cluster"`
	NodeID      string  `json:"node_id"`
	NodeVersion string  `json:"node_version"`
	Kind        string  `json:"kind"`
	Reason      string  `json:"reason"`
	Actor       string  `json:"actor"`
	ParamsPatch *Params `json:"params_patch,omitempty"`
	CreatedSeq  int64   `json:"created_seq"`
	Reverted    bool    `json:"reverted,omitempty"`
}

// Event is the append-only history unit.
type Event struct {
	Seq       int64           `json:"seq"`
	ProjectID string          `json:"project_id"`
	Type      string          `json:"type"`
	Actor     string          `json:"actor"`
	At        string          `json:"at"`
	Payload   json.RawMessage `json:"payload"`
}

// Event types.
const (
	EvProjectCreated   = "project_created"
	EvSourceCommitted  = "source_committed"
	EvDerivedComputed  = "derived_computed"
	EvDecision         = "decision"
	EvDecisionConflict = "decision_conflict"
	EvDecisionReverted = "decision_reverted"
	EvImportPartial    = "import_partial"
	EvArchiveExported  = "archive_exported"
	EvArchiveImported  = "archive_imported"
	EvAnnotation       = "annotation"
)
