package core

import (
	"math"
	"sort"
	"strconv"
)

const AlgorithmVersion = "tga-stage-v1.3.0"

func DefaultParameters() Parameters {
	return Parameters{AlgorithmVersion: AlgorithmVersion, SmoothingWindowSeconds: 30, DTGThresholdMgPerMin: 0.012, OverlapShoulderRatio: 0.55, MinimumStageSeconds: 40, Fingerprint: CanonicalFingerprint("30", "0.012", "0.55", "40", AlgorithmVersion)}
}

func BuildSeries(state State) ([]PlotPoint, []ValveView) {
	drift := latestDrift(state.Drifts)
	points := make([]PlotPoint, 0, len(state.Samples))
	prevTime := math.NaN()
	segment := 0
	for i, sample := range state.Samples {
		gap := false
		nonMono := false
		if !sample.Time.OK || !sample.Temperature.OK || !sample.Mass.OK {
			points = append(points, PlotPoint{Seq: i + 1, Segment: segment, Unplottable: true})
			continue
		}
		if math.IsNaN(prevTime) {
			segment = 1
		} else if sample.Time.Value <= prevTime || sample.Time.Value-prevTime > 45 {
			gap = true
			segment++
		}
		corrected := sample.Mass.Value
		if drift.Rate.OK {
			corrected -= drift.Rate.Value * sample.Time.Value / 60
		}
		if len(points) > 0 && !points[len(points)-1].Unplottable && corrected > points[len(points)-1].CorrMassMg+0.03 {
			nonMono = true
		}
		dtg := 0.0
		if len(points) > 0 && !gap && !points[len(points)-1].Unplottable {
			last := points[len(points)-1]
			if sample.Time.Value > last.TimeS {
				dtg = (corrected - last.CorrMassMg) / (sample.Time.Value - last.TimeS) * 60
			}
		}
		points = append(points, PlotPoint{Seq: i + 1, TimeS: sample.Time.Value, TempC: sample.Temperature.Value, RawMassMg: sample.Mass.Value, CorrMassMg: corrected, DTGMgPerMin: dtg, Segment: segment, GapBefore: gap, NonMono: nonMono})
		prevTime = sample.Time.Value
	}
	valves := make([]ValveView, 0, len(state.Valves))
	for _, valve := range state.Valves {
		if !valve.Time.OK || !valve.ReactionStart.OK {
			continue
		}
		valves = append(valves, ValveView{ID: valve.EventID, TimeS: valve.Time.Value, ReactionTimeS: valve.ReactionStart.Value, FromGas: valve.FromGas, ToGas: valve.ToGas, DelayS: delayOrZero(valve.Delay), LagS: delayOrZero(valve.ThermalLag)})
	}
	return points, valves
}

func DetectCandidates(state State, version int) []StageCandidate {
	first := firstValid(state.Plot)
	last := lastValid(state.Plot)
	if first == nil || last == nil {
		return nil
	}
	init := latestInitialMass(state.InitialMasses)
	initMass := 0.0
	initSource := "first-valid-sample"
	if init.Mass.OK {
		initMass = init.Mass.Value
		initSource = init.SourceID + "/" + init.RevisionID
	} else {
		initMass = first.CorrMassMg
	}
	if initMass == 0 {
		return nil
	}
	var candidates []StageCandidate
	for segmentID, points := range splitSegments(state.Plot) {
		for _, peak := range localPeaks(points, state.Parameters) {
			candidates = append(candidates, makeCandidate(state, points, peak, segmentID+1, initMass, initSource, version))
		}
	}
	candidates = append(candidates, finalResidue(state, *first, *last, initMass, initSource, version))
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Start.Value == candidates[j].Start.Value {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Start.Value < candidates[j].Start.Value
	})
	return candidates
}

type localPeakInfo struct{ index, strength float64 }

