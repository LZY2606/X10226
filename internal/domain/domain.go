// Package domain defines the TGA review domain model: immutable imports,
// versioned derived artifacts, human decisions and the append-only event log.
package domain

import "time"

// Kind identifies a layered source input type.
type Kind string

const (
	KindSamples Kind = "samples"   // raw TG sample points
	KindDrift   Kind = "drift"     // balance drift / baseline calibration
	KindFurnace Kind = "furnace"   // furnace temperature program
	KindValves  Kind = "valves"    // gas valve events
	KindBlank   Kind = "blank"     // blank (empty-pan) curve
	KindM0      Kind = "m0"        // layered initial sample mass readings
)

func (k Kind) Valid() bool {
	switch k {
	case KindSamples, KindDrift, KindFurnace, KindValves, KindBlank, KindM0:
		return true
	}
	return false
}

// RowStatus marks the parse/normalization verdict for a single raw row.
type RowStatus string

const (
	RowOK          RowStatus = "ok"          // fully normalized
	RowUnconverted RowStatus = "unconverted" // kept in place, unit/conversion failed
	RowInvalid     RowStatus = "invalid"     // structurally bad; kept with diagnosis
	RowMissing     RowStatus = "missing"     // a time gap marker produced at import
)

// Q is a physical quantity carrying its original representation plus a
// normalized value in a documented reference system.
type Q struct {
	Original string  `json:"original"`        // exact text as imported
	Unit     string  `json:"unit"`            // declared unit token (original)
	Value    float64 `json:"value,omitempty"` // normalized numeric value
	NormUnit string  `json:"norm_unit,omitempty"`
	Basis    string  `json:"basis,omitempty"` // coordinate / reference basis
	OK       bool    `json:"ok"`              // conversion succeeded
	Diag     string  `json:"diag,omitempty"`  // diagnosis when OK == false
}

// SamplePoint is one normalized TG sample. Gaps are never interpolated:
// a point after a gap is tagged GapAfter.
type SamplePoint struct {
	UID        string  `json:"uid"`
	T          float64 `json:"t"`              // furnace time, s (basis: furnace_clock)
	RawMass    Q       `json:"raw_mass"`
	RawTemp    Q       `json:"raw_temp"`
	Mass       float64 `json:"mass"`           // normalized mg, corrected after drift
	Temp       float64 `json:"temp"`           // normalized deg C (furnace_program)
	Spike      bool    `json:"spike,omitempty"`
	GapAfter   bool    `json:"gap_after,omitempty"`
	GapBefore  bool    `json:"gap_before,omitempty"`
	Status     RowStatus `json:"status"`
	Diag       string  `json:"diag,omitempty"`
}

// DriftPoint is a balance drift/baseline calibration point.
type DriftPoint struct {
	UID    string `json:"uid"`
	T      float64 `json:"t"` // s
	Offset Q      `json:"offset"`
	Status RowStatus `json:"status"`
	Diag   string  `json:"diag,omitempty"`
}

// FurnaceSegment is one programmed segment of the furnace profile.
type FurnaceSegment struct {
	UID    string  `json:"uid"`
	StartT float64 `json:"start_t"` // s
	EndT   float64 `json:"end_t"`
	T0     float64 `json:"t0"` // deg C at start
	T1     float64 `json:"t1"`
	Rate   float64 `json:"rate,omitempty"` // K/min, informational
}

// ValveEvent is a gas valve switch logged on the controller clock.
type ValveEvent struct {
	UID      string  `json:"uid"`
	T        float64 `json:"t"` // valve/controller clock, s
	FromGas  string  `json:"from_gas"`
	ToGas    string  `json:"to_gas"`
	Note     string  `json:"note,omitempty"`
}

// BlankPoint is one blank (empty pan) measurement point.
type BlankPoint struct {
	UID  string  `json:"uid"`
	T    float64 `json:"t"`
	Mass Q       `json:"mass"`
	Temp Q       `json:"temp,omitempty"`
	Status RowStatus `json:"status"`
	Diag string  `json:"diag,omitempty"`
}

