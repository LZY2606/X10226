package tga

import "time"

// Section identifies the six independently-layered source kinds that must be
// preserved separately on import.
type Section string

const (
	SecSamples   Section = "samples"
	SecDrift     Section = "drift"
	SecFurnace   Section = "furnace"
	SecGas       Section = "gas"
	SecBlank     Section = "blank"
	SecInitialMass Section = "initial_mass"
)

// AllSections lists the import layers in canonical (storage) order.
var AllSections = []Section{SecSamples, SecDrift, SecFurnace, SecGas, SecBlank, SecInitialMass}

// ---------------------------------------------------------------------------
// Raw layer: original representations only
// ---------------------------------------------------------------------------

// RawCommit is an immutable submitted chunk of input. Revisions are expressed
// as new RawCommits whose ParentCommit points at the previous commit for the
// same section.
type RawCommit struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Seq         int       `json:"seq"`
	Section     Section   `json:"section"`
	ParentCommit string   `json:"parent_commit,omitempty"`
	Format      string    `json:"format"` // e.g. "tga-ascii/1"
	RawText     string    `json:"raw_text"`
	Accepted    bool      `json:"accepted"` // false => import half-failure for this section
	Diagnostics []Diagnostic `json:"diagnostics"`
	SubmittedAt time.Time `json:"submitted_at"`
	Label       string    `json:"label"`
}

// Diagnostic pins an issue to a position (section/row/column/field) without
// discarding the surrounding data.
type Diagnostic struct {
	Severity string  `json:"severity"` // error | warning
	Section  Section `json:"section"`
	Row      int     `json:"row,omitempty"`
	Field    string  `json:"field,omitempty"`
	Message  string  `json:"message"`
	Cell     string  `json:"cell,omitempty"`
}

// RawSamplePoint is a faithful copy of one raw measurement row.
type RawSamplePoint struct {
	Row      int      `json:"row"`
	CommitID string   `json:"commit_id"`
	Time     Quantity `json:"time"`
	Temp     Quantity `json:"temp"`
	Mass     Quantity `json:"mass"`
}

type RawFurnaceStep struct {
	Row      int      `json:"row"`
	CommitID string   `json:"commit_id"`
	Start    Quantity `json:"start"`
	End      Quantity `json:"end"`
	From     Quantity `json:"from"`
	To       Quantity `json:"to"`
	Kind     string   `json:"kind"` // hold | ramp
}

type RawGasEvent struct {
	Row      int      `json:"row"`
	CommitID string   `json:"commit_id"`
	At       Quantity `json:"at"`
	Gas      string   `json:"gas"`
	Flow     Quantity `json:"flow"`
	Action   string   `json:"action"`
}

type RawBlankPoint struct {
	Row      int      `json:"row"`
	CommitID string   `json:"commit_id"`
	Time     Quantity `json:"time"`
	BlankMass Quantity `json:"blank_mass"`
}

// RawInitialMass records the separately-declared sample starting mass layer.
type RawInitialMass struct {
	CommitID string   `json:"commit_id"`
	Mass     Quantity `json:"mass"`
	Balance  string   `json:"balance,omitempty"`
	Note     string   `json:"note,omitempty"`
}

// RawDrift is the separately-declared balance baseline drift correction layer.
type RawDrift struct {
	CommitID string   `json:"commit_id"`
	Rate     Quantity `json:"rate"` // mass per minute, signed
	Basis    string   `json:"basis"`
}

// ---------------------------------------------------------------------------
// Normalised layer
// ---------------------------------------------------------------------------

// SourceVersion is the lineage pointer to one accepted section layer.
type SourceVersion struct {
	Section  Section `json:"section"`
	CommitID string  `json:"commit_id"`
	Seq      int     `json:"seq"`
}

