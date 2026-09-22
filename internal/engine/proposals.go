package engine

import (
	"fmt"
	"math"

	"tga-review/internal/domain"
)

// NoiseFloorMgPerMin: excursions weaker than this are reported as uncertain
// rather than promoted to stages.
const NoiseFloorMgPerMin = 0.02

type excursion struct {
	start, end int
	peak       int
	minDTG     float64
	crossesGap bool
	hasValley  bool
	valley     int
}

func buildProposals(snap *Snapshot, corrID, dtgID, furnID, gasID string,
	cs *domain.CorrectedSeries, dtg *domain.DTGSeries, segs []domain.Segment,
	fp *domain.FurnaceProgram, gas *domain.GasTimeline) domain.Artifact {

	set := &domain.CandidateSet{}
	smoothed := smooth(dtg)
	init := currentInitial(snap)

	for _, ex := range findExcursions(dtg, smoothed, segs) {
		region := domain.MassLossRegion{
			ID: regionID(ex), CrossesGap: ex.crossesGap, Overlap: ex.hasValley,
		}
		region.StartS = cs.Samples[ex.start].TimeS
		region.EndS = cs.Samples[ex.end].TimeS
		region.PeakS = cs.Samples[ex.peak].TimeS
		region.PeakTempC = cs.Samples[ex.peak].TempC

		// Variant 1: single stage spanning the whole excursion.
		singleStages := []domain.Stage{makeStage(snap, cs, dtg, gas, "single", ex.start, ex.end, ex.peak, init, ex.crossesGap)}
		region.Proposals = append(region.Proposals, domain.StageProposal{
			ID: propID(region.ID, "single"), RegionID: region.ID, Variant: "single",
			Stages: singleStages, Status: domain.StatusPending,
		})

		if ex.crossesGap {
			region.Proposals[0].RejectionReasons = append(region.Proposals[0].RejectionReasons,
				domain.ReasonCrossesGap, domain.ReasonUnbounded)
			region.Proposals[0].Unknowns = append(region.Proposals[0].Unknowns,
				"data gap inside excursion: boundary mass cannot be fixed without interpolation")
		}

		// Variants 2/3 require a resolved valley and a complete local frame.
		if ex.hasValley && !ex.crossesGap {
			v := ex.valley
			a := makeStage(snap, cs, dtg, gas, "a", ex.start, v, ex.peak, init, false)
			bPeak := strongestPeak(smoothed, v, ex.end)
			b := makeStage(snap, cs, dtg, gas, "b", v, ex.end, bPeak, init, false)
			region.Proposals = append(region.Proposals, domain.StageProposal{
				ID: propID(region.ID, "split_a_b"), RegionID: region.ID, Variant: "split_a_b",
				Stages: []domain.Stage{a, b}, Status: domain.StatusPending,
			})
			// Overlapping stages are allowed: both may claim the valley region.
			ovA := makeStage(snap, cs, dtg, gas, "ov-a", ex.start, min(ex.end, v+2), ex.peak, init, false)
			ovB := makeStage(snap, cs, dtg, gas, "ov-b", max(ex.start, v-2), ex.end, bPeak, init, false)
			region.Proposals = append(region.Proposals, domain.StageProposal{
				ID: propID(region.ID, "overlap_split"), RegionID: region.ID, Variant: "overlap_split",
				Stages:   []domain.Stage{ovA, ovB},
				Status:   domain.StatusPending,
				Unknowns: []string{"overlapping peaks: stage losses intentionally not forced to sum to 100%"},
			})
		} else if !ex.crossesGap && -ex.minDTG < NoiseFloorMgPerMin {
			region.Proposals[0].RejectionReasons = append(region.Proposals[0].RejectionReasons,
				domain.ReasonBelowNoiseFloor)
			region.Proposals[0].Unknowns = append(region.Proposals[0].Unknowns,
				"non-monotonic noise comparable to loss signal")
		}

		if gasAmbiguous(cs.Samples[ex.start].TimeS, cs.Samples[ex.end].TimeS, gas) {
			for i := range region.Proposals {
				region.Proposals[i].RejectionReasons = append(region.Proposals[i].RejectionReasons,
					domain.ReasonGasAttributionAmbiguous)
			}
		}
		set.Regions = append(set.Regions, region)
	}
	return mkArtifact(nil, domain.ArtCandSet, "candidate-set",
		"All legal stage candidates with rejection reasons and unknowns",
		nil, []string{corrID, dtgID, furnID, gasID}, set)
}

