package domain

import "encoding/json"

const (
	AlgoNormalize = "normalize"
	AlgoCorrect   = "drift_correction"
	AlgoBlank     = "blank_subtraction"
	AlgoDTG       = "dtg_central"
	AlgoGap       = "gap_detect"
	AlgoWindow    = "reaction_window"
	AlgoPeak      = "dtg_peaks"
	AlgoStage     = "stage_decompose"
	AlgoResidue   = "residue_chain"
	AlgoInitMass  = "init_mass_select"

	AlgoVersion = "1.0.0"
)

type Params struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"project_id"`
	Version        int     `json:"version"`
	ParentID       string  `json:"parent_id,omitempty"`
	Name           string  `json:"name"`

	DriftApply     bool    `json:"drift_apply"`
	BlankApply     bool    `json:"blank_apply"`

	GapMaxIntervalMin float64 `json:"gap_max_interval_min"`
	GapMaxDTMin       float64 `json:"gap_max_dt_min"`

	DTGWindowPoints int     `json:"dtg_window_points"`
	NoiseRelThreshold float64 `json:"noise_rel_threshold"`
	SpikeRelThreshold float64 `json:"spike_rel_threshold"`

	PeakAbsMgMin   float64 `json:"peak_abs_mg_min"`
	PeakProminence float64 `json:"peak_prominence_mg"`
	OverlapPeakWindowMin float64 `json:"overlap_peak_window_min"`

	AtmosphereDelayMin float64 `json:"atmosphere_delay_min"`
	ThermalLagK        float64 `json:"thermal_lag_k"`

	StageMarginMin float64 `json:"stage_margin_min"`

	InitMassStrategy string `json:"init_mass_strategy"`
	InitMassRow      int    `json:"init_mass_row"`
}

func DefaultParams(projectID string) Params {
	return Params{
		ProjectID: projectID, Name: "default",
		DriftApply: true, BlankApply: true,
		GapMaxIntervalMin: 2.0, GapMaxDTMin: 2.0,
		DTGWindowPoints: 3,
		NoiseRelThreshold: 0.25, SpikeRelThreshold: 3.0,
		PeakAbsMgMin: 0.02, PeakProminence: 0.01, OverlapPeakWindowMin: 8.0,
		AtmosphereDelayMin: 1.5, ThermalLagK: 4.0,
		StageMarginMin: 1.0,
		InitMassStrategy: "first",
	}
}

// CanonicalJSON deterministically serializes the analysis parameters.
func (p Params) CanonicalJSON() string {
	type slim struct {
		DriftApply bool `json:"drift_apply"`
		BlankApply bool `json:"blank_apply"`
		GapMaxIntervalMin float64 `json:"gap_max_interval_min"`
		GapMaxDTMin float64 `json:"gap_max_dt_min"`
		DTGWindowPoints int `json:"dtg_window_points"`
		NoiseRelThreshold float64 `json:"noise_rel_threshold"`
		SpikeRelThreshold float64 `json:"spike_rel_threshold"`
		PeakAbsMgMin float64 `json:"peak_abs_mg_min"`
		PeakProminence float64 `json:"peak_prominence_mg"`
		OverlapPeakWindowMin float64 `json:"overlap_peak_window_min"`
		AtmosphereDelayMin float64 `json:"atmosphere_delay_min"`
		ThermalLagK float64 `json:"thermal_lag_k"`
		StageMarginMin float64 `json:"stage_margin_min"`
		InitMassStrategy string `json:"init_mass_strategy"`
		InitMassRow int `json:"init_mass_row"`
	}
	b, _ := json.Marshal(slim{
		p.DriftApply, p.BlankApply, p.GapMaxIntervalMin, p.GapMaxDTMin,
		p.DTGWindowPoints, p.NoiseRelThreshold, p.SpikeRelThreshold,
		p.PeakAbsMgMin, p.PeakProminence, p.OverlapPeakWindowMin,
		p.AtmosphereDelayMin, p.ThermalLagK, p.StageMarginMin,
		p.InitMassStrategy, p.InitMassRow,
	})
	return string(b)
}
