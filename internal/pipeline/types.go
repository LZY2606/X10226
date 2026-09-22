// Package pipeline turns the current immutable raw batches plus a parameter
// version into deterministic derived nodes. It never mutates inputs; when a
// value cannot be uniquely determined it emits alternative candidates or
// explicit unknown states instead of guessing.
package pipeline

import (
	"tgaaudit/internal/domain"
)

// Point is one sample trajectory point after normalisation.
type Point struct {
	I             int      `json:"i"`
	TS            float64  `json:"ts"`          // seconds since run start (furnace time basis)
	MassRawKg     *float64 `json:"mass_raw_kg"` // null = explicit missing point
	MassCorrKg    *float64 `json:"mass_corr_kg"`
	MassFraction  *float64 `json:"mass_fraction"` // corrected / initial mass
	SampleTempK   *float64 `json:"sample_temp_k"`
	FurnaceTempK  *float64 `json:"furnace_temp_k"`
	Missing       bool     `json:"missing,omitempty"`
	NonMonotonic  bool     `json:"non_monotonic,omitempty"`
	InitialMassKg *float64 `json:"initial_mass_kg,omitempty"`
	Gas           string   `json:"gas,omitempty"`
	RawTime       string   `json:"raw_time,omitempty"`
	RawMass       string   `json:"raw_mass,omitempty"`
	RawTemp       string   `json:"raw_temp,omitempty"`
	TimeUnit      string   `json:"time_unit,omitempty"`
	MassUnit      string   `json:"mass_unit,omitempty"`
	TempUnit      string   `json:"temp_unit,omitempty"`
}

// Gap describes a region over which interpolation is forbidden.
type Gap struct {
	Index            int     `json:"index"`
	FromT            float64 `json:"from_t"`
	ToT              float64 `json:"to_t"`
	Reason           string  `json:"reason"`
	SpansMissingRows int     `json:"spans_missing_rows"`
}

// CorrectedData is the corrected_series node payload.
type CorrectedData struct {
	Key             string   `json:"key"`
	InitialMassKg   *float64 `json:"initial_mass_kg"`
	InitialMassRaw  string   `json:"initial_mass_raw,omitempty"`
	InitialMassNote string   `json:"initial_mass_note,omitempty"`
	DriftApplied    bool     `json:"drift_applied"`
	BlankScale      float64  `json:"blank_scale"`
	BlankOffsetKg   float64  `json:"blank_offset_kg"`
	BlankApplied    bool     `json:"blank_applied"`
	Points          []Point  `json:"points"`
	Diagnostics     []string `json:"diagnostics,omitempty"`
	SourceBatchIDs  []string `json:"source_batch_ids"`
}

// DtgPoint is one DTG sample (null rate inside gaps).
type DtgPoint struct {
	I       int      `json:"i"`
	TS      float64  `json:"ts"`
	Smooth  *float64 `json:"smooth"`
	Raw     *float64 `json:"raw"`
	Segment int      `json:"segment"`
}

// DtgData is the dtg_series node payload.
type DtgData struct {
	Key          string     `json:"key"`
	Unit         string     `json:"unit"` // 1/s, mass-fraction rate
	WindowS      float64    `json:"window_s"`
	SmoothS      float64    `json:"smooth_s"`
	Points       []DtgPoint `json:"points"`
	Gaps         []Gap      `json:"gaps"`
	SourceNodeID string     `json:"source_node_id"`
}

// Peak is one DTG minimum within a hypothesis.
type Peak struct {
	Index       int     `json:"index"`
	TS          float64 `json:"ts"`
	Rate        float64 `json:"rate"`
	SampleTempK float64 `json:"sample_temp_k,omitempty"`
	Prominence  float64 `json:"prominence"`
	LeftT       float64 `json:"left_t"`
	RightT      float64 `json:"right_t"`
	Note        string  `json:"note,omitempty"`
}

