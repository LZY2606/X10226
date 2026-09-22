package domain

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Analyze runs the immutable tga-v1 algorithm. It is pure: the same input
// (sources + params) always produces the same derivation ids, order and
// values. outID/outSeq are the stable derivation identity.
func Analyze(in AnalysisInput, projectID string, dseq int, eventID string, at time.Time, outID string) *Derivation {
	p := in.Params
	d := &Derivation{
		ID: outID, Seq: dseq, ProjectID: projectID,
		Algorithm: p.Algorithm, ParamFingerprint: ParamsFingerprint(p),
		ParamsHash: Fingerprint(p), Sources: cloneSources(in.Sources),
		EventID: eventID, CreatedAt: at,
	}
	if d.Algorithm == "" {
		d.Algorithm = "tga-v1"
	}

	// --- 1. initial mass (stratified; revisions replace by provenance) ---
	var m0Mg float64
	var m0SubID string
	m0OK := false
	if in.InitialMass != nil && in.InitialMass.Mass.OK {
		m0Mg = in.InitialMass.Mass.Value
		m0OK = true
		for _, s := range in.Sources {
			if s.Layer == LayerInitialMass {
				m0SubID = s.SubID
			}
		}
	}
	if !m0OK {
		d.Diags = append(d.Diags, Diag{
			Level: "error", Code: "initial_mass_missing",
			Layer:   string(LayerInitialMass),
			Message: "样品初重缺失或不可转换，质量百分比无法规范化",
		})
	}

	// --- 2. blank linear drift: fit blank; imported drift stratum overrides ---
	fit := FitBlankLinear(in.Blank)
	d.Diags = append(d.Diags, fit.diags...)
	slope, intercept := fit.slopeMgPerS, fit.interceptMg
	driftSrc := "blank_curve:ols"
	if in.Drift != nil && in.Drift.SlopeMgPerS.OK && in.Drift.InterceptMg.OK {
		slope = in.Drift.SlopeMgPerS.Value
		intercept = in.Drift.InterceptMg.Value
		driftSrc = "balance_drift:record"
	}

	// --- 3. normalise series ---
	var firstTime float64
	var prevValid *SeriesPoint
	seriesByLine := map[int]*SeriesPoint{}
	validCount := 0
	for _, rp := range in.Points {
		sp := SeriesPoint{ID: fmt.Sprintf("%s-P%04d", outID, rp.Line), Line: rp.Line}
		if !rp.Valid() {
			for _, msg := range invalidDiags(rp) {
				d.Diags = append(d.Diags, Diag{
					Level: "warning", Code: "point_unparseable",
					Layer: string(LayerRawPoints), Line: rp.Line,
					Message: msg, Raw: rp.Raw,
				})
			}
			sp.Excluded = true
			d.Series = append(d.Series, sp)
			seriesByLine[rp.Line] = &sp
			continue
		}
		if validCount == 0 {
			firstTime = rp.Time.Value
		}
		sp.TimeS = rp.Time.Value
		sp.SensorTempC = rp.Temp.Value
		// temperature sensor lag: reaction temperature is shifted.
		sp.ReactTempC = sensorToReaction(in.Furnace, rp.Time.Value, rp.Temp.Value, p.TempLagS)
		// blank/drift correction (baseline relative to first sample time).
		corr := rp.Mass.Value - (intercept + slope*(rp.Time.Value-firstTime))
		sp.MassMgCorr = corr
		if m0OK {
			sp.MassPct = corr / m0Mg * 100
		} else {
			sp.MassPct = math.NaN()
		}
		if prevValid != nil {
			gap := sp.TimeS - prevValid.TimeS
			prevValid.GapAfterS = gap
			if gap > p.GapMaxS {
				prevValid.NoAcrossGap = true
				d.Gaps = append(d.Gaps, Gap{
					FromLine: prevValid.Line, ToLine: sp.Line,
					FromS: prevValid.TimeS, ToS: sp.TimeS,
				})
			}
			if sp.MassMgCorr > prevValid.MassMgCorr+p.NoiseSigma {
				sp.NonMono = true
				d.Diags = append(d.Diags, Diag{
					Level: "warning", Code: "non_monotonic_noise",
					Layer: string(LayerRawPoints), Line: sp.Line,
					Message: fmt.Sprintf(
						"非单调噪声：校正质量上升 %.3f mg（阈值 %.3f mg），保留原值不抹平",
						sp.MassMgCorr-prevValid.MassMgCorr, p.NoiseSigma),
				})
			}
		}
		d.Series = append(d.Series, sp)
		seriesByLine[rp.Line] = &sp
		prevValid = &sp
		validCount++
	}

	// --- 4. DTG: central difference per contiguous segment only ---
	segs := segmentsByGaps(d.Series, p.GapMaxS)
	hw := p.DTGHalfWidth
	if hw < 1 {
		hw = 1
	}
	for _, seg := range segs {
		for k, i := range seg {
			lo := k - hw
			hi := k + hw
			if lo < 0 || hi >= len(seg) {
				d.Series[i].DTGPctPerMin = 0
				continue
			}
			a, b := d.Series[seg[lo]], d.Series[seg[hi]]
			dt := b.TimeS - a.TimeS
			if dt <= 0 || !m0OK {
				continue
			}
			d.Series[i].DTGPctPerMin = (b.MassPct - a.MassPct) / (dt / 60.0)
		}
	}

	// --- 5. DTG peaks within segments ---
	peaks := detectPeaks(d.Series, segs, p)

	// --- 6. ambiguity groups: split overlapping peaks into legal alternatives ---
	groups := buildAmbiguityGroups(peaks, p)

	// --- 7. gas attribution with valve delay + temperature lag ---
	valveReaction := func(v ValveEvent) (float64, float64) {
		reactionS := v.AtS.Value + p.GasDelayS
		return reactionS, sensorToReaction(in.Furnace, reactionS, 0, p.TempLagS)
	}

	for gi, g := range groups {
		for ai, alt := range g.alternatives {
			for pkInAlt, pk := range alt {
				c := buildStage(in, d.Series, pk, p, m0Mg, m0OK, gi+1, ai+1, pkInAlt+1, valveReaction, driftSrc)
				c.ID = CandidateID(projectID, dseq, gi+1, ai+1, pkInAlt+1)
				d.Stages = append(d.Stages, c)
			}
		}
	}
	sort.SliceStable(d.Stages, func(i, j int) bool {
		if d.Stages[i].Group != d.Stages[j].Group {
			return d.Stages[i].Group < d.Stages[j].Group
		}
		if d.Stages[i].Alternative != d.Stages[j].Alternative {
			return d.Stages[i].Alternative < d.Stages[j].Alternative
		}
		return d.Stages[i].OnsetS < d.Stages[j].OnsetS
	})

	// overlap flag: stages (in the same alternative) whose intervals overlap.
	markOverlaps(d.Stages, p)

	// --- 8. residues with expandable computation chains ---
	d.Residues = buildResidues(in, d.Series, segs, p, m0Mg, m0OK, m0SubID, slope, intercept, driftSrc, outID, dseq, projectID)

	// --- 9. machine-readable judgement basis ---
	d.AttributionRules = []MachineRule{
		{
			Code:      "VALVE_TIME_NE_REACTION_TIME",
			Statement: "气氛延迟与温度滞后使阀切换时间不等于样品反应时间；归因使用 reaction_s = valve_s + gas_delay_s，温度坐标使用反应温度",
			Evidence: map[string]string{
				"gas_delay_s": fmt.Sprintf("%g", p.GasDelayS),
				"temp_lag_s":  fmt.Sprintf("%g", p.TempLagS),
			},
		},
		{
			Code:      "STAGES_MAY_OVERLAP",
			Statement: "阶段可重叠且不强制各段失重之和为百分之百；重叠峰保留全部合法候选并标注 overlap",
			Evidence: map[string]string{
				"overlap_touch_s": fmt.Sprintf("%g", p.OverlapTouchS),
			},
		},
		{
			Code:      "GAPS_NO_INTERPOLATION",
			Statement: "数据缺口不跨越插值；DTG 与候选阶段均在连续段内计算，缺口两端不合并",
			Evidence: map[string]string{
				"gap_max_s": fmt.Sprintf("%g", p.GapMaxS),
				"gaps":      fmt.Sprintf("%d", len(d.Gaps)),
			},
		},
		{
			Code:      "KEEP_ALL_LEGAL_CANDIDATES",
			Statement: "无法唯一决定时保留全部合法候选；规则拒绝的候选也作为一等数据保留并给出拒绝原因",
		},
	}
	return d
}

