// Package analysis is the pure, version-stamped TGA pipeline:
//
//   drift-corrected TG -> sampling-gap segmentation -> non-monotonic
//   detection -> raw DTG -> peak/stage candidates (overlaps retained) ->
//   effective atmosphere timeline (gas delay + thermal lag) -> residual-rate
//   calculation chains.
//
// It never mutates its inputs and never interpolates across a sampling gap.
// Candidate ambiguity is a first-class output: when peaks overlap or data is
// missing, every legal candidate is returned with reasons instead of one
// forced number.
package analysis

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"tga-lineage/internal/identity"
	"tga-lineage/internal/model"
)

type Input struct {
	Samples *model.LayerVersion
	Blank   *model.LayerVersion
	Drift   *model.LayerVersion
	Furnace *model.LayerVersion
	Valves  *model.LayerVersion
	Initial *model.LayerVersion
	Params  model.ParamSet
}

type Result struct {
	Artifacts []model.Artifact
	Judgments []model.Judgment
	// InputsOK is false when a required layer is entirely missing.
	InputsOK bool
	Missing  []string
}

// SourceUIDs lists the layer/param uids consumed, used for dependency edges.
func (in Input) SourceUIDs() []string {
	var out []string
	add := func(l *model.LayerVersion) {
		if l != nil {
			out = append(out, l.UID)
		}
	}
	add(in.Samples)
	add(in.Blank)
	add(in.Drift)
	add(in.Furnace)
	add(in.Valves)
	add(in.Initial)
	out = append(out, in.Params.UID)
	return out
}

type sp struct {
	t, m, temp float64
	ref        string
	flags      []string
}

type seg struct{ pts []sp }

func Build(in Input) Result {
	var res Result
	if in.Samples == nil {
		res.Missing = append(res.Missing, "samples")
	}
	if in.Initial == nil {
		res.Missing = append(res.Missing, "initial")
	}
	if len(res.Missing) > 0 {
		return res
	}
	res.InputsOK = true

	raw := usablePoints(in.Samples)
	gapLimit := in.Params.Gap.MaxStepMin
	segs := splitSegments(raw, gapLimit)
	if len(segs) > 1 {
		res.Judgments = append(res.Judgments, model.Judgment{
			Code: "gap.sampling", Severity: "warning", Subject: in.Samples.UID,
			Statement: fmt.Sprintf("samples split into %d segments at gaps exceeding %.2g min; no interpolation crosses a gap",
				len(segs), gapLimit),
			Rule: "gap.no_interpolation.v1",
		})
	}

	blankAt := blankInterpolator(in.Blank)
	driftAt := driftInterpolator(in.Drift)
	corrected := correct(segs, blankAt, driftAt, in.Params.Drift)
	flagNonMonotonic(corrected, in.Params.Noise.RelTolerance, &res)
	tg := correctedArtifact(in, corrected)
	dtg := dtgArtifact(in, tg, corrected)
	stages, sj := stageArtifacts(in, tg, dtg, corrected)
	res.Judgments = append(res.Judgments, sj...)
	valves, vj := valveArtifact(in)
	res.Judgments = append(res.Judgments, vj...)
	program := programArtifact(in, corrected)
	frac := massFractionArtifact(in, tg)
	resid, rj := residualArtifacts(in, tg, corrected)
	res.Judgments = append(res.Judgments, rj...)

	res.Artifacts = []model.Artifact{tg, dtg, stages, valves, program, frac, resid}
	return res
}

// ---- layer extraction ----------------------------------------------------

func usablePoints(lv *model.LayerVersion) []sp {
	var out []sp
	for _, r := range lv.Records {
		if !r.OK {
			continue
		}
		t, ok1 := r.Parsed["t_min"]
		m, ok2 := r.Parsed["mass_mg"]
		te, ok3 := r.Parsed["temp_c"]
		if !(ok1 && ok2 && ok3) {
			continue
		}
		out = append(out, sp{t: t, m: m, temp: te, ref: r.ID})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].t != out[j].t {
			return out[i].t < out[j].t
		}
		return out[i].ref < out[j].ref
	})
	return out
}

