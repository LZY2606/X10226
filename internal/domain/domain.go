// Package domain defines the core TGA review model: layered sources,
// versioned provenance, derived artifacts with fingerprints, and the
// append-only event ledger.
package domain

import "encoding/json"

// Algorithm versions. Bump when a derived-layer algorithm changes so old
// artifacts are never silently re-interpreted with new semantics.
const (
	AlgoNormalize = "normalize/v1"
	AlgoDrift     = "drift-correct/v1"
	AlgoDTG       = "dtg/v1"
	AlgoPeaks     = "peak-detect/v1"
	AlgoStages    = "stage-decompose/v1"
	AlgoAtmos     = "atmosphere-map/v1"
	AlgoResidue   = "residue/v1"

	// GapThresholdSec defines when adjacent furnace-time samples are treated
	// as a sampling gap: no DTG value, interpolation, or stage boundary may
	// ever cross such a gap.
	GapThresholdSec = 30.0
)

// Source layers. Every layer is stored independently; a failure in one
// layer never discards the others.
const (
	LayerSamples   = "samples"    // raw sampled points: furnace time, temperature, mass
	LayerDrift     = "drift"      // balance drift correction specification
	LayerFurnace   = "furnace"    // furnace temperature program
	LayerGas       = "gas"        // gas valve events
	LayerBlank     = "blank"      // blank/baseline curve
	LayerInitial   = "initial"    // stratified initial sample mass
	LayerParams    = "params"     // analysis parameters (optional, revisioned)
)

// Layers in display/storage order.
var Layers = []string{
	LayerSamples, LayerDrift, LayerFurnace, LayerGas, LayerBlank, LayerInitial, LayerParams,
}

// Artifact kinds emitted by the derived layer.
const (
	ArtNormalized = "normalized" // unit-normalized raw series + diagnostics
	ArtCorrected  = "corrected"  // drift-corrected mass series
	ArtDTG        = "dtg"        // derivative mass loss
	ArtPeaks      = "peaks"      // DTG peak candidates incl. overlapping alternatives
	ArtAtmos      = "atmosphere" // reaction-window mapping after delays/lag
	ArtStages     = "stages"     // stage decomposition candidates + residue
)

// Diagnostic severity.
const (
	SevInfo    = "info"
	SevWarning = "warning"
	SevError   = "error"
)

// Value statuses. Unknown/unconvertible values stay in place as first-class
// data rather than being dropped for plotting.
const (
	StatusOK      = "ok"
	StatusUnknown = "unknown"
	StatusError   = "error"
)

// Decision outcomes.
const (
	DecisionAccept  = "accept"
	DecisionReject  = "reject"
	DecisionAdjust  = "adjust"
	DecisionResolve = "resolve_conflict"
)

// Event types in the append-only ledger.
const (
	EvProjectCreated = "project_created"
	EvImport         = "import"          // a (possibly partial) layered import
	EvAnalyze        = "analyze"         // derived artifacts produced
	EvDecision       = "decision"        // human accept/reject/adjust
	EvConflict       = "conflict"        // contradiction detected
	EvUndo           = "undo"            // a decision reversed, recorded
	EvExport         = "export"          // bundle exported (audit)
	EvNote           = "note"
)

// Unit is a domain unit marker. All domain quantities carry either a unit
// or an explicit coordinate basis.
type Unit string

const (
	UnitMG       Unit = "mg"
	UnitG        Unit = "g"
	UnitUG       Unit = "ug"
	UnitCelsius  Unit = "celsius"
	UnitKelvin   Unit = "kelvin"
	UnitSeconds  Unit = "s"
	UnitMinutes  Unit = "min"
	UnitPercent  Unit = "percent"
	UnitMGperMin Unit = "mg/min"
	UnitPctperMin Unit = "percent/min"
	UnitSCCM     Unit = "sccm"
	UnitNone     Unit = "none"
)

// Number is a quantity that preserves the original textual representation.
// Normalization to a canonical unit happens only in the derived layer; if
// conversion fails, Value stays 0, Status=error and the original is kept.
type Number struct {
	Raw      string `json:"raw"`
	Value    float64 `json:"value"`
	Unit     Unit   `json:"unit"`
	Status   string `json:"status"`
	DiagCode string `json:"diag_code,omitempty"`
	DiagMsg  string `json:"diag_msg,omitempty"`
}

// Diagnostic is an import- or analysis-level finding.
type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Layer    string `json:"layer,omitempty"`
	Ref      string `json:"ref,omitempty"` // stable identity of affected object
}

