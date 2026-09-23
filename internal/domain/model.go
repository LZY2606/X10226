package domain

import "time"

// Source kinds: the six required stratified raw layers.
const (
	KindSamples     = "samples"      // raw sampling points (time, temperature, mass)
	KindDrift       = "drift"        // balance drift / baseline correction
	KindProgram     = "program"      // furnace temperature program
	KindGas         = "gas"          // gas valve switch events
	KindBlank       = "blank"        // blank curve
	KindInitialMass = "initial_mass" // sample initial mass (revised as new versions)
)

var SourceKinds = []string{KindSamples, KindDrift, KindProgram, KindGas, KindBlank, KindInitialMass}

// Project is the review container.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Nonce     string    `json:"nonce"`
	CreatedAt time.Time `json:"created_at"`
}

// Source is one immutable version of one raw-input layer. Once committed it is
// never overwritten; revisions are expressed as a new version with ParentID.
type Source struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"project_id"`
	Kind        string       `json:"kind"`
	Version     int          `json:"version"`
	ParentID    string       `json:"parent_id,omitempty"`
	Origin      string       `json:"origin"` // filename/section provenance
	SubmittedAt time.Time    `json:"submitted_at"`
	Data        *SourceData  `json:"data"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"` // row-level, non-fatal
}

// Diagnostic is attached to an exact location but never deletes the value.
type Diagnostic struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // error | warning
	Location string `json:"location"` // e.g. "line 12 field mass"
	Message  string `json:"message"`
}

// SourceData holds parsed-but-raw layers. Point fields keep original strings;
// normalization happens only in the derived layer.
type SourceData struct {
	Samples     []RawSample     `json:"samples,omitempty"`
	Drift       *DriftCorrection `json:"drift,omitempty"`
	Program     []ProgramSegment `json:"program,omitempty"`
	GasEvents   []GasEvent       `json:"gas_events,omitempty"`
	Blank       []RawSample      `json:"blank,omitempty"`
	InitialMass *InitialMass     `json:"initial_mass,omitempty"`
}

// RawSample is one original sampling point. Every numeric field keeps its raw text.
type RawSample struct {
	Line     int    `json:"line"`
	TimeRaw  string `json:"time_raw"`
	TempRaw  string `json:"temp_raw"`
	MassRaw  string `json:"mass_raw"`
	TimeUnit string `json:"time_unit"`
	TempUnit string `json:"temp_unit"`
	MassUnit string `json:"mass_unit"`
}

type DriftCorrection struct {
	Line    int             `json:"line"`
	Model   string          `json:"model"` // linear | points
	RateRaw string          `json:"rate_raw,omitempty"`
	RateUnit string         `json:"rate_unit,omitempty"` // mg/min
	Points  []DriftPoint    `json:"points,omitempty"`
	Basis   string          `json:"basis"` // coordinate basis note
}

type DriftPoint struct {
	Line    int    `json:"line"`
	TimeRaw string `json:"time_raw"`
	MassRaw string `json:"mass_raw"`
}

type ProgramSegment struct {
	Line     int    `json:"line"`
	Kind     string `json:"kind"` // ramp | hold
	ToTempRaw string `json:"to_temp_raw,omitempty"`
	RateRaw  string `json:"rate_raw,omitempty"` // °C/min
	HoldRaw  string `json:"hold_raw,omitempty"` // s
	StartTempRaw string `json:"start_temp_raw,omitempty"`
}

type GasEvent struct {
	Line    int    `json:"line"`
	TimeRaw string `json:"time_raw"`
	FromGas string `json:"from_gas"`
	ToGas   string `json:"to_gas"`
}

type InitialMass struct {
	Line    int    `json:"line"`
	MassRaw string `json:"mass_raw"`
	Unit    string `json:"unit"`
	Method  string `json:"method"` // how initial mass was established
	Reason  string `json:"reason,omitempty"` // why revised
}

// Event kinds.
const (
	EvtSourceCommitted = "source_committed"
	EvtImportRejected  = "import_rejected" // whole-section fatal parse error -> fragment kept
	EvtParamSet        = "params_set"
	EvtDecision        = "decision"
	EvtConflictResolve = "conflict_resolved"
	EvtReversal        = "reversal" // undo: reverses a prior decision/parameter event
)

