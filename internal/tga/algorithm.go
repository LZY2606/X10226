package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

func normalizeParams(params Params) Params {
	def := DefaultParams()
	if params.GasDelayS == 0 {
		params.GasDelayS = def.GasDelayS
	}
	if params.ThermalLagS == 0 {
		params.ThermalLagS = def.ThermalLagS
	}
	if params.PeakThresholdPctMin == 0 {
		params.PeakThresholdPctMin = def.PeakThresholdPctMin
	}
	if params.OverlapWindowMin == 0 {
		params.OverlapWindowMin = def.OverlapWindowMin
	}
	if params.GapLimitSec == 0 {
		params.GapLimitSec = def.GapLimitSec
	}
	return params
}

func correctSamples(raw []RawSample, drift driftCurve, blank blankCurve, initial initialMass, params Params, judgments *[]Judgment) []CorrectedSample {
	if len(raw) < 2 || !initial.valid {
		return nil
	}
	start := raw[0].FurnaceTime.Value
	end := raw[len(raw)-1].FurnaceTime.Value
	out := make([]CorrectedSample, 0, len(raw))
	for index, point := range raw {
		t := point.FurnaceTime.Value
		fraction := 0.0
		if end > start {
			fraction = (t - start) / (end - start)
		}
		driftMg := (drift.startUg+(drift.endUg-drift.startUg)*fraction) / 1000
		blankMg := interpolateBlank(blank.points, t)
		observed := point.Mass.Value
		corrected := observed - driftMg
		net := corrected - blankMg
		item := CorrectedSample{Index: point.Index, TimeMin: t, TemperatureC: point.Temperature.Value, ObservedMg: observed, CorrectedMg: corrected, BlankMg: blankMg, NetMg: net, TGPercent: 100 * net / initial.massMg, SourceSample: fmt.Sprintf("sample:%d", point.Index)}
		if index == 0 {
			item.GapBefore = false
		} else {
			gap := t-raw[index-1].FurnaceTime.Value > params.GapLimitSec/60
			item.GapBefore = gap
			if gap {
				*judgments = append(*judgments, judgment("gap_no_interpolation", "warning", fmt.Sprintf("%.2f min 处的采样缺口保持断开，TG/DTG 不跨缺口插值。", t), "derived:samples:corrected"))
			}
		}
		out = append(out, item)
	}
	for index := range out {
		if index > 0 {
			dt := out[index].TimeMin - out[index-1].TimeMin
			out[index].NonMonotonic = out[index].NetMg > out[index-1].NetMg+0.000001
			if !out[index].GapBefore && dt > 0 {
				out[index].DTGPercentMin = (out[index].TGPercent - out[index-1].TGPercent) / dt
				out[index].DTGValid = true
				if out[index].NonMonotonic {
					*judgments = append(*judgments, judgment("non_monotonic_noise", "info", fmt.Sprintf("%.2f min 出现非单调质量变化；保留噪声，不平滑成单调失重。", out[index].TimeMin), "derived:samples:corrected"))
				}
			}
		}
	}
	if drift.specified {
		*judgments = append(*judgments, judgment("baseline_drift_corrected", "info", "天平基线漂移按始末偏移线性校正。", "derived:samples:corrected"))
	}
	return out
}

func interpolateBlank(points []blankPoint, t float64) float64 {
	if len(points) == 0 {
		return 0
	}
	if t <= points[0].timeMin {
		return points[0].massMg
	}
	last := points[len(points)-1]
	if t >= last.timeMin {
		return last.massMg
	}
	for i := 0; i+1 < len(points); i++ {
		a, b := points[i], points[i+1]
		if t >= a.timeMin && t <= b.timeMin && b.timeMin > a.timeMin {
			f := (t - a.timeMin) / (b.timeMin - a.timeMin)
			return a.massMg + (b.massMg-a.massMg)*f
		}
	}
	return 0
}

type peak struct{ stage int; peakIndex, startIndex, endIndex int }