// RawPoint is one original sampling row. The original strings are kept
// verbatim; parsed/normalized values live alongside, never replacing them.
type RawPoint struct {
	ID        string `json:"id"` // stable, content-derived: <sourceID>#p<seq>
	Seq       int    `json:"seq"`
	TimeRaw   string `json:"time_raw"`
	TempRaw   string `json:"temp_raw"`
	MassRaw   string `json:"mass_raw"`
	Time      Number `json:"time"`
	Temp      Number `json:"temp"`
	Mass      Number `json:"mass"`
	GapAfter  bool   `json:"gap_after,omitempty"`
	NonMono   bool   `json:"non_monotonic,omitempty"` // mass rose above noise floor
}

// DriftSpec describes balance baseline drift correction.
type DriftSpec struct {
	ID        string  `json:"id"`
	Model     string  `json:"model"`      // "blank_subtract" | "linear" | "none"
	RateMgMin float64 `json:"rate_mg_min,omitempty"` // linear drift rate
	BlankID   string  `json:"blank_id,omitempty"`    // direct source blank record
}

// BlankPoint is one blank-curve sampling point (time, mass).
type BlankPoint struct {
	ID      string  `json:"id"`
	Seq     int     `json:"seq"`
	TimeSec float64 `json:"time_s"`
	MassMg  float64 `json:"mass_mg"`
}

// FurnaceSegment is one program segment: hold or ramp to target temperature.
type FurnaceSegment struct {
	ID        string  `json:"id"`
	Seq       int     `json:"seq"`
	Kind      string  `json:"kind"` // "ramp" | "hold"
	StartSec  float64 `json:"start_s"`
	EndSec    float64 `json:"end_s"`
	FromC     float64 `json:"from_c"`
	ToC       float64 `json:"to_c"`
	RateCMin  float64 `json:"rate_c_min,omitempty"`
}

// GasEvent is a gas valve switch at the valve. Valve time != reaction time.
type GasEvent struct {
	ID           string  `json:"id"`
	Seq          int     `json:"seq"`
	TimeSec      float64 `json:"time_s"` // valve switch time
	Gas          string  `json:"gas"`    // N2, air, O2 ...
	FlowSCCM     float64 `json:"flow_sccm,omitempty"`
	Note         string  `json:"note,omitempty"`
}

// InitialMass is the stratified initial sample mass record.
type InitialMass struct {
	ID       string  `json:"id"`
	MassMg   float64 `json:"mass_mg"`
	Basis    string  `json:"basis"` // "tare" | "instrument" | "revised"
	AtSec    float64 `json:"at_s"`
	Note     string  `json:"note,omitempty"`
}

// Params are revisioned analysis parameters.
type Params struct {
	ID              string  `json:"id"`
	AtmosDelaySec   float64 `json:"atmos_delay_s"`    // gas travel delay
	TempLagSec      float64 `json:"temp_lag_s"`       // furnace->sample temperature lag
	OverlapMergeC   float64 `json:"overlap_merge_c"`  // peaks closer than this may merge
	NoiseFloorMG    float64 `json:"noise_floor_mg"`   // non-monotonic threshold
	PeakPromMGMin   float64 `json:"peak_prom_mg_min"` // minimum DTG prominence
}

// Source is one submitted, immutable input layer version.
type Source struct {
	ID        string                 `json:"id"` // proj/S<seq>-<layer>
	ProjectID string                 `json:"project_id"`
	Layer     string                 `json:"layer"`
	Version   int                    `json:"version"`
	ParentID  string                 `json:"parent_id,omitempty"`
	EventID   string                 `json:"event_id"`
	SubmittedAtStep int64            `json:"submitted_at_step"`
	Fingerprint string               `json:"fingerprint"`
	Raw       json.RawMessage        `json:"raw"` // original payload fragment, verbatim
	Status    string                 `json:"status"` // "accepted" | "partial" | "rejected"
	Diagnostics []Diagnostic         `json:"diagnostics,omitempty"`
}

