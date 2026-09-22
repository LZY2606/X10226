package analysis

import (
	"fmt"

	"tga-archive/internal/model"
)

// attachGasHint links a candidate to a gas switch. The reaction window is the
// valve switch time shifted by the transport delay (and interpreted with
// temperature lag); it is explicitly not equal to the raw switch time.
func attachGasHint(c *model.Candidate, src Sources, p model.Params) {
	if src.Gases == nil || src.Gases.Gases == nil || c.OnsetS == nil {
		return
	}
	best := -1
	bestd := 1e18
	for i, e := range src.Gases.Gases.Events {
		eff := e.TSec + p.GasDelayS
		d := abs(eff - *c.OnsetS)
		if d < bestd {
			bestd, best = d, i
		}
	}
	if best < 0 {
		return
	}
	e := src.Gases.Gases.Events[best]
	eff := e.TSec + p.GasDelayS
	c.GasHint = fmt.Sprintf("%s %s at valve t=%.1fs -> sample ~%.1fs (delay %.1fs, temp lag %.1fK)", e.Gas, e.Action, e.TSec, eff, p.GasDelayS, p.TempLagK)
	c.Node.Meta["valveSwitchS"] = qround(e.TSec)
	c.Node.Meta["reactionS"] = qround(eff)
	c.Node.Direct = append(c.Node.Direct, src.Gases.ID)
	if bestd > 30 {
		c.Node.Reason = "no gas switch within 30s of candidate onset even after delay model"
	}
}

// recordGasDelayFindings emits machine-readable findings that (a) explain
// valve-time != reaction-time, and (b) flag a switch whose delayed reaction
// window contains no DTG peak.
func recordGasDelayFindings(peaks []peak, src Sources, p model.Params, d *model.Derived, cands []model.Candidate) {
	if src.Gases == nil || src.Gases.Gases == nil {
		return
	}
	for _, e := range src.Gases.Gases.Events {
		if e.Action != model.GasOn {
			continue
		}
		eff := e.TSec + p.GasDelayS
		matched := false
		for _, pk := range peaks {
			if abs(pk.tS-eff) <= 45 {
				matched = true
				break
			}
		}
		f := model.Finding{
			Code:     "gas_delay_lag",
			Severity: model.SevWarning,
			Message:  fmt.Sprintf("valve %s ON at %.1fs; reaction window ~%.1fs after %.1fs delay (temp lag %.1fK)", e.Gas, e.TSec, eff, p.GasDelayS, p.TempLagK),
			Refs:     []string{src.Gases.ID},
		}
		start, end := eff-20.0, eff+45.0
		f.StartS, f.EndS = &start, &end
		if !matched {
			f.Severity = model.SevError
			f.Code = "gas_event_without_reaction"
			f.Message = fmt.Sprintf("gas %s ON at %.1fs has no DTG peak within its delayed window [%.1f,%.1f]s; candidate cannot be uniquely assigned", e.Gas, e.TSec, start, end)
		}
		d.Findings = append(d.Findings, f)
	}
}
