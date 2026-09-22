// Package types holds the shared domain model for the TGA review system:
// immutable layered source inputs, versioned parameters, derived runs with
// provenance, stage candidates and human decisions.
package types

// Source kinds kept in separate layers. Raw inputs are never overwritten; a
// revision creates a new version with a parent version.
const (
	SampleSeries    = "sample_series"    // raw sampled points: time, temperature, mass
	DriftCorrection = "drift_correction" // balance baseline drift correction
	FurnaceProgram  = "furnace_program"  // programmed temperature vs time
	GasValveEvents  = "gas_valve_events" // valve switch timeline
	BlankCurve      = "blank_curve"      // empty-pan baseline curve
	InitialMass     = "initial_mass"     // stratified sample initial mass record
)

// SourceKinds is the fixed list of layered input kinds.
var SourceKinds = []string{
	SampleSeries, DriftCorrection, FurnaceProgram,
	GasValveEvents, BlankCurve, InitialMass,
}

// FragmentStatus marks the import judgement of a single input fragment.
type FragmentStatus string

const (
	FragAccepted FragmentStatus = "accepted"
	FragRejected FragmentStatus = "rejected"
	FragPartial  FragmentStatus = "partial"
)

// Fragment is a single raw input record exactly as submitted. RawText is never
// rewritten; Diagnostics keeps per-record import problems.
type Fragment struct {
	LocalID     int               `json:"local_id"`
	RawText     string            `json:"raw_text"`
	Fields      map[string]string `json:"fields"`
	Status      FragmentStatus    `json:"status"`
	Diagnostics []string          `json:"diagnostics,omitempty"`
}

// SourceVersion is an immutable revision of one input layer. Parent points to
// the previous version of the same kind (empty for the first version).
type SourceVersion struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"project_id"`
	Kind       string     `json:"kind"`
	Seq        int        `json:"seq"`
	Parent     string     `json:"parent,omitempty"`
	Origin     string     `json:"origin"` // filename, paste label, casepack id ...
	SHA256     string     `json:"sha256"`
	ImportMode string     `json:"import_mode"` // all | partial_failed
	At         string     `json:"at"`          // virtual ISO-8601 time
	Fragments  []Fragment `json:"fragments"`
}

// Ruleset is a versioned, fingerprinted parameter/algorithm configuration.
type Ruleset struct {
	ID          string             `json:"id"`
	ProjectID   string             `json:"project_id"`
	Seq         int                `json:"seq"`
	Parent      string             `json:"parent,omitempty"`
	AlgoVersion string             `json:"algo_version"`
	Params      map[string]float64 `json:"params"`
	Fingerprint string             `json:"fingerprint"`
	Note        string             `json:"note,omitempty"`
	At          string             `json:"at"`
}

// Provenance attached to every derived object.
type Provenance struct {
	AlgoVersion string            `json:"algo_version"`
	ParamFP     string            `json:"param_fp"`
	RulesetID   string            `json:"ruleset_id"`
	RunID       string            `json:"run_id"`
	Direct      map[string]string `json:"direct"` // kind -> source version id
}

// Candidate is a proposed stage or residue value with its full computation
// chain. Status is one of proposed/accepted/rejected/superseded/ambiguous.
type Candidate struct {
	ID            string        `json:"id"`
	StageID       string        `json:"stage_id"`
	Kind          string        `json:"kind"` // stage | final_residue
	Status        string        `json:"status"`
	Value         float64       `json:"value"`
	Unit          string        `json:"unit"`
	Bounds        [2]float64    `json:"bounds,omitempty"` // min,max when ambiguous
	Alternatives  []Alternative `json:"alternatives,omitempty"`
	RejectReasons []string      `json:"reject_reasons,omitempty"`
	Chain         []ChainStep   `json:"chain"`
	Prov          Provenance    `json:"provenance"`
}

// Alternative is a legal candidate that cannot be uniquely decided.
type Alternative struct {
	StageID string  `json:"stage_id"`
	Value   float64 `json:"value"`
	Unit    string  `json:"unit"`
	Reason  string  `json:"reason"`
}

// ChainStep is one reproducible calculation step with inputs, operator and
// output, each display value carrying a stable identity.
type ChainStep struct {
	StepID string            `json:"step_id"`
	Op     string            `json:"op"`
	Inputs map[string]string `json:"inputs"`
	Output string            `json:"output"`
	Unit   string            `json:"unit,omitempty"`
	Note   string            `json:"note,omitempty"`
	Refs   []string          `json:"refs,omitempty"` // stable ids of source values
}

// Run is one derived computation pass over fixed source versions + ruleset.
type Run struct {
	ID          string            `json:"id"`
	ProjectID   string            `json:"project_id"`
	Seq         int               `json:"seq"`
	RulesetID   string            `json:"ruleset_id"`
	Sources     map[string]string `json:"sources"` // kind -> version id
	AlgoVersion string            `json:"algo_version"`
	ParamFP     string            `json:"param_fp"`
	At          string            `json:"at"`
	Status      string            `json:"status"` // ok | blocked | stale
	BlockedBy   []string          `json:"blocked_by,omitempty"`
	Summary     RunSummary        `json:"summary"`
	Candidates  []Candidate       `json:"candidates"`
	Series      map[string]Series `json:"series"` // tg, dtg, corrected ...
	Notes       []string          `json:"notes,omitempty"`
}

// RunSummary holds headline derived numbers.
type RunSummary struct {
	InitialMassMG   float64 `json:"initial_mass_mg"`
	FinalMassMG     float64 `json:"final_mass_mg"`
	FinalResiduePct float64 `json:"final_residue_pct"`
	StageCount      int     `json:"stage_count"`
	HasGap          bool    `json:"has_gap"`
	HasNonMonotonic bool    `json:"has_non_monotonic"`
	HasAmbiguity    bool    `json:"has_ambiguity"`
}

// Point is one displayable series value with a stable identity.
type Point struct {
	ID    string  `json:"id"`
	T     float64 `json:"t"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	Gap   bool    `json:"gap,omitempty"`
	Flags string  `json:"flags,omitempty"`
}

// Series is a named plotted curve with coordinate basis.
type Series struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	XUnit  string  `json:"x_unit"`
	YUnit  string  `json:"y_unit"`
	Points []Point `json:"points"`
}

// Decision is a human action on a candidate (accept/reject/adjust).
type Decision struct {
	ID          string  `json:"id"`
	ProjectID   string  `json:"project_id"`
	Seq         int     `json:"seq"`
	CandidateID string  `json:"candidate_id"`
	Action      string  `json:"action"` // accept | reject | adjust | conflict_resolved | undo
	Value       float64 `json:"value,omitempty"`
	Unit        string  `json:"unit,omitempty"`
	Reason      string  `json:"reason"`
	BasedOnRun  string  `json:"based_on_run"`
	Supersedes  string  `json:"supersedes,omitempty"`
	At          string  `json:"at"`
}

// Event kinds in the append-only log.
const (
	EvProjectCreated   = "project_created"
	EvSourceImported   = "source_imported"
	EvRulesetRevised   = "ruleset_revised"
	EvRunDerived       = "run_derived"
	EvDecision         = "decision"
	EvConflict         = "conflict"
	EvConflictResolved = "conflict_resolved"
	EvFinalized        = "finalized"
	EvReopened         = "reopened"
)

// Event is one append-only log entry. Payload is canonical JSON.
type Event struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Seq       int64  `json:"seq"`
	Kind      string `json:"kind"`
	At        string `json:"at"`
	Payload   string `json:"payload"`
}
