package tga

import "math"

// ValveEvent is a raw gas valve switch as recorded by the instrument.
type ValveEvent struct {
	TimeMin   float64 `json:"time_min"`
	Valve     string  `json:"valve"`
	Gas       string  `json:"gas"`
	FlowMlMin float64 `json:"flow_ml_min"`
}

// AtmosParams models the furnace gas delivery: a transport delay and a
// first-order thermal/mixing lag.
type AtmosParams struct {
	DelayMin float64 `json:"delay_min"` // gas transport delay
	TauMin   float64 `json:"tau_min"`   // exchange time constant (thermal lag)
}

// EffectiveSwitch is a valve event mapped into sample-reaction time.
type EffectiveSwitch struct {
	ValveTimeMin     float64 `json:"valve_time_min"`
	EffectiveTimeMin float64 `json:"effective_time_min"` // valve + delay
	Gas              string  `json:"gas"`
}

// AtmosJudgment is the machine-readable record that valve switch time is
// NOT the sample reaction time.
type AtmosJudgment struct {
	Rule             string  `json:"rule"` // "atmosphere.valve_time_not_reaction_time"
	ValveTimeMin     float64 `json:"valve_time_min"`
	EffectiveTimeMin float64 `json:"effective_time_min"`
	DelayMin         float64 `json:"delay_min"`
	TauMin           float64 `json:"tau_min"`
	Gas              string  `json:"gas"`
}

// Atmosphere is the derived effective-atmosphere timeline.
type Atmosphere struct {
	Params     AtmosParams       `json:"params"`
	Switches   []EffectiveSwitch `json:"switches"`
	InitialGas string            `json:"initial_gas"`
}

// BuildAtmosphere converts raw valve events into the effective timeline and
// returns one judgment per switch.
func BuildAtmosphere(events []ValveEvent, initialGas string, p AtmosParams) (*Atmosphere, []AtmosJudgment) {
	a := &Atmosphere{Params: p, InitialGas: initialGas}
	var judgments []AtmosJudgment
	for _, e := range events {
		eff := e.TimeMin + p.DelayMin
		a.Switches = append(a.Switches, EffectiveSwitch{
			ValveTimeMin:     e.TimeMin,
			EffectiveTimeMin: eff,
			Gas:              e.Gas,
		})
		judgments = append(judgments, AtmosJudgment{
			Rule:             "atmosphere.valve_time_not_reaction_time",
			ValveTimeMin:     e.TimeMin,
			EffectiveTimeMin: eff,
			DelayMin:         p.DelayMin,
			TauMin:           p.TauMin,
			Gas:              e.Gas,
		})
	}
	return a, judgments
}

// GasAt returns the dominant gas at sample time t and the fraction to which
// the most recent switch has equilibrated (first-order lag).
func (a *Atmosphere) GasAt(t float64) (string, float64) {
	gas := a.InitialGas
	frac := 1.0
	for _, sw := range a.Switches {
		if sw.EffectiveTimeMin <= t {
			gas = sw.Gas
			if a.Params.TauMin > 0 {
				frac = 1 - math.Exp(-(t-sw.EffectiveTimeMin)/a.Params.TauMin)
			} else {
				frac = 1
			}
		}
	}
	return gas, frac
}
