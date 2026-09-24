package analysis

import (
	"fmt"

	"tga-evidence/internal/domain"
)

func buildTimeline(d preparedData, p domain.Params) []domain.TimelineEvent {
	var out []domain.TimelineEvent
	for _, f := range d.furnaces {
		sampleT := f.timeS + p.ThermalLagSeconds
		temp := interpolateTemp(d.points, sampleT)
		out = append(out, domain.TimelineEvent{
			ID: domain.JoinID("timeline", "furnace", f.recID), Kind: "furnace", Second: f.timeS,
			Celsius: &f.target, Label: fmt.Sprintf("Furnace %s target %g degC", f.segment, f.target),
			SourceIDs: []string{f.recID}, Note: fmt.Sprintf("program ramp %g degC/min; thermal lag shifts sample reaction estimate by %g s", f.ramp, p.ThermalLagSeconds),
		})
		out = append(out, domain.TimelineEvent{
			ID: domain.JoinID("timeline", "sample-effective", f.recID), Kind: "sample-effective", Second: sampleT,
			Celsius: temp, Label: fmt.Sprintf("Estimated sample response %g degC", valueOrNil(temp)),
			SourceIDs: []string{f.recID}, Note: "valve/program time is not asserted as reaction time",
		})
	}
	for _, g := range d.gases {
		sampleT := g.timeS + p.GasDelaySeconds + p.ThermalLagSeconds
		temp := interpolateTemp(d.points, sampleT)
		out = append(out, domain.TimelineEvent{
			ID: domain.JoinID("timeline", "valve", g.recID), Kind: "valve", Second: g.timeS, Gas: g.gas,
			Label: "Valve: " + g.gas, SourceIDs: []string{g.recID}, Note: g.note,
		})
		out = append(out, domain.TimelineEvent{
			ID: domain.JoinID("timeline", "gas-effective", g.recID), Kind: "gas-effective", Second: sampleT,
			Celsius: temp, Gas: g.gas, Label: "Effective sample atmosphere: " + g.gas,
			SourceIDs: []string{g.recID}, Note: fmt.Sprintf("gas delay %g s + thermal lag %g s", p.GasDelaySeconds, p.ThermalLagSeconds),
		})
	}
	for i := 1; i < len(d.points); i++ {
		if d.points[i].segment != d.points[i-1].segment {
			t0, t1 := d.points[i-1].timeS, d.points[i].timeS
			out = append(out, domain.TimelineEvent{ID: domain.JoinID("gap", d.points[i-1].recID, d.points[i].recID), Kind:"data-gap", Second:t0, Label:fmt.Sprintf("Sampling gap %g s", t1-t0), SourceIDs:[]string{d.points[i-1].recID,d.points[i].recID}, Note:"no interpolation across gap"})
		}
	}
	return out
}

func interpolateTemp(points []point, t float64) *float64 {
	if len(points)==0 || t < points[0].timeS || t > points[len(points)-1].timeS { return nil }
	for i:=1;i<len(points);i++ {
		if points[i-1].segment != points[i].segment { continue }
		if t >= points[i-1].timeS && t <= points[i].timeS {
			frac := 0.0
			if points[i].timeS != points[i-1].timeS { frac=(t-points[i-1].timeS)/(points[i].timeS-points[i-1].timeS) }
			v := points[i-1].tempC+frac*(points[i].tempC-points[i-1].tempC)
			return &v
		}
	}
	return nil
}

func valueOrNil(v *float64) float64 { if v==nil { return 0 }; return *v }
