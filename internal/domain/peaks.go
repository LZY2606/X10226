package domain

import (
	"fmt"
	"math"
	"sort"
)

type dtgPeak struct {
	idx          int
	timeS        float64
	valPctPerMin float64
	prominence   float64
}

// detectPeaks finds significant DTG minima (mass loss => negative DTG) inside
// each contiguous (gap-separated) segment.
func detectPeaks(series []SeriesPoint, segs [][]int, p Params) []dtgPeak {
	var peaks []dtgPeak
	for _, seg := range segs {
		if len(seg) < 3 {
			continue
		}
		for k := 1; k < len(seg)-1; k++ {
			i := seg[k]
			v := series[i].DTGPctPerMin
			if v >= -p.PeakPromPct {
				continue
			}
			if v > series[seg[k-1]].DTGPctPerMin || v > series[seg[k+1]].DTGPctPerMin {
				continue
			}
			// prominence relative to local shoulders within the segment.
			left := v
			for j := k - 1; j >= 0 && j >= k-12; j-- {
				if series[seg[j]].DTGPctPerMin < left {
					left = series[seg[j]].DTGPctPerMin
				}
			}
			right := v
			for j := k + 1; j < len(seg) && j <= k+12; j++ {
				if series[seg[j]].DTGPctPerMin < right {
					right = series[seg[j]].DTGPctPerMin
				}
			}
			shoulder := math.Max(left, right)
			prom := math.Abs(shoulder - v)
			if prom < p.PeakPromPct {
				continue
			}
			peaks = append(peaks, dtgPeak{
				idx: i, timeS: series[i].TimeS,
				valPctPerMin: v, prominence: prom,
			})
		}
	}
	sort.SliceStable(peaks, func(i, j int) bool { return peaks[i].timeS < peaks[j].timeS })
	// merge duplicate detections closer than MinStageS.
	var merged []dtgPeak
	for _, pk := range peaks {
		if len(merged) > 0 && pk.timeS-merged[len(merged)-1].timeS < p.MinStageS {
			if pk.valPctPerMin < merged[len(merged)-1].valPctPerMin {
				merged[len(merged)-1] = pk
			}
			continue
		}
		merged = append(merged, pk)
	}
	return merged
}

type ambiguityGroup struct {
	peaks        []dtgPeak
	alternatives [][]dtgPeak // each alternative is a full set of stage peaks
}

// buildAmbiguityGroups clusters peaks that are close enough to plausibly be
// overlapping peaks vs. one shoulder. For a cluster of n peaks it emits
// alternatives: [merged single stage] plus [each peak as its own stage],
// which keeps every legal decomposition instead of silently choosing one.
func buildAmbiguityGroups(peaks []dtgPeak, p Params) []ambiguityGroup {
	var groups []ambiguityGroup
	for i := 0; i < len(peaks); {
		j := i + 1
		for j < len(peaks) && peaks[j].timeS-peaks[j-1].timeS <= 4*p.MinStageS {
			j++
		}
		cluster := peaks[i:j]
		if len(cluster) == 1 {
			groups = append(groups, ambiguityGroup{
				peaks:        cluster,
				alternatives: [][]dtgPeak{{cluster[0]}},
			})
		} else {
			g := ambiguityGroup{peaks: cluster}
			// alternative 1: merged envelope (deepest peak as stage peak)
			deepest := cluster[0]
			for _, pk := range cluster[1:] {
				if pk.valPctPerMin < deepest.valPctPerMin {
					deepest = pk
				}
			}
			g.alternatives = append(g.alternatives, []dtgPeak{deepest})
			// alternative 2: resolve into separate stages
			sep := append([]dtgPeak(nil), cluster...)
			g.alternatives = append(g.alternatives, sep)
			groups = append(groups)
		}
		i = j
	}
	return groups
}

