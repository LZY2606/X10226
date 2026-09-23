package core

import "encoding/json"

// Raw, immutable input strata ("分层保存"). Each kind of source material is
// stored in its own layer with an explicit version chain.
const (
	LayerSamplePoints = "sample_points"
	LayerDrift        = "drift"
	LayerFurnace      = "furnace"
	LayerGasEvents    = "gas_events"
	LayerBlank        = "blank"
	LayerInitialMass  = "initial_mass"
)

// Layers is the canonical order of the six raw strata.
var Layers = []string{
	LayerSamplePoints,
	LayerDrift,
	LayerFurnace,
	LayerGasEvents,
	LayerBlank,
	LayerInitialMass,
}

// Envelope is the import representation. Data is kept verbatim in the raw
// layer; only the derived layer parses and normalizes it.
type Envelope struct {
	Layer string            `json:"layer"`
	Units map[string]string `json:"units,omitempty"`
	Data  json.RawMessage   `json:"data"`
}

// Diagnostic is a first-class import / derivation problem. Values that cannot
// be converted stay where they were and carry a diagnostic instead of being
// dropped to make a chart render.
type Diagnostic struct {
	Layer   string `json:"layer"`
	Index   int    `json:"index"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Raw     string `json:"raw,omitempty"`
}

// Input is one committed, immutable version of a raw layer.
type Input struct {
	ID        string
	Layer     string
	Version   int
	ParentID  string
	Content   string
	Units     string
	CreatedAt int64
	EventSeq  int64
}

// Event is a machine-readable fact in the append-only history.
type Event struct {
	Seq     int64
	ID      string
	Kind    string
	Ts      int64
	Payload string
}

// Derived is an immutable, content-addressed derived result.
type Derived struct {
	ID          string
	Kind        string
	AlgoVersion string
	ParamsFP    string
	Sources     string // JSON array of direct source IDs
	Content     string
	Status      string // current | stale (computed per snapshot)
	EventSeq    int64
}

// Derived kinds.
const (
	DerCorrected  = "corrected"
	DerNorm       = "norm"
	DerDTG        = "dtg"
	DerAtmos      = "atmosphere"
	DerCandidate  = "stage_candidate"
	DerResidue    = "residue"
)

// Decision is a human adjudication against a derived object.
type Decision struct {
	ID        string
	TargetID  string
	Action    string // accept | reject | adjust
	Params    string
	Rationale string
	Ts        int64
	Undone    bool
	Overruled bool
	EventSeq  int64
}

// Rule assertions are the machine-readable judgment basis emitted by the
// derivation engine.
const (
	RuleAtmosphereDelay = "atmosphere_delay"
	RuleStagesOverlap   = "stages_overlap"
	RuleSumNotForced    = "sum_not_forced"
	RuleGapNoInterp     = "gap_no_interpolation"
)

// RuleAssertion is serialized verbatim as a rule_assertion event payload.
type RuleAssertion struct {
	Rule      string         `json:"rule"`
	Subject   string         `json:"subject"`
	Statement string         `json:"statement"`
	Detail    map[string]any `json:"detail"`
}