func localPeaks(points []PlotPoint, params Parameters) []localPeakInfo {
	if len(points) < 5 {
		return nil
	}
	var out []localPeakInfo
	for i := 2; i < len(points)-2; i++ {
		strength := -points[i].DTGMgPerMin
		if strength < params.DTGThresholdMgPerMin {
			continue
		}
		neighbor := math.Max(-points[i-1].DTGMgPerMin, -points[i+1].DTGMgPerMin)
		if strength >= neighbor {
			out = append(out, localPeakInfo{index: float64(i), strength: strength})
		}
	}
	merged := []localPeakInfo{}
	for _, peak := range out {
		idx := int(peak.index)
		if len(merged) == 0 || points[idx].TimeS-points[int(merged[len(merged)-1].index)].TimeS > 25 {
			merged = append(merged, peak)
		} else if peak.strength > merged[len(merged)-1].strength {
			merged[len(merged)-1] = peak
		}
	}
	return merged
}

func makeCandidate(state State, points []PlotPoint, peakInfo localPeakInfo, segmentID, initMass intOrFloat... no) {
}

func makeCandidate(state State, points []PlotPoint, peakInfo localPeakInfo, segmentID int, initMass float64, initSource string, version int) StageCandidate {
	peak := int(peakInfo.index)
	left, right := peak, peak
	threshold := peakInfo.strength * state.Parameters.OverlapShoulderRatio
	for left > 0 && -points[left-1].DTGMgPerMin > threshold { left-- }
	for right < len(points)-1 && -points[right+1].DTGMgPerMin > threshold { right++ }
	start, end := points[left], points[right]
	loss := start.CorrMassMg - end.CorrMassMg
	id := PrefixedID("stage", state.ProjectID, itoa(segmentID), fmtFloat(start.TimeS), fmtFloat(end.TimeS), itoa(peak))
	provenance := provenanceFor(state, version, initSource, "plot:segment="+itoa(segmentID))
	return StageCandidate{ID:id, RevisionID:PrefixedID("candidate-revision", id, itoa(version)), Kind:"overlapping_stage", Status:"candidate", Title:"DTG stage near "+fmtFloat(points[peak].TempC)+" degC", Start:qf(start.TimeS,"s","sample_reaction_time"), End:qf(end.TimeS,"s","sample_reaction_time"), Peak:qf(points[peak].TempC,"degC","sample_temperature_at_dtg_peak"), MassLoss:qf(loss,"mg","drift_corrected_mass_difference"), MassLossPct:qpct(loss/initMass*100), ResiduePct:qpct(end.CorrMassMg/initMass*100), ResidueMass:qf(end.CorrMassMg,"mg","drift_corrected_mass"), Overlaps:overlappingAtmospheres(state,start.TimeS,end.TimeS), Provenance:provenance, Current:true, Chain:stageChain(start,end,points[peak],loss,initMass)}
}

func finalResidue(state State, first, last PlotPoint, initMass float64, initSource string, version int) StageCandidate {
	id := PrefixedID("final-residue", state.ProjectID, "current-initial-mass")
	residue := last.CorrMassMg / initMass * 100
	totalLoss := first.CorrMassMg - last.CorrMassMg
	return StageCandidate{ID:id, RevisionID:PrefixedID("candidate-revision",id,itoa(version)), Kind:"final_residue", Status:"current", Title:"Final residue after last valid segment", Start:qf(first.TimeS,"s","first_valid_sample_time"), End:qf(last.TimeS,"s","last_valid_sample_time"), Peak:qf(last.TempC,"degC","last_valid_sample_temperature"), MassLoss:qf(totalLoss,"mg","total_drift_corrected_change"), MassLossPct:qpct(totalLoss/initMass*100), ResiduePct:qpct(residue), ResidueMass:qf(last.CorrMassMg,"mg","last_drift_corrected_mass"), Provenance:provenanceFor(state,version,initSource,"plot:last-valid"), Current:true, Chain:residueChain(id,first,last,initMass,residue)}
}

