// Package algo implements the domain calculations.
//
// Domain rules enforced here (see README and the machine-readable rationale
// events emitted by the project layer):
//
//  1. atmosphere delay + thermal lag: valve switch time != reaction time;
//     reaction windows start at valve_time + transport delay and are further
//     reconciled against the observed DTG evidence;
//  2. stages may overlap; stage mass losses are NOT forced to sum to 100%;
//  3. sampling gaps are detected, never interpolated across, and their span is
//     excluded from integration and flagged as unknown;
//  4. when evidence is ambiguous, ALL legal alternatives are retained as
//     first-class candidates (with rejection reasons for the rest).
package algo

import "tgalineage/internal/model"

// AlgoVersion is bumped whenever the calculation semantics change.
const AlgoVersion = "tga-decomp/v3"

// Inputs collects the current (or as-of) sources for a computation.
type Inputs struct {
	Points  []model.SampleRow
	Drift   []model.DriftAnchor
	Blank   []model.BlankPoint
	Furnace model.FurnaceProgram
	Gas     []model.GasEvent
	Mass    model.InitialMass
	Params  model.Params
	Refs    []model.SourceRef // provenance: exactly the direct sources used
}

// Point is a normalized working-series point. It still references the raw row
// identity so every derived number points back to its source cells.
type Point struct {
	ID       string  `json:"id"`
	TS       float64 `json:"t_s"`
	TempC    float64 `json:"temp_c"`
	RawMg    float64 `json:"raw_mg"`
	CorrMg   float64 `json:"corr_mg"`
	DtgPctC  float64 `json:"dtg_pct_per_c"`
	GapStart bool    `json:"gap_start"`
	Flag     string  `json:"flag,omitempty"` // nonmonotonic-outlier | ...
}

// Gap is a sampling gap: no interpolation is allowed across [AfterID].
type Gap struct {
	BeforeID string  `json:"before_id"`
	AfterID  string  `json:"after_id"`
	FromS    float64 `json:"from_s"`
	ToS      float64 `json:"to_s"`
	WidthS   float64 `json:"width_s"`
}

// AtmosphereWindow reconciles valve events with delay + observed reactions.
type AtmosphereWindow struct {
	EventID      string  `json:"event_id"`
	FromGas      string  `json:"from_gas"`
	ToGas        string  `json:"to_gas"`
	ValveTimeS   float64 `json:"valve_time_s"`
	DelayS       float64 `json:"delay_s"`
	ReactionFrom float64 `json:"reaction_from_s"` // valve + delay
	Note         string  `json:"note"`
}

// Peak is one detected DTG peak.
type Peak struct {
	ID       string  `json:"id"`
	CenterID string  `json:"center_id"`
	CenterS  float64 `json:"center_s"`
	CenterC  float64 `json:"center_c"`
	Depth    float64 `json:"depth_pct_per_c"`
	LeftID   string  `json:"left_id,omitempty"`
	RightID  string  `json:"right_id,omitempty"`
	LeftS    float64 `json:"left_s"`
	RightS   float64 `json:"right_s"`
	AltGroup string  `json:"alt_group,omitempty"`
	Note     string  `json:"note,omitempty"`
}

// StageCandidate is the stage portion of a run payload.
type StageCandidate struct {
	ID            string      `json:"id"`
	AltGroup      string      `json:"alt_group"`
	PeakID        string      `json:"peak_id"`
	LeftID        string      `json:"left_id"`
	RightID       string      `json:"right_id"`
	LeftS         float64     `json:"left_s"`
	RightS        float64     `json:"right_s"`
	LeftC         float64     `json:"left_c"`
	RightC        float64     `json:"right_c"`
	LossPct       float64     `json:"loss_pct"`
	OverlapsWith  []string    `json:"overlaps_with,omitempty"`
	CrossesGap    bool        `json:"crosses_gap"`
	UnknownMassMg float64     `json:"unknown_mass_mg,omitempty"`
	Atmosphere    string      `json:"atmosphere,omitempty"`
	Basis         string      `json:"basis"` // why these bounds
	Rejected      bool        `json:"rejected,omitempty"`
	RejectReason  string      `json:"reject_reason,omitempty"`
	Preferred     bool        `json:"preferred"`
	Chain         []ChainNode `json:"chain"`
}

// ResidueCandidate is the final-residue portion of a run payload.
type ResidueCandidate struct {
	ID         string      `json:"id"`
	ValuePct   float64     `json:"value_pct"`
	AtS        float64     `json:"at_s"`
	AtC        float64     `json:"at_c"`
	PointID    string      `json:"point_id"`
	CrossesGap bool        `json:"crosses_gap"`
	Unknown    bool        `json:"unknown"`
	Note       string      `json:"note"`
	Chain      []ChainNode `json:"chain"`
}

// ChainNode is one expandable calculation-chain step.
type ChainNode struct {
	Step   string   `json:"step"`
	Value  string   `json:"value"`
	Unit   string   `json:"unit,omitempty"`
	Source string   `json:"source,omitempty"`
	RefIDs []string `json:"ref_ids,omitempty"`
	Note   string   `json:"note,omitempty"`
}

// Payload is the full derived result of one run.
type Payload struct {
	Series        []Point            `json:"series"`
	Gaps          []Gap              `json:"gaps"`
	Noise         []NoiseReport      `json:"noise"`
	Atmospheres   []AtmosphereWindow `json:"atmospheres"`
	FurnaceTraj   []FurnacePoint     `json:"furnace_traj"`
	Peaks         []Peak             `json:"peaks"`
	Stages        []StageCandidate   `json:"stages"`
	Residues      []ResidueCandidate `json:"residues"`
	Rejected      []StageCandidate   `json:"rejected"`
	Diagnostics   []model.Diagnostic `json:"diagnostics"`
	InitialMassMg float64            `json:"initial_mass_mg"`
	InitialMassID string             `json:"initial_mass_id"`
	Notes         []string           `json:"notes"`
}

// NoiseReport flags a non-monotonic excursion without deleting the point.
type NoiseReport struct {
	PointID string  `json:"point_id"`
	TS      float64 `json:"t_s"`
	UpMg    float64 `json:"up_mg"`
	Z       float64 `json:"z"`
	Action  string  `json:"action"`
}

// FurnacePoint is the simulated furnace program trajectory.
type FurnacePoint struct {
	TS    float64 `json:"t_s"`
	TempC float64 `json:"temp_c"`
}
