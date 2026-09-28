package engine

import (
	"fmt"

	"tga/internal/domain"
)

// collectJudgements turns the domain reasoning attached to derived results into
// machine-readable judgement records. These are the persisted "judgement basis"
// that the review interface and replay read back.
func collectJudgements(series *domain.SeriesResult, dtg *domain.DTGResult,
	stages *domain.StagesResult, residue *domain.ResidueResultSet) []*domain.Judgement {

	var js []*domain.Judgement
	add := func(code, msg string, subjects ...string) {
		js = append(js, &domain.Judgement{Code: code, Message: msg, Subjects: subjects})
	}

	// Sampling gaps are never interpolated across.
	for _, g := range series.Gaps {
		if g.Interpolated {
			continue
		}
		j := &domain.Judgement{
			Code:    "gap_not_bridged",
			Message: fmt.Sprintf("sampling gap from t=%gs to t=%gs (%d missing) is not interpolated across; no stage or DTG value crosses it", g.FromS, g.ToS, g.NSkipped),
			StartS:  g.FromS, EndS: g.ToS,
			Facts: map[string]any{
				"from_s": g.FromS, "to_s": g.ToS, "n_skipped": g.NSkipped,
				"interpolation": "forbidden",
			},
		}
		js = append(js, j)
	}

	// Balance baseline drift handling.
	for _, d := range series.Diagnostics {
		if d.Code == "drift_linear" || d.Code == "drift_blank" || d.Code == "drift_blank_unavailable" {
			add(d.Code, d.Message)
		}
		if d.Code == "nonmonotonic_noise" {
			js = append(js, &domain.Judgement{
				Code: "nonmonotonic_noise", StartS: d.StartS,
				Message: d.Message, Facts: map[string]any{"policy": "flag_not_smoothed"},
			})
		}
	}

	// Atmosphere delay / temperature lag: valve time != reaction time.
	if stages != nil {
		for _, w := range stages.GasTimeline {
			if w.ValveFromS != w.FromS {
				js = append(js, &domain.Judgement{
					Code:    "atmosphere_delay",
					Message: fmt.Sprintf("valve switch at t=%gs reaches the sample reaction clock at t=%gs; switch time is not reaction time", w.ValveFromS, w.FromS),
					StartS:  w.ValveFromS, EndS: w.FromS,
					Subjects: []string{w.Gas},
					Facts: map[string]any{
						"valve_time_s": w.ValveFromS, "effective_time_s": w.FromS, "gas": w.Gas,
						"temperature_lag_present": len(stages.ProgramCurve) > 0,
					},
				})
			}
		}
		if len(stages.ProgramCurve) > 0 {
			first := stages.ProgramCurve[0]
			if last := stages.ProgramCurve[len(stages.ProgramCurve)-1]; last.ExpectedSampleC != last.ProgramTempC {
				add("temperature_lag",
					"furnace program temperature and measured sample temperature differ by the configured lag; program time is aligned but not equated with reaction temperature")
			}
			_ = first
		}

		// Overlapping stages + no forced 100% sum; alternatives retained.
		for _, st := range stages.Stages {
			if st.Ambiguous {
				var altIDs []string
				for _, a := range st.Alternatives {
					altIDs = append(altIDs, a.ID)
				}
				js = append(js, &domain.Judgement{
					Code:     "overlapping_peaks",
					Message:  fmt.Sprintf("stage %s overlaps %v; all legal decompositions retained and losses are not forced to total 100%%", st.Label, st.Overlaps),
					StartS:   st.StartS, EndS: st.EndS, Subjects: append([]string{st.ID}, altIDs...),
					Facts: map[string]any{
						"stage_id": st.ID, "alternatives": len(st.Alternatives),
						"sum_constraint": "not_forced_100pct",
					},
				})
			}
			if st.RejectReason != "" {
				add("stage_rejected", st.Label+": "+st.RejectReason, st.ID)
			}
		}
	}

	// Unknown / assumption residue results.
	if residue != nil {
		for _, r := range residue.Results {
			if r.Unknown {
				js = append(js, &domain.Judgement{
					Code:     "residue_unknown",
					Message:  r.Chain[len(r.Chain)-1].Detail,
					Subjects: []string{r.ID},
					Facts:    map[string]any{"result_id": r.ID, "status": "unknown"},
				})
			}
		}
		for _, d := range residue.Diagnostics {
			if d.Code == "residue_reference_assumed" || d.Code == "initial_mass_unconverted" {
				add(d.Code, d.Message)
			}
		}
	}
	return js
}