func detectCandidates(samples []CorrectedSample, gas []GasInterval, furnace []FurnaceSegment, initial initialMass, params Params, judgments *[]Judgment) ([]Candidate, float64) {
	if len(samples) == 0 || !initial.valid {
		return nil, math.NaN()
	}
	peaks := findPeaks(samples, params)
	if len(peaks) == 0 {
		*judgments = append(*judgments, judgment("no_unique_stage_candidate", "warning", "没有超过阈值的 DTG 峰；阶段候选未知。"))
		return nil, samples[len(samples)-1].TGPercent
	}
	candidates := make([]Candidate, 0, len(peaks)+1)
	for ordinal, found := range peaks {
		start, peakPoint, end := samples[found.startIndex], samples[found.peakIndex], samples[found.endIndex]
		candidate := Candidate{
			ID: fmt.Sprintf("stage-%d-alt-1", ordinal+1), Stage: ordinal + 1, Alternative: 1,
			Label: labelForGas(gas, peakPoint.TimeMin, ordinal), Gas: gasAt(gas, peakPoint.TimeMin),
			StartMin: start.TimeMin, PeakMin: peakPoint.TimeMin, EndMin: end.TimeMin,
			TemperatureStartC: start.TemperatureC, TemperaturePeakC: peakPoint.TemperatureC, TemperatureEndC: end.TemperatureC,
			MassLossMg: start.NetMg - end.NetMg, MassLossPercent: start.TGPercent - end.TGPercent,
			ResidueAtEndPct: end.TGPercent,
			CalculationChain: []string{
				"derived:samples:corrected", fmt.Sprintf("sample:%d", start.Index), fmt.Sprintf("sample:%d", peakPoint.Index), fmt.Sprintf("sample:%d", end.Index),
			},
			DirectSources: []string{"derived:samples:corrected", "derived:gas:timeline", "derived:furnace:program"},
		}
		candidates = append(candidates, candidate)
		if isOverlappedPeak(samples, found, params) {
			altEnd := boundedIndex(samples, found.endIndex+2)
			alt := candidate
			alt.ID = fmt.Sprintf("stage-%d-alt-2", ordinal+1)
			alt.Alternative = 2
			alt.Unknown = true
			alt.Label = candidate.Label + "（重叠峰合法替代边界）"
			alt.EndMin = samples[altEnd].TimeMin
			alt.TemperatureEndC = samples[altEnd].TemperatureC
			alt.MassLossMg = start.NetMg - samples[altEnd].NetMg
			alt.MassLossPercent = start.TGPercent - samples[altEnd].TGPercent
			alt.ResidueAtEndPct = samples[altEnd].TGPercent
			alt.RejectedReason = ""
			alt.CalculationChain = append(append([]string{}, candidate.CalculationChain...), fmt.Sprintf("sample:%d", samples[altEnd].Index))
			candidates = append(candidates, alt)
			*judgments = append(*judgments, judgment("overlapping_peaks_retain_all_legal_candidates", "info", "DTG 峰肩未被强制拆分或合并；保留重叠阶段的多个合法候选。", "candidate:"+candidate.ID, "candidate:"+alt.ID))
		}
	}
	for i := range candidates {
		for j := i + 1; j < len(candidates); j++ {
			if intervalsOverlap(candidates[i], candidates[j]) {
				candidates[i].OverlapsWith = append(candidates[i].OverlapsWith, candidates[j].ID)
				candidates[j].OverlapsWith = append(candidates[j].OverlapsWith, candidates[i].ID)
			}
		}
	}
	*judgments = append(*judgments, judgment("stage_mass_loss_not_forced_to_100", "info", "阶段按重叠时间窗报告，失重百分比不被归一化到 100%。"))
	return candidates, samples[len(samples)-1].TGPercent
}

func findPeaks(samples []CorrectedSample, params Params) []peak {
	type marked struct{ index int }
	centers := []marked{}
	for i := 1; i+1 < len(samples); i++ {
		if samples[i].GapBefore || samples[i+1].GapBefore || !samples[i].DTGValid || !samples[i+1].DTGValid {
			continue
		}
		if samples[i].DTGPercentMin <= -params.PeakThresholdPctMin && samples[i].DTGPercentMin <= samples[i-1].DTGPercentMin && samples[i].DTGPercentMin <= samples[i+1].DTGPercentMin {
			centers = append(centers, marked{i})
		}
	}
	found := make([]peak, 0, len(centers))
	for ordinal, center := range centers {
		start := center.index - 1
		for start > 0 && samples[start].DTGValid && samples[start].DTGPercentMin < 0 && !samples[start].GapBefore {
			start--
		}
		end := center.index + 1
		for end+1 < len(samples) && samples[end].DTGValid && samples[end].DTGPercentMin < 0 && !samples[end+1].GapBefore {
			end++
		}
		if ordinal > 0 {
			prev := found[ordinal-1].endIndex
			if start <= prev {
				start = prev
			}
		}
		found = append(found, peak{stage: ordinal + 1, peakIndex: center.index, startIndex: start, endIndex: end})
	}
	return found
}

func isOverlappedPeak(samples []CorrectedSample, found peak, params Params) bool {
	for i := found.peakIndex + 1; i <= found.endIndex && i < len(samples); i++ {
		if samples[i].DTGValid && samples[i].DTGPercentMin <= -params.PeakThresholdPctMin*0.72 && math.Abs(samples[i].TimeMin-samples[found.peakIndex].TimeMin) <= params.OverlapWindowMin {
			return true
		}
}
	return false
}

func boundedIndex(samples []CorrectedSample, index int) int {
	if index < 0 { return 0 }
	if index >= len(samples) { return len(samples)-1 }
	return index
}

func gasAt(gas []GasInterval, t float64) string {
	current := "unknown"
	for _, interval := range gas {
		if t >= interval.ReactionTimeMin && (math.IsInf(interval.EndReactionTimeMin, 1) || t < interval.EndReactionTimeMin) {
			current = interval.Gas
		}
	}
	return current
}

func labelForGas(gas []GasInterval, t float64, ordinal int) string {
	name := gasAt(gas, t)
	if name == "unknown" { name = "气氛未知" }
	return fmt.Sprintf("阶段 %d（%s）", ordinal+1, name)
}

func intervalsOverlap(a, b Candidate) bool {
	return math.Max(a.StartMin, b.StartMin) < math.Min(a.EndMin, b.EndMin)
}

func parameterFingerprint(params Params, sources []string) string {
	payload, _ := json.Marshal(struct{ Params Params; Sources []string }{params, sources})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:16])
}

func sourceVersionSet(ids []string) int {
	if len(ids) == 0 { return 1 }
	values := make([]int, 0, len(ids))
	for _, id := range ids {
		var version int
		if index := strings.LastIndex(id, "-v"); index >= 0 {
			fmt.Sscanf(id[index+2:], "%d", &version)
		} else {
			fmt.Sscanf(id[idLastDash(id)+1:], "%d", &version)
		}
		if version > 0 { values = append(values, version) }
	}
	sort.Ints(values)
	if len(values) == 0 { return 1 }
	return values[len(values)-1]
}

func idLastDash(id string) int {
	for i := len(id)-1; i >= 0; i-- { if id[i] == '-' || id[i] == '/' { return i } }
	return -1
}
