package derive

import (
	"fmt"

	"tga/internal/domain"
)

// mapAtmosphere shifts every logged valve switch by the gas transport delay
// and sample temperature lag to obtain a reaction-time interval. Valve time
// and reaction time are deliberately different first-class values.
func mapAtmosphere(p domain.Params, in *Inputs) []domain.AtmosReaction {
	out := make([]domain.AtmosReaction, 0, len(in.Valves))
	shift := p.AtmosDelay
	if p.TempLag > shift {
		shift = p.TempLag
	}
	for i := range in.Valves {
		v := in.Valves[i]
		t, _ := normalizeTime(v.Time.Raw, v.Time.Unit)
		if t == nil {
			continue
		}
		id := fmt.Sprintf("valve:%d", i)
		out = append(out, domain.AtmosReaction{
			ValveID:      id,
			ValveTime:    round(*t, 6),
			ReactionTime: round(*t+shift, 6),
			FromGas:      v.FromGas,
			ToGas:        v.ToGas,
			DelayS:       round(shift, 6),
			UncertaintyS: round(abs(p.AtmosDelay-p.TempLag), 6),
		})
	}
	return out
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// atmosRules returns the machine readable atmosphere judgments.
func atmosRules(p domain.Params, in *Inputs, atmos []domain.AtmosReaction) []domain.RuleEvent {
	if len(atmos) == 0 {
		return nil
	}
	subs := make([]string, 0, len(atmos))
	for _, a := range atmos {
		subs = append(subs, a.ValveID)
	}
	return []domain.RuleEvent{
		{
			Code: domain.RuleAtmosDelay, Severity: "info",
			Message:   "gas transport delay shifts valve switch time away from sample reaction time",
			Evidence:  map[string]any{"delay_s": p.AtmosDelay, "subjects": subs},
			SourceIDs: sourceID(in, domain.KindGasEvent),
		},
		{
			Code: domain.RuleTempLag, Severity: "info",
			Message:  "furnace program temperature leads sample temperature; reaction mapped with lag",
			Evidence: map[string]any{"temp_lag_s": p.TempLag},
		},
	}
}
