package domain

// Layer kinds: the six stratified source layers the review must reconcile.
const (
	LayerSamples   = "samples"     // original TG sampling points (mass, temperature, furnace time)
	LayerDrift     = "drift"       // balance drift / baseline correction records
	LayerProgram   = "program"     // furnace program segments
	LayerGas       = "gas"         // gas valve switch events
	LayerBlank     = "blank"       // blank/baseline curve points
	LayerInitMass  = "init_mass"   // weighed-in sample mass strata
)

var LayerOrder = []string{LayerSamples, LayerDrift, LayerProgram, LayerGas, LayerBlank, LayerInitMass}

// SamplePoint is one raw TG acquisition row. All quantities retain original text.
type SamplePoint struct {
	Row       int      `json:"row"`
	Time      Quantity `json:"time"`
	Temperature Quantity `json:"temperature"`
	Mass      Quantity `json:"mass"`
}

// DriftRecord is one balance drift/baseline correction entry.
type DriftRecord struct {
	Row       int      `json:"row"`
	Time      Quantity `json:"time"`
	Offset    Quantity `json:"offset"` // signed mass offset to subtract (after normalization)
	Note      string   `json:"note,omitempty"`
}

// ProgramSegment is a furnace program segment.
type ProgramSegment struct {
	Row        int      `json:"row"`
	StartTime  Quantity `json:"start_time"`
	EndTime    Quantity `json:"end_time"`
	StartTemp  Quantity `json:"start_temp"`
	EndTemp    Quantity `json:"end_temp"`
	Kind       string   `json:"kind"` // ramp | isothermal
	Label      string   `json:"label,omitempty"`
}

// GasEvent is a valve switch. Valve time is not sample reaction time.
type GasEvent struct {
	Row      int      `json:"row"`
	Time     Quantity `json:"time"`
	Gas      string   `json:"gas"`      // N2 | air | O2 | Ar ...
	Action   string   `json:"action"`   // open | close | switch
	Flow     Quantity `json:"flow"`
	Note     string   `json:"note,omitempty"`
}

// BlankPoint is one point of the empty-pan blank curve.
type BlankPoint struct {
	Row       int      `json:"row"`
	Time      Quantity `json:"time"`
	Temperature Quantity `json:"temperature"`
	Mass      Quantity `json:"mass"`
}

// InitMass is a weighed-in mass stratum (a weighing record, not necessarily accepted).
type InitMass struct {
	Row       int      `json:"row"`
	Time      Quantity `json:"time"` // when weighed (may be missing => zero time)
	Mass      Quantity `json:"mass"`
	BalanceID string   `json:"balance_id,omitempty"`
	Note      string   `json:"note,omitempty"`
}

// SourceVersion is one immutable committed revision of one layer.
// Once committed it is never overwritten; corrections become new versions
// linked to ParentVersion.
type SourceVersion struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	Layer         string `json:"layer"`
	Version       int    `json:"version"`        // 1-based per layer
	ParentVersion string `json:"parent_version"` // previous SourceVersion.ID, may be empty
	MediaType     string `json:"media_type"`     // e.g. text/csv
	RawText       string `json:"raw_text"`       // verbatim submitted bytes/text
	SHA256        string `json:"sha256"`
	CommittedAt   string `json:"committed_at"` // RFC3339, from the clock
	CommitEventID string `json:"commit_event_id"`
	// Parsed rows (only rows parsed without fatal error; bad rows are diagnostics)
	Samples  []SamplePoint    `json:"samples,omitempty"`
	Drifts   []DriftRecord    `json:"drifts,omitempty"`
	Program  []ProgramSegment `json:"program,omitempty"`
	Gases    []GasEvent       `json:"gases,omitempty"`
	Blanks   []BlankPoint     `json:"blanks,omitempty"`
	Masses   []InitMass       `json:"masses,omitempty"`
	Accepted bool             `json:"accepted"` // initial versions are accepted until a revision supersedes
}

// Diagnostic captures an unconvertible / malformed value left in place.
type Diagnostic struct {
	ID       string `json:"id"`
	SessionID string `json:"session_id,omitempty"`
	Layer    string `json:"layer"`
	Row      int    `json:"row"`
	Field    string `json:"field"`
	Token    string `json:"token"`
	Severity string `json:"severity"` // error | warning
	Message  string `json:"message"`
}