// M0Layer is one layered initial-mass reading.
type M0Layer struct {
	UID    string `json:"uid"`
	Layer  string `json:"layer"` // tare / buoyancy_corrected / reported_initial ...
	Mass   Q      `json:"mass"`
	At     float64 `json:"at"` // furnace time s of the weighing
	Status RowStatus `json:"status"`
	Diag   string `json:"diag,omitempty"`
}

// Import is an immutable submitted input bundle (one kind, one new version).
type Import struct {
	UID       string    `json:"uid"`
	ProjectID string    `json:"project_id"`
	Kind      Kind      `json:"kind"`
	ParentUID string    `json:"parent_uid,omitempty"` // previous version of same kind
	Label     string    `json:"label"`
	MediaType string    `json:"media_type"` // text/csv
	RawText   string    `json:"-"`          // stored separately, never overwritten
	Status    string    `json:"status"`     // complete | partial
	Submitted time.Time `json:"submitted"`  // virtual clock
	Seq       int64     `json:"seq"`        // event/version clock
	Diag      string    `json:"diag,omitempty"`
}

// ImportRow keeps every original row, even rejected ones, with its verdict.
type ImportRow struct {
	UID      string    `json:"uid"`
	ImportID string    `json:"import_id"`
	LineNo   int       `json:"line_no"`
	RawLine  string    `json:"raw_line"`
	Status   RowStatus `json:"status"`
	Diag     string    `json:"diag,omitempty"`
}

// Params are analysis parameters. Every revision is fingerprinted and
// versioned; the parent chain preserves history.
type Params struct {
	UID       string  `json:"uid"`
	ParentUID string  `json:"parent_uid,omitempty"`
	Seq       int64   `json:"seq"`
	// GasDelayS maps atmosphere label to additional transport delay (s):
	// reaction_time = valve_time + delay.
	GasDelayS map[string]float64 `json:"gas_delay_s"`
	// TempLagS is the sample-vs-furnace thermal lag model (first-order, s).
	TempLagS float64 `json:"temp_lag_s"`
	// DTG smoothing window (s) and stage prominence threshold (mg/min).
	SmoothWindowS float64 `json:"smooth_window_s"`
	ProminencePct float64 `json:"prominence_pct"` // of total observed loss
	Note          string  `json:"note,omitempty"`
	Fingerprint   string  `json:"fingerprint"`
	IssuedAt      time.Time `json:"issued_at"`
}

// ValueNode is a stable, identity-bearing calculated value with its chain.
type ValueNode struct {
	UID        string       `json:"uid"`
	Path       string       `json:"path"` // e.g. stages.s3.residue_pct
	Label      string       `json:"label"`
	Value      string       `json:"value"`       // display representation
	Unit       string       `json:"unit,omitempty"`
	Status     string       `json:"status"`      // ok | unknown | error
	Algorithm  string       `json:"algorithm"`
	AlgoVer    string       `json:"algo_ver"`
	ParamFP    string       `json:"param_fp"`
	Sources    []SourceRef  `json:"sources,omitempty"`
	Inputs     []ValueInput `json:"inputs,omitempty"`
	Diag       string       `json:"diag,omitempty"`
	CreatedSeq int64        `json:"created_seq"`
}

type SourceRef struct {
	Kind     string `json:"kind"`     // import | params | artifact | decision
	UID      string `json:"uid"`
	Label    string `json:"label,omitempty"`
	LineHint int    `json:"line_hint,omitempty"`
}