func splitSegments(pts []sp, maxStep float64) []seg {
	var segs []seg
	cur := seg{}
	for i, p := range pts {
		if i > 0 && p.t-pts[i-1].t > maxStep {
			if len(cur.pts) > 0 {
				cur.pts[len(cur.pts)-1].flags = append(cur.pts[len(cur.pts)-1].flags, "gap_edge")
			}
			segs = append(segs, cur)
			p.flags = append(p.flags, "gap_edge")
			cur = seg{pts: []sp{p}}
			continue
		}
		cur.pts = append(cur.pts, p)
	}
	if len(cur.pts) > 0 {
		segs = append(segs, cur)
	}
	if len(segs) == 0 && len(pts) == 0 {
		return nil
	}
	return segs
}

func seriesInterpolator(lv *model.LayerVersion, yKey string) func(t float64) (float64, bool) {
	if lv == nil {
		return func(float64) (float64, bool) { return 0, false }
	}
	type tp struct{ t, y float64 }
	var data []tp
	for _, r := range lv.Records {
		if !r.OK {
			continue
		}
		t, ok1 := r.Parsed["t_min"]
		y, ok2 := r.Parsed[yKey]
		if ok1 && ok2 {
			data = append(data, tp{t, y})
		}
	}
	sort.Slice(data, func(i, j int) bool { return data[i].t < data[j].t })
	return func(t float64) (float64, bool) {
		// Strict containment: never extrapolate outside the measured blank.
		if len(data) == 0 || t < data[0].t || t > data[len(data)-1].t {
			return 0, false
		}
		i := sort.Search(len(data)-1, func(i int) bool { return data[i+1].t >= t })
		a, b := data[i], data[i+1]
		if b.t == a.t {
			return a.y, true
		}
		return a.y + (b.y-a.y)*(t-a.t)/(b.t-a.t), true
	}
}

func blankInterpolator(lv *model.LayerVersion) func(float64) (float64, bool) {
	return seriesInterpolator(lv, "blank_mg")
}
func driftInterpolator(lv *model.LayerVersion) func(float64) (float64, bool) {
	return seriesInterpolator(lv, "drift_mg")
}

func correct(segs []seg, blank, drift func(float64) (float64, bool), dp model.DriftParams) []seg {
	out := make([]seg, len(segs))
	for si, s := range segs {
		out[si] = seg{}
		for _, p := range s.pts {
			c := p
			if dp.Enabled {
				if b, ok := blank(p.t); ok {
					c.m -= dp.BlankGain * b
				}
				if d, ok := drift(p.t); ok {
					c.m -= d
				}
				c.m -= dp.Offset
			}
			out[si].pts = append(out[si].pts, c)
		}
	}
	return out
}

// flagNonMonotonic marks relative mass increases beyond tolerance. The raw
// points are kept; flags drive candidate certainty rather than smoothing.
func flagNonMonotonic(segs []seg, tol float64, res *Result) {
	noisy := 0
	for si := range segs {
		base := 0.0
		for i := range segs[si].pts {
			p := &segs[si].pts[i]
			if i == 0 {
				base = math.Max(math.Abs(p.m), 1e-9)
				continue
			}
			prev := segs[si].pts[i-1].m
			if p.m > prev {
				if math.Abs(p.m-prev)/base > tol {
					p.flags = append(p.flags, "nonmonotonic", "noisy")
					noisy++
				}
			}
			if base == 0 {
				base = math.Max(math.Abs(p.m), 1e-9)
			}
		}
	}
	if noisy > 0 {
		res.Judgments = append(res.Judgments, model.Judgment{
			Code: "noise.nonmonotonic", Severity: "warning", Subject: "",
			Statement: fmt.Sprintf("%d samples rise against the mass-loss direction beyond tolerance %.3g; retained and flagged, not smoothed", noisy, tol),
			Rule: "noise.flag_dont_smooth.v1",
		})
	}
}

// ---- artifact constructors -----------------------------------------------