// NormSample is one usable sample in canonical units. Bad rows are kept in the
// raw layer only and reported, never silently patched.
type NormSample struct {
	Index    int     `json:"index"`
	Row      int     `json:"row"`
	TimeMin  float64 `json:"t"`
	TempC    float64 `json:"T"`
	MassMg   float64 `json:"m"`
	MonotonicOK bool  `json:"monotonic_ok"` // false => non-monotone noise flagged here
}

// Segment is a run of consecutive usable samples. Segments are split at
// sampling gaps and at bad rows; derivation never bridges between segments.
type Segment struct {
	ID     string `json:"id"`
	Start  int    `json:"start"` // inclusive index into norm samples
	End    int    `json:"end"`   // inclusive
	GapBefore bool `json:"gap_before"`
}

type NormSeries struct {
	ArtifactID string        `json:"artifact_id"`
	Algorithm  string        `json:"algorithm"`
	ParamFP    string        `json:"param_fp"`
	Sources    []SourceVersion `json:"sources"`
	Samples    []NormSample  `json:"samples"`
	Segments   []Segment     `json:"segments"`
	Diags      []Diagnostic  `json:"diags"`
}

type NormFurnace struct {
	ArtifactID string          `json:"artifact_id"`
	Algorithm  string          `json:"algorithm"`
	ParamFP    string          `json:"param_fp"`
	Sources    []SourceVersion `json:"sources"`
	Steps      []RawFurnaceStep `json:"steps"`
}

type NormGas struct {
	ArtifactID string          `json:"artifact_id"`
	Algorithm  string          `json:"algorithm"`
	ParamFP    string          `json:"param_fp"`
	Sources    []SourceVersion `json:"sources"`
	Events     []RawGasEvent   `json:"events"`
}

type NormBlank struct {
	ArtifactID string          `json:"artifact_id"`
	Algorithm  string          `json:"algorithm"`
	ParamFP    string          `json:"param_fp"`
	Sources    []SourceVersion `json:"sources"`
	Points     []NormSample    `json:"points"` // T and blank mass
}

type NormInitialMass struct {
	ArtifactID string          `json:"artifact_id"`
	Algorithm  string          `json:"algorithm"`
	ParamFP    string          `json:"param_fp"`
	Sources    []SourceVersion `json:"sources"`
	Initial    *RawInitialMass `json:"initial"`
}

type NormDrift struct {
	ArtifactID string          `json:"artifact_id"`
	Algorithm  string          `json:"algorithm"`
	ParamFP    string          `json:"param_fp"`
	Sources    []SourceVersion `json:"sources"`
	Drift      *RawDrift       `json:"drift"`
}

// ---------------------------------------------------------------------------
// Derivation: TG/DTG, stage candidates, residues
// ---------------------------------------------------------------------------

// Params are the versioned analysis parameters.
type Params struct {
	Version       int     `json:"version"`
	ParentEventID string  `json:"parent_event,omitempty"`
	GapMin        float64 `json:"gap_min"`          // furnace minutes; larger => gap
	AtmosDelayMin float64 `json:"atmos_delay_min"`  // valve -> sample atmosphere
	TempLagC      float64 `json:"temp_lag_c"`       // programmed -> sample temperature
	DriftCorrect  bool    `json:"drift_correct"`
	BlankCorrect  bool    `json:"blank_correct"`
	SmoothWindow  int     `json:"smooth_window"`    // DTG smoothing window (odd)
	ProminenceMg  float64 `json:"prominence_mg_per_min"`
}

func (p Params) fingerprint() string {
	return hashCanonical(map[string]any{
		"gap_min": p.GapMin, "atmos_delay_min": p.AtmosDelayMin,
		"temp_lag_c": p.TempLagC, "drift_correct": p.DriftCorrect,
		"blank_correct": p.BlankCorrect, "smooth_window": p.SmoothWindow,
		"prominence_mg_per_min": p.ProminenceMg,
	})
}

