package pipeline

import (
	"fmt"
	"sort"

	"tga/internal/domain"
)

// buildAtmosphere records, per valve switch, both the valve time and the
// estimated reaction-zone time. Valve switch time is NOT reaction time: the
// reaction atmosphere changes gasDelayS later (transport) and the thermal
// response trails by tempLagS.
func buildAtmosphere(an *Analysis, layers *domain.ImportedLayers, p domain.AnalysisParams) {
	if len(layers.Gas) == 0 {
		return
	}
	sorted := make([]domain.GasEvent, len(layers.Gas))
	copy(sorted, layers.Gas)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time.Value < sorted[j].Time.Value })
	for i, g := range sorted {
		if !g.Time.Valid {
			an.Findings = append(an.Findings, domain.Finding{
				Code: "gas_time_unconvertible", Severity: "warn",
				Message: fmt.Sprintf("gas event on line %d has no furnace time; timeline not shifted", g.Line),
			})
			continue
		}
		rt := g.Time.Value + p.GasDelayS + p.TempLagS
		an.Atmosphere = append(an.Atmosphere, AtmosphereState{
			ValveTimeS: g.Time.Value, ReactionTimeS: rt,
			FromGas: g.FromGas, ToGas: g.ToGas,
		})
		an.Findings = append(an.Findings, domain.Finding{
			Code: "gas_reaction_delay", Severity: "info",
			Message: fmt.Sprintf("valve %s->%s at %.1fs reaches reaction zone at %.1fs (+%.0fs gas, +%.0fs lag)",
				g.FromGas, g.ToGas, g.Time.Value, rt, p.GasDelayS, p.TempLagS),
			StartS: g.Time.Value, EndS: rt, Ref: hnum("gasdelay", i),
		})
	}
}

// gasAt returns the valve-log gas and the reaction-zone gas at time t.
func gasAt(an *Analysis, t float64, layers *domain.ImportedLayers) (valve, reaction string) {
	current := ""
	valveGas := ""
	sorted := make([]domain.GasEvent, 0, len(layers.Gas))
	for _, g := range layers.Gas {
		if g.Time.Valid {
			sorted = append(sorted, g)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Time.Value < sorted[j].Time.Value })
	for _, g := range sorted {
		if current == "" {
			current = g.FromGas
		}
		if g.Time.Value <= t {
			valveGas = g.ToGas
		}
	}
	if valveGas == "" && len(sorted) > 0 {
		valveGas = sorted[0].FromGas
	}
	reactionGas := valveGas
	for _, st := range an.Atmosphere {
		if st.ReactionTimeS <= t {
			reactionGas = st.ToGas
		}
	}
	if current == "" && valveGas == "" {
		return "unknown", "unknown"
	}
	return valveGas, reactionGas
}