func cloneSources(in []SourceRef) []SourceRef {
	out := make([]SourceRef, len(in))
	copy(out, in)
	return out
}

func invalidDiags(rp RawPoint) []string {
	var msgs []string
	for _, rv := range []struct {
		name string
		rv   RawValue
	}{{"time", rp.Time}, {"temp", rp.Temp}, {"mass", rp.Mass}} {
		for _, dg := range rv.rv.Diags {
			msgs = append(msgs, rv.name+": "+dg)
		}
		if rv.rv.Text != "" && !rv.rv.OK && len(rv.rv.Diags) == 0 {
			msgs = append(msgs, rv.name+": 无法转换，已留在原位置")
		}
	}
	return msgs
}

// sensorToReaction evaluates reaction-side temperature for a time instant by
// shifting along the furnace program with tempLagS. tSensor is used as the
// value reference near the requested time.
func sensorToReaction(furnace []FurnaceSegment, atS, tSensor, lagS float64) float64 {
	if lagS == 0 {
		return tSensor
	}
	target := atS - lagS
	// Linear search is fine for tiny furnace programs; result deterministic.
	for _, f := range furnace {
		if !f.StartS.OK || !f.EndS.OK || !f.StartTempC.OK || !f.EndTempC.OK {
			continue
		}
		s0, s1 := f.StartS.Value, f.EndS.Value
		if s1 < s0 {
			s0, s1 = s1, s0
		}
		if target >= s0 && target <= s1 {
			if s1 == s0 {
				return f.StartTempC.Value
			}
			frac := (target - s0) / (s1 - s0)
			return f.StartTempC.Value + frac*(f.EndTempC.Value-f.StartTempC.Value)
		}
	}
	if tSensor != 0 {
		return tSensor
	}
	return 0
}