// PeakSet is one complete (alternative) decomposition hypothesis.
type PeakSet struct {
	Hypothesis string `json:"hypothesis"` // h0 = permissive, h1 = conservative
	Label      string `json:"label"`
	Peaks      []Peak `json:"peaks"`
	MergedNote string `json:"merged_note,omitempty"`
}

// Stage is one mass-loss stage candidate.
type Stage struct {
	Hypothesis    string   `json:"hypothesis"`
	Index         int      `json:"index"`
	OnsetT        float64  `json:"onset_t"`
	EndT          float64  `json:"end_t"`
	PeakT         float64  `json:"peak_t"`
	OnsetTempK    *float64 `json:"onset_temp_k"`
	EndTempK      *float64 `json:"end_temp_k"`
	MassLossFrac  *float64 `json:"mass_loss_frac"`
	MassLossPct   *float64 `json:"mass_loss_pct"`
	Status        string   `json:"status"` // retained | rejected | unknown
	RejectReasons []string `json:"reject_reasons,omitempty"`
	OverlapsWith  []int    `json:"overlaps_with,omitempty"`
	CrossesGap    *Gap     `json:"crosses_gap,omitempty"`
	PeakIndex     int      `json:"peak_index"`
}

// ReactionWindow maps a gas valve switch to estimated sample reaction time.
type ReactionWindow struct {
	StageKey      string  `json:"stage_key"`
	StageHyp      string  `json:"stage_hypothesis"`
	StageIndex    int     `json:"stage_index"`
	ValveT        float64 `json:"valve_t"`
	GasFrom       string  `json:"gas_from,omitempty"`
	GasTo         string  `json:"gas_to"`
	DelayS        float64 `json:"delay_s"`
	LagTauS       float64 `json:"lag_tau_s"`
	ReactionTLo   float64 `json:"reaction_t_lo"`
	ReactionTHi   float64 `json:"reaction_t_hi"`
	ObservedPeakT float64 `json:"observed_peak_t"`
	PeakMatches   bool    `json:"peak_matches"`
}

// ChainStep is one readable calculation-chain step for a residue rate.
type ChainStep struct {
	Order  int     `json:"order"`
	Label  string  `json:"label"`
	NodeID string  `json:"node_id"`
	Path   string  `json:"path"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
}

// ResidueData is the final residue candidate with full calculation chain.
type ResidueData struct {
	Key           string      `json:"key"`
	Hypothesis    string      `json:"hypothesis"`
	Status        string      `json:"status"` // retained | rejected | unknown
	Value         *float64    `json:"value"`  // fraction
	Percent       *float64    `json:"percent"`
	Uncertainty   *float64    `json:"uncertainty"`
	TailFromT     *float64    `json:"tail_from_t,omitempty"`
	TailToT       *float64    `json:"tail_to_t,omitempty"`
	NPoints       int         `json:"n_points"`
	InitialMassKg *float64    `json:"initial_mass_kg"`
	RejectReasons []string    `json:"reject_reasons,omitempty"`
	Chain         []ChainStep `json:"chain"`
}

// Derivation is the full deterministic output for one inputs+params set.
type Derivation struct {
	SetID          string                 `json:"set_id"`
	Params         domain.Params          `json:"params"`
	Nodes          []domain.Node          `json:"nodes"`
	RuleEvents     []domain.RuleEvent     `json:"rule_events"`
	Corrected      *CorrectedData         `json:"corrected"`
	Dtg            *DtgData               `json:"dtg"`
	Gaps           []Gap                  `json:"gaps"`
	PeakSets       []PeakSet              `json:"peak_sets"`
	Stages         map[string][]Stage     `json:"stages"`
	Reactions      []ReactionWindow       `json:"reactions"`
	Residues       map[string]ResidueData `json:"residues"`
	SourceBatchIDs map[string]string      `json:"source_batch_ids"`
	Diagnostics    []string               `json:"diagnostics,omitempty"`

	segOf      []int
	dtgPoints  []DtgPoint
	corrNodeID string
	dtgNodeID  string
}