// DTGPoint is a derived TG/DTG point. dtg is -dm/dt (mg/min), positive for
// mass loss; it is left nil-equivalent across gaps (no interpolation).
type DTGPoint struct {
	Index   int     `json:"index"`
	TimeMin float64 `json:"t"`
	TempC   float64 `json:"T"`
	MassMg  float64 `json:"m"`
	MassPct float64 `json:"mass_pct"`
	DTG     float64 `json:"dtg"`
	Segment string  `json:"segment"`
}

// TGResult holds the derived curves.
type TGResult struct {
	ArtifactID string          `json:"artifact_id"`
	Algorithm  string          `json:"algorithm"`
	ParamFP    string          `json:"param_fp"`
	Sources    []SourceVersion `json:"sources"`
	Points     []DTGPoint      `json:"points"`
	Diags      []Diagnostic    `json:"diags"`
}

// Peak is a detected DTG peak within one segment.
type Peak struct {
	ID        string  `json:"id"`
	TimeMin   float64 `json:"t"`
	TempC     float64 `json:"T"`
	DTG       float64 `json:"dtg"`
	ClusterID string  `json:"cluster"`
}

// StageCandidate is one legal decomposition hypothesis. Candidates carry an
// accept/reject/unknown status and, when rejected, structured reasons. They
// are first-class data, not just a final number.
type StageCandidate struct {
	ID          string   `json:"id"`
	ClusterID   string   `json:"cluster"`
	Variant     string   `json:"variant"` // merged | shoulder | sub1 | sub2 ...
	StartMin    float64  `json:"start_min"`
	EndMin      float64  `json:"end_min"`
	StartTempC  float64  `json:"start_temp_c"`
	EndTempC    float64  `json:"end_temp_c"`
	MassLossMg  float64  `json:"mass_loss_mg"`
	MassLossPct float64  `json:"mass_loss_pct"` // of the candidate's own baseline; not forced to sum
	PeakIDs     []string `json:"peak_ids"`
	CrossesGap  bool     `json:"crosses_gap"`
	Status      string   `json:"status"` // proposed | rejected | accepted
	RejectReasons []string `json:"reject_reasons,omitempty"`
	// ReactionTimeMin is the atmosphere-aware reaction window: valve times are
	// shifted by the atmosphere delay and temperature lag are not equal to
	// valve switching times.
	ValveNearMin  *float64 `json:"valve_near_min,omitempty"`
	ReactionStartMin float64 `json:"reaction_start_min"`
}

// StageResult is the decomposition artifact.
type StageResult struct {
	ArtifactID string          `json:"artifact_id"`
	Algorithm  string          `json:"algorithm"`
	ParamFP    string          `json:"param_fp"`
	Sources    []SourceVersion `json:"sources"`
	Peaks      []Peak          `json:"peaks"`
	Candidates []StageCandidate `json:"candidates"`
	Diags      []Diagnostic    `json:"diags"`
}

// ChainStep is one link in an expandable residue calculation chain. ValueID is
// a stable identity that the UI attaches to every displayed number.
type ChainStep struct {
	ValueID  string  `json:"value_id"`
	Label    string  `json:"label"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Formula  string  `json:"formula,omitempty"`
	Source   *SourceRef `json:"source,omitempty"`
	Detail   string  `json:"detail,omitempty"`
}

// SourceRef points from a derived value back to an input cell / artifact.
type SourceRef struct {
	Kind      string `json:"kind"` // raw_commit | artifact | param | decision
	ID        string `json:"id"`
	Row       int    `json:"row,omitempty"`
	Field     string `json:"field,omitempty"`
	ArtifactID string `json:"artifact_id,omitempty"`
}

type ResidueResult struct {
	ArtifactID string          `json:"artifact_id"`
	Algorithm  string          `json:"algorithm"`
	ParamFP    string          `json:"param_fp"`
	Sources    []SourceVersion `json:"sources"`
	FinalMassMg *float64       `json:"final_mass_mg"`
	InitialMassMg *float64     `json:"initial_mass_mg"`
	ResiduePct *float64        `json:"residue_pct"`
	Unknown    string          `json:"unknown,omitempty"`
	Chain      []ChainStep     `json:"chain"`
}
