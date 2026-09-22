package compute

import (
	"sort"

	"tgareview/internal/domain"
)

// atmosphereIntervals maps valve events to the atmosphere at the sample.
// Valve switch time + atmosphere delay = effective reaction-window start;
// the two are never treated as equal.
func atmosphereIntervals(gas []domain.GasEvent, p domain.Params, prov domain.Provenance) []domain.AtmosphereInterval {
	ev := make([]domain.GasEvent, len(gas))
	copy(ev, gas)
	sort.Slice(ev, func(i, j int) bool { return ev[i].ValveAtS.Value < ev[j].ValveAtS.Value })
	var out []domain.AtmosphereInterval
	current := "ambient"
	startT := 0.0
	flush := func(end float64, valve string) {
		id := domain.ShortID("atm", f64(startT), current)
		out = append(out, domain.AtmosphereInterval{
			ID: id, StartS: startT, EndS: end, Gas: current,
			ValveEvent: valve, Status: domain.StatusCurrent,
			Prov: withSources(prov,
				domain.Source{Kind: "derived", ID: id, Detail: codeValveNotReaction}),
		})
	}
	for _, e := range ev {
		switchAt := e.ValveAtS.Value + p.AtmosphereDelayS
		if switchAt > startT {
			flush(switchAt, e.ID)
		}
		current = e.ToGas
		startT = switchAt
	}
	if len(out) > 0 || startT >= 0 {
		flush(1e12, "")
	}
	return out
}

// gasAt returns the atmosphere labels overlapping [t-margin, t+margin].
// When the peak sits inside the uncertainty margin of a boundary, every
// legal assignment is returned (the stage is marked ambiguous).
func gasAt(atms []domain.AtmosphereInterval, t, margin float64) []string {
	seen := map[string]bool{}
	var labels []string
	add := func(g string) {
		if !seen[g] {
			seen[g] = true
			labels = append(labels, g)
		}
	}
	for _, a := range atms {
		if t >= a.StartS && t < a.EndS {
			add(a.Gas)
		}
		if t+margin >= a.StartS && t-margin <= a.StartS {
			add(a.Gas)
		}
	}
	if len(labels) == 0 {
		labels = append(labels, "unknown")
	}
	return labels
}
