package engine

import (
	"fmt"
	"tgareview/internal/domain"
)

type stageResult struct {
	stages   []Stage
	rejected []StageCandidate
}

func findStages(entries []entry, pct, dtg []*float64, nonMono []bool,
	segs []segment, p Params, r *Report) stageResult {

	var peaks []int
	for _, s := range segs {
		for i := s.from + 1; i < s.to; i++ {
			if dtg[i] == nil || dtg[i-1] == nil || dtg[i+1] == nil {
				continue
			}
			if *dtg[i] < *dtg[i-1] && *dtg[i] < *dtg[i+1] && *dtg[i] < 0 {
				peaks = append(peaks, i)
			}
		}
	}

	// window expansion helper: stop at segment boundary or DTG ~0
	expand := func(pk int) (int, int) {
		lo, hi := pk, pk
		for lo > 0 && segContains(segs, lo-1, lo) && dtg[lo-1] != nil && *dtg[lo-1] < 0 {
			lo--
		}
		for hi < len(dtg)-1 && segContains(segs, hi, hi+1) && dtg[hi+1] != nil && *dtg[hi+1] < 0 {
			hi++
		}
		return lo, hi
	}

	var out stageResult
	used := map[int]bool{}
	stageNo, candNo := 0, 0
	for _, pk := range peaks {
		if used[pk] {
			continue
		}
		lo, hi := expand(pk)
		// group overlapping peaks sharing part of the same DTG valley
		var group []int
		for _, q := range peaks {
			if q == pk || used[q] {
				continue
			}
			ql, qh := expand(q)
			if ql <= hi && qh >= lo {
				group = append(group, q)
			}
		}
		group = append([]int{pk}, group...)
		prom := 0.0
		if dtg[pk] != nil {
			prom = -(*dtg[pk])
		}
		stageNo++
		label := fmt.Sprintf("S%d", stageNo)

		// Peaks below prominence threshold are retained but rejected.
		if prom < p.PeakPromPctPerMin {
			cand := makeCandidate(entries, pct, dtg, nonMono, segs, lo, hi, pk,
				fmt.Sprintf("C%d", candNo+1), r, p)
			cand.Status = StatusRejected
			cand.Reason = fmt.Sprintf("peak prominence %.4f %%/min below threshold %.4f", prom, p.PeakPromPctPerMin)
			out.rejected = append(out.rejected, cand)
			for _, q := range group {
				used[q] = true
			}
			candNo++
			stageNo--
			continue
		}

		primary := makeCandidate(entries, pct, dtg, nonMono, segs, lo, hi, pk,
			fmt.Sprintf("%s-a", label), r, p)
		cands := []StageCandidate{primary}
		used[pk] = true

		// Overlapping, unresolved peaks become alternative legal candidates;
		// stages are not forced to sum to 100% and may overlap.
		for k, q := range group[1:] {
			ql, qh := expand(q)
			used[q] = true
			c := makeCandidate(entries, pct, dtg, nonMono, segs, ql, qh, q,
				fmt.Sprintf("%s-%s", label, string(rune('b'+k))), r, p)
			c.Reason = "overlapping peak; boundary not uniquely resolvable"
			cands = append(cands, c)
			candNo++
		}

		// Minimum mass loss filter: small stages become rejected but stay visible.
		if primary.MassLossPct != nil && *primary.MassLossPct < p.MinLossPct {
			primary.Status = StatusRejected
			primary.Reason = fmt.Sprintf("mass loss %.3f%% below minimum %.3f%%", *primary.MassLossPct, p.MinLossPct)
			out.rejected = append(out.rejected, primary)
			for _, c := range cands[1:] {
				c.Status = StatusRejected
				c.Reason = "candidate of below-minimum stage"
				out.rejected = append(out.rejected, c)
			}
			stageNo--
			continue
		}

		st := Stage{
			UID:        fmt.Sprintf("%s-%s", domain.UIDStage, label),
			Label:      label,
			Candidates: cands,
			Primary:    primary,
			Chain:      stageChain(primary, r, p),
			Sources:    stageSources(primary, r),
		}
		st.DerID = domain.DerivationID("stage", r.ParamFP, r.Sources...)
		for i := range st.Candidates {
			st.Candidates[i].DerID = st.DerID
		}
		st.Primary.DerID = st.DerID
		out.stages = append(out.stages, st)
	}
	return out
}

