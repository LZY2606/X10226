package calc

import (
	"fmt"
	"math"

	"gotga/internal/domain"
)

// buildStages turns peaks into stage candidates. Valve switch time is mapped to
// reaction time via atmosphere delay and thermal lag; they are explicitly not
// equal. Stages are intervals around the DTG peak bounded by shoulders, may
// overlap, and their losses are never normalized to sum 100%.
func buildStages(segs [][]domain.Point, peaks []domain.Peak, gaps []domain.Gap, in *Inputs, p domain.Params, fp string, seq int64) []domain.StageCandidate {
	var flat []domain.Point
	for _, s := range segs {
		flat = append(flat, s...)
	}
	var stages []domain.StageCandidate
	provDocs := []string{in.SampleDoc.ID}
	if in.GasDoc != nil {
		provDocs = append(provDocs, in.GasDoc.ID)
	}
	if in.Mass0Doc != nil {
		provDocs = append(provDocs, in.Mass0Doc.ID)
	}
	for _, pk := range peaks {
		a, b := stageWindow(flat, pk, gaps)
		if a == nil || b == nil {
			continue
		}
		st := domain.StageCandidate{
			ID:           domain.HashID("stage", fp, pk.ID, a.T, b.T),
			GroupID:      pk.GroupID,
			AltOf:        pk.AltOf,
			StartT:       a.T,
			EndT:         b.T,
			StartTempC:   a.Tmp,
			EndTempC:     b.Tmp,
			StartM:       a.M,
			EndM:         b.M,
			LossMG:       a.M - b.M,
			PeakID:       pk.ID,
			ReactionT:    &pk.T,
			ReactionTemp: pk.TempC,
			Status:       "current",
		}
		if in.Mass0.Valid && in.Mass0.MG > 0 {
			pct := st.LossMG / in.Mass0.MG * 100
			st.LossPct = &pct
		}
		if v := nearestValve(pk.T, in.Valves, p); v != nil {
			vt := v.T
			st.ValveT = &vt
			st.ValveGas = v.Gas
		}
		st.Prov = domain.Provenance{
			Algorithm: p.Version + ":stage-window", ParamFP: fp, DocIDs: provDocs, EventSeq: seq,
			Formula: fmt.Sprintf("reaction_t = valve_t + atm_delay(%.0fs); T_sample = T_program - temp_lag(%.0fC); stage loss = m_start-m_end", p.AtmDelayS, p.TempLagC),
			Inputs: []domain.ValueRef{
				{ID: pk.ID, Label: "DTG peak", Value: pk.T, Unit: "s"},
			},
		}
		stages = append(stages, st)
	}
	// Overlap relations and ambiguity rejection flags.
	for i := range stages {
		for j := range stages {
			if i == j {
				continue
			}
			if stages[i].StartT < stages[j].EndT && stages[j].StartT < stages[i].EndT {
				stages[i].OverlapsWith = append(stages[i].OverlapsWith, stages[j].ID)
			}
		}
	}
	groupLead := map[string]string{}
	for _, st := range stages {
		if st.AltOf == "" {
			groupLead[st.GroupID] = st.ID
		}
	}
	for i := range stages {
		if stages[i].AltOf != "" {
			stages[i].JudgmentIDs = append(stages[i].JudgmentIDs,
				domain.HashID("judg", domain.AxiomAmbiguityKept, stages[i].ID))
		}
	}
	return stages
}

// stageWindow brackets a peak between DTG shoulders (derivative near zero),
// never crossing a gap.
func stageWindow(flat []domain.Point, pk domain.Peak, gaps []domain.Gap) (*domain.Point, *domain.Point) {
	idx := -1
	for i := range flat {
		if flat[i].T == pk.T {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, nil
	}
	left := idx
	for left > 0 && flat[left-1].D < -1e-9 && !gapBetween(flat[left-1].T, flat[left].T, gaps) {
		left--
		if math.Abs(flat[left].D) <= math.Abs(pk.Rate)*0.05 {
			break
		}
	}
	right := idx
	for right < len(flat)-1 && flat[right+1].D < -1e-9 && !gapBetween(flat[right].T, flat[right+1].T, gaps) {
		right++
		if math.Abs(flat[right].D) <= math.Abs(pk.Rate)*0.05 {
			break
		}
	}
	return &flat[left], &flat[right]
}

func gapBetween(t1, t2 float64, gaps []domain.Gap) bool {
	for _, g := range gaps {
		if t1 <= g.FromT+1e-9 && t2 >= g.ToT-1e-9 {
			return true
		}
	}
	return false
}

// nearestValve finds the valve event whose delayed reaction time best matches
// the observed peak: reaction_t = valve_t + atm_delay.
func nearestValve(reactionT float64, valves []ValveEvent, p domain.Params) *ValveEvent {
	var best *ValveEvent
	bestD := math.MaxFloat64
	for i := range valves {
		predicted := valves[i].T + p.AtmDelayS
		if d := math.Abs(predicted - reactionT); d < bestD {
			bestD = d
			best = &valves[i]
		}
	}
	return best
}

func formatWindow(t float64) string {
	return fmt.Sprintf("%.0fs", t)
}
