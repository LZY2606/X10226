package server

import "tga-review/internal/domain"

type paramsPayload struct {
	GasDelayS           float64 `json:"gas_delay_s"`
	TempLagS            float64 `json:"temp_lag_s"`
	BlankSubtraction    bool    `json:"blank_subtraction"`
	DriftCorrection     bool    `json:"drift_correction"`
	SmoothingWindow     int     `json:"smoothing_window"`
	PeakProminenceMgMin float64 `json:"peak_prominence_mg_min"`
	GapThresholdS       float64 `json:"gap_threshold_s"`
	OverlapWindowS      float64 `json:"overlap_window_s"`
}

func (p paramsPayload) toDomain() domain.Params {
	// zero-value booleans are legitimate (disable corrections); numeric zeros
	// fall back to defaults only when the whole object looks empty.
	d := domain.DefaultParams()
	out := domain.Params{
		GasDelayS:           p.GasDelayS,
		TempLagS:            p.TempLagS,
		BlankSubtraction:    p.BlankSubtraction,
		DriftCorrection:     p.DriftCorrection,
		SmoothingWindow:     p.SmoothingWindow,
		PeakProminenceMgMin: p.PeakProminenceMgMin,
		GapThresholdS:       p.GapThresholdS,
		OverlapWindowS:      p.OverlapWindowS,
	}
	if p.SmoothingWindow == 0 {
		out.SmoothingWindow = d.SmoothingWindow
	}
	if p.GapThresholdS == 0 {
		out.GapThresholdS = d.GapThresholdS
	}
	if p.PeakProminenceMgMin == 0 {
		out.PeakProminenceMgMin = d.PeakProminenceMgMin
	}
	if p.OverlapWindowS == 0 {
		out.OverlapWindowS = d.OverlapWindowS
	}
	return out
}

type decideReq struct {
	Reviewer  string   `json:"reviewer"`
	TargetType string  `json:"target_type"`
	TargetID  string   `json:"target_id"`
	Action    string   `json:"action"`
	Reason    string   `json:"reason"`
	NewOnsetT *float64 `json:"new_onset_t"`
	NewEndT   *float64 `json:"new_end_t"`
}

func (d decideReq) toDomain() domain.DecisionPayload {
	return domain.DecisionPayload{
		TargetType: d.TargetType,
		TargetID:   d.TargetID,
		Action:     d.Action,
		Reason:     d.Reason,
		NewOnsetT:  d.NewOnsetT,
		NewEndT:    d.NewEndT,
	}
}
