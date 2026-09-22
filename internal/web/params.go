package web

import "tga-lineage/internal/model"

// ParamsRequest carries only the fields a reviewer may adjust. Every change
// becomes a new, parent-linked parameter version with a fresh fingerprint.
type ParamsRequest struct {
	Reason        *string  `json:"reason"`
	GasDelayMin   *float64 `json:"gas_delay_min"`
	ThermalLagMin *float64 `json:"thermal_lag_min"`
	BlankGain     *float64 `json:"blank_gain"`
	DriftOffsetMg *float64 `json:"drift_offset_mg"`
	DriftEnabled  *bool    `json:"drift_enabled"`
	MaxStepMin    *float64 `json:"max_step_min"`
	RelTolerance  *float64 `json:"rel_tolerance"`
	Prominence    *float64 `json:"prominence_mg_per_min"`
	WindowMin     *float64 `json:"window_min"`
}

func (s *Server) applyParamsToState(p model.ParamSet, req ParamsRequest) model.ParamSet {
	if req.GasDelayMin != nil {
		p.Atmosphere.GasDelayMin = *req.GasDelayMin
	}
	if req.ThermalLagMin != nil {
		p.Atmosphere.ThermalLagMin = *req.ThermalLagMin
	}
	if req.BlankGain != nil {
		p.Drift.BlankGain = *req.BlankGain
	}
	if req.DriftOffsetMg != nil {
		p.Drift.Offset = *req.DriftOffsetMg
	}
	if req.DriftEnabled != nil {
		p.Drift.Enabled = *req.DriftEnabled
	}
	if req.MaxStepMin != nil {
		p.Gap.MaxStepMin = *req.MaxStepMin
	}
	if req.RelTolerance != nil {
		p.Noise.RelTolerance = *req.RelTolerance
	}
	if req.Prominence != nil {
		p.Peak.Prominence = *req.Prominence
	}
	if req.WindowMin != nil {
		p.Peak.WindowMin = *req.WindowMin
	}
	return p
}