// Event is one append-only ledger entry.
type Event struct {
	ID        string          `json:"id"` // proj/E<seq>
	ProjectID string          `json:"project_id"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	AtStep    int64           `json:"at_step"` // virtual clock
	ParentID  string          `json:"parent_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

// Run is one derived-layer execution over a pinned set of source versions.
type Run struct {
	ID        string         `json:"id"` // proj/R<seq>
	ProjectID string         `json:"project_id"`
	EventID   string         `json:"event_id"`
	AtStep    int64          `json:"at_step"`
	Basis     map[string]string `json:"basis"` // layer -> sourceID pinned
	AlgoVers  map[string]string `json:"algo_vers"`
}

// Ref is a stable identity attached to every displayed value: it names the
// object, the artifact version and the direct source so any value can be
// traced back to inputs, steps and human decisions.
type Ref struct {
	ID         string `json:"id"`          // stable within artifact lineage
	ArtifactID string `json:"artifact_id"` // artifact version holding the value
	RunID      string `json:"run_id"`
	SourceIDs  []string `json:"source_ids,omitempty"`
	Algo       string `json:"algo,omitempty"`
}

// ValueRef pairs a display value with its stable identity, unit and status.
type ValueRef struct {
	Ref    Ref     `json:"ref"`
	Label  string  `json:"label"`
	Value  float64 `json:"value"`
	Unit   Unit    `json:"unit"`
	Status string  `json:"status"`
}

// CalcChainStep is one node in an expandable residue/value calculation chain.
type CalcChainStep struct {
	Order   int      `json:"order"`
	Op      string   `json:"op"`
	Detail  string   `json:"detail"`
	Inputs  []Ref    `json:"inputs,omitempty"`
	Output  ValueRef `json:"output"`
}

// Candidate is a stage decomposition candidate (first-class; alternatives
// and rejection reasons are retained, never silently discarded).
type Candidate struct {
	Ref            Ref            `json:"ref"`
	LocalID        string         `json:"local_id"` // e.g. stage-2, stable across runs
	StartSec       float64        `json:"start_s"`
	EndSec         float64        `json:"end_s"`
	StartC         float64        `json:"start_c"`
	EndC           float64        `json:"end_c"`
	PeakSec        float64        `json:"peak_s"`
	PeakC          float64        `json:"peak_c"`
	PeakRate       ValueRef       `json:"peak_rate"`
	MassLossMg     ValueRef       `json:"mass_loss_mg"`
	MassLossPct    ValueRef       `json:"mass_loss_pct"` // relative to chosen initial mass
	Atmosphere     string         `json:"atmosphere"`
	AtmosUncertain bool           `json:"atmos_uncertain"`
	Overlaps       []string       `json:"overlaps,omitempty"` // local ids overlapping in time
	CrossesGap     bool           `json:"crosses_gap,omitempty"`
	Status         string         `json:"status"` // candidate | accepted | rejected | adjusted | stale
	RejectReasons  []string       `json:"reject_reasons,omitempty"`
	Chain          []CalcChainStep `json:"chain,omitempty"`
}

// Residue is the final residue result with full calculation chain.
type Residue struct {
	Ref        Ref             `json:"ref"`
	AtSec      float64         `json:"at_s"`
	AtC        float64         `json:"at_c"`
	MassMg     ValueRef        `json:"mass_mg"`
	Percent    ValueRef        `json:"percent"`
	InitialID  string          `json:"initial_id"`
	Status     string          `json:"status"`
	Chain      []CalcChainStep `json:"chain"`
}

// Artifact is one versioned derived result.
type Artifact struct {
	ID         string `json:"id"` // proj/A<seq>
	ProjectID  string `json:"project_id"`
	RunID      string `json:"run_id"`
	EventID    string `json:"event_id"`
	Kind       string `json:"kind"`
	AlgoVer    string `json:"algo_ver"`
	ParamFP    string `json:"param_fp"`
	DependsOn  []string `json:"depends_on"` // direct source + artifact IDs
	Current    bool   `json:"current"`
	SupersededBy string `json:"superseded_by,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

// DecisionRecord is an immutable human decision.
type DecisionRecord struct {
	ID          string          `json:"id"` // proj/D<seq>
	ProjectID   string          `json:"project_id"`
	EventID     string          `json:"event_id"`
	AtStep      int64           `json:"at_step"`
	Outcome     string          `json:"outcome"`
	TargetRef   Ref             `json:"target_ref"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Undone      bool            `json:"undone"`
	UndoneBy    string          `json:"undone_by,omitempty"`
	Note        string          `json:"note,omitempty"`
}

// ConflictRecord captures contradictory human decisions.
type ConflictRecord struct {
	ID         string   `json:"id"`
	ProjectID  string   `json:"project_id"`
	EventID    string   `json:"event_id"`
	AtStep     int64    `json:"at_step"`
	ObjectRef  Ref      `json:"object_ref"`
	DecisionIDs []string `json:"decision_ids"`
	Reason     string   `json:"reason"`
	Resolved   bool     `json:"resolved"`
	ResolvedBy string   `json:"resolved_by,omitempty"`
	Resolution string   `json:"resolution,omitempty"`
}
