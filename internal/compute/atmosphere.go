package compute

import (
	"tga-review/internal/domain"
)

// mapAtmospheres shifts every valve switch by the configured transport delay.
// The furnace lag only affects temperature reasoning, never valve time, so the
// two delays stay separate and auditable.
func mapAtmospheres(p Params, b Bundle) []AtmosphereWindow {
	var out []AtmosphereWindow
	for _, ev := range b.Gas {
		if !ev.Time.Normalised {
			continue
		}
		w := AtmosphereWindow{
			ValveSource: b.Sources[domain.LayerGas],
			ValveT:      ev.Time.Canonical,
			ReactionT:   round(ev.Time.Canonical+p.AtmosphereDelay, 6),
			FromGas:     ev.From,
			ToGas:       ev.To,
			DelayMin:    p.AtmosphereDelay,
			Note:        "valve switch time != sample reaction time; shifted by transport delay",
		}
		w.ID = ID("gasw", ftoa(ev.Time.Canonical), ev.From, ev.To)
		out = append(out, w)
	}
	return out
}

// gasAt returns the gas reacting at the sample at time t, plus the source
// window id. Empty means unknown and the caller keeps the unknown, not a guess.
func gasAt(windows []AtmosphereWindow, t float64) (string, string) {
	gas, id := "", ""
	for _, w := range windows {
		if t+1e-9 >= w.ReactionT {
			gas, id = w.ToGas, w.ID
		}
	}
	return gas, id
}