func findExcursions(dtg *domain.DTGSeries, smoothed []float64, segs []domain.Segment) []excursion {
	var out []excursion
	segByIndex := map[int]int{}
	segIDs := map[int]int{}
	for si, sg := range segs {
		for i := sg.StartIndex; i <= sg.EndIndex; i++ {
			segByIndex[i] = si
			segIDs[i] = si
		}
	}
	n := len(dtg.Points)
	in := false
	var cur excursion
	flush := func(end int) {
		cur.end = end
		cur.minDTG = smoothed[cur.peak]
		cur.valley, cur.hasValley = findValley(smoothed, cur.start, cur.end)
		out = append(out, cur)
		in = false
	}
	for i := 0; i < n; i++ {
		active := smoothed[i] < -NoiseFloorMgPerMin
		gapHere := i > 0 && segIDs[i] != segIDs[i-1]
		if !active {
			if in {
				flush(i - 1)
			}
			continue
		}
		if !in {
			in = true
			cur = excursion{start: i, peak: i}
		} else {
			if smoothed[i] < smoothed[cur.peak] {
				cur.peak = i
			}
		}
		if gapHere && in {
			cur.crossesGap = true
		}
	}
	if in {
		flush(n - 1)
	}
	_ = segByIndex
	return out
}

func findValley(sm []float64, start, end int) (int, bool) {
	// A local maximum of DTG (least-negative point) strictly between two
	// minima indicates overlapping peaks.
	best, bestVal := -1, math.Inf(-1)
	for i := start + 1; i < end; i++ {
		if sm[i] > sm[i-1] && sm[i] >= sm[i+1] && sm[i] < -NoiseFloorMgPerMin {
			if sm[i] > bestVal {
				best, bestVal = i, sm[i]
			}
		}
	}
	if best < 0 {
		return -1, false
	}
	// require the two shoulders to be distinctly negative
	leftMin := sm[start]
	rightMin := sm[end]
	for i := start; i < best; i++ {
		leftMin = math.Min(leftMin, sm[i])
	}
	for i := best + 1; i <= end; i++ {
		rightMin = math.Min(rightMin, sm[i])
	}
	if bestVal-leftMin < NoiseFloorMgPerMin && bestVal-rightMin < NoiseFloorMgPerMin {
		return -1, false
	}
	return best, true
}

func strongestPeak(sm []float64, from, to int) int {
	pk := from
	for i := from; i <= to; i++ {
		if sm[i] < sm[pk] {
			pk = i
		}
	}
	return pk
}

func makeStage(snap *Snapshot, cs *domain.CorrectedSeries, dtg *domain.DTGSeries,
	gas *domain.GasTimeline, tag string, start, end, peak int, init float64, crossesGap bool) domain.Stage {
	st := domain.Stage{
		ID:     stageID(start, end, tag),
		StartS: cs.Samples[start].TimeS, EndS: cs.Samples[end].TimeS,
		PeakS: cs.Samples[peak].TimeS, PeakTempC: cs.Samples[peak].TempC,
		StartTempC: cs.Samples[start].TempC, EndTempC: cs.Samples[end].TempC,
	}
	st.LossMg = math.Max(0, cs.Samples[start].CorrectedMg-cs.Samples[end].CorrectedMg)
	if init > 0 {
		st.LossPercent = 100 * st.LossMg / init
	}
	st.CrossesGap = crossesGap
	assignGas(&st, gas)
	st.AttributionNote = "valve switch time is not sample reaction time; " +
		"reaction window shifted by atmosphere delay and temperature lag"
	return st
}

func assignGas(st *domain.Stage, gas *domain.GasTimeline) {
	at := func(t float64) (string, string) {
		cur := "initial"
		var activeEv string
		for _, e := range gas.Events {
			if t >= e.ReactionAtS {
				cur = e.ToGas
				activeEv = fmt.Sprintf("gas-%d", e.Index)
			}
		}
		return cur, activeEv
	}
	st.GasAtStart, _ = at(st.StartS)
	st.GasAtPeak, st.GasEventID = at(st.PeakS)
}

func gasAmbiguous(startS, endS float64, gas *domain.GasTimeline) bool {
	for _, e := range gas.Events {
		// valve time inside window but reaction time outside: attribution ambiguous
		valveIn := e.AtS >= startS && e.AtS <= endS
		reactionIn := e.ReactionAtS >= startS && e.ReactionAtS <= endS
		if valveIn != reactionIn {
			return true
		}
	}
	return false
}

func smooth(dtg *domain.DTGSeries) []float64 {
	n := len(dtg.Points)
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		lo, hi := i-1, i+1
		if lo < 0 {
			lo = 0
		}
		if hi > n-1 {
			hi = n - 1
		}
		out[i] = (dtg.Points[lo].DTGMgPerMin + dtg.Points[i].DTGMgPerMin + dtg.Points[hi].DTGMgPerMin) / 3
	}
	return out
}

func regionID(ex excursion) string {
	return fmt.Sprintf("region-%d-%d", ex.start, ex.end)
}
func propID(region, variant string) string {
	return fmt.Sprintf("prop-%s-%s", region, variant)
}
func stageID(start, end int, tag string) string {
	return fmt.Sprintf("stage-%s-%d-%d", tag, start, end)
}
