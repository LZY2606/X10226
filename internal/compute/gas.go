package compute

import (
	"sort"

)

func (e *engine) buildGas() {
	type ev struct {
		t   float64
		gas string
		id  string
	}
	var evs []ev
	for _, r := range e.gas {
		tv, g := r.Fields["t"], r.Fields["gas"]
		if tv.Bad != "" {
			continue
		}
		evs = append(evs, ev{t: tv.Num, gas: g.Raw, id: r.ID})
	}
	sort.Slice(evs, func(i, j int) bool { return evs[i].t < evs[j].t })
	for i, x := range evs {
		end := 1e30
		if i+1 < len(evs) {
			end = evs[i+1].t + e.p.GasDelayS
		}
		eff := x.t + e.p.GasDelayS
		s := GasSpan{
			ValveT: x.t, EffectiveT: eff, EndT: end, Gas: x.gas, RawID: x.id,
		}
		s.Node = e.node("gas", "valve "+x.gas+" @"+strconvF(x.t)+"s",
			"effective_t = valve_t + gas_delay", eff, UnitTime, "ok",
			[]Ref{{Kind: "raw", ID: x.id, Role: "valve"},
				{Kind: "param", ID: "param:gas_delay_s", Role: "delay"}})
		e.spans = append(e.spans, s)
	}
	if len(evs) > 0 {
		e.judge = append(e.judge, Judgment{
			Code: "atmosphere_delay", Rule: "atmosphere:valve-time!=reaction-time",
			Message: "valve switch time is not the sample reaction time; effective atmosphere is shifted by the gas delay and programmed temperature by the thermal lag",
			Refs:    []Ref{{Kind: "param", ID: "param:gas_delay_s"}, {Kind: "param", ID: "param:temp_lag_s"}},
			Detail:  map[string]float64{"gas_delay_s": e.p.GasDelayS, "temp_lag_s": e.p.TempLagS},
		})
	}
}

// gasAt returns the atmosphere effective at sample time t and the raw ref.
func (e *engine) gasAt(t float64) (string, string) {
	gas, id := "", ""
	for _, s := range e.spans {
		if t >= s.EffectiveT && t < s.EndT {
			gas, id = s.Gas, s.RawID
		}
	}
	return gas, id
}
