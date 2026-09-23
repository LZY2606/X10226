package domain

import "math"

// atmosphereStages turns valve events into reaction windows. Valve time plus the
// instrument atmosphere delay is where the atmosphere actually changes at the
// sample; furnace-temperature lag is reported so reviewers see that valve
// switching time != sample reaction time.
func (e *engine) atmosphereStages(pts []pt, out *ComputeResult) {
	gv := e.gas()
	fu := e.furnace()
	if len(gv.Events) == 0 || len(pts) < 2 {
		return
	}
	cleanRef := e.refs[NodeCleanedSeries]
	refs := append(append([]Ref{}, cleanRef...), e.sourceRefs(KindFurnaceProgram, KindGasValve)...)
	tMin, tMax := pts[0].t, pts[len(pts)-1].t

	tempAt := e.tempInterp(pts)
	for i, ev := range gv.Events {
		start := ev.TimeS + ev.DelayS
		end := tMax
		if i+1 < len(gv.Events) {
			end = gv.Events[i+1].TimeS + gv.Events[i+1].DelayS
		}
		key := "atmo:" + ev.ID
		start = math.Max(start, tMin)
		sc := StageCandidatePayload{
			Key: key, Origin: "atmosphere_window", Gas: ev.ToGas, ValveID: ev.ID,
			StartS: start, EndS: math.Min(end, tMax),
		}
		if v, ok := tempAt(sc.StartS); ok {
			sc.StartC = v
		} else {
			sc.Reasons = append(sc.Reasons, "sample_temperature_unknown_at_reaction_start:temperature_lag_or_gap")
			sc.Unknown = true
		}
		if v, ok := tempAt(sc.EndS); ok {
			sc.EndC = v
		}
		// furnace setpoint vs actual sample temperature at valve time (lag)
		if sp, ok := furnaceSetpoint(fu, ev.TimeS); ok {
			if v, ok2 := tempAt(ev.TimeS); ok2 {
				lag := sp - v
				if math.Abs(lag) > 0.001 {
					sc.Reasons = append(sc.Reasons,
						"valve_time_ne_sample_reaction_time:delay_s="+f2s(ev.DelayS)+";temp_lag_c="+f2s(lag))
				}
			}
		}
		sc.Loss = e.integrateLoss(pts, sc.StartS, sc.EndS)
		params := map[string]interface{}{"valve_id": ev.ID, "valve_time_s": ev.TimeS,
			"delay_s": ev.DelayS, "window_start_s": sc.StartS, "window_end_s": sc.EndS}
		e.emit(out, NodeAtmoStage, key, params, sc, refs...)
	}
}

// dtgStages finds decomposition candidates from smoothed DTG minima; overlapping
// peaks are kept as separate candidates with recorded overlaps (no forced split
// to percentages that must sum to 100).
func (e *engine) dtgStages(pts []pt, out *ComputeResult) {
	dn, ok := e.findNode(out, NodeDTG, "series")
	if !ok {
		return
	}
	var dp DTGPayload
	_ = jsonUnmarshal(dn.Payload, &dp)
	refs := append([]Ref{}, dn.DirectSources...)
	refs = append(refs, Ref{Kind: "node", ID: dn.ID, Role: NodeDTG})

	minV := 0.0
	for _, p := range dp.Points {
		if p.MgPerMin < minV {
			minV = p.MgPerMin
		}
	}
	if minV >= 0 {
		return
	}
	// threshold: significant loss rate beyond noise floor
	thr := -math.Max(3.0*dp.PeakNoiseStd, 0.05*math.Abs(minV))
	type peak struct {
		idx    int
		val    float64
		startS float64
		endS   float64
	}
	var peaks []peak
	n := len(dp.Points)
	i := 0
	for i < n {
		if dp.Points[i].MgPerMin < thr {
			j := i
			best := i
			for j < n && dp.Points[j].Interval == dp.Points[i].Interval && dp.Points[j].MgPerMin < thr {
				if dp.Points[j].MgPerMin < dp.Points[best].MgPerMin {
					best = j
				}
				j++
			}
			start := dp.Points[i].TimeS
			if i > 0 {
				start = (dp.Points[i-1].TimeS + dp.Points[i].TimeS) / 2
			}
			end := dp.Points[j-1].TimeS
			if j < n {
				end = (dp.Points[j-1].TimeS + dp.Points[j].TimeS) / 2
			}
			peaks = append(peaks, peak{idx: best, val: dp.Points[best].MgPerMin, startS: start, endS: end})
			i = j
		} else {
			i++
		}
	}

	tempAt := e.tempInterp(pts)
	// integrate per candidate, detect overlaps and sub-peaks
	cands := make([]StageCandidatePayload, 0, len(peaks))
	for k, pk := range peaks {
		key := stageKey(pk.idx, pk.val)
		c := StageCandidatePayload{Key: key, Origin: "dtg_peak",
			StartS: pk.startS, EndS: pk.endS, PeakS: dp.Points[pk.idx].TimeS}
		if v, ok := tempAt(c.StartS); ok {
			c.StartC = v
		} else {
			c.Unknown = true
			c.Reasons = append(c.Reasons, "start_temperature_unknown_gap_or_lag")
		}
		if v, ok := tempAt(c.EndS); ok {
			c.EndC = v
		}
		// sub-peaks inside the window indicate unresolved overlap
		for p2 := 0; p2 < n; p2++ {
			if dp.Points[p2].MgPerMin < thr && dp.Points[p2].TimeS >= c.StartS && dp.Points[p2].TimeS <= c.EndS {
				c.SubPeaks = append(c.SubPeaks, SubPeak{TimeS: dp.Points[p2].TimeS, Value: dp.Points[p2].MgPerMin})
			}
		}
		if len(c.SubPeaks) > 1 {
			c.Reasons = append(c.Reasons, "overlapping_peaks_present:not_uniquely_decomposable")
		}
		c.Loss = e.integrateLoss(pts, c.StartS, c.EndS)
		for q, o := range peaks {
			if q == k {
				continue
			}
			if o.startS < c.EndS && c.StartS < o.endS {
				c.OverlapsWith = append(c.OverlapsWith, stageKey(o.idx, o.val))
			}
		}
		cands = append(cands, c)
	}
	for _, c := range cands {
		params := map[string]interface{}{
			"peak_time_s": c.PeakS, "threshold_mg_per_min": thr,
			"noise_std": dp.PeakNoiseStd, "smoothing": SmoothingID,
		}
		e.emit(out, NodeStage, c.Key, params, c, refs...)
	}
}

