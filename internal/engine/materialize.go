package engine

import (
	"tgareview/internal/compute"
	"tgareview/internal/domain"
)

// materialize reruns the pure pipeline from current layered inputs, then
// overlays human decisions. Local input changes only invalidate results that
// depend on them (computed ids are content-derived); superseded ids stay
// viewable with status=stale and can never be presented as current.
func (s *State) materialize() {
	in := compute.Input{
		ProjectID: s.ProjectID, AtSeq: s.Seq, Params: s.Params,
		Raw: s.Raw, Drifts: s.Drifts, Blanks: s.Blanks, Program: s.Program,
		Gas: s.Gas, Weight: s.Weights, Notes: s.Notes,
	}
	view := compute.Run(in)

	// Track ids surviving this recomputation.
	alive := map[string]bool{}
	mark := func(id string, st *domain.Status) {
		alive[id] = true
	}
	for i := range view.Stages {
		mark(view.Stages[i].ID, &view.Stages[i].Status)
	}
	for i := range view.Residues {
		mark(view.Residues[i].ID, &view.Residues[i].Status)
	}
	for i := range view.Peaks {
		mark(view.Peaks[i].ID, &view.Peaks[i].Status)
	}

	// Overlay accepted/rejected/adjusted decisions on current candidates.
	type adj struct {
		start, end float64
		note       string
		basis      string
	}
	adjusts := map[string]adj{}
	for _, d := range s.Decisions {
		switch d.TargetKind {
		case "stage":
			for i := range view.Stages {
				st := &view.Stages[i]
				if st.ID != d.TargetID {
					continue
				}
				switch d.Kind {
				case domain.DecAcceptStage:
					st.Verdict = "accepted"
				case domain.DecRejectStage:
					st.Verdict = "rejected"
					st.Rejected = &domain.Rejection{
						Code: "reviewer_rejected", Message: d.Note, AtSeq: d.SeqRef,
					}
				case domain.DecAdjustStage:
					adjusts[st.ID] = adj{
						start: valueOr(d.StartS, st.StartS),
						end:   valueOr(d.EndS, st.EndS),
						note:  d.Note, basis: d.Basis,
					}
				}
			}
		case "residue":
			for i := range view.Residues {
				r := &view.Residues[i]
				if r.ID != d.TargetID {
					continue
				}
				if d.Kind == domain.DecAcceptResidue {
					r.Verdict = "accepted"
				}
				if d.Kind == domain.DecRejectResidue {
					r.Verdict = "rejected"
				}
			}
		case "peak":
			for i := range view.Peaks {
				pk := &view.Peaks[i]
				if pk.ID == d.TargetID && d.Kind == domain.DecAcceptPeak {
					_ = pk // acceptance recorded as decision history
				}
			}
		}
	}
	// Adjustments produce new, explicitly-versioned manual candidates linked
	// back to the original; the pre-adjustment candidate remains for history.
	for origID, a := range adjusts {
		orig := findStage(view, origID)
		if orig == nil {
			continue
		}
		newID := domain.ShortID("stage-manual", origID, f64s(a.start), f64s(a.end))
		if alive[newID] {
			continue
		}
		cand := *orig
		cand.ID = newID
		cand.StartS, cand.EndS = a.start, a.end
		cand.Origin, cand.Adjusted = "manual", true
		cand.Verdict = "accepted"
		cand.Rejected = nil
		cand.Prov = withProv(orig.Prov,
			domain.Source{Kind: "decision", ID: a.basis, Detail: "stage_adjustment"})
		s.refillStageBounds(&cand, view)
		view.Stages = append(view.Stages, cand)
		alive[newID] = true
		// The original candidate is superseded by the adjustment.
		orig.Verdict = "superseded"
	}

	// Carry over candidates that disappeared after a local source change as
	// stale, first-class records. They stay viewable but are never current.
	prev := s.View
	if prev != nil {
		aliveStage := map[string]bool{}
		for _, st := range view.Stages {
			aliveStage[st.ID] = true
		}
		for _, st := range prev.Stages {
			if !aliveStage[st.ID] && (st.Verdict != "" || st.Origin == "manual") {
				st.Status = domain.StatusStale
				view.StaleStages = append(view.StaleStages, st)
			}
		}
		aliveRes := map[string]bool{}
		for _, r := range view.Residues {
			aliveRes[r.ID] = true
		}
		for _, r := range prev.Residues {
			if !aliveRes[r.ID] && r.Verdict != "" {
				r.Status = domain.StatusStale
				view.StaleResidues = append(view.StaleResidues, r)
			}
		}
		alivePeak := map[string]bool{}
		for _, pk := range view.Peaks {
			alivePeak[pk.ID] = true
		}
		for _, pk := range prev.Peaks {
			if !alivePeak[pk.ID] {
				pk.Status = domain.StatusStale
				view.StalePeaks = append(view.StalePeaks, pk)
			}
		}
	}

	// Open conflicts surface in diagnostics; resolved ones keep the evidence.
	for _, c := range s.Conflicts {
		if c.Open {
			view.Diagnostics = append(view.Diagnostics,
				"open conflict "+c.ID+" between human decisions; resolution required")
		}
	}
	if !view.InitialWeightKnown {
		view.Status = domain.StatusUnknown
	}
	s.View = view
}

func f64s(v float64) string { return f64f(v) }

func findStage(v *domain.DerivedView, id string) *domain.StageCandidate {
	for i := range v.Stages {
		if v.Stages[i].ID == id {
			return &v.Stages[i]
		}
	}
	return nil
}

func valueOr(p *float64, fallback float64) float64 {
	if p == nil {
		return fallback
	}
	return *p
}

func withProv(pr domain.Provenance, src domain.Source) domain.Provenance {
	pr.DirectSources = append(append([]domain.Source{}, pr.DirectSources...), src)
	return pr
}

func (s *State) refillStageBounds(cand *domain.StageCandidate, view *domain.DerivedView) {
	compute.RefillStage(cand, view, s.Weights, s.Params)
}