type ValueInput struct {
	Path  string `json:"path"`
	UID   string `json:"uid"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

// Stage is one candidate decomposition stage. Stages may overlap and their
// losses are never forced to sum to 100%.
type Stage struct {
	UID           string  `json:"uid"`
	Index         int     `json:"index"`
	StartT        float64 `json:"start_t"`
	EndT          float64 `json:"end_t"`
	PeakT         float64 `json:"peak_t"`
	PeakSampleT   float64 `json:"peak_sample_temp"`
	StartMass     float64 `json:"start_mass"`
	EndMass       float64 `json:"end_mass"`
	LossMg        float64 `json:"loss_mg"`
	LossPctOfM0   float64 `json:"loss_pct_of_m0"`
	Atmosphere    string  `json:"atmosphere"`
	ValveT        float64 `json:"valve_t,omitempty"`
	OverlapsGroup []int   `json:"overlaps_group,omitempty"`
	NoiseSpike    bool    `json:"noise_spike,omitempty"`
	Status        string  `json:"status"` // candidate
	Diag          string  `json:"diag,omitempty"`
}

// Decision is an immutable human verdict on a candidate/result.
type Decision struct {
	UID        string    `json:"uid"`
	TargetPath string    `json:"target_path"` // stages.s3 / final_residue ...
	TargetUID  string    `json:"target_uid"`
	Verdict    string    `json:"verdict"`     // accept | reject | adjust
	Comment    string    `json:"comment,omitempty"`
	Adjustment string    `json:"adjustment,omitempty"` // e.g. {"end_t": 4200}
	Seq        int64     `json:"seq"`
	MadeAt     time.Time `json:"made_at"`
	// Provenance snapshot taken at decision time.
	ArtifactUID string `json:"artifact_uid"`
	ParamFP     string `json:"param_fp"`
	SourcesHash string `json:"sources_hash"`
	Superseded  bool   `json:"superseded,omitempty"` // a later decision exists
}

// Derived is a content-addressed version of the whole analysis output set.
type Derived struct {
	UID        string                 `json:"uid"` // hash of canonical payload
	Seq        int64                  `json:"seq"`
	ParamUID   string                 `json:"param_uid"`
	ParamFP    string                 `json:"param_fp"`
	Sources    []SourceRef            `json:"sources"`
	Points     []SamplePoint          `json:"points"`
	Blank      []BlankPoint           `json:"blank,omitempty"`
	Drift      []DriftPoint           `json:"drift,omitempty"`
	Stages     []Stage                `json:"stages"`
	Values     []ValueNode            `json:"values"`
	AtmTimeline[]AtmWindow            `json:"atm_timeline"`
	Furnace    []FurnaceSegment        `json:"furnace"`
	Unknowns   []Unknown              `json:"unknowns,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
}

// AtmWindow is an atmosphere interval on the *sample reaction* clock.
type AtmWindow struct {
	StartT   float64 `json:"start_t"` // reaction clock, s
	EndT     float64 `json:"end_t"`
	Gas      string  `json:"gas"`
	ValveT   float64 `json:"valve_t"` // originating valve time
	DelayS   float64 `json:"delay_s"`
}

// Unknown records an ambiguity with all legal alternatives kept.
type Unknown struct {
	Path     string   `json:"path"`
	Reason   string   `json:"reason"`
	Options  []string `json:"options"`
	Affects  []string `json:"affects"`
}

// Event is one append-only history record.
type Event struct {
	Seq       int64     `json:"seq"`
	At        time.Time `json:"at"`
	Type      string    `json:"type"`
	UID       string    `json:"uid"` // entity uid the event refers to
	Payload   string    `json:"payload,omitempty"` // short human/JSON note
	ProjectID string    `json:"project_id"`
}

// Event types.
const (
	EvImportSubmitted = "import.submitted"
	EvImportRejected  = "import.row_rejected"
	EvParamsIssued    = "params.issued"
	EvDerivedBuilt    = "derived.built"
	EvDecisionMade    = "decision.made"
	EvConflictResolved= "conflict.resolved"
	EvCaseReplay      = "case.replay_step"
	EvProjectCreated  = "project.created"
	EvBundleImported  = "bundle.imported"
)

// Project metadata.
type Project struct {
	UID       string    `json:"uid"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Clock     string    `json:"clock"` // virtual|real, informational
}
