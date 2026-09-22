package analysis

import (
	"fmt"

	"tga-archive/internal/model"
)

// buildResidues computes the final residue rate as the mean mass % over the
// last plateau window of the final segment. If the tail is a gap or has too
// few valid points, the residue is UNKNOWN rather than guessed.
func buildResidues(pts []workPoint, src Sources, p model.Params, stateHash string, d *model.Derived) []model.Residue {
	window := 60.0
	res := model.Residue{}
	res.Node = model.Node{
		ID:      nid(stateHash, "residue", 0),
		Label:   "final residue",
		Kind:    "residue",
		Algo:    p.AlgoVersion,
		ParamFP: d.ParamFP,
		Unit:    model.MassPctUnit,
		Direct:  []string{nid(stateHash, "series")},
	}
	lastSeg := 0
	var tmax float64
	for i := range pts {
		if pts[i].massOk && pts[i].seg > lastSeg {
			lastSeg = pts[i].seg
		}
		if pts[i].massOk && pts[i].t > tmax {
			tmax = pts[i].t
		}
	}
	res.WindowEnd = qround(tmax)
	var tail []float64
	for i := range pts {
		if pts[i].massOk && pts[i].seg == lastSeg && tmax-pts[i].t <= window {
			tail = append(tail, pts[i].pct)
		}
	}
	if lastSeg == 0 || len(tail) < 2 {
		res.Status = model.StatusUnknown
		res.Reason = "final plateau window overlaps a data gap / too few valid points; residue not interpolated"
		res.Value = "unknown"
		d.Findings = append(d.Findings, model.Finding{Code: "residue_unknown", Severity: model.SevError, Message: res.Reason, Refs: []string{res.Node.ID}})
		return []model.Residue{res}
	}
	sum := 0.0
	for _, v := range tail {
		sum += v
	}
	mean := sum / float64(len(tail))
	res.Pct = fp(mean)
	res.Value = fmt.Sprintf("%.2f%%", mean)
	res.Status = model.StatusOK
	res.Meta = map[string]any{"windowS": window, "points": len(tail)}
	return []model.Residue{res}
}
