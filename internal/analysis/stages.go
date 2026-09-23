package analysis

import (
	"fmt"
	"sort"

	"tga-review/internal/domain"
)

// Decompose turns peaks into stage proposals. Every overlap cluster yields:
//   - an alt-separate proposal (one stage per peak);
//   - an alt-merged proposal (single combined stage);
// and each stage whose valley or extent crosses a sampling gap is retained
// but marked illegal with an explicit reject reason. Stages are allowed to
// overlap and percentages are never normalized to total 100%.
func (e *Engine) Decompose(in Input, corr CorrectedPayload, dtg DTGPayload, peaks PeakPayload, atmos AtmosPayload) StagesPayload {
	var diags []domain.Diagnostic
	out := StagesPayload{}

	// choose initial mass: latest record is current; chain records the choice
	initial, initialID := chooseInitial(in.Initial)
	out.InitialID = initialID
	if initial == nil {
		diags = append(diags, domain.Diagnostic{Code: "NO_INITIAL_MASS", Severity: domain.SevError,
			Message: "no initial sample mass present; mass-loss percentages and residue unknown"})
	}

	groups := orderedGroups(peaks.Peaks)
	stageCounter := 0
	nextLocal := func() string {
		s := fmt.Sprintf("stage-%d", stageCounter+1)
		stageCounter++
		return s
	}

	mkStage := func(p Peak, startS, endS float64, localID string, overlaps []string) domain.Candidate {
		startC, startOK := tempAt(corr, startS)
		endC, endOK := tempAt(corr, endS)
		gas, gasUncertain := gasAtTime(atmos, startS, endS)
		crosses := crossesAnyGap(corr, startS, endS)
		c := domain.Candidate{
			LocalID: localID, StartSec: startS, EndSec: endS,
			StartC: startC, EndC: endC, PeakSec: p.TimeS, PeakC: p.TempC,
			Atmosphere: gas, AtmosUncertain: gasUncertain, Overlaps: overlaps, CrossesGap: crosses,
			Status: "candidate",
		}
		if !startOK || !endOK {
			c.CrossesGap = true
		}
		// value refs
		artID := in.ArtifactID(domain.ArtStages)
		c.Ref = domain.Ref{ID: localID, ArtifactID: artID, RunID: in.RunID,
			SourceIDs: []string{in.SourceIDs[domain.LayerSamples]}, Algo: domain.AlgoStages}
		c.PeakRate = domain.ValueRef{Ref: domain.Ref{ID: localID + ":peakrate", ArtifactID: artID, RunID: in.RunID,
			SourceIDs: []string{in.SourceIDs[domain.LayerSamples]}, Algo: domain.AlgoPeaks},
			Label: "DTG peak rate", Value: p.MGperMin, Unit: domain.UnitMGperMin, Status: domain.StatusOK}
		// mass loss between extent endpoints (corrected series, no gap cross)
		chain := buildMassLossChain(in, artID, localID, corr, startS, endS, initial, initialID, p, gas)
		last := chain[len(chain)-1]
		c.MassLossMg = domain.ValueRef{Ref: domain.Ref{ID: localID + ":lossmg", ArtifactID: artID, RunID: in.RunID,
			SourceIDs: []string{in.SourceIDs[domain.LayerSamples], in.SourceIDs[domain.LayerDrift], in.SourceIDs[domain.LayerBlank]},
			Algo: domain.AlgoStages},
			Label: "stage mass loss", Value: last.Output.Value, Unit: domain.UnitMG,
			Status: map[bool]string{true: domain.StatusOK, false: domain.StatusUnknown}[last.Output.Status == domain.StatusOK]}
		// percent step output (second-to-last is mg loss, last is pct): find pct
		for i := len(chain) - 1; i >= 0; i-- {
			if chain[i].Output.Unit == domain.UnitPercent {
				c.MassLossPct = chain[i].Output
				break
			}
		}
		c.Chain = chain
		if crosses {
			c.Status = "candidate"
			c.RejectReasons = append(c.RejectReasons, "crosses_sampling_gap: extent spans a gap; interpolation forbidden")
		}
		if !startOK || !endOK {
			c.RejectReasons = append(c.RejectReasons, "endpoint_not_observed: stage endpoint lies inside a gap")
		}
		if gasUncertain {
			c.RejectReasons = append(c.RejectReasons, "atmosphere_uncertain: stage crosses a valve reaction window")
		}
		return c
	}

	for _, g := range groups {
		if len(g) == 1 {
			p := g[0]
			startS, endS := extent(dtg, p)
			local := nextLocal()
			c := mkStage(p, startS, endS, local, nil)
			out.Proposals = append(out.Proposals, StageProposal{
				GroupID: p.LocalID, Kind: "unique", Legal: len(c.RejectReasons) == 0, Candidates: []domain.Candidate{c},
			})
			if len(c.RejectReasons) > 0 {
				diags = append(diags, domain.Diagnostic{Code: "CANDIDATE_WITH_REJECTIONS", Severity: domain.SevWarning,
					Ref: c.LocalID, Message: fmt.Sprintf("stage %s retained with reject reasons: %v", c.LocalID, c.RejectReasons)})
			}
			continue
		}
		// overlap cluster: both legal readings retained
		gid := g[0].GroupID
		// alt-separate
		var sep []domain.Candidate
		var sepLocals []string
		var sepLegal = true
		base := stageCounter
		for _, p := range g {
			sepLocals = append(sepLocals, fmt.Sprintf("stage-%d", base+1))
		}
		for i, p := range g {
			startS, endS := extent(dtg, p)
			c := mkStage(p, startS, endS, sepLocals[i], without(sepLocals, sepLocals[i]))
			if len(c.RejectReasons) > 0 {
				sepLegal = false
			}
			sep = append(sep, c)
		}
		stageCounter = base + len(g)
		out.Proposals = append(out.Proposals, StageProposal{GroupID: gid, Kind: "alt-separate", Legal: sepLegal, Candidates: sep})
		// alt-merged
		first, lastP := g[0], g[len(g)-1]
		startS, _ := extent(dtg, first)
		_, endS := extent(dtg, lastP)
		mergedPeak := Peak{LocalID: gid, SegmentID: first.SegmentID,
			TimeS: g[0].TimeS, TempC: g[0].TempC, MGperMin: minRate(g)}
		merged := mkStage(mergedPeak, startS, endS, nextLocal(), nil)
		mergedLegal := len(merged.RejectReasons) == 0
		out.Proposals = append(out.Proposals, StageProposal{GroupID: gid, Kind: "alt-merged", Legal: mergedLegal, Candidates: []domain.Candidate{merged}})
		diags = append(diags, domain.Diagnostic{Code: "AMBIGUOUS_DECOMPOSITION", Severity: domain.SevInfo,
			Ref: gid,
			Message: fmt.Sprintf("cluster %s has %d legal/illegal readings; reviewers must choose; percentages are not forced to sum to 100", gid, 2)})
	}

	out.Residue = buildResidue(in, corr, initial, initialID)
	out.Diagnostics = diags
	return out
}

