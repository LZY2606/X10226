package analysis

import (
	"fmt"

	"tga-archive/internal/model"
)

func buildChain(pts []workPoint, src Sources, p model.Params, stateHash string, d *model.Derived, m0 float64, m0Status model.Status) model.Node {
	pf := d.ParamFP
	root := model.Node{
		ID:      nid(stateHash, "root"),
		Label:   "TGA calculation chain",
		Kind:    "root",
		Status:  model.StatusOK,
		Algo:    p.AlgoVersion,
		ParamFP: pf,
	}
	add := func(n model.Node) { root.Children = append(root.Children, n) }

	// 1. raw sources
	raw := model.Node{ID: nid(stateHash, "src", "raw"), Label: "raw samples (original representation retained)", Kind: "group", Status: model.StatusOK, Algo: p.AlgoVersion, ParamFP: pf}
	if src.Samples != nil {
		nValid, nBad := 0, 0
		for _, q := range pts {
			if q.massOk {
				nValid++
			} else {
				nBad++
			}
		}
		raw.Direct = []string{src.Samples.ID}
		raw.Children = append(raw.Children, model.Node{
			ID: nid(stateHash, "src", "samples"), Label: "raw sample points", Kind: "scalar",
			Value: fmt.Sprintf("%d valid / %d unconvertible", nValid, nBad), Status: ternary(nBad > 0, model.StatusUnknown, model.StatusOK),
			Algo: p.AlgoVersion, ParamFP: pf, Direct: []string{src.Samples.ID},
			Reason: ternaryS(nBad > 0, "unconvertible rows stay in place with diagnostics; never dropped for plotting", ""),
		})
	}
	for _, l := range []*model.Layer{src.Blank, src.Furnace, src.Gases, src.Drift, src.Initial} {
		if l == nil {
			continue
		}
		raw.Children = append(raw.Children, model.Node{
			ID: nid(stateHash, "src", string(l.Kind)), Label: string(l.Kind) + " v" + fmt.Sprint(l.Version),
			Kind: "source", Status: model.StatusOK, Algo: p.AlgoVersion, ParamFP: pf, Direct: []string{l.ID},
		})
	}
	add(raw)

	// 2. correction steps
	corr := model.Node{ID: nid(stateHash, "corr"), Label: "baseline corrections", Kind: "group", Status: model.StatusOK, Algo: p.AlgoVersion, ParamFP: pf}
	if src.Blank != nil {
		corr.Children = append(corr.Children, model.Node{ID: nid(stateHash, "blank-sub"), Label: "blank curve subtraction (m_sample = m_raw - m_blank(t))", Kind: "note", Status: model.StatusOK, Algo: p.AlgoVersion, ParamFP: pf, Direct: []string{src.Blank.ID, src.Samples.ID}})
	} else {
		corr.Children = append(corr.Children, model.Node{ID: nid(stateHash, "blank-sub"), Label: "no blank curve layer submitted", Kind: "note", Status: model.StatusUnknown, Algo: p.AlgoVersion, ParamFP: pf})
	}
	if src.Drift != nil {
		corr.Children = append(corr.Children, model.Node{ID: nid(stateHash, "drift-sub"), Label: fmt.Sprintf("balance drift subtraction a=%.5f mg/s b=%.4f mg (%s)", src.Drift.Drift.AMgPerS, src.Drift.Drift.BMg, src.Drift.Drift.Basis), Kind: "note", Status: model.StatusOK, Algo: p.AlgoVersion, ParamFP: pf, Direct: []string{src.Drift.ID}})
	} else {
		corr.Children = append(corr.Children, model.Node{ID: nid(stateHash, "drift-sub"), Label: "no drift correction layer", Kind: "note", Status: model.StatusUnknown, Algo: p.AlgoVersion, ParamFP: pf})
	}
	corr.Children = append(corr.Children, model.Node{
		ID: nid(stateHash, "m0"), Label: "sample initial mass m0", Kind: "scalar",
		Value: fmt.Sprintf("%.4f mg", m0), Unit: model.MassBase, Status: m0Status, Algo: p.AlgoVersion, ParamFP: pf,
		Direct: ternarySlice(src.Initial != nil, []string{src.Initial.ID}, nil),
		Reason: ternaryS(m0Status == model.StatusUnknown, "no initial layer: m0 inferred from first valid point", ""),
	})
	add(corr)

	// 3. normalization + gas/temp model
	norm := model.Node{ID: nid(stateHash, "norm"), Label: "normalization and atmosphere/temperature model", Kind: "group", Status: model.StatusOK, Algo: p.AlgoVersion, ParamFP: pf}
	norm.Children = append(norm.Children,
		model.Node{ID: nid(stateHash, "series"), Label: "corrected TG mass % (per segment, gaps not interpolated)", Kind: "series", Status: model.StatusOK, Algo: p.AlgoVersion, ParamFP: pf, Unit: model.MassPctUnit},
		model.Node{ID: nid(stateHash, "dtg"), Label: "DTG dm/dt (central difference on smoothed %, %/min)", Kind: "series", Status: model.StatusOK, Algo: p.AlgoVersion, ParamFP: pf, Unit: model.RateUnit},
		model.Node{ID: nid(stateHash, "gas-model"), Label: fmt.Sprintf("gas delay %.1fs; temperature lag %.1fK (valve time != reaction time)", p.GasDelayS, p.TempLagK), Kind: "note", Status: ternary(p.GasDelayS != 0 || p.TempLagK != 0, model.StatusOK, model.StatusUnknown), Algo: p.AlgoVersion, ParamFP: pf, Direct: gasDirect(src)},
	)
	for i, g := range d.Gaps {
		gg := g
		norm.Children = append(norm.Children, model.Node{ID: nid(stateHash, "gap", i), Label: fmt.Sprintf("gap %.1f..%.1fs — no interpolation", gg.StartS, gg.EndS), Kind: "gap", Status: model.StatusUnknown, Algo: p.AlgoVersion, ParamFP: pf, Reason: gg.Reason})
	}
	add(norm)

	// 4. findings
	find := model.Node{ID: nid(stateHash, "findings"), Label: "machine-readable judgments", Kind: "group", Status: model.StatusOK, Algo: p.AlgoVersion, ParamFP: pf}
	for i, f := range d.Findings {
		find.Children = append(find.Children, model.Node{ID: nid(stateHash, "finding", i), Label: f.Code, Kind: "note", Status: sevStatus(f.Severity), Algo: p.AlgoVersion, ParamFP: pf, Reason: f.Message, Direct: f.Refs})
	}
	add(find)

	// 5. candidates
	cg := model.Node{ID: nid(stateHash, "candidates"), Label: "stage candidates (may overlap; losses not forced to 100%)", Kind: "group", Status: model.StatusOK, Algo: p.AlgoVersion, ParamFP: pf}
	for i := range d.Candidates {
		cg.Children = append(cg.Children, d.Candidates[i].Node)
	}
	add(cg)

	// 6. residue
	rg := model.Node{ID: nid(stateHash, "residues"), Label: "residue rates (expandable calculation chain)", Kind: "group", Status: model.StatusOK, Algo: p.AlgoVersion, ParamFP: pf}
	for i := range d.Residues {
		rg.Children = append(rg.Children, d.Residues[i].Node)
	}
	add(rg)

	return root
}

func indexNodes(n *model.Node, m map[string]*model.Node) {
	m[n.ID] = n
	for i := range n.Children {
		indexNodes(&n.Children[i], m)
	}
}

func ternary(cond bool, a, b model.Status) model.Status {
	if cond {
		return a
	}
	return b
}
func ternaryS(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
func ternarySlice(cond bool, a, b []string) []string {
	if cond {
		return a
	}
	return b
}
func sevStatus(s model.Severity) model.Status {
	if s == model.SevError {
		return model.StatusError
	}
	return model.StatusUnknown
}
func gasDirect(src Sources) []string {
	if src.Gases != nil {
		return []string{src.Gases.ID}
	}
	return nil
}
