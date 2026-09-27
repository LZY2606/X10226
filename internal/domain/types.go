// Package domain defines the stable, append-only vocabulary of the TGA review
// system: raw inputs are immutable; revisions are new versions linked to a
// parent; derived values carry algorithm version, parameter fingerprint and
// direct source references.
package domain

// CoordinateSystem identifies the time/value basis attached to every sample.
// Raw seconds are "furnace seconds" as recorded; reaction seconds are a
// derived coordinate shifted by atmosphere/temperature lag.
type CoordinateSystem string

const (
	CoordFurnaceTimeS   CoordinateSystem = "furnace_time_s"
	CoordReactionTimeS  CoordinateSystem = "reaction_time_s"
	CoordValveTimeS     CoordinateSystem = "valve_time_s"
)

// Unit enumerates the physical units preserved on input.
type Unit string

const (
	UnitMilligram Unit = "mg"
	UnitGram      Unit = "g"
	UnitMicrogram Unit = "ug"
	UnitCelsius   Unit = "C"
	UnitKelvin    Unit = "K"
	UnitPercent   Unit = "%"
	UnitSeconds   Unit = "s"
	UnitSCCM      Unit = "sccm"
	UnitPerCelsius Unit = "1/C"
	UnitPerSecond  Unit = "1/s"
	UnitUnknown    Unit = "unknown"
)

// Kind enumerates the layered raw source types.
type Kind string

const (
	KindSamples    Kind = "samples"     // original sampling points (mass + temperature over furnace time)
	KindBlank      Kind = "blank"       // blank curve (baseline run without sample)
	KindProgram    Kind = "program"     // furnace program (temperature ramps/holds)
	KindValve      Kind = "valve"       // gas valve events
	KindInitialMass Kind = "initial_mass" // sample initial mass (stratified: reported/measured/adopted)
)

// Status values used across sources, batches and derived records.
type Status string

const (
	StatusAccepted   Status = "accepted"
	StatusRejected   Status = "rejected"
	StatusDiagnostic Status = "diagnostic" // parseable but unconvertible / flagged
	StatusPartial    Status = "partial"    // import half-failure
	StatusStale      Status = "stale"      // superseded derived result, still viewable
	StatusCurrent    Status = "current"
	StatusUnknown    Status = "unknown"    // not uniquely decidable
)

// Project is a review workspace.
type Project struct {
	ID        string
	Name      string
	CreatedAt string // RFC3339 virtual clock
	Seed      int64
}

// Batch groups a single import transaction. A batch is atomic per section:
// a malformed section is rejected while well-formed sections still land,
// producing a "partial" half-failure batch with diagnostics attached.
type Batch struct {
	ID        string
	ProjectID string
	Status    Status // accepted | partial | rejected
	Reason    string
	CreatedAt string
	Seq       int64
}

// Source is one immutable raw input layer. Revisions create a new row with
// ParentID set to the previous version of the same logical slot.
type Source struct {
	ID        string
	ProjectID string
	BatchID   string
	Kind      Kind
	Version   int
	ParentID  string // empty for the first version
	Status    Status // accepted | diagnostic (kept, not plotted) | rejected
	RawText   string // original representation, byte-for-byte
	Diagnostic string
	CreatedAt string
	Seq       int64
}

// Point is one original sampling triple. Values keep their recorded units;
// normalization to mg / C happens only in the derived layer.
type Point struct {
	SourceID string
	Idx      int
	TimeRaw  string
	TimeUnit Unit
	TempRaw  string
	TempUnit Unit
	MassRaw  string
	MassUnit Unit
	// Parsed values; zero + ParseOK=false means "left in place with diagnostic".
	TimeS     float64
	TempC     float64
	MassMG    float64
	TimeOK    bool
	TempOK    bool
	MassOK    bool
	Diagnostic string
}

// BlankPoint is one baseline (blank curve) point: empty-pan mass vs time.
type BlankPoint struct {
	SourceID string
	Idx      int
	TimeS    float64
	MassMG   float64
	Raw      string
	OK       bool
}

// ProgramStep is one furnace program segment.
type ProgramStep struct {
	SourceID string
	Idx      int
	StartS   float64
	EndS     float64
	FromC    float64
	ToC      float64
	RateCPerMin float64
	Raw      string
}

// ValveEvent is one gas valve switch recorded at the controller.
type ValveEvent struct {
	SourceID string
	Idx      int
	AtS      float64
	Gas      string // e.g. N2, air
	FlowSCCM float64
	Raw      string
	OK       bool
}

// InitialMass is the stratified initial mass record.
type InitialMass struct {
	SourceID    string
	ReportedMG  float64 // as reported on the protocol
	MeasuredMG  float64 // balance reading at t0
	AdoptedMG   float64 // version used by derived layer
	ReportedRaw string
	MeasuredRaw string
	Reason      string
}