// Event is the append-only history. Payload is canonical JSON; derived state is
// a fold of events. Reopening the project replays it step by step.
type Event struct {
	Seq        int64     `json:"seq"`
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	Kind       string    `json:"kind"`
	OccurredAt time.Time `json:"occurred_at"`
	Actor      string    `json:"actor"`
	Payload    string    `json:"payload"` // canonical JSON
	Refs       []string  `json:"refs,omitempty"` // source/derived/batch ids touched
}

// Payload types (serialized into Event.Payload).
type SourceCommittedPayload struct {
	BatchID  string `json:"batch_id"`
	SourceID string `json:"source_id"`
	Kind     string `json:"kind"`
	Version  int    `json:"version"`
	ParentID string `json:"parent_id,omitempty"`
	Origin   string `json:"origin"`
}

type ImportRejectedPayload struct {
	BatchID    string       `json:"batch_id"`
	FragmentID string       `json:"fragment_id"`
	KindHint   string       `json:"kind_hint"`
	Origin     string       `json:"origin"`
	RawSnippet string       `json:"raw_snippet"` // input fragment preserved
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Params are the review parameters. Fingerprint covers all fields + schema version.
type Params struct {
	SchemaVersion      string  `json:"schema_version"`
	AtmosDelaySec      float64 `json:"atmos_delay_s"`        // gas valve -> sample atmosphere lag
	TempLagSec         float64 `json:"temp_lag_s"`           // program -> sample temperature lag
	DriftEnabled       bool    `json:"drift_enabled"`
	BlankSubtract      bool    `json:"blank_subtract"`
	DTGWindowPoints    int     `json:"dtg_window_points"`    // smoothing/derivative window
	PeakProminencePct  float64 `json:"peak_prominence_pct"`  // minimum peak prominence (% mass)
	OverlapTolSec      float64 `json:"overlap_tol_s"`
	GapMaxSec          float64 `json:"gap_max_s"`            // gaps larger than this are NOT bridged
	NoiseMonotonicTol  float64 `json:"noise_monotonic_tol_mg"`
}

func DefaultParams() Params {
	return Params{
		SchemaVersion:     "params.v1",
		AtmosDelaySec:     45,
		TempLagSec:        30,
		DriftEnabled:      true,
		BlankSubtract:     true,
		DTGWindowPoints:   5,
		PeakProminencePct: 1.5,
		OverlapTolSec:     60,
		GapMaxSec:         600,
		NoiseMonotonicTol: 0.02,
	}
}

type ParamsPayload struct {
	Params      Params `json:"params"`
	Fingerprint string `json:"fingerprint"`
	ParentEvent int64  `json:"parent_event,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

const (
	VerdictAccept   = "accept"
	VerdictReject   = "reject"
	VerdictAdjust   = "adjust" // parameter adjustment requested against candidate
	StatusCandidate = "candidate"
	StatusAccepted  = "accepted"
	StatusRejected  = "rejected"
	StatusAmbiguous = "ambiguous" // not uniquely decidable: multiple legal candidates
	StatusConflict  = "conflict"
)

type DecisionPayload struct {
	TargetKind string  `json:"target_kind"` // stages | residue
	TargetID   string  `json:"target_id"`   // content-addressed derived candidate id
	Verdict    string  `json:"verdict"`
	Actor      string  `json:"actor"`
	Reason     string  `json:"reason"`
	Overrides  *Params `json:"overrides,omitempty"` // local parameter adjustment
}

type ConflictResolvePayload struct {
	TargetKind      string `json:"target_kind"`
	TargetID        string `json:"target_id"`
	WinnerEventSeq  int64  `json:"winner_event_seq"`
	Reason          string `json:"reason"`
}

type ReversalPayload struct {
	TargetEventSeq int64  `json:"target_event_seq"`
	Reason         string `json:"reason"`
}

// Derived kinds.
const (
	DerCorrectedSeries = "corrected_series"
	DerDTGSeries       = "dtg_series"
	DerStages          = "stages"
	DerResidue         = "residue"
)

const DerAlgoVersion = "tga-algo.v1"

// Derived is an immutable computed artifact. Its id is a function of
// (kind, algo version, parameter fingerprint, direct source version ids),
// so it is stable across export/re-import and shared across replays.
type Derived struct {
	ID              string   `json:"id"`
	ProjectID       string   `json:"project_id"`
	Kind            string   `json:"kind"`
	AlgoVersion     string   `json:"algo_version"`
	ParamFingerprint string  `json:"param_fingerprint"`
	InputIDs        []string `json:"input_ids"`
	ProducedByEvent int64    `json:"produced_by_event"`
	Payload         string   `json:"payload"`
}