func fp(in Input) string {
	return identity.Hash("fp_", strings.Join(in.SourceUIDs(), "|"))[3:]
}

func artUID(kind string, in Input, extra ...string) string {
	parts := append([]string{string(rune(0)), kind, model.AnalysisAlgVersion,
		paramsFingerprint(in.Params), fp(in)}, extra...)
	return identity.Hash("art_", parts...)
}

func paramsFingerprint(p model.ParamSet) string {
	if p.Drift.Fingerprint != "" {
		// the set-level fingerprint dominates
	}
	return identity.Hash("pfp_",
		strconv.FormatBool(p.Drift.Enabled), f64(p.Drift.BlankGain), f64(p.Drift.Offset),
		f64(p.Gap.MaxStepMin), f64(p.Noise.RelTolerance),
		f64(p.Peak.Prominence), f64(p.Peak.WindowMin),
		f64(p.Atmosphere.GasDelayMin), f64(p.Atmosphere.ThermalLagMin),
	)[4:]
}

func f64(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func correctedArtifact(in Input, segs []seg) model.Artifact {
	a := model.Artifact{
		UID: artUID(model.ArtCorrectedTG, in), Project: in.Samples.Project,
		Kind: model.ArtCorrectedTG, Alg: model.AnalysisAlgVersion,
		ParamUID: in.Params.UID, ParamsFP: paramsFingerprint(in.Params),
		Direct: directLayers(in, model.LayerSamples, model.LayerBlank, model.LayerDrift, model.LayerInitial),
		Status: model.ImportOK,
	}
	for si, s := range segs {
		for _, p := range s.pts {
			a.Series = append(a.Series, model.Point{
				UID:    identity.Hash("pt_", a.UID, strconv.Itoa(si), f64(p.t)),
				TMin:   p.t, TUnit: "min", XLabel: "furnace_elapsed_min",
				Y: p.m, YUnit: "mg", Seg: si, Flags: p.flags, RawRef: p.ref,
			})
		}
	}
	if in.Samples.Status == model.ImportPartial || (in.Blank != nil && in.Blank.Status == model.ImportPartial) {
		a.Status = "uncertain"
		a.Notes = append(a.Notes, "computed from a partially imported layer")
	}
	return a
}

func directLayers(in Input, kinds ...string) []string {
	have := map[string]*model.LayerVersion{
		model.LayerSamples: in.Samples, model.LayerBlank: in.Blank, model.LayerDrift: in.Drift,
		model.LayerFurnace: in.Furnace, model.LayerValves: in.Valves, model.LayerInitial: in.Initial,
	}
	var out []string
	for _, k := range kinds {
		if l := have[k]; l != nil {
			out = append(out, l.UID)
		}
	}
	out = append(out, in.Params.UID)
	return out
}

func dtgArtifact(in Input, tg model.Artifact, segs []seg) model.Artifact {
	a := model.Artifact{
		UID: artUID(model.ArtDTG, in, tg.UID), Project: in.Samples.Project,
		Kind: model.ArtDTG, Alg: model.AnalysisAlgVersion,
		ParamUID: in.Params.UID, ParamsFP: paramsFingerprint(in.Params),
		Direct:     []string{tg.UID, in.Params.UID},
		DependsOn:  []string{tg.UID},
		Status:     "current",
	}
	for si, s := range segs {
		for i := 1; i < len(s.pts); i++ {
			p0, p1 := s.pts[i-1], s.pts[i]
			dt := p1.t - p0.t
			if dt <= 0 {
				continue
			}
			dm := (p1.m - p0.m) / dt
			mid := (p0.t + p1.t) / 2
			flags := append([]string{}, p1.flags...)
			a.Series = append(a.Series, model.Point{
				UID:   identity.Hash("pt_", a.UID, strconv.Itoa(si), f64(mid)),
				TMin:  mid, TUnit: "min", XLabel: "furnace_elapsed_min",
				Y: dm, YUnit: "mg/min", Seg: si, Flags: flags,
				RawRef: p1.ref,
			})
		}
	}
	if tg.Status == "uncertain" {
		a.Status = "uncertain"
	}
	return a
}
