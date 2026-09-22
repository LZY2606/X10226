package model

// Status is first-class per derived value.
type Status string

const (
	StatusOK      Status = "ok"
	StatusUnknown Status = "unknown" // cannot be uniquely decided
	StatusError   Status = "error"
)

// Node is one node in a calculation chain. Every displayed value carries a
// stable ID and traces back to direct sources.
type Node struct {
	ID       string         `json:"id"`
	Label    string         `json:"label"`
	Kind     string         `json:"kind"` // series|scalar|candidate|gap|note
	Value    string         `json:"value"`
	Unit     string         `json:"unit,omitempty"`
	Status   Status         `json:"status"`
	Reason   string         `json:"reason,omitempty"`
	Algo     string         `json:"algoVersion,omitempty"`
	ParamFP  string         `json:"paramFingerprint,omitempty"`
	Direct   []string       `json:"directSources,omitempty"` // node/layer ids
	Children []Node         `json:"children,omitempty"`
	Meta     map[string]any `json:"meta,omitempty"`
}

type SeriesPoint struct {
	TSec      float64  `json:"tSec"`
	TK        float64  `json:"tK"`
	TempValid bool     `json:"tempValid"`
	Pct       float64  `json:"pct"`
	DTG       *float64 `json:"dtg"` // null across gap boundaries/edges
	Suspect   bool     `json:"suspect"`
	Seg       int      `json:"seg"` // series segment; gaps separate segments
}

type Gap struct {
	StartS float64 `json:"startS"`
	EndS   float64 `json:"endS"`
	Reason string  `json:"reason"`
}

// Candidate is one stage-decomposition candidate. Candidates may overlap and
// their losses are NOT forced to sum to 100%.
type Candidate struct {
	Node
	PeakS    float64  `json:"peakS"`
	OnsetS   *float64 `json:"onsetS"`
	OffsetS  *float64 `json:"offsetS"`
	LossPct  *float64 `json:"lossPct"`
	GasHint  string   `json:"gasHint,omitempty"`
	Overlap  []string `json:"overlapWith,omitempty"`
	Rejected []string `json:"rejectedReasons,omitempty"`
	Group    string   `json:"group,omitempty"`
	// Decision state from the event log (empty if undecided).
	DecisionKind DecisionKind `json:"decisionKind,omitempty"`
	DecisionNote string       `json:"decisionNote,omitempty"`
}

// Residue is a final residual-rate figure with its full calculation chain.
type Residue struct {
	Node
	Pct       *float64 `json:"pct"`
	WindowEnd float64  `json:"windowEndS"`
}

// Derived is the full computed report for a state (event prefix + params).
// It is content-addressed: the same inputs always yield the same IDs.
type Derived struct {
	StateHash string         `json:"stateHash"`
	Algo      string         `json:"algoVersion"`
	ParamFP   string         `json:"paramFingerprint"`
	Series    []SeriesPoint  `json:"series"`
	Gaps      []Gap          `json:"gaps"`
	Root      Node           `json:"root"` // calculation chain root
	SeriesID  string         `json:"seriesId"`
	DTGID     string         `json:"dtgId"`
	Residues  []Residue      `json:"residues"`
	Candidates []Candidate   `json:"candidates"`
	// Index maps stable id -> pointer chain location is used server-side;
	// exported JSON keeps the tree. Map is for lookup convenience.
	ByID     map[string]*Node `json:"-"`
	Findings []Finding        `json:"findings"`
}

// Finding is a machine-readable judgment produced by the analysis pipeline
// (gas delay/lag, overlap, gap non-interpolation, noise, etc.).
type Finding struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Refs     []string `json:"refs,omitempty"`
	StartS   *float64 `json:"startS,omitempty"`
	EndS     *float64 `json:"endS,omitempty"`
}
