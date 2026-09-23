package domain

import "math"

// dtgSeries builds raw + smoothed DTG per sampling interval and estimates a
// non-monotonic noise floor from high-frequency residual variation.
func (e *engine) dtgSeries(pts []pt, out *ComputeResult) {
	if len(pts) < 2 {
		return
	}
	initMg := 0.0
	if e.src.InitMass != nil {
		var ip InitialMassPayload
		_ = jsonUnmarshal(e.src.InitMass.Payload, &ip)
		initMg = ip.MassMg
	}

	type dp struct {
		idx      int
		t, v     float64
		interval int
	}
	raw := []dp{}
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if a.interval != b.interval {
			continue // never differentiate across a sampling gap
		}
		dt := (b.t - a.t) / 60.0
		if dt <= 0 {
			continue
		}
		raw = append(raw, dp{idx: b.idx, t: (a.t + b.t) / 2, v: (b.raw - a.raw) / dt, interval: a.interval})
	}
	// centered moving average inside each interval
	smoothAt := func(k int) (float64, bool) {
		var sum float64
		n := 0
		for j := k - SmoothingK; j <= k+SmoothingK; j++ {
			if j >= 0 && j < len(raw) && raw[j].interval == raw[k].interval {
				sum += raw[j].v
				n++
			}
		}
		if n == 0 {
			return 0, false
		}
		return sum / float64(n), true
	}
	// noise std of raw-minus-smoothed
	var sq, mean float64
	nr := 0
	for k := range raw {
		s, ok := smoothAt(k)
		if !ok {
			continue
		}
		mean += raw[k].v - s
		sq += (raw[k].v - s) * (raw[k].v - s)
		nr++
	}
	noise := 0.0
	if nr > 1 {
		mean /= float64(nr)
		noise = math.Sqrt(math.Max(0, sq/float64(nr)-mean*mean))
	}

	dps := make([]DTGPoint, 0, len(raw))
	for k, r := range raw {
		s, ok := smoothAt(k)
		if !ok {
			s = r.v
		}
		p := DTGPoint{Index: r.idx, TimeS: r.t, MgPerMin: s, Interval: r.interval}
		if initMg > 0 {
			p.PctPerMin = s / initMg * 100
		}
		dps = append(dps, p)
	}

	// gaps carried over from cleaned series
	var gaps []GapInfo
	if n, ok := e.findNode(out, NodeCleanedSeries, "series"); ok {
		var cp CleanedSeriesPayload
		_ = jsonUnmarshal(n.Payload, &cp)
		gaps = cp.Gaps
	}
	payload := DTGPayload{Points: dps, Gaps: gaps, Smoothing: SmoothingID, PeakNoiseStd: noise}
	params := map[string]interface{}{"smoothing": SmoothingID, "k": SmoothingK, "source": NodeCleanedSeries}
	refs := e.refs[NodeCleanedSeries]
	refs = append(append([]Ref{}, refs...), e.sourceRefs(KindInitialMass)...)
	e.emit(out, NodeDTG, "series", params, payload, refs...)
}

func (e *engine) findNode(out *ComputeResult, kind, group string) (Node, bool) {
	for _, n := range out.Nodes {
		if n.Kind == kind && n.GroupKey == group {
			return n, true
		}
	}
	return Node{}, false
}