func segContains(segs []segment, a, b int) bool {
	for _, s := range segs {
		if a >= s.from && a <= s.to && b >= s.from && b <= s.to {
			return true
		}
	}
	return false
}

func makeCandidate(entries []entry, pct, dtg []*float64, nonMono []bool,
	segs []segment, lo, hi, pk int, label string, r *Report, p Params) StageCandidate {
	c := StageCandidate{
		UID:      fmt.Sprintf("%s-%s", domain.UIDStage, label),
		Label:    label,
		StartIdx: entries[lo].idx,
		EndIdx:   entries[hi].idx,
		StartTS:  entries[lo].t,
		EndTS:    entries[hi].t,
		PeakIdx:  entries[pk].idx,
		PeakTS:   entries[pk].t,
		StartTC:  entries[lo].furn,
		EndTC:    entries[hi].furn,
		PeakTC:   entries[pk].furn,
		Status:   StatusOK,
	}
	if !segContains(segs, lo, hi) {
		c.CrossesGap = true
		c.Status = StatusUnknown
		c.Reason = "window crosses a sampling gap; not interpolated"
	}
	for i := lo; i <= hi; i++ {
		if nonMono[i] {
			c.Noisy = true
			break
		}
	}
	if dtg[pk] != nil {
		v := *dtg[pk]
		c.PeakDTG = &v
	}
	if pct[lo] != nil && pct[hi] != nil {
		loss := *pct[lo] - *pct[hi]
		c.MassLossPct = &loss
	} else {
		c.Status = StatusUnknown
		if c.Reason == "" {
			c.Reason = "mass percent unknown within window"
		}
	}
	c.GasWindow = gasWindowAt(r, entries[pk].t)
	return c
}

func gasWindowAt(r *Report, t float64) string {
	current := "?"
	for _, sw := range r.Switches {
		if t >= sw.EffTS {
			current = sw.To
		}
	}
	if len(r.Switches) == 0 {
		return "no gas events"
	}
	return current
}

func stageSources(c StageCandidate, r *Report) []Ref {
	refs := []Ref{{r.SampleSrc, "raw", "sample series"}}
	if r.GasSrc != "" {
		refs = append(refs, Ref{r.GasSrc, "raw", "gas valve events"})
	}
	return refs
}

func stageChain(c StageCandidate, r *Report, p Params) []ChainStep {
	loss := ChainStep{UID: c.UID + "-chain-loss", Op: "mass_loss",
		Text: "window mass loss from corrected percent"}
	if c.MassLossPct != nil {
		loss.Value = &StepVal{Number: *c.MassLossPct, Unit: "%", Status: c.Status}
	} else {
		loss.Value = &StepVal{Unit: "%", Status: StatusUnknown}
	}
	peak := ChainStep{UID: c.UID + "-chain-peak", Op: "dtg_peak",
		Text: "DTG minimum (central difference within one gap segment)"}
	if c.PeakDTG != nil {
		peak.Value = &StepVal{Number: *c.PeakDTG, Unit: "%/min", Status: c.Status}
	}
	atm := ChainStep{UID: c.UID + "-chain-atm", Op: "atmosphere_lag",
		Text: fmt.Sprintf("valve time shifted by %.0f s; gas at reaction: %s", p.AtmDelayS, c.GasWindow)}
	atm.Value = &StepVal{Number: p.AtmDelayS, Unit: "s"}
	atm.Refs = []Ref{{r.GasSrc, "raw", "gas valve events"}}
	return []ChainStep{loss, peak, atm}
}

