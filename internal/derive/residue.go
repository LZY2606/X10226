package derive

import (
	"fmt"
	"sort"

	"tga-review/internal/domain"
)

// BuildResidues computes final residue with an expandable calculation chain,
// plus one residue-per-stage. Unknown windows stay unknown with reasons.
func (e *Engine) BuildResidues(s Series, st StagesResult) []Residue {
	win := e.pNum("residue_window_s")
	out := e.finalResidue(s, win)
	var residues []Residue
	residues = append(residues, out.residue)
	if !out.hasFinal {
		residues[0].Unknown = true
		residues[0].UnknownReason = "no observed points in final residue window"
		residues[0].Value = nil
	}
	for _, stage := range st.Stages {
		if stage.Status != "candidate" {
			continue
		}
		r := e.stageResidue(s, stage)
		residues = append(residues, r)
	}
	return residues
}

type finalOut struct {
	residue  Residue
	hasFinal bool
}

func (e *Engine) finalResidue(s Series, win float64) finalOut {
	if len(s.Points) == 0 {
		return finalOut{residue: Residue{Label: "final_residue", Unknown: true,
			UnknownReason: "empty series"}}
	}
	tEnd := s.Points[len(s.Points)-1].T
	t0 := tEnd - win
	// only observed points inside window (no gap interpolation)
	var masses []float64
	var gapInside bool
	for _, p := range s.Points {
		if p.T >= t0 && p.T <= tEnd {
			masses = append(masses, p.CorrMassKg)
		}
	}
	for _, g := range s.Gaps {
		if g.T0 >= t0-1 && g.T1 <= tEnd+1 {
			gapInside = true
		}
	}

	r := Residue{
		Label: "final_residue", WindowT0: t0, WindowT1: tEnd,
		Sources:     append([]string{}, s.Sources...),
		AlgoVersion: AlgoVersion, ParamFP: e.FP, ParamRev: e.Rev,
	}
	r.Chain = []ChainStep{
		{Order: 1, Text: "initial mass stratum (revision preserved)",
			Value: formatNum(s.InitialMassKg * 1e3), Unit: "mg", Source: s.InitialMassID},
	}
	if len(masses) > 0 {
		mean := 0.0
		for _, m := range masses {
			mean += m
		}
		mean /= float64(len(masses))
		v := mean / s.InitialMassKg
		r.Value = &v
		r.Chain = append(r.Chain,
			ChainStep{Order: 2, Text: fmt.Sprintf("mean corrected mass over last %.0fs observed window (%d points, no interpolation)", win, len(masses)),
				Value: formatNum(mean * 1e3), Unit: "mg"},
			ChainStep{Order: 3, Text: "residue fraction = mean final mass / initial mass",
				Value: formatNum(v * 100), Unit: "wt%"},
		)
		if gapInside {
			r.Chain = append(r.Chain, ChainStep{Order: 4,
				Text: "window contains a sampling gap; missing points excluded, not interpolated"})
		}
	}
	r.ID = e.derID("residue", map[string]any{
		"label": r.Label, "t0": t0, "t1": tEnd, "points": len(masses),
		"initial": s.InitialMassID,
	})
	return finalOut{residue: r, hasFinal: len(masses) > 0}
}

func (e *Engine) stageResidue(s Series, stage Stage) Residue {
	r := Residue{
		Label: "residue_after_" + stage.Label, WindowT0: stage.FromT, WindowT1: stage.ToT,
		Sources:     append([]string{}, stage.Sources...),
		AlgoVersion: AlgoVersion, ParamFP: e.FP, ParamRev: e.Rev,
	}
	mEnd, okEnd := massAtObserved(s, stage.ToT)
	r.Chain = []ChainStep{
		{Order: 1, Text: "initial mass stratum", Value: formatNum(s.InitialMassKg * 1e3),
			Unit: "mg", Source: s.InitialMassID},
	}
	if !okEnd || stage.Unknown {
		r.Unknown = true
		r.UnknownReason = "stage crosses gap or endpoint unobserved"
		r.Chain = append(r.Chain, ChainStep{Order: 2,
			Text: "terminal mass unknown (gap is not interpolated)"})
	} else {
		v := mEnd / s.InitialMassKg
		r.Value = &v
		r.Chain = append(r.Chain,
			ChainStep{Order: 2, Text: "corrected mass at stage end (observed point only)",
				Value: formatNum(mEnd * 1e3), Unit: "mg"},
			ChainStep{Order: 3, Text: "residue = terminal corrected mass / initial mass",
				Value: formatNum(v * 100), Unit: "wt%"},
		)
	}
	r.ID = e.derID("residue", map[string]any{
		"label": r.Label, "stage": stage.ID, "initial": s.InitialMassID,
	})
	return r
}

func (e *Engine) derID(kind string, payload map[string]any) string {
	payload["algo"] = AlgoVersion
	payload["fp"] = e.FP
	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return domain.MustID("d", map[string]any{"kind": kind, "body": payload})
}
