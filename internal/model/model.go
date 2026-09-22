// Package model holds the domain language of the TGA review system:
// immutable layer versions, parameter sets, derived artifacts, decisions,
// judgments and the counterexample archive. Nothing here depends on SQLite
// or HTTP, so the state machine is testable in isolation.
package model

// Layer kinds: the six stratified inputs imported by the thermal-analysis
// team. Raw layers are append-only; a revision is a new LayerVersion with a
// parent pointer, never an overwrite.
const (
	LayerSamples   = "samples"   // raw TGA sampling points (mass, temperature, furnace time)
	LayerDrift     = "drift"     // balance drift correction parameters
	LayerBlank     = "blank"     // blank-curve sampling points
	LayerFurnace   = "furnace"   // furnace program segments
	LayerValves    = "valves"    // gas valve switching events
	LayerInitial   = "initial"   // sample initial mass (revisable)
)

var LayerKinds = []string{LayerSamples, LayerDrift, LayerBlank, LayerFurnace, LayerValves, LayerInitial}

// Import status of a layer version. "partial" means some submitted records
// could not be parsed/measured: the usable records remain, the unusable ones
// stay in place with diagnostics, and derived results declare the gap.
const (
	ImportOK      = "ok"
	ImportPartial = "partial"
	ImportFailed  = "failed" // the whole submitted file was rejected
)

type Diagnostic struct {
	Ref     string `json:"ref"`     // stable id of the offending raw record / file
	Field   string `json:"field"`   // column or "<file>"
	Raw     string `json:"raw"`     // original textual representation, verbatim
	Code    string `json:"code"`    // e.g. non_numeric, unit_unknown, non_monotonic_time
	Message string `json:"message"` // human readable explanation
}

type Record struct {
	ID  string            `json:"id"`  // stable within the project: rec_<hash>
	Row int               `json:"row"` // 1-based source row for traceability
	OK  bool              `json:"ok"`
	// Raw preserves every submitted cell as text. Values are never dropped
	// just because they cannot be plotted.
	Raw map[string]string `json:"raw"`
	// Parsed holds typed, normalized values for cells we could convert.
	Parsed map[string]float64 `json:"parsed,omitempty"`
	Unit   map[string]string  `json:"unit,omitempty"`
	Reason string             `json:"reason,omitempty"` // why OK == false
}

type LayerVersion struct {
	UID      string       `json:"uid"` // lyr_<hash(project,kind,version)>
	Project  string       `json:"project"`
	Kind     string       `json:"kind"`
	Version  int          `json:"version"`
	Parent   string       `json:"parent,omitempty"` // previous LayerVersion.UID
	Source   string       `json:"source"`           // file name / bundle ref
	SHA256   string       `json:"sha256"`           // fingerprint of the submitted bytes
	Status   string       `json:"status"`
	Records  []Record     `json:"records,omitempty"`
	Diags    []Diagnostic `json:"diagnostics,omitempty"`
	// Basis records the coordinate basis for the layer, e.g.
	// time_basis="furnace_elapsed_min", mass_basis="balance_mg".
	Basis map[string]string `json:"basis,omitempty"`
}

// ---- Parameter sets -----------------------------------------------------
// Parameter revisions are versioned, fingerprinted and carry the algorithm
// version that consumes them.

type DriftParams struct {
	Enabled    bool    `json:"enabled"`
	BlankGain  float64 `json:"blank_gain"`            // blank curve multiplier
	Offset     float64 `json:"offset_mg"`             // constant tare, mg
	Algorithm  string  `json:"algorithm"`             // alg version consuming params
	Fingerprint string `json:"fingerprint,omitempty"`
}

