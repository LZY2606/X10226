package domain

import "fmt"

// Provenance identifies the exact recipe that produced a derived object.
type Provenance struct {
	AlgorithmVersion string   `json:"algorithm_version"`
	ParamFingerprint string   `json:"param_fingerprint"`
	RunID            string   `json:"run_id"`
	SourceIDs        []string `json:"source_ids"`
	ParentRunIDs     []string `json:"parent_run_ids,omitempty"`
}

// Finding is a machine-readable judgement emitted by the pipeline. Severity is
// info | warning | error; Code is a stable machine code.
type Finding struct {
	ID       string   `json:"id"`
	Code     string   `json:"code"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	ObjectID string   `json:"object_id,omitempty"`
	SourceIDs []string `json:"source_ids,omitempty"`
}

// CorrectionPoint is the drift-corrected/blank-subtracted series at one time.
type CorrectionPoint struct {
	ID         string  `json:"id"`
	SampleID   string  `json:"sample_id"`
	T          float64 `json:"t"`            // canonical s
	Temp       float64 `json:"temp"`         // canonical degC
	RawMass    float64 `json:"raw_mass"`     // canonical mg
	BlankMass  float64 `json:"blank_mass"`   // canonical mg, interpolated
	DriftOffset float64 `json:"drift_offset"` // canonical mg
	CorrMass   float64 `json:"corr_mass"`    // canonical mg
	AcrossGap  bool    `json:"across_gap"`    // no interpolation crosses this
	Usable     bool    `json:"usable"`
}

// DTGPoint is one differentiated point. DTG is never interpolated across gaps;
// points adjacent to a gap or a non-monotone perturbation stay UNCERTAIN.
type DTGPoint struct {
	ID          string  `json:"id"`
	CorrectionID string `json:"correction_id"`
	T           float64 `json:"t"`
	Temp        float64 `json:"temp"`
	Mass        float64 `json:"mass"`
	DTG         float64 `json:"dtg"` // smoothed mg/min, negative during loss
	Confidence  string  `json:"confidence"` // OK | UNCERTAIN
	AcrossGap   bool    `json:"across_gap"`
}

// Peak is one detected DTG minimum (a candidate mass-loss event).
type Peak struct {
	ID          string  `json:"id"`
	T           float64 `json:"t"`
	Temp        float64 `json:"temp"`
	DTG         float64 `json:"dtg"`
	Prominence  float64 `json:"prominence"`
	Overlap     bool    `json:"overlap"`
	Attribution string  `json:"attribution"` // UNIQUE | AMBIGUOUS
}

// AtmosphereEpisode is the reaction-time interval at the sample of a gas
// atmosphere. Valve time is stored separately; gas_delay_s and temp_lag_s are
// the applied corrections.
type AtmosphereEpisode struct {
	ID            string  `json:"id"`
	GasEventID    string  `json:"gas_event_id"`
	Gas           string  `json:"gas"`
	ValveT        float64 `json:"valve_t"`
	ReactionT     float64 `json:"reaction_t"`
	GasDelay      float64 `json:"gas_delay_s"`
	TempLag       float64 `json:"temp_lag_s"`
	EndT          float64 `json:"end_t"` // start of next reaction episode
}

// CalcStep is one line of the expandable residue calculation chain.
type CalcStep struct {
	Label  string  `json:"label"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Detail string  `json:"detail"`
}

// StageCandidate is one proposed decomposition stage. Stages may overlap and
// their mass losses are not forced to sum to 100%. Rejected candidates keep
// their rejection reason.
type StageCandidate struct {
	ID           string     `json:"id"`
	PeakIDs      []string   `json:"peak_ids"`
	TOnset       float64    `json:"t_onset"`
	TEnd         float64    `json:"t_end"`
	TempOnset    float64    `json:"temp_onset"`
	TempEnd      float64    `json:"temp_end"`
	MassOnset    float64    `json:"mass_onset_mg"`
	MassEnd      float64    `json:"mass_end_mg"`
	MassLossMg   float64    `json:"mass_loss_mg"`
	MassLossPct  float64    `json:"mass_loss_pct"`
	OverlapsWith []string   `json:"overlaps_with,omitempty"`
	Gas          string     `json:"gas,omitempty"`
	Attribution  string     `json:"attribution"` // UNIQUE | AMBIGUOUS
	Status       string     `json:"status"`      // PROPOSED | ACCEPTED | REJECTED | ADJUSTED
	RejectReason string     `json:"reject_reason,omitempty"`
	DecisionID   string     `json:"decision_id,omitempty"`
	Chain        []CalcStep `json:"chain,omitempty"`
}

// ResidueResult is the final residue fraction with its full calculation chain.
type ResidueResult struct {
	ID             string     `json:"id"`
	InitialMassID  string     `json:"initial_mass_id"`
	InitialMassMg  float64    `json:"initial_mass_mg"`
	FinalMassMg    float64    `json:"final_mass_mg"`
	FinalT         float64    `json:"final_t"`
	FinalTemp      float64    `json:"final_temp"`
	ResiduePct     float64    `json:"residue_pct"`
	Unknown        bool       `json:"unknown"`
	UnknownReason  string     `json:"unknown_reason,omitempty"`
	Chain          []CalcStep `json:"chain"`
	Status         string     `json:"status"` // CURRENT | STALE
}

// DerivedSet bundles everything one pipeline run produces.
type DerivedSet struct {
	RunID       string             `json:"run_id"`
	Prov        Provenance         `json:"provenance"`
	Params      Params             `json:"params"`
	Corrections []CorrectionPoint  `json:"corrections"`
	DTG         []DTGPoint         `json:"dtg"`
	Peaks       []Peak             `json:"peaks"`
	Episodes    []AtmosphereEpisode `json:"episodes"`
	Stages      []StageCandidate   `json:"stages"`
	Residue     ResidueResult      `json:"residue"`
	Findings    []Finding          `json:"findings"`
}

// Params are the tunable analysis parameters. Every change produces a new
// parameter version (params revision event), never an in-place overwrite.
type Params struct {
	GasDelayS            float64 `json:"gas_delay_s"`
	TempLagS             float64 `json:"temp_lag_s"`
	BlankSubtraction     bool    `json:"blank_subtraction"`
	DriftCorrection      bool    `json:"drift_correction"`
	SmoothingWindow      int     `json:"smoothing_window"` // points
	PeakProminenceMgMin  float64 `json:"peak_prominence_mg_min"`
	GapThresholdS        float64 `json:"gap_threshold_s"`
	OverlapWindowS       float64 `json:"overlap_window_s"`
}

// DefaultParams is the v1 parameter set.
func DefaultParams() Params {
	return Params{
		GasDelayS:           20,
		TempLagS:            15,
		BlankSubtraction:    true,
		DriftCorrection:     true,
		SmoothingWindow:     5,
		PeakProminenceMgMin: 0.02,
		GapThresholdS:       60,
		OverlapWindowS:      40,
	}
}

// Validate normalises and validates parameters.
func (p Params) Validate() error {
	if p.GasDelayS < 0 || p.TempLagS < 0 {
		return fmt.Errorf("delays must be non-negative")
	}
	if p.SmoothingWindow < 1 {
		return fmt.Errorf("smoothing_window must be >= 1")
	}
	if p.GapThresholdS <= 0 {
		return fmt.Errorf("gap_threshold_s must be positive")
	}
	return nil
}
