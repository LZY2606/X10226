package engine

import (
	"fmt"
	"math"

	"tga-audit/internal/domain"
)

// findCandidates discovers DTG loss peaks per gap-bounded run. Where two peaks
// are closer than OverlapResolveWidthS they are also emitted as a single
// overlap_cluster candidate whose mass loss spans both: both interpretations
// are first-class legal candidates and neither is forced to sum to 100%.
func findCandidates(pts []DerivedPoint, gaps []Gap, p Params, timeline []GasReaction, revs map[domain.Kind]string) []Candidate {
	runs := gapRuns(pts, gaps)
	var cands []Candidate
	maxAmp := 0.0
	for _, pts := range pts {
		if a := math.Abs(pts.DTG); a > maxAmp {
			maxAmp = a
		}
	}
	prom := p.PeakProminenceUgPerS
	if minF := p.MinPeakFraction * maxAmp; minF > prom {
		prom = minF
	}
	for _, run := range runs {
		var peakIdx []int
		for i := 1; i < len(run)-1; i++ {
			if run[i].DTG < -prom && run[i].DTG <= run[i-1].DTG && run[i].DTG <= run[i+1].DTG {
				peakIdx = append(peakIdx, i)
			}
		}
		if len(peakIdx) == 0 {
			continue
		}
		// cluster peaks that are too close to resolve uniquely
		clusters := clusterPeaks(run, peakIdx, p.OverlapResolveWidthS)
		for _, cl := range clusters {
			if len(cl) == 1 {
				cands = append(cands, peakCandidate(run, cl[0], timeline))
			} else {
				for _, pi := range cl {
					cands = append(cands, peakCandidate(run, pi, timeline))
				}
				cands = append(cands, overlapCandidate(run, cl, timeline))
			}
		}
	}
	return cands
}

func gapRuns(pts []DerivedPoint, gaps []Gap) [][]DerivedPoint {
	cut := map[int]bool{}
	for _, g := range gaps {
		cut[g.BeforeIndex] = true
	}
	var runs [][]DerivedPoint
	start := 0
	for i := range pts {
		if cut[pts[i].Index] {
			runs = append(runs, pts[start:i])
			start = i
		}
	}
	runs = append(runs, pts[start:])
	return runs
}

func clusterPeaks(run []DerivedPoint, peaks []int, widthS float64) [][]int {
	var clusters [][]int
	cur := []int{peaks[0]}
	for _, pi := range peaks[1:] {
		if run[pi].T-run[cur[len(cur)-1]].T <= widthS {
			cur = append(cur, pi)
		} else {
			clusters = append(clusters, cur)
			cur = []int{pi}
		}
	}
	clusters = append(clusters, cur)
	return clusters
}

// boundaryFor walks outward from the peak until DTG approaches zero (loss
// onset/offset). It stops at the run edge and never crosses a gap; if the zero
// crossing is unobservable because a gap intervenes, ForcedByGap is set and the
// candidate remains legal but carries the reason rather than being interpolated.
func boundaryFor(run []DerivedPoint, peak int) (l, r int, forcedL, forcedR bool) {
	l, r = peak, peak
	for l > 0 {
		if run[l-1].DTG >= -0.05*math.Abs(run[peak].DTG) || run[l-1].DTG >= 0 {
			break
		}
		l--
	}
	if l == 0 {
		forcedL = true
	}
	for r < len(run)-1 {
		if run[r+1].DTG >= -0.05*math.Abs(run[peak].DTG) || run[r+1].DTG >= 0 {
			break
		}
		r++
	}
	if r == len(run)-1 {
		forcedR = true
	}
	return
}

func memberKeys(run []DerivedPoint, l, r int) []string {
	keys := make([]string, 0, r-l+1)
	for i := l; i <= r; i++ {
		keys = append(keys, "points#"+itoa(run[i].Index))
	}
	return keys
}

func linkGas(run []DerivedPoint, peak int, timeline []GasReaction) string {
	best := ""
	bestDt := math.MaxFloat64
	for _, g := range timeline {
		dt := math.Abs(g.ReactionAtS - run[peak].T)
		if dt < bestDt && dt <= 60 {
			bestDt, best = dt, g.SourceKey
		}
	}
	return best
}

func peakCandidate(run []DerivedPoint, peak int, timeline []GasReaction) Candidate {
	l, r, fl, fr := boundaryFor(run, peak)
	c := Candidate{
		Key:           fmt.Sprintf("cand:peak:%d", run[peak].Index),
		Kind:          "peak",
		PeakIndex:     run[peak].Index,
		BoundaryL:     run[l].Index,
		BoundaryR:     run[r].Index,
		PeakT:         run[peak].T,
		PeakSampleT:   run[peak].SampleT,
		PeakDTGUgPerS: run[peak].DTG,
		MassLossMg:    run[l].CorrMassMg - run[r].CorrMassMg,
		ForcedByGap:   fl || fr,
		LinkedGasKey:  linkGas(run, peak, timeline),
		MemberKeys:    memberKeys(run, l, r),
	}
	if c.ForcedByGap {
		c.RejectedReason = "boundary_uncertain_gap_not_interpolated"
	}
	if c.MassLossMg < 0 {
		c.RejectedReason = "non_monotonic_mass_gain"
	}
	return c
}