type GapParams struct {
	MaxStepMin float64 `json:"max_step_min"` // larger time deltas open a gap
	Algorithm  string  `json:"algorithm"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type NoiseParams struct {
	// Non-monotonic excursions above this (relative, mg/mg) are flagged
	// rather than silently smoothed.
	RelTolerance float64 `json:"rel_tolerance"`
	Algorithm    string  `json:"algorithm"`
	Fingerprint  string  `json:"fingerprint,omitempty"`
}

type PeakParams struct {
	Prominence float64 `json:"prominence_mg_per_min"` // min DTG prominence
	WindowMin  float64 `json:"window_min"`            // smoothing window used ONLY for
	// prominence estimation, never to replace raw DTG
	Algorithm   string `json:"algorithm"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

type AtmosphereParams struct {
	// Delay between a valve switch and the gas front reaching the sample.
	GasDelayMin float64 `json:"gas_delay_min"`
	// Lag between program temperature and the sample temperature sensor /
	// reaction zone. Valve time is therefore NOT the reaction time.
	ThermalLagMin float64 `json:"thermal_lag_min"`
	Algorithm     string  `json:"algorithm"`
	Fingerprint   string  `json:"fingerprint,omitempty"`
}

type ParamSet struct {
	UID        string            `json:"uid"` // par_<hash>
	Version    int               `json:"version"`
	Parent     string            `json:"parent,omitempty"`
	Drift      DriftParams       `json:"drift"`
	Gap        GapParams         `json:"gap"`
	Noise      NoiseParams       `json:"noise"`
	Peak       PeakParams        `json:"peak"`
	Atmosphere AtmosphereParams  `json:"atmosphere"`
	Reason     string            `json:"reason,omitempty"`
}

func DefaultParams(uid string) ParamSet {
	return ParamSet{
		UID: uid,
		Drift: DriftParams{Enabled: true, BlankGain: 1.0, Offset: 0,
			Algorithm: AnalysisAlgVersion},
		Gap:        GapParams{MaxStepMin: 1.5, Algorithm: AnalysisAlgVersion},
		Noise:      NoiseParams{RelTolerance: 0.002, Algorithm: AnalysisAlgVersion},
		Peak:       PeakParams{Prominence: 0.10, WindowMin: 2.0, Algorithm: AnalysisAlgVersion},
		Atmosphere: AtmosphereParams{GasDelayMin: 0, ThermalLagMin: 0, Algorithm: AnalysisAlgVersion},
	}
}

// AnalysisAlgVersion is the single version stamp of the pure analysis
// pipeline. Changing an algorithm bumps this, which changes derived
// fingerprints and therefore marks older derived results as from an older
// rule version without deleting them.
const AnalysisAlgVersion = "tga-analysis-1.0.0"

// ---- Artifacts -----------------------------------------------------------

const (
	ArtCorrectedTG = "corrected_tg" // drift-corrected, gap-split sample TG
	ArtDTG         = "dtg"          // derivative mass/time per segment
	ArtStages      = "stages"       // candidate decomposition stages
	ArtResiduals   = "residuals"    // residual-rate chain (multiple nodes)
	ArtValveEff    = "valve_effective" // effective atmosphere timeline
	ArtProgramT    = "program_sample_t" // program mapped to sample time
	ArtMassFrac    = "mass_fraction" // normalized TG / initial mass
)

// Point is a single display sample with a stable identity derived from its
// series uid and its (segment,index) position. Each plotted value can
// therefore be cited and traced back.
type Point struct {
	UID    string  `json:"uid"`
	TMin   float64 `json:"t_min"`
	TUnit  string  `json:"t_unit"`
	XLabel string  `json:"x_label,omitempty"` // furnace time coordinate basis
	Y      float64 `json:"y"`
	YUnit  string  `json:"y_unit"`
	Seg    int     `json:"seg"` // sampling segment; gaps increment this
	Flags  []string `json:"flags,omitempty"` // nonmonotonic, gap_edge, noisy
	RawRef string  `json:"raw_ref,omitempty"` // originating Record.ID
}

type Stage struct {
	UID          string   `json:"uid"`
	Index        int      `json:"index"`
	StartMin     float64  `json:"start_min"`
	EndMin       float64  `json:"end_min"`
	PeakMin      float64  `json:"peak_min"`
	PeakTempC    float64  `json:"peak_temp_c"`
	DTGPeak      float64  `json:"dtg_peak_mg_per_min"`
	StartMassMg  float64  `json:"start_mass_mg"`
	EndMassMg    float64  `json:"end_mass_mg"`
	LossMg       float64  `json:"loss_mg"`
	LossPct      *float64 `json:"loss_pct,omitempty"` // nil when initial mass unknown/revised away
	OverlapsWith []int    `json:"overlaps_with,omitempty"`
	GasAtStart   string   `json:"gas_at_start"`
	GasAtPeak    string   `json:"gas_at_peak"`
	// Effective (reaction-time) atmosphere window, distinct from valve time.
	EffStartMin float64 `json:"eff_start_min"`
	EffEndMin   float64 `json:"eff_end_min"`
	Certain     string  `json:"certain"` // certain | uncertain | unknown
	Reasons     []string `json:"reasons,omitempty"`
	Status      string  `json:"status"` // candidate
	GapLimited  bool    `json:"gap_limited,omitempty"`
}

type ChainNode struct {
	UID    string            `json:"uid"`
	Label  string            `json:"label"`
	Value  string            `json:"value"` // display text with unit
	Unit   string            `json:"unit,omitempty"`
	Number *float64          `json:"number,omitempty"`
	Source string            `json:"source"` // uid of upstream artifact/record
	Note   string            `json:"note,omitempty"`
}

type ResidualChain struct {
	UID         string      `json:"uid"`
	Label       string      `json:"label"`
	AtMin       float64     `json:"at_min"`
	WindowMin   float64     `json:"window_min"`
	ValuePct    *float64    `json:"value_pct"`
	Unit        string      `json:"unit"`
	Certain     string      `json:"certain"`
	Reasons     []string    `json:"reasons,omitempty"`
	Chain       []ChainNode `json:"chain"`
}

type ValveWindow struct {
	UID        string  `json:"uid"`
	ValveAtMin float64 `json:"valve_at_min"`
	EffAtMin   float64 `json:"eff_at_min"`
	Gas        string  `json:"gas"`
	DelayMin   float64 `json:"delay_min"`
}

type Artifact struct {
	UID       string         `json:"uid"`
	Project   string         `json:"project"`
	Kind      string         `json:"kind"`
	Alg       string         `json:"alg"`
	ParamUID  string         `json:"param_uid"`
	ParamsFP  string         `json:"params_fp"`
	Direct    []string       `json:"direct"`     // direct source layer/param uids
	DependsOn []string       `json:"depends_on"` // upstream artifact uids
	Series    []Point        `json:"series,omitempty"`
	Stages    []Stage        `json:"stages,omitempty"`
	Residuals []ResidualChain `json:"residuals,omitempty"`
	Valves    []ValveWindow  `json:"valves,omitempty"`
	Status    string         `json:"status"` // current | uncertain
	Notes     []string       `json:"notes,omitempty"`
}

// ---- Decisions, judgments, counterexamples ------------------------------

const (
	DecisionAccepted = "accepted"
	DecisionRejected = "rejected"
	DecisionAdjusted = "adjusted"
	DecisionRetracted = "retracted"
	DecisionConflict = "conflict"
)

type Decision struct {
	UID        string `json:"uid"`
	TargetUID  string `json:"target"`  // artifact / stage / residual uid
	TargetKind string `json:"target_kind"`
	Verdict    string `json:"verdict"` // accepted|rejected|adjusted|retracted|conflict
	Note       string `json:"note"`
	ParamsUID  string `json:"params_uid,omitempty"` // for adjustments: new param set
	ConflictsWith string `json:"conflicts_with,omitempty"`
}

// Judgment is a machine-readable statement emitted by the analysis pipeline.
// It is how the domain rules (atmosphere delay, non-summing stages, no
// interpolation across gaps) become first-class, queryable evidence instead
// of prose.
type Judgment struct {
	Code      string   `json:"code"`
	Severity  string   `json:"severity"` // info|warning|unknown
	Subject   string   `json:"subject"`  // uid the statement is about
	Statement string   `json:"statement"`
	Rule      string   `json:"rule"` // e.g. atmosphere.delay.v1
	Refs      []string `json:"refs,omitempty"`
}

const (
	CounterAtmosphereDelay = "atmosphere_delay"
	CounterDrift           = "drift_baseline"
	CounterOverlap         = "overlapping_peaks"
	CounterNonmonotonic    = "nonmonotonic_noise"
	CounterGap             = "sampling_gap"
	CounterMassRevision    = "initial_mass_revision"
	CounterPartialImport   = "partial_import"
)

type CounterExample struct {
	UID            string            `json:"uid"`
	Scenario       string            `json:"scenario"`
	Project        string            `json:"project"` // branch project uid
	DirectSource   string            `json:"direct_source_changed"`
	Affected       []string          `json:"affected"`
	Unaffected     []string          `json:"unaffected"`
	ExpectedScope  []string          `json:"expected_scope"`
	BlastOK        bool              `json:"blast_radius_confined"`
	Adopted        bool              `json:"adopted"` // adopted vs. rejected result both kept
	Verdict        string            `json:"review_verdict"`
	RejectionNote  string            `json:"rejection_note,omitempty"`
	Judgments      []Judgment        `json:"judgments,omitempty"`
}
