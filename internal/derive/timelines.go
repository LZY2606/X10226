package derive

import (
	"sort"

	"tgaline/internal/domain"
)

func buildGasTimeline(projectID string, in Inputs, g *Graph) *domain.Artifact {
	status := "ok"
	var diags []domain.Diagnostic
	var bands []domain.GasBand
	if in.Gas == nil {
		status = "unknown"
		diags = append(diags, domain.Diagnostic{Scope: "artifact", Code: "no_gas_events",
			Message: "no gas valve events submitted", Severity: domain.SeverityError})
	} else {
		type ev struct{ t float64; gas, action string }
		var evs []ev
		for _, ge := range in.Gas.Gas {
			if len(ge.Invalid) > 0 || ge.Atmosphere == "" {
				continue
			}
			evs = append(evs, ev{ge.TimeS, ge.Atmosphere, ge.Action})
		}
		sort.Slice(evs, func(i, j int) bool { return evs[i].t < evs[j].t })
		// Current atmosphere becomes active at sample time = valve + delay.
		delay := in.Params.AtmosphereDelayS
		cur := ""
		var start float64
		for i, e := range evs {
			reactionAt := e.t + delay
			if cur != "" && reactionAt > start {
				to := reactionAt
				if i+1 < len(evs) {
					to = evs[i+1].t + delay
				}
				bands = append(bands, domain.GasBand{
					FromS: start, ToS: to, ValveAtS: evs[i-1].t,
					Atmosphere: cur, DelayS: delay,
				})
			}
			cur = e.gas
			start = reactionAt
		}
		if cur != "" {
			bands = append(bands, domain.GasBand{
				FromS: start, ToS: 1e12, ValveAtS: evs[len(evs)-1].t,
				Atmosphere: cur, DelayS: delay,
			})
		}
		if delay != 0 {
			diags = append(diags, domain.Diagnostic{Scope: "artifact", Code: "atmosphere_delay",
				Message: "valve switch time differs from sample reaction time by transport delay",
				Severity: domain.SeverityInfo, Raw: fkv(delay) + " s"})
		}
	}
	docs := []*domain.Document{in.Gas}
	ih := inputsHash(docs, in.Params, "gas_timeline")
	id := ArtifactID("gas_timeline", ih)
	data := map[string]any{"bands": bands, "delay_s": in.Params.AtmosphereDelayS}
	return newArtifact(id, projectID, domain.ArtGasTimeline, status, data,
		prov(docs, in.Params, g.ParamsFP, ih), diags)
}

func gasAt(bands []domain.GasBand, t float64) string {
	for _, b := range bands {
		if t >= b.FromS && t < b.ToS {
			return b.Atmosphere
		}
	}
	return ""
}

func buildFurnaceTimeline(projectID string, in Inputs, g *Graph) *domain.Artifact {
	status := "ok"
	var diags []domain.Diagnostic
	var segs []domain.FurnaceSegment
	if in.Furnace == nil {
		status = "unknown"
		diags = append(diags, domain.Diagnostic{Scope: "artifact", Code: "no_furnace_program",
			Message: "no furnace program submitted", Severity: domain.SeverityError})
	} else {
		var tab []tableEntry
		for _, cp := range in.Furnace.Curve {
			if len(cp.Invalid) > 0 {
				continue
			}
			tab = append(tab, tableEntry{cp.TimeS + in.Params.TempLagS, cp.Value})
		}
		sort.Slice(tab, func(i, j int) bool { return tab[i].t < tab[j].t })
		for i := 1; i < len(tab); i++ {
			a, b := tab[i-1], tab[i]
			dur := b.t - a.t
			kind := "ramp"
			rate := 0.0
			if dur > 0 {
				rate = (b.v - a.v) / (dur / 60.0)
				if absF(b.v-a.v) < 1e-9 {
					kind = "hold"
				}
			}
			segs = append(segs, domain.FurnaceSegment{
				FromS: a.t, ToS: b.t, FromC: a.v, ToC: b.v,
				RateCPerMin: rate, Kind: kind,
			})
		}
		if in.Params.TempLagS != 0 {
			diags = append(diags, domain.Diagnostic{Scope: "artifact", Code: "temp_lag",
				Message: "furnace time shifted to sample coordinates by thermal lag",
				Severity: domain.SeverityInfo, Raw: fkv(in.Params.TempLagS) + " s"})
		}
	}
	docs := []*domain.Document{in.Furnace}
	ih := inputsHash(docs, in.Params, "furnace_timeline")
	id := ArtifactID("furnace_timeline", ih)
	data := map[string]any{"segments": segs, "lag_s": in.Params.TempLagS}
	return newArtifact(id, projectID, domain.ArtFurnaceTimeline, status, data,
		prov(docs, in.Params, g.ParamsFP, ih), diags)
}

func buildBlank(projectID string, in Inputs, g *Graph) *domain.Artifact {
	tab := tableFromCurve(in.Blank)
	docs := []*domain.Document{in.Blank}
	ih := inputsHash(docs, in.Params, "blank_profile")
	id := ArtifactID("blank_profile", ih)
	data := map[string]any{"points": tab}
	return newArtifact(id, projectID, domain.ArtBlankProfile, "ok", data,
		prov(docs, in.Params, g.ParamsFP, ih), nil)
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
