package domain

// ChainNode is one step of an auditable calculation chain. Each displayed
// value can be expanded back to its direct inputs, operator and algorithm.
type ChainNode struct {
	Step     string         `json:"step"`
	Operator string         `json:"operator"`
	Inputs   map[string]any `json:"inputs,omitempty"`
	Output   any            `json:"output"`
	Unit     string         `json:"unit,omitempty"`
	Note     string         `json:"note,omitempty"`
}

// CorrectedPoint is a normalized sample point after unit conversion, balance
// drift and blank correction. Segments separated by sampling gaps are marked.
type CorrectedPoint struct {
	TimeS      float64  `json:"time_s"`
	TempC      float64  `json:"temp_c"`
	RawMassMg  float64  `json:"raw_mass_mg"`
	CorrMassMg float64  `json:"corr_mass_mg"`
	MassPct    float64  `json:"mass_pct"`
	Segment    int      `json:"segment"`
	GapAfter   bool     `gap_after,omitempty"`
	Known      bool     `json:"known"`
	Flags      []string `json:"flags,omitempty"`
}

// DTGPoint is one derivative point. Points across a sampling gap are never
// interpolated; the derivative at a gap boundary is unknown.
type DTGPoint struct {
	TimeS     float64 `json:"time_s"`
	DTGPctMin float64 `json:"dtg_pct_per_min"`
	Known     bool    `json:"known"`
	Segment   int     `json:"segment"`
}

// Peak is a DTG minimum (mass-loss peak). Overlapping/merged peaks carry
// alternatives and rejection reasons rather than a forced single answer.
type Peak struct {
	ID            string   `json:"id"`
	TimeS         float64  `json:"time_s"`
	TempC         float64  `json:"temp_c"`
	DepthPctMin   float64  `json:"depth_pct_per_min"`
	WindowL       float64  `json:"window_l_s"`
	WindowR       float64  `json:"window_r_s"`
	MergedIDs     []string `json:"merged_peak_ids,omitempty"`
	Status        string   `json:"status"` // ok | rejected
	RejectReasons []string `json:"reject_reasons,omitempty"`
}

// AtmosphereWindow is the reaction window inferred from a valve event after
// atmosphere delay and temperature lag. Valve time != reaction time.
type AtmosphereWindow struct {
	GasEventID  string  `json:"gas_event_id"`
	ValveAtS    float64 `json:"valve_at_s"`
	ReactStartS float64 `json:"react_start_s"`
	ReactEndS   float64 `json:"react_end_s"`
	ToGas       string  `json:"to_gas"`
	FromGas     string  `json:"from_gas"`
	Known       bool    `json:"known"`
	Note        string  `json:"note,omitempty"`
}

// StageCandidate is a proposed decomposition stage. Stages may overlap and
// their mass losses are never forced to sum to 100%.
type StageCandidate struct {
	ID            string   `json:"id"`
	PeakID        string   `json:"peak_id"`
	StartS        float64  `json:"start_s"`
	EndS          float64  `json:"end_s"`
	StartTempC    float64  `json:"start_temp_c"`
	EndTempC      float64  `json:"end_temp_c"`
	Gas           string   `json:"gas,omitempty"`
	MassLossMg    float64  `json:"mass_loss_mg"`
	MassLossPct   *float64 `json:"mass_loss_pct,omitempty"`
	OverlapsWith  []string `json:"overlaps_with,omitempty"`
	Status        string   `json:"status"` // candidate | accepted | rejected | unknown
	UnknownReason string   `json:"unknown_reason,omitempty"`
	RejectReasons []string `json:"reject_reasons,omitempty"`
	Alternatives  []string `json:"alternatives,omitempty"`
}

// Residue is a final (or intermediate) residual fraction with a full chain.
type Residue struct {
	ID            string      `json:"id"`
	AtS           float64     `json:"at_s"`
	TempC         float64     `json:"temp_c"`
	MassMg        float64     `json:"mass_mg"`
	BasisID       string      `json:"initial_mass_id"`
	Pct           *float64    `json:"pct,omitempty"`
	Known         bool        `json:"known"`
	UnknownReason string      `json:"unknown_reason,omitempty"`
	Chain         []ChainNode `json:"chain"`
}

// DeriveResult is the full artifact set of one derivation run.
type DeriveResult struct {
	Points        []CorrectedPoint   `json:"points"`
	DTG           []DTGPoint         `json:"dtg"`
	Peaks         []Peak             `json:"peaks"`
	AtmWindows    []AtmosphereWindow `json:"atmosphere_windows"`
	Stages        []StageCandidate   `json:"stages"`
	Residues      []Residue          `json:"residues"`
	Diags         []Diagnostic       `json:"diagnostics"`
	AcceptedInput map[string]bool    `json:"accepted_inputs"`
	UnknownFields map[string]string  `json:"unknown_fields,omitempty"`
}
