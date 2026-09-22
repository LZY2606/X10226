package analysis

import (
	"tga-review/internal/domain"
)

// gasAt returns the gas composition active at furnace time t, based solely on
// the raw valve events (no delay). Unknown before the first event.
func gasAt(events []domain.GasEvent, t float64) string {
	gas := "unknown"
	for _, e := range events {
		if e.Time.Value <= t {
			switch e.Action {
			case "open", "switch":
				gas = e.Gas
			case "close":
				gas = "none"
			}
		}
	}
	return gas
}

// sampleTemp estimates the temperature actually experienced by the sample at
// time t. Measured trace temperature is preferred; thermal lag is reported
// separately. Falls back to the furnace program minus lag.
func sampleTemp(tr domain.CorrectedTrace, furnace *domain.FurnaceProgramLayer, t, lag float64) float64 {
	// nearest present trace temperature at-or-before t
	idx := -1
	for i := range tr.Times {
		if tr.Times[i] <= t && tr.Present[i] {
			idx = i
		}
	}
	if idx >= 0 {
		return tr.TempC[idx] - lag
	}
	if furnace != nil {
		for _, s := range furnace.Segments {
			if t >= s.StartT.Value && t <= s.EndT.Value {
				dur := s.EndT.Value - s.StartT.Value
				if dur <= 0 {
					return s.StartTemp.Value - lag
				}
				frac := (t - s.StartT.Value) / dur
				return (s.StartTemp.Value + (s.EndTemp.Value-s.StartTemp.Value)*frac) - lag
			}
		}
	}
	return 0
}

// mapAtmosphere applies the atmosphere delay to valve events: the reaction
// time is t_valve + delay, at which the sample temperature is T - thermal lag.
// This explicitly encodes "valve switch time != reaction time".
func mapAtmosphere(in Inputs, tr domain.CorrectedTrace) []domain.AtmosphereReach {
	p := in.Params
	out := make([]domain.AtmosphereReach, 0, len(in.GasValves.Events))
	for _, e := range in.GasValves.Events {
		reach := e.Time.Value + p.AtmosphereDelayS
		out = append(out, domain.AtmosphereReach{
			ValveIndex: e.Index,
			ValveTime:  e.Time.Value,
			ReachTime:  reach,
			ReachTempC: sampleTemp(tr, in.Furnace, reach, p.ThermalLagDegC),
			Action:     e.Action,
			Gas:        e.Gas,
		})
	}
	return out
}

func atmosphereJudgements(reach []domain.AtmosphereReach, p domain.Params) []domain.Judgement {
	var js []domain.Judgement
	for _, r := range reach {
		js = append(js, j(domain.SevInfo, "atmosphere_delay",
			"valve switch does not equal reaction time; gas arrival shifted at the sample",
			"gas-"+itoa(r.ValveIndex),
			map[string]any{
				"valve_time_s":      r.ValveTime,
				"reach_time_s":      r.ReachTime,
				"delay_s":           p.AtmosphereDelayS,
				"reach_temp_c":      r.ReachTempC,
				"thermal_lag_deg_c": p.ThermalLagDegC,
				"gas":               r.Gas,
				"action":            r.Action,
			}))
	}
	return js
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
