package domain

// Status values shared by points, candidates and chain nodes.
const (
	StatusValid       = "valid"
	StatusUnknown     = "unknown"
	StatusGapAdjacent = "gap_adjacent"
	StatusNonMonotone = "non_monotone"
	StatusProposed    = "proposed"
	StatusAccepted    = "accepted"
	StatusRejected    = "rejected"
	StatusAdjusted    = "adjusted"
)

// Gap is a contiguous sampling gap; nothing is interpolated across it.
type Gap struct {
	ID        string  `json:"id"`
	FromTS    float64 `json:"from_furnace_time_s"`
	ToTS      float64 `json:"to_furnace_time_s"`
	MissingN  int     `json:"missing_points_estimate"`
}

// CorrectedPoint carries identity for every displayed value.
type CorrectedPoint struct {
	ID              string   `json:"id"`
	Row             int      `json:"row"`
	FurnaceTimeS    Quantity `json:"furnace_time_s"`
	MeasuredTempC   Quantity `json:"measured_temp_c"`
	ProgramTempC    Quantity `json:"program_temp_c"`
	RawMassMg       Quantity `json:"raw_mass_mg"`
	DriftCorrMg     Quantity `json:"drift_correction_mg"`
	CorrectedMassMg Quantity `json:"corrected_mass_mg"`
	Status          string   `json:"status"`
}

// CorrectedSeriesPayload.
type CorrectedSeriesPayload struct {
	Points       []CorrectedPoint `json:"points"`
	Gaps         []Gap            `json:"gaps"`
	NonMonotone  []int            `json:"non_monotone_rows"`
	InitialMass  Quantity         `json:"initial_mass_mg"`
}

// TGPoint is one normalized TG point.
type TGPoint struct {
	ID           string   `json:"id"`
	Row          int      `json:"row"`
	FurnaceTimeS Quantity `json:"furnace_time_s"`
	TempC        Quantity `json:"measured_temp_c"`
	CorrectedMg  Quantity `json:"corrected_mass_mg"`
	Percent      Quantity `json:"percent_of_initial"`
	Status       string   `json:"status"`
}

// TGPayload.
type TGPayload struct {
	Points []TGPoint `json:"points"`
	Gaps   []Gap     `json:"gaps"`
}

// DTGPoint. DTG is computed only from adjacent valid points; gap neighbors are
// marked and never bridged.
type DTGPoint struct {
	ID           string   `json:"id"`
	Row          int      `json:"row"`
	FurnaceTimeS Quantity `json:"furnace_time_s"`
	RawPctMin    Quantity `json:"raw_pct_per_min"`
	SmoothPctMin Quantity `json:"smooth_pct_per_min"`
	Status       string   `json:"status"`
}

// DTGPayload.
type DTGPayload struct {
	Points []DTGPoint `json:"points"`
	Gaps   []Gap      `json:"gaps"`
}

// ReactionWindow is a valve switch mapped onto the sample reaction clock.
type ReactionWindow struct {
	ID             string   `json:"id"`
	ValveEventID   string   `json:"valve_event_id"`
	ValveTimeS     Quantity `json:"valve_furnace_time_s"`
	ReactionTimeS  Quantity `json:"sample_reaction_time_s"`
	MeasuredTempC  Quantity `json:"measured_temp_at_reaction_C"`
	ProgramTempC   Quantity `json:"program_temp_at_valve_C"`
	FromGas        string   `json:"from_gas"`
	ToGas          string   `json:"to_gas"`
	GasDelayS      float64  `json:"gas_delay_s"`
	TempLagS       float64  `json:"temp_lag_s"`
}

// ReactionWindowsPayload is the machine-readable basis for the rule that valve
// switch time does not equal sample reaction time.
type ReactionWindowsPayload struct {
	Windows []ReactionWindow `json:"windows"`
}

// StageCandidate is one legal decomposition stage. Stages may overlap and
// their losses are not forced to sum to 100%.
type StageCandidate struct {
	ID           string   `json:"id"`
	GroupID      string   `json:"group_id"`
	Label        string   `json:"label"`
	StartTS      float64  `json:"start_furnace_time_s"`
	EndTS        float64  `json:"end_furnace_time_s"`
	StartTempC   float64  `json:"start_temp_C"`
	EndTempC     float64  `json:"end_temp_C"`
	PeakTS       float64  `json:"peak_furnace_time_s"`
	PeakPctMin   float64  `json:"peak_pct_per_min"`
	MassLossPct  Quantity `json:"mass_loss_pct_of_initial"`
	GasAtPeak    string   `json:"gas_at_peak"`
	WindowID     string   `json:"reaction_window_id,omitempty"`
	OverlapsWith []string `json:"overlaps,omitempty"`
	Status       string   `json:"status"`
	RejectReason string   `json:"reject_reason,omitempty"`
	// Decision records the human ruling, if any.
	Decision *Decision `json:"decision,omitempty"`
	// Adjusted is present when a human moved a boundary; the proposal stays.
	Adjusted *StageCandidate `json:"adjusted,omitempty"`
}

// StagesPayload keeps all legal candidates, including rejected ones.
type StagesPayload struct {
	Candidates []StageCandidate `json:"candidates"`
	Groups     []string         `json:"groups"`
}

// ChainNode is one node in the expandable residue calculation chain.
type ChainNode struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Value   Quantity `json:"value"`
	Status  string   `json:"status"`
	DepID   string   `json:"dep_id"`
	Formula string   `json:"formula,omitempty"`
	Order   int      `json:"order"`
}

// ResiduePayload is the final residue plus the full calculation chain.
type ResiduePayload struct {
	FinalPercent Quantity    `json:"final_residue_pct"`
	FinalMassMg  Quantity    `json:"final_residue_mass_mg"`
	Chain        []ChainNode `json:"chain"`
	TailStartTS  float64     `json:"tail_start_furnace_time_s"`
	TailEndTS    float64     `json:"tail_end_furnace_time_s"`
}
