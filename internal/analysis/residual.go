package analysis

import (
	"fmt"
	"math"
	"sort"

	"tga-lineage/internal/identity"
	"tga-lineage/internal/model"
)

// residualArtifacts builds one expandable residual-rate chain per evaluated
// endpoint window (default: the final 10% of measured time in the final
// segment, plus an end-of-data node). Every node cites a direct source uid so
// the UI can expand a percentage into its full computation chain.
func residualArtifacts(in Input, tg model.Artifact, segs []seg) (model.Artifact, []model.Judgment) {
	a := model.Artifact{
		UID: artUID(model.ArtResiduals, in, tg.UID), Project: in.Samples.Project,
		Kind: model.ArtResiduals, Alg: model.AnalysisAlgVersion,
		ParamUID: in.Params.UID, ParamsFP: paramsFingerprint(in.Params),
		Direct:    append([]string{tg.UID}, directLayers(in, model.LayerInitial)...),
		DependsOn: []string{tg.UID},
		Status:    "current",
	}
	m0 := initialMass(in.Initial)
	if len(tg.Series) == 0 {
		a.Status = "uncertain"
		a.Notes = append(a.Notes, "no corrected samples available")
		return a, nil
	}
	finalSeg := 0
	tMax := 0.0
	for _, p := range tg.Series {
		if p.TMin >= tMax {
			tMax, finalSeg = p.TMin, p.Seg
		}
	}
	window := 3.0
	if tMax > 0 {
		window = math.Max(1.0, 0.1*tMax)
	}
	var tail []model.Point
	for _, p := range tg.Series {
		if p.Seg == finalSeg && p.TMin >= tMax-window {
			tail = append(tail, p)
		}
	}
	if len(tail) < 2 {
		for _, p := range tg.Series {
			if p.Seg == finalSeg {
				tail = append(tail, p)
			}
		}
	}
	med := medianPoint(tail)
	end := tail[len(tail)-1]

	certain := "certain"
	var reasons []string
	if touchesGap(tg.Series, tMax-window, tMax) {
		certain = "uncertain"
		reasons = append(reasons, "residual window borders a sampling gap")
	}
	noisy := 0
	for _, p := range tail {
		for _, f := range p.Flags {
			if f == "noisy" {
				noisy++
			}
		}
	}
	if noisy > 0 {
		certain = "uncertain"
		reasons = append(reasons, fmt.Sprintf("%d non-monotonic samples inside the residual window", noisy))
	}

	chain := func(label string, node model.Point) model.ResidualChain {
		rc := model.ResidualChain{
			UID: identity.Hash("res_", a.UID, label), Label: label,
			AtMin: node.TMin, WindowMin: window, Unit: "%", Certain: certain,
			Reasons: reasons,
		}
		rc.Chain = append(rc.Chain, model.ChainNode{
			UID: identity.Hash("cn_", rc.UID, "tail_mass"), Label: "corrected final mass (median of tail window)",
			Value: fmt.Sprintf("%.4f mg", med), Unit: "mg", Number: &med,
			Source: node.UID, Note: fmt.Sprintf("median over %d samples, window %.2g min, segment %d", len(tail), window, finalSeg),
		})
		if m0 != nil {
			pct := med / *m0 * 100
			rc.ValuePct = &pct
			rc.Chain = append(rc.Chain,
				model.ChainNode{
					UID: identity.Hash("cn_", rc.UID, "m0"), Label: "sample initial mass (current version)",
					Value: fmt.Sprintf("%.4f mg", *m0), Unit: "mg", Number: m0,
					Source: in.Initial.UID, Note: "residual = final mass / initial mass; revising m0 invalidates this node",
				},
				model.ChainNode{
					UID: identity.Hash("cn_", rc.UID, "ratio"), Label: "residual fraction",
					Value: fmt.Sprintf("%.4f mg / %.4f mg x 100", med, *m0),
					Number: &pct, Unit: "%", Source: a.UID,
					Note: "no renormalization against stage losses",
				})
		} else {
			rc.Certain = "unknown"
			rc.Chain = append(rc.Chain, model.ChainNode{
				UID: identity.Hash("cn_", rc.UID, "m0"), Label: "sample initial mass",
				Value: "unknown", Source: in.Initial.UID,
				Note:  "initial mass missing; percent residual cannot be produced",
			})
		}
		return rc
	}

	a.Residuals = append(a.Residuals, chain("final_residual_median", end))
	if len(tail) > 0 {
		last := tg.Series[len(tg.Series)-1]
		a.Residuals = append(a.Residuals, chain("end_of_data", last))
	}
	var js []model.Judgment
	if m0 != nil {
		js = append(js, model.Judgment{
			Code: "residual.definition", Severity: "info", Subject: a.UID,
			Statement: fmt.Sprintf("residual %.2f%% is median corrected mass over the final %.2g-min window divided by current initial mass; independent of stage loss sums",
				*a.Residuals[0].ValuePct, window),
			Rule: "residual.independent_chain.v1",
		})
	}
	return a, js
}

func medianPoint(ps []model.Point) float64 {
	ys := make([]float64, len(ps))
	for i, p := range ps {
		ys[i] = p.Y
	}
	sort.Float64s(ys)
	n := len(ys)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return ys[n/2]
	}
	return (ys[n/2-1] + ys[n/2]) / 2
}