func stageChain(start,end,peak PlotPoint,loss,initMass float64) []CalculationStep {
	id := StableID(fmtFloat(start.TimeS),fmtFloat(end.TimeS))
	return []CalculationStep{{ID:"calc_"+id+"_1",Op:"stage_bounds",Detail:"Boundaries use DTG shoulder threshold and sample reaction time, not valve time",Value:fmtFloat(start.TimeS)+".."+fmtFloat(end.TimeS),Unit:"s"},{ID:"calc_"+id+"_2",Op:"mass_loss",Detail:"Start corrected mass minus end corrected mass",Value:fmtFloat(loss),Unit:"mg"},{ID:"calc_"+id+"_3",Op:"mass_loss_percent",Detail:"Independent percent relative to current initial mass; overlap means percentages are not normalized",Value:fmtFloat(loss/initMass*100),Unit:"%"},{ID:"calc_"+id+"_4",Op:"dtg_peak_temperature",Detail:"Local minimum of mass derivative",Value:fmtFloat(peak.TempC),Unit:"degC"}}
}

func residueChain(id string, first,last PlotPoint,initMass,residue float64) []CalculationStep {
	return []CalculationStep{{ID:id+":chain:1",Op:"initial_mass",Detail:"Current initial mass layer",Value:fmtFloat(initMass),Unit:"mg"},{ID:id+":chain:2",Op:"last_corrected_mass",Detail:"Last point after linear balance drift; gaps are not bridged",Value:fmtFloat(last.CorrMassMg),Unit:"mg"},{ID:id+":chain:3",Op:"residue_percent",Detail:"last_corrected_mass / initial_mass * 100; stages are not forced to sum to 100",Value:fmtFloat(residue),Unit:"%"}}
}

func overlappingAtmospheres(state State,start,end float64) []string {
	var out []string
	for _, valve := range state.Valves {
		if valve.ReactionStart.OK && valve.ReactionStart.Value >= start && valve.ReactionStart.Value <= end {
			out = append(out,"atmosphere:"+valve.EventID+":"+valve.ToGas+"@reaction:"+fmtFloat(valve.ReactionStart.Value)+"s")
		}
	}
	return out
}

func splitSegments(points []PlotPoint) [][]PlotPoint {
	var segments [][]PlotPoint
	var current []PlotPoint
	for _, point := range points {
		if point.Unplottable { continue }
		if point.GapBefore && len(current) > 0 {
			segments = append(segments,current)
			current = nil
		}
		current = append(current,point)
	}
	if len(current)>0 { segments=append(segments,current) }
	return segments
}
func firstValid(points []PlotPoint) *PlotPoint { for i := range points { if !points[i].Unplottable { return &points[i] } }; return nil }
func lastValid(points []PlotPoint) *PlotPoint { for i:=len(points)-1;i>=0;i-- { if !points[i].Unplottable { return &points[i] } }; return nil }
func latestDrift(values []DriftCorrection) DriftCorrection { if len(values)==0 { return DriftCorrection{} }; return values[len(values)-1] }
func latestInitialMass(values []InitialMass) InitialMass { if len(values)==0 { return InitialMass{} }; return values[len(values)-1] }
func delayOrZero(q Quantity) float64 { if q.OK { return q.Value }; return 0 }
func qf(value float64,unit,coordinate string) Quantity { return Quantity{Value:value,Unit:unit,Coordinate:coordinate,OK:true,Original:fmtFloat(value)} }
func qpct(value float64) Quantity { return qf(value,"%","percent_of_current_initial_mass") }
func provenanceFor(state State, version int, initSource string, extra ...string) Provenance {
	direct := []string{"samples:current","drift:"+latestDrift(state.Drifts).CorrectionID,"initial_mass:"+initSource,"furnace:current","valves:current"}
	return Provenance{AlgorithmVersion:state.Parameters.AlgorithmVersion,ParameterSource:state.Parameters.SourceID,ParameterHash:state.Parameters.Fingerprint,DirectSources:append(direct,extra...),InputVersion:version}
}
func fmtFloat(v float64) string { return strconv.FormatFloat(math.Round(v*1000)/1000,'f',3,64) }
func itoa(v int) string { return strconv.Itoa(v) }