func overlapCandidate(run []DerivedPoint, cl []int, timeline []GasReaction) Candidate {
	l0, _, fl, _ := boundaryFor(run, cl[0])
	_, r0, _, fr := boundaryFor(run, cl[len(cl)-1])
	members := make([]string, len(cl))
	for i, pi := range cl {
		members[i] = fmt.Sprintf("cand:peak:%d", run[pi].Index)
	}
	c := Candidate{
		Key:               fmt.Sprintf("cand:overlap:%d-%d", run[cl[0]].Index, run[cl[len(cl)-1]].Index),
		Kind:              "overlap_cluster",
		PeakIndex:         run[cl[0]].Index,
		BoundaryL:         run[l0].Index,
		BoundaryR:         run[r0].Index,
		PeakT:             run[cl[0]].T,
		PeakSampleT:       run[cl[0]].SampleT,
		PeakDTGUgPerS:     run[cl[0]].DTG,
		MassLossMg:        run[l0].CorrMassMg - run[r0].CorrMassMg,
		OverlapMemberKeys: members,
		ForcedByGap:       fl || fr,
		LinkedGasKey:      linkGas(run, cl[0], timeline),
		MemberKeys:        memberKeys(run, l0, r0),
	}
	if c.ForcedByGap {
		c.RejectedReason = "boundary_uncertain_gap_not_interpolated"
	}
	return c
}

func assignMassLoss(cands []Candidate, pts []DerivedPoint, m0 *float64) {
	if m0 == nil || *m0 <= 0 {
		return
	}
	for i := range cands {
		pct := 100 * cands[i].MassLossMg / *m0
		cands[i].MassLossPct = &pct
	}
}

func buildResidue(pts []DerivedPoint, gaps []Gap, in Inputs, m0 *float64) *Residue {
	res := &Residue{Status: "ok", StageSumNote: "stages_may_overlap_and_need_not_sum_to_100pct"}
	last := pts[len(pts)-1]

	// chain step 1: drift-corrected final mass, with direct point source
	res.Chain = append(res.Chain, ChainNode{
		ID: "chain:final_raw", Step: "final_balance_reading",
		Value: last.RawMassMg, Unit: "mg",
		SourceKey: "points#" + itoa(last.Index), SourceKind: "points",
	})
	if in.Drift != nil {
		correction := in.Drift.Slope.Numeric*last.T + driftIntercept(in.Drift)
		res.Chain = append(res.Chain,
			ChainNode{ID: "chain:drift_model", Step: "baseline_drift_model", Value: correction, Unit: "mg",
				SourceKey: "drift#" + itoa(in.Drift.Index), SourceKind: "drift",
				Note: "subtracted; slope*elapsed+intercept"},
			ChainNode{ID: "chain:final_corr", Step: "final_drift_corrected_mass",
				Value: last.CorrMassMg, Unit: "mg",
				Note: "final_raw - drift_correction"},
		)
	}
	v := last.CorrMassMg
	res.FinalMassMg = &v

	// a gap immediately before the final point means "final" plateau is unobserved
	for _, g := range gaps {
		if g.BeforeIndex == last.Index {
			res.Status = "gap_before_final"
			res.Chain = append(res.Chain, ChainNode{
				ID: "chain:gap", Step: "sampling_gap_before_final", Unknown: true,
				Note: "no interpolation across gap; final plateau not directly observed",
			})
		}
	}

	if m0 == nil {
		res.Status = "unknown_m0"
		res.Chain = append(res.Chain, ChainNode{
			ID: "chain:m0_unknown", Step: "initial_mass", Unknown: true, Unit: "mg",
			SourceKind: "m0", Note: "no usable initial mass revision; residue percent is unknown",
		})
		return res
	}
	res.InitialMassMg = m0
	res.Chain = append(res.Chain, ChainNode{
		ID: "chain:m0", Step: "initial_mass_m0", Value: *m0, Unit: "mg",
		SourceKey: "m0#" + itoa(in.M0.Index), SourceKind: "m0",
		Note: "analyst " + in.M0.Analyst + ": " + in.M0.Basis,
	})
	pct := 100 * last.CorrMassMg / *m0
	res.ValuePct = &pct
	res.Chain = append(res.Chain, ChainNode{
		ID: "chain:residue", Step: "residue_percent", Value: pct, Unit: "%",
		Note: "100 * final_drift_corrected_mass / m0",
	})
	return res
}