func buildStage(in AnalysisInput, series []SeriesPoint, pk dtgPeak, p Params,
	m0Mg float64, m0OK bool, group, alt, posInAlt int,
	valveReaction func(ValveEvent) (float64, float64), driftSrc string) StageCandidate {

	// onset/end: walk out from the peak until DTG recovers toward zero,
	// stopping at segment boundaries (never cross a gap).
	onsetIdx, endIdx := pk.idx, pk.idx
	for onsetIdx > 0 {
		prev := onsetIdx - 1
		if series[onsetIdx].NoAcrossGap || series[prev].Excluded {
			break
		}
		if series[prev].DTGPctPerMin <= -0.25*p.PeakPromPct {
			onsetIdx = prev
			continue
		}
		break
	}
	for endIdx < len(series)-1 {
		next := endIdx + 1
		if series[endIdx].NoAcrossGap || series[next].Excluded {
			break
		}
		if series[next].DTGPctPerMin <= -0.25*p.PeakPromPct {
			endIdx = next
			continue
		}
		break
	}
	o, e := series[onsetIdx], series[endIdx]
	lossPct := 0.0
	lossMg := 0.0
	if m0OK {
		lossPct = o.MassPct - e.MassPct
		lossMg = o.MassMgCorr - e.MassMgCorr
	}
	c := StageCandidate{
		Key:              stageKey(group, alt, posInAlt),
		Group:            group,
		Alternative:      alt,
		Label:            stageLabel(group, alt, posInAlt),
		OnsetS:           o.TimeS,
		PeakS:            pk.timeS,
		EndS:             e.TimeS,
		OnsetTempC:       o.ReactTempC,
		PeakTempC:        series[pk.idx].ReactTempC,
		EndTempC:         e.ReactTempC,
		MassLossPct:      lossPct,
		MassLossMg:       lossMg,
		DTGPeakPctPerMin: pk.valPctPerMin,
		DependsOn:        []Layer{LayerRawPoints, LayerBlankCurve, LayerInitialMass, LayerParams},
		Basis: []ChainStep{
			{Label: "起点质量", Value: fmt.Sprintf("%.3f mg / %.2f %% @ %.0f s", o.MassMgCorr, o.MassPct, o.TimeS),
				SourceKind: "derived", Ref: o.ID, Formula: "mass_corr = mass_raw - (b0 + b1*(t-t0))"},
			{Label: "终点质量", Value: fmt.Sprintf("%.3f mg / %.2f %% @ %.0f s", e.MassMgCorr, e.MassPct, e.TimeS),
				SourceKind: "derived", Ref: e.ID},
			{Label: "失重", Value: fmt.Sprintf("%.3f mg = %.2f %%", lossMg, lossPct),
				SourceKind: "derived", Ref: "", Formula: "m_onset - m_end"},
			{Label: "DTG 峰", Value: fmt.Sprintf("%.4f %%/min @ %.1f °C", pk.valPctPerMin, series[pk.idx].ReactTempC),
				SourceKind: "derived", Ref: series[pk.idx].ID, Formula: "central difference within gap-separated segment"},
		},
		Status: "pending_review",
	}
	if e.TimeS-o.TimeS < p.MinStageS {
		c.RuleRejected = true
		c.RejectReason = "STAGE_TOO_SHORT"
	}
	if lossPct <= 0 && m0OK {
		c.RuleRejected = true
		c.RejectReason = "NO_NET_MASS_LOSS"
	}

	// gas attribution: which valve switch, after delay, brackets the peak
	type vr struct {
		valveS, reactS float64
		gas            string
	}
	var events []vr
	for _, v := range in.Valves {
		if !v.AtS.OK {
			continue
		}
		rs, _ := valveReaction(v)
		events = append(events, vr{v.AtS.Value, rs, v.ToGas})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].reactS < events[j].reactS })
	gas, attr, valveS, reactS := "", "unknown", 0.0, 0.0
	for k, ev := range events {
		var next float64 = math.Inf(1)
		if k+1 < len(events) {
			next = events[k+1].reactS
		}
		if pk.timeS >= ev.reactS && pk.timeS < next {
			gas, attr, valveS, reactS = ev.gas, "delayed", ev.valveS, ev.reactS
			if math.Abs(ev.reactS-ev.valveS) < 1e-9 {
				attr = "unique"
			}
			break
		}
	}
	if gas == "" {
		attr = "unknown"
		c.RuleRejected = true
		c.RejectReason = "NO_GAS_WINDOW_FOR_PEAK"
	}
	c.Gas, c.GasAttribution, c.ValveS, c.ReactionS = gas, attr, valveS, reactS
	if len(in.Valves) > 0 {
		c.DependsOn = append(c.DependsOn, LayerGasValve)
	}
	c.Basis = append(c.Basis, ChainStep{
		Label:      "气氛归因",
		Value:      fmt.Sprintf("%s（阀 %.0f s -> 反应 %.0f s，归因 %s）", gas, valveS, reactS, attr),
		SourceKind: "submission", Ref: "",
		Formula: "reaction_s = valve_s + gas_delay_s",
	})
	return c
}

func markOverlaps(stages []StageCandidate, p Params) {
	for i := range stages {
		for j := i + 1; j < len(stages); j++ {
			a, b := stages[i], stages[j]
			if a.Alternative != b.Alternative || a.Group != b.Group {
				// overlap can also occur across groups within same alternative set
			}
			lo := math.Max(a.OnsetS, b.OnsetS)
			hi := math.Min(a.EndS, b.EndS)
			if hi-lo > -p.OverlapTouchS {
				if a.Group == b.Group && a.Alternative == b.Alternative {
					stages[i].Overlap = true
					stages[j].Overlap = true
				}
				if a.Group != b.Group {
					stages[i].Overlap = true
					stages[j].Overlap = true
				}
			}
		}
	}
}

func stageKey(group, alt, pos int) string {
	return fmt.Sprintf("G%d-A%d-P%d", group, alt, pos)
}

func stageLabel(group, alt, pos int) string {
	return fmt.Sprintf("阶段候选 G%d-A%d-P%d", group, alt, pos)
}
