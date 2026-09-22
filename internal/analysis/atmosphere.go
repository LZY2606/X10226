package analysis

import (
	"fmt"
	"strconv"

	"tga-lineage/internal/identity"
	"tga-lineage/internal/model"
)

// gasTimeline returns valve windows ordered by valve time (the raw operator
// time). Reaction-zone time is derived separately through the gas delay.
func gasTimeline(in Input) []model.ValveWindow {
	var out []model.ValveWindow
	if in.Valves == nil {
		return out
	}
	type ev struct{ t float64; gas string }
	var evs []ev
	for _, r := range in.Valves.Records {
		if !r.OK {
			continue
		}
		t, ok := r.Parsed["t_min"]
		if !ok {
			continue
		}
		gas := r.Raw["gas"]
		if g, ok := r.Parsed["gas"]; ok {
			_ = g
		}
		if gas == "" {
			continue
		}
		evs = append(evs, ev{t, gas})
	}
	// stable order by t, preserving source row
	for i := 1; i < len(evs); i++ {
		for j := i; j > 0 && evs[j-1].t > evs[j].t; j-- {
			evs[j-1], evs[j] = evs[j], evs[j-1]
		}
	}
	uidBase := artUID(model.ArtValveEff, in)
	for i, e := range evs {
		out = append(out, model.ValveWindow{
			UID:        identity.Hash("vw_", uidBase, strconv.Itoa(i)),
			ValveAtMin: e.t,
			EffAtMin:   e.t + in.Params.Atmosphere.GasDelayMin,
			Gas:        e.gas,
			DelayMin:   in.Params.Atmosphere.GasDelayMin,
		})
	}
	return out
}

func valveArtifact(in Input) (model.Artifact, []model.Judgment) {
	ws := gasTimeline(in)
	a := model.Artifact{
		UID: artUID(model.ArtValveEff, in), Project: in.Samples.Project,
		Kind: model.ArtValveEff, Alg: model.AnalysisAlgVersion,
		ParamUID: in.Params.UID, ParamsFP: paramsFingerprint(in.Params),
		Direct: directLayers(in, model.LayerValves),
		Valves: ws, Status: "current",
	}
	var js []model.Judgment
	if in.Valves != nil {
		js = append(js, model.Judgment{
			Code: "atmosphere.valve_effective", Severity: "info", Subject: a.UID,
			Statement: fmt.Sprintf("%d valve switch(es) projected to reaction time with gas delay %.2g min",
				len(ws), in.Params.Atmosphere.GasDelayMin),
			Rule: "atmosphere.delay.v1",
		})
	}
	return a, js
}

func programArtifact(in Input, segs []seg) model.Artifact {
	a := model.Artifact{
		UID: artUID(model.ArtProgramT, in), Project: in.Samples.Project,
		Kind: model.ArtProgramT, Alg: model.AnalysisAlgVersion,
		ParamUID: in.Params.UID, ParamsFP: paramsFingerprint(in.Params),
		Direct: directLayers(in, model.LayerFurnace),
		Status: "current",
	}
	if in.Furnace != nil {
		for _, r := range in.Furnace.Records {
			if !r.OK {
				continue
			}
			t, ok1 := r.Parsed["t_min"]
			sp, ok2 := r.Parsed["setpoint_c"]
			if !(ok1 && ok2) {
				continue
			}
			a.Series = append(a.Series, model.Point{
				UID:   identity.Hash("pt_", a.UID, f64(t)),
				TMin:  t, TUnit: "min", XLabel: "furnace_elapsed_min",
				Y: sp, YUnit: "degC", RawRef: r.ID,
			})
		}
	}
	// sample-temperature series with thermal-lag shift, drawn from measured
	// sample sensor points (shifted by lag), not from the program setpoint.
	var sample []model.Point
	uidS := identity.Hash("arts_", a.UID, "sample")
	for si, s := range segs {
		for _, p := range s.pts {
			sample = append(sample, model.Point{
				UID:   identity.Hash("pt_", uidS, strconv.Itoa(si), f64(p.t)),
				TMin:  p.t + in.Params.Atmosphere.ThermalLagMin, TUnit: "min",
				XLabel: "reaction_zone_min", Y: p.temp, YUnit: "degC", Seg: si,
				Flags:  p.flags, RawRef: p.ref,
			})
		}
	}
	a.Series = append(a.Series, sample...)
	return a
}

func massFractionArtifact(in Input, tg model.Artifact) model.Artifact {
	a := model.Artifact{
		UID: artUID(model.ArtMassFrac, in, tg.UID), Project: in.Samples.Project,
		Kind: model.ArtMassFrac, Alg: model.AnalysisAlgVersion,
		ParamUID: in.Params.UID, ParamsFP: paramsFingerprint(in.Params),
		Direct:    append([]string{tg.UID}, directLayers(in, model.LayerInitial)...),
		DependsOn: []string{tg.UID},
		Status:    "current",
	}
	m0 := initialMass(in.Initial)
	if m0 == nil || *m0 == 0 {
		a.Status = "uncertain"
		a.Notes = append(a.Notes, "initial mass missing or zero; fractions unavailable")
		return a
	}
	for _, p := range tg.Series {
		y := p.Y / *m0 * 100
		q := model.Point{
			UID: identity.Hash("pt_", a.UID, strconv.Itoa(p.Seg), f64(p.TMin)),
			TMin: p.TMin, TUnit: "min", XLabel: "furnace_elapsed_min",
			Y: y, YUnit: "%", Seg: p.Seg, Flags: p.Flags, RawRef: p.RawRef,
		}
		a.Series = append(a.Series, q)
	}
	return a
}