func buildResidues(sr stageResult, entries []entry, pct []*float64,
	corrected []*float64, src Sources, p Params) []ResidueResult {
	var out []ResidueResult

	// one residue at the end of every stage window
	for _, st := range sr.stages {
		c := st.Primary
		rr := residueAt(fmt.Sprintf("%s-%s", domain.UIDResidue, st.Label),
			"stage_end", st.Label+" end", c.EndIdx-1, entries, pct, corrected, src, p, c)
		out = append(out, rr)
	}

	// final residue from the last valid corrected point
	last := -1
	for i := len(corrected) - 1; i >= 0; i-- {
		if corrected[i] != nil && entries[i].tOK {
			last = i
			break
		}
	}
	if last >= 0 {
		out = append(out, residueAt(fmt.Sprintf("%s-final", domain.UIDResidue),
			"final", "final residue", last, entries, pct, corrected, src, p, StageCandidate{}))
	}
	return out
}

func residueAt(uid, kind, label string, idx int, entries []entry, pct,
	corrected []*float64, src Sources, p Params, c StageCandidate) ResidueResult {
	rr := ResidueResult{UID: uid, Kind: kind, Label: label,
		AtIdx: entries[idx].idx, AtTS: entries[idx].t, AtTC: entries[idx].furn,
		Status: StatusUnknown}
	if pct[idx] != nil {
		v := *pct[idx]
		rr.Percent = &v
		rr.Status = StatusOK
	}
	if corrected[idx] != nil {
		v := *corrected[idx]
		rr.MassMg = &v
	}
	rr.Chain = residueChain(uid, idx, entries, pct, corrected, src, p, rr)
	rr.Sources = []Ref{{src.Sample.ID, "raw", "sample series"}}
	if src.InitialMass != nil {
		rr.Sources = append(rr.Sources, Ref{src.InitialMass.ID, "raw", "initial mass"})
	}
	if src.Blank != nil {
		rr.Sources = append(rr.Sources, Ref{src.Blank.ID, "raw", "blank curve"})
	}
	if src.Drift != nil {
		rr.Sources = append(rr.Sources, Ref{src.Drift.ID, "raw", "balance drift"})
	}
	return rr
}

func residueChain(uid string, idx int, entries []entry, pct,
	corrected []*float64, src Sources, p Params, rr ResidueResult) []ChainStep {
	var chain []ChainStep
	rawStep := ChainStep{UID: uid + "-raw", Op: "raw_mass", Text: "raw balance reading"}
	if entries[idx].mg != nil {
		rawStep.Value = &StepVal{Number: *entries[idx].mg, Unit: "mg", Raw: entries[idx].rawM}
	} else {
		rawStep.Value = &StepVal{Status: StatusUnknown, Raw: entries[idx].rawM}
	}
	rawStep.Refs = []Ref{{src.Sample.ID, "raw", "sample series"}}
	chain = append(chain, rawStep)

	corrStep := ChainStep{UID: uid + "-corrected", Op: "correct",
		Text: "blank subtraction and balance drift correction (piecewise linear, no gap crossing)"}
	if corrected[idx] != nil {
		corrStep.Value = &StepVal{Number: *corrected[idx], Unit: "mg", Status: rr.Status}
	} else {
		corrStep.Value = &StepVal{Unit: "mg", Status: StatusUnknown}
	}
	if src.Blank != nil {
		corrStep.Refs = append(corrStep.Refs, Ref{src.Blank.ID, "raw", "blank curve"})
	}
	if src.Drift != nil {
		corrStep.Refs = append(corrStep.Refs, Ref{src.Drift.ID, "raw", "balance drift"})
	}
	chain = append(chain, corrStep)

	imStep := ChainStep{UID: uid + "-m0", Op: "initial_mass", Text: "revised initial mass (latest accepted version)"}
	if src.InitialMass != nil {
		imStep.Refs = []Ref{{src.InitialMass.ID, "raw", "initial mass"}}
	}
	chain = append(chain, imStep)

	final := ChainStep{UID: uid + "-pct", Op: "residue_pct",
		Text: "residue % = corrected mass / initial mass x 100"}
	if rr.Percent != nil {
		final.Value = &StepVal{Number: *rr.Percent, Unit: "%", Status: rr.Status}
	} else {
		final.Value = &StepVal{Unit: "%", Status: StatusUnknown}
	}
	chain = append(chain, final)
	return chain
}