// Finding is a machine-readable domain event encoding the judgement basis:
// lag offsets, baseline drift, overlapping peaks, non-monotonic noise, gaps,
// initial-mass revisions and half-failed imports.
type Finding struct {
	ID         string
	ProjectID  string
	Type       FindingType
	Severity   string // info | warning | error
	AtS        float64 // furnace-time coordinate (0 if n/a)
	EndS       float64 // for spans/gaps/overlaps
	Message    string
	Evidence   string // direct source refs, e.g. "src-3#p12..p15"
	Rule       string // rule id producing it
	DerivedID  string // derived version this finding belongs to
	SortKey    int
}

type FindingType string

const (
	FindingAtmosphereLag   FindingType = "atmosphere_lag"
	FindingTempLag         FindingType = "temperature_lag"
	FindingBaselineDrift   FindingType = "baseline_drift"
	FindingOverlapPeaks    FindingType = "overlapping_peaks"
	FindingNonMonotonic    FindingType = "nonmonotonic_noise"
	FindingGap             FindingType = "sample_gap"
	FindingInitialMassRev  FindingType = "initial_mass_revision"
	FindingHalfFailure     FindingType = "import_half_failure"
	FindingGapNoInterp     FindingType = "gap_not_interpolated"
	FindingAmbiguousStage  FindingType = "ambiguous_stage_candidates"
)

// Peak is a DTG extremum; OverlapGroup links co-resolved peaks.
type Peak struct {
	ID          string
	AtS         float64
	AtReactionS float64
	DTG         float64 // mg/min, negative for mass loss
	Height      float64 // abs DTG
	WindowStart float64
	WindowEnd   float64
	OverlapID   string // empty unless part of an overlap group
	Note        string
}

// OverlapGroup documents a set of peaks that cannot be resolved into a single
// event; every legal stage candidate is retained.
type OverlapGroup struct {
	ID     string
	PeakIDs []string
	StartS float64
	EndS   float64
}

// StageCandidate is one legal decomposition stage. Stages may overlap and
// their mass losses are NOT forced to sum to 100%.
type StageCandidate struct {
	ID          string
	Label       string
	StartS      float64 // furnace-time window
	EndS        float64
	ReactionStartS float64
	ReactionEndS   float64
	StartC      float64
	EndC        float64
	MassLossMG  float64
	MassLossPct float64 // relative to adopted initial mass
	Status      Status  // candidate | accepted | rejected | unknown
	PeakID      string
	OverlapID   string
	RejectReason string
	// Direct provenance
	SourceRefs  string
	AlgoVersion string
	ParamFingerprint string
}

// ChainStep is one row in an expandable residue calculation chain.
type ChainStep struct {
	ID         string
	Order      int
	Op         string
	Expression string
	Value      string
	Unit       Unit
	SourceRef  string
}

// ResidueResult is a residual-mass fraction with full calculation chain.
type ResidueResult struct {
	ID         string
	Label      string
	AtS        float64
	AtC        float64
	MassMG     float64
	Percent    float64
	Status     Status // current | stale | unknown
	UnknownReason string
	Chain      []ChainStep
	AlgoVersion string
	ParamFingerprint string
	DirectSources string
}

// Decision is a human adjudication. Conflicts are appended, never overwritten;
// resolution and undo are themselves decisions with parent links.
type Decision struct {
	ID         string
	ProjectID  string
	Kind       DecisionKind
	TargetType string // stage | residue | derived | source
	TargetID   string
	Verdict    string // accept | reject | adjust
	Payload    string // JSON: parameter adjustments etc.
	ParentID   string // decision superseded/resolved by this one (conflict/undo)
	ConflictOf string // decision this one conflicts with ("" if none)
	ResolvesID string // conflict id this decision resolves ("" if n/a)
	Note       string
	CreatedAt  string
	Seq        int64
}

type DecisionKind string

const (
	DecisionStage    DecisionKind = "stage_adjudication"
	DecisionResidue  DecisionKind = "residue_adjudication"
	DecisionParam    DecisionKind = "parameter_revision"
	DecisionConflict DecisionKind = "conflict_resolution"
	DecisionUndo     DecisionKind = "undo"
)

// Derived is one versioned bundle of computed results.
type Derived struct {
	ID              string
	ProjectID       string
	Version         int
	ParentID        string
	Status          Status // current | stale
	AlgoVersion     string
	ParamFingerprint string
	Trigger         string // import | decision:<id>
	SampleSourceID  string
	BlankSourceID   string
	ProgramSourceID string
	ValveSourceID   string
	MassSourceID    string
	GapStartS       float64
	GapEndS         float64
	HasGap          bool
	DriftMG         float64
	LagS            float64 // atmosphere delay applied
	TempLagS        float64
	CreatedAt       string
	Seq             int64
}

// Diagnostic is an import-time diagnostic record.
type Diagnostic struct {
	ID        string
	BatchID   string
	ProjectID string
	Section   Kind
	Severity  string
	Message   string
	RawRef    string // position/snippet reference
	CreatedAt string
}
