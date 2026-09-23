// Package model defines the domain quantities, immutable inputs, append-only
// events and derived artifacts of the TGA review system.
package model

// Quantity carries both the original representation and the normalized value.
// Raw text is never dropped: a value that cannot be converted stays in place
// with Status set and a diagnostic.
type Quantity struct {
	Value      *float64 `json:"value,omitempty"`
	Unit       string   `json:"unit,omitempty"`     // canonical unit
	Raw        string   `json:"raw,omitempty"`      // original token
	RawUnit    string   `json:"raw_unit,omitempty"` // original unit token
	Basis      string   `json:"basis,omitempty"`    // coordinate/reference basis
	Status     string   `json:"status,omitempty"`   // ok | unconvertible | unknown
	Diagnostic string   `json:"diagnostic,omitempty"`
}

// Diagnostic is attached to an imported fragment or derived value.
type Diagnostic struct {
	Scope   string `json:"scope"`
	Line    int    `json:"line,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Raw     string `json:"raw,omitempty"`
}

// RawRow preserves the submitted text of a single imported row.
type RawRow struct {
	Line  int               `json:"line"`
	Cells map[string]string `json:"cells"`
	Raw   string            `json:"raw"`
}

type SamplePoint struct {
	Index int      `json:"index"`
	Line  int      `json:"line"`
	T     Quantity `json:"t"`
	Temp  Quantity `json:"temp"`
	M     Quantity `json:"m"`
}

type DriftModel struct {
	T0   Quantity `json:"t0"`
	A0   Quantity `json:"a0"`
	Rate Quantity `json:"rate"` // mass per time, g/s canonical
}

type FurnacePoint struct {
	Line int      `json:"line"`
	T    Quantity `json:"t"`
	Temp Quantity `json:"temp"`
}

type ValveEvent struct {
	Line int      `json:"line"`
	At   Quantity `json:"at"`
	Gas  string   `json:"gas"`
	Raw  string   `json:"raw"`
}

type BlankPoint struct {
	Line int      `json:"line"`
	Temp Quantity `json:"temp"`
	M    Quantity `json:"m"`
}

type Mass0 struct {
	M    Quantity `json:"m"`
	Note string   `json:"note,omitempty"`
	Raw  string   `json:"raw,omitempty"`
}

// InputVersion is an immutable, versioned direct source. Revisions create a new
// version with ParentID set; the original is never overwritten.
type InputVersion struct {
	Kind       string         `json:"kind"` // samples|drift|furnace|valves|blank|mass
	ID         string         `json:"id"`
	ParentID   string         `json:"parent_id,omitempty"`
	EventSeq   int64          `json:"event_seq"`
	EventID    string         `json:"event_id"`
	Status     string         `json:"status"` // ok | partial
	Reason     string         `json:"reason,omitempty"`
	RawRows    []RawRow       `json:"raw_rows"`
	Diagnostic []Diagnostic   `json:"diagnostics"`
	Samples    []SamplePoint  `json:"samples,omitempty"`
	Drift      *DriftModel    `json:"drift,omitempty"`
	Furnace    []FurnacePoint `json:"furnace,omitempty"`
	Valves     []ValveEvent   `json:"valves,omitempty"`
	Blank      []BlankPoint   `json:"blank,omitempty"`
	Mass0      *Mass0         `json:"mass0,omitempty"`
}

// Assertion is a machine-readable judgment record (see codes in analyze).
type Assertion struct {
	ID       string         `json:"id"`
	Code     string         `json:"code"`
	Severity string         `json:"severity"` // info|warning
	Detail   map[string]any `json:"detail,omitempty"`
	Text     string         `json:"text"`
}

// ValueNode is any value shown in the UI. IDs are content derived and therefore
// stable across export/delete/import.
type ValueNode struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Quantity  Quantity `json:"q"`
	Kind      string   `json:"kind"` // raw|corrected|dtg|parameter|result
	SourceIDs []string `json:"source_ids,omitempty"`
	Current   bool     `json:"current"`
}

type Artifact struct {
	ID          string      `json:"id"`
	Kind        string      `json:"kind"`
	AlgoVersion string      `json:"algo_version"`
	ParamFP     string      `json:"param_fp"`
	SourceIDs   []string    `json:"source_ids"`
	Current     bool        `json:"current"`
	CreatedSeq  int64       `json:"created_seq"`
	Assertions  []Assertion `json:"assertions,omitempty"`
	Payload     any         `json:"payload"`
}

type Decision struct {
	EventSeq   int64  `json:"event_seq"`
	EventID    string `json:"event_id"`
	TargetID   string `json:"target_id"`
	TargetKind string `json:"target_kind"`
	Verdict    string `json:"verdict"` // accept|reject
	Note       string `json:"note"`
	Active     bool   `json:"active"` // reversed after undo
	ReversedBy string `json:"reversed_by,omitempty"`
}

type Conflict struct {
	ID          string   `json:"id"`
	Group       string   `json:"group"`
	DecisionIDs []string `json:"decision_ids"`
	Status      string   `json:"status"` // open|resolved
	Resolution  string   `json:"resolution,omitempty"`
	EventSeq    int64    `json:"event_seq,omitempty"`
	Note        string   `json:"note,omitempty"`
}

type Params struct {
	AlgoVersion        float64            `json:"algo_version"`
	GapFactor          float64            `json:"gap_factor"`
	NoiseBandMgPerMin  float64            `json:"noise_band_mg_per_min"`
	ProminenceMgPerMin float64            `json:"prominence_mg_per_min"`
	MergeWindowS       float64            `json:"merge_window_s"`
	TempLagS           float64            `json:"temperature_lag_s"`
	AtmosphereDelayS   map[string]float64 `json:"atmosphere_delay_s"`
}

func DefaultParams() Params {
	return Params{
		AlgoVersion:        1.0,
		GapFactor:          5,
		NoiseBandMgPerMin:  0.05,
		ProminenceMgPerMin: 0.20,
		MergeWindowS:       120,
		TempLagS:           30,
		AtmosphereDelayS:   map[string]float64{"air": 45, "n2": 0, "co2": 20},
	}
}

func (p Params) VersionID() string { return "tgax-stage-v1" }

// Event is the append-only record. Payload is raw JSON.
type Event struct {
	ProjectID string `json:"project_id"`
	Seq       int64  `json:"seq"`
	EventID   string `json:"event_id"`
	Type      string `json:"type"`
	Payload   string `json:"payload"`
	CreatedAt string `json:"created_at"`
}