func without(xs []string, x string) []string {
	var out []string
	for _, v := range xs {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}

func minRate(g []Peak) float64 {
	m := g[0].MGperMin
	for _, p := range g[1:] {
		if p.MGperMin < m {
			m = p.MGperMin
		}
	}
	return m
}

func orderedGroups(peaks []Peak) [][]Peak {
	byGroup := map[string][]Peak{}
	var order []string
	var singles []Peak
	for _, p := range peaks {
		if p.GroupID == "" {
			singles = append(singles, p)
			continue
		}
		if _, ok := byGroup[p.GroupID]; !ok {
			order = append(order, p.GroupID)
		}
		byGroup[p.GroupID] = append(byGroup[p.GroupID], p)
	}
	var groups [][]Peak
	for _, s := range singles {
		groups = append(groups, []Peak{s})
	}
	for _, gid := range order {
		g := byGroup[gid]
		sort.Slice(g, func(i, j int) bool { return g[i].TimeS < g[j].TimeS })
		groups = append(groups, g)
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i][0].TimeS < groups[j][0].TimeS })
	return groups
}

func chooseInitial(recs []domain.InitialMass) (*domain.InitialMass, string) {
	if len(recs) == 0 {
		return nil, ""
	}
	// latest record (revision) wins; provenance keeps all versions
	latest := recs[0]
	for _, r := range recs[1:] {
		if r.AtSec >= latest.AtSec {
			latest = r
		}
	}
	return &latest, latest.ID
}

// extent finds the stage window around a peak: walk the DTG series away
// from the minimum until the rate returns toward zero (valley shoulders),
// stopping at segment boundaries (gaps).
func extent(dtg DTGPayload, p Peak) (float64, float64) {
	var pts []DTGPoint
	for _, seg := range dtg.Segments {
		if seg.ID == p.SegmentID {
			pts = seg.Points
		}
	}
	idx := -1
	for i, q := range pts {
		if q.TimeS == p.TimeS {
			idx = i
			break
		}
	}
	if idx < 0 {
		return p.TimeS, p.TimeS
	}
	shoulder := 0.02 * absF(p.MGperMin)
	if shoulder < 0.001 {
		shoulder = 0.001
	}
	start := pts[idx].StartS
	end := pts[idx].EndS
	for i := idx; i >= 0; i-- {
		if absF(pts[i].MGperMin) <= shoulder {
			start = pts[i].StartS
			break
		}
		start = pts[i].StartS
	}
	for i := idx; i < len(pts); i++ {
		if absF(pts[i].MGperMin) <= shoulder {
			end = pts[i].EndS
			break
		}
		end = pts[i].EndS
	}
	return start, end
}

func tempAt(corr CorrectedPayload, t float64) (float64, bool) {
	for _, seg := range corr.Segments {
		for i := 1; i < len(seg.Points); i++ {
			a, b := seg.Points[i-1], seg.Points[i]
			if t >= a.TimeS && t <= b.TimeS {
				if b.TimeS-a.TimeS > domain.GapThresholdSec {
					return 0, false
				}
				if b.TimeS == a.TimeS {
					return b.TempC, true
				}
				f := (t - a.TimeS) / (b.TimeS - a.TimeS)
				return a.TempC + f*(b.TempC-a.TempC), true
			}
		}
	}
	return 0, false
}