// integrateLoss computes mass loss within [t0,t1]; it never sums across a
// sampling gap. If the interval is interrupted, the computable part is retained
// and the status is unknown with the reason recorded.
func (e *engine) integrateLoss(pts []pt, t0, t1 float64) StageLoss {
	im, ok := e.initMass()
	_ = ok
	// bounds within same sampling intervals only
	gapCross := false
	var parts []struct{ a, b pt }
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if a.interval != b.interval {
			if t0 < b.t && t1 > a.t {
				gapCross = true
			}
			continue
		}
		lo, hi := math.Max(t0, a.t), math.Min(t1, b.t)
		if hi <= lo {
			continue
		}
		parts = append(parts, struct{ a, b pt }{a, b})
	}
	massAt := func(t float64) (float64, bool) {
		for i := 1; i < len(pts); i++ {
			a, b := pts[i-1], pts[i]
			if a.interval != b.interval {
				continue
			}
			if t >= a.t && t <= b.t {
				if b.t == a.t {
					return a.raw, true
				}
				f := (t - a.t) / (b.t - a.t)
				return a.raw + f*(b.raw-a.raw), true
			}
		}
		return 0, false
	}
	_ = massAt
	var area float64
	first, last := math.NaN(), math.NaN()
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if a.interval != b.interval {
			continue
		}
		lo, hi := math.Max(t0, a.t), math.Min(t1, b.t)
		if hi <= lo {
			continue
		}
		mlo := interpMass(pts, lo)
		mhi := interpMass(pts, hi)
		area += mlo - mhi
		if math.IsNaN(first) {
			first = mlo
		}
		last = mhi
	}
	res := StageLoss{}
	if gapCross {
		res.Status = "unknown"
		res.ValueMg = area
		if im.MassMg > 0 {
			res.ValuePct = area / im.MassMg * 100
		}
		res.Reason = "sampling_gap_within_window:no_interpolation_across_gap;partial_value_only"
		return res
	}
	if math.IsNaN(first) || math.IsNaN(last) {
		res.Status = "unknown"
		res.Reason = "no_points_inside_window"
		return res
	}
	res.Status = "ok"
	res.ValueMg = first - last
	if im.MassMg > 0 {
		res.ValuePct = res.ValueMg / im.MassMg * 100
	}
	return res
}

func interpMass(pts []pt, t float64) float64 {
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if a.interval != b.interval {
			continue
		}
		if t >= a.t && t <= b.t {
			if b.t == a.t {
				return a.raw
			}
			f := (t - a.t) / (b.t - a.t)
			return a.raw + f*(b.raw-a.raw)
		}
	}
	if t <= pts[0].t {
		return pts[0].raw
	}
	return pts[len(pts)-1].raw
}

func (e *engine) tempInterp(pts []pt) func(float64) (float64, bool) {
	return func(t float64) (float64, bool) {
		for i := 1; i < len(pts); i++ {
			a, b := pts[i-1], pts[i]
			if a.interval != b.interval {
				continue
			}
			if t >= a.t && t <= b.t {
				if b.t == a.t {
					return a.tc, true
				}
				f := (t - a.t) / (b.t - a.t)
				return a.tc + f*(b.tc-a.tc), true
			}
		}
		return 0, false
	}
}

func furnaceSetpoint(fu FurnacePayload, t float64) (float64, bool) {
	for _, s := range fu.Segments {
		if t >= s.StartS && t <= s.EndS {
			if s.Kind == "hold" {
				return s.SetpointC, true
			}
			if s.EndS == s.StartS {
				return s.StartC, true
			}
			f := (t - s.StartS) / (s.EndS - s.StartS)
			return s.StartC + f*(s.EndC-s.StartC), true
		}
	}
	return 0, false
}

func stageKey(idx int, val float64) string {
	return "dtg:p" + itoa(idx) + ":" + shortFloat(val)
}
