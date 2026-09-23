package compute

import (
	"math"
	"sort"

	"tga-archive/internal/domain"
)

// Row is one accepted, typed raw row presented to the engine.
type Row struct {
	ID      string
	Stratum string
	LineNo  int
	Order   int
	Fields  map[string]domain.Value
}

// Run executes algorithm alg1 over the accepted rows.
func Run(rows []Row, p domain.Params) Result {
	e := &engine{p: p, paramFP: p.Fingerprint(), refs: map[string]bool{}}
	for _, r := range rows {
		e.refs[r.ID] = true
		switch r.Stratum {
		case "sample":
			e.samples = append(e.samples, r)
		case "drift":
			e.drift = append(e.drift, r)
		case "blank":
			e.blank = append(e.blank, r)
		case "program":
			e.program = append(e.program, r)
		case "gas":
			e.gas = append(e.gas, r)
		case "mass0":
			e.mass0 = append(e.mass0, r)
		}
	}
	e.build()
	res := Result{
		AlgoVersion: AlgoVersion,
		ParamFP:     e.paramFP,
		Curve:       e.curve,
		Gaps:        e.gaps,
		DTG:         e.dtg,
		Peaks:       e.peaks,
		GasSpans:    e.spans,
		Program:     e.progPts,
		Stages:      e.stages,
		Residues:    e.residues,
		Judgments:   e.judge,
		Diags:       e.diags,
	}
	for id := range e.refs {
		res.RawRefs = append(res.RawRefs, id)
	}
	sort.Strings(res.RawRefs)
	return res
}

type engine struct {
	p       domain.Params
	paramFP string

	samples, drift, blank, program, gas, mass0 []Row

	curve   []CurvePoint
	gaps    []Gap
	dtg     []DTGPoint
	peaks   []Peak
	spans   []GasSpan
	progPts []ProgPoint
	stages  []StageCandidate
	residues []Residue
	judge   []Judgment
	diags   []string
	refs    map[string]bool
}

type spt struct {
	row Row
	t   float64
	m   float64
	tOK bool
}

func (e *engine) build() {
	// ---- samples: sort by time (numeric, accepted) ----
	var pts []spt
	for _, r := range e.samples {
		tv, mv := r.Fields["t"], r.Fields["m"]
		sp := spt{row: r}
		if tv.Bad == "" {
			sp.t, sp.tOK = tv.Num, true
		} else {
			e.diags = append(e.diags, "sample row kept out of plot, bad t: "+r.ID)
		}
		if mv.Bad == "" {
			sp.m = mv.Num
		} else {
			e.diags = append(e.diags, "sample row kept out of plot, bad m: "+r.ID)
		}
		if sp.tOK && mv.Bad == "" {
			pts = append(pts, sp)
		}
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].t < pts[j].t })

	// ---- drift model ----
	var slope, intercept float64
	driftOK := false
	if e.p.DriftModel == "linear" && len(e.drift) >= 2 {
		var xs, ys []float64
		for _, r := range e.drift {
			tv, dv := r.Fields["t"], r.Fields["dm"]
			if tv.Bad == "" && dv.Bad == "" {
				xs = append(xs, tv.Num)
				ys = append(ys, dv.Num)
			}
		}
		if len(xs) >= 2 {
			slope, intercept = linfit(xs, ys)
			driftOK = true
		}
	}
	if e.p.DriftModel == "linear" && !driftOK {
		e.judge = append(e.judge, Judgment{
			Code: "drift_unavailable", Rule: "drift:linear-requires>=2points",
			Message: "linear balance drift correction requested but fewer than 2 valid drift points; correction = 0 (rule node kept)",
		})
	}
	driftRule := e.ruleNode("drift-linear", "linear balance drift a*t+b", slope, intercept, driftOK)

	// ---- blank curve (interpolate only *within* the blank's own span) ----
	var bT, bM []float64
	blankRefs := []Ref{}
	for _, r := range e.blank {
		tv, mv := r.Fields["t"], r.Fields["mblank"]
		if tv.Bad == "" && mv.Bad == "" {
			bT, bM = append(bT, tv.Num), append(bM, mv.Num)
			blankRefs = append(blankRefs, Ref{Kind: "raw", ID: r.ID, Role: "blank"})
		}
	}
	blankRule := e.node("rule", "blank-correction", "blank subtraction "+
		"only where sample t is within blank span", 0, "", "ok", nil)

	// ---- gaps + segments ----
	segments := [][]spt{}
	if len(pts) > 0 {
		cur := []spt{pts[0]}
		for i := 1; i < len(pts); i++ {
			dt := pts[i].t - pts[i-1].t
			if dt >= e.p.GapMinS {
				g := Gap{
					TBefore: pts[i-1].t, TAfter: pts[i].t,
					MBefore: pts[i-1].m, MAfter: pts[i].m, SpanS: dt,
					CrossDelta: pts[i].m - pts[i-1].m,
				}
				g.Node = e.node("gap", "sampling gap", "no interpolation across gap",
					0, UnitTime, "ok",
					[]Ref{{Kind: "raw", ID: pts[i-1].row.ID, Role: "before"},
						{Kind: "raw", ID: pts[i].row.ID, Role: "after"}})
				e.gaps = append(e.gaps, g)
				e.judge = append(e.judge, Judgment{
					Code: "gap_no_interpolation", Rule: "gap:never-interpolate",
					Message: "sampling gap of " + f(dt) + " s is not bridged by interpolation; mass change across it stays unallocated",
					Refs:    []Ref{{Kind: "node", ID: g.Node.ID}},
					Detail:  map[string]float64{"span_s": dt, "cross_delta_mg": g.CrossDelta},
				})
				segments = append(segments, cur)
				cur = []spt{pts[i]}
			} else {
				cur = append(cur, pts[i])
			}
		}
		segments = append(segments, cur)
	}

	// ---- corrected curve, noise flags, sample-temperature lag ----
	noiseCount := 0
	for si, seg := range segments {
		for _, sp := range seg {
			t, m := sp.t, sp.m
			driftCorr := 0.0
			if driftOK {
				driftCorr = slope*t + intercept
			}
			bCorr, bOK := 0.0, false
			if e.p.BlankApply && len(bT) >= 2 && t >= bT[0] && t <= bT[len(bT)-1] {
				bCorr = interp(t, bT, bM)
				bOK = true
			}
			corrStatus := "ok"
			if e.p.BlankApply && len(bT) >= 2 && !bOK {
				corrStatus = "blank_outside_span"
			}
			if e.p.BlankApply && len(bT) < 2 {
				corrStatus = "blank_missing"
			}
			mCorr := m - driftCorr - bCorr

			// noise: non-monotonic uptick larger than tolerance
			noisy := false
			if len(e.curve) > 0 {
				prev := e.curve[len(e.curve)-1]
				if !prev.InGap && mCorr > prev.MCorr+noiseTol {
					noisy = true
				}
			}
			if noisy {
				noiseCount++
			}

			// sample temperature: programmed temperature seen at t+lag
			sampleTemp, tempStatus := e.sampleTempAt(t)

			srcs := []Ref{{Kind: "raw", ID: sp.row.ID, Role: "sample"},
				{Kind: "node", ID: driftRule.ID, Role: "drift"}}
			if bOK {
				srcs = append(srcs, blankRefs...)
			} else {
				srcs = append(srcs, Ref{Kind: "node", ID: blankRule.ID, Role: "blank"})
			}
			cp := CurvePoint{
				T: t, TRawUnit: fieldUnit(sp.row, "t"),
				SampleTemp: sampleTemp, TempStatus: tempStatus,
				MRaw: m, MCorr: mCorr, CorrStatus: corrStatus,
				Segment: si, Noise: noisy, RawID: sp.row.ID,
			}
			cp.Node = e.node("curve", "TG point @"+f(t)+"s",
				"m_corr = m_raw - drift(t) - blank(t)", mCorr, UnitMass,
				mapStatus(corrStatus, noisy), srcs)
			e.curve = append(e.curve, cp)
		}
	}
	if noiseCount > 0 {
		e.judge = append(e.judge, Judgment{
			Code: "nonmonotonic_noise", Rule: "noise:flag-dont-force-monotonic",
			Message: "non-monotonic upward blips detected; points are flagged noisy, no forced smoothing of mass",
			Detail:  map[string]float64{"count": float64(noiseCount)},
		})
	}
	if e.p.BlankApply && len(bT) < 2 {
		e.judge = append(e.judge, Judgment{
			Code: "blank_missing", Rule: "blank:optional-not-extrapolated",
			Message: "blank subtraction enabled but blank curve absent; raw mass shown with blank_missing status, no extrapolation",
		})
	}

	// ---- furnace program points ----
	for _, r := range e.program {
		tv, T := r.Fields["t"], r.Fields["Tprog"]
		if tv.Bad != "" || T.Bad != "" {
			continue
		}
		pp := ProgPoint{TProg: tv.Num, SampleT: tv.Num + e.p.TempLagS, Temp: T.Num, RawID: r.ID}
		pp.Node = e.node("program", "program @"+f(tv.Num)+"s",
			"sample sees programmed T after temp lag", T.Num, UnitTemp, "ok",
			[]Ref{{Kind: "raw", ID: r.ID, Role: "program"},
				{Kind: "param", ID: "param:temp_lag_s", Role: "lag"}})
		e.progPts = append(e.progPts, pp)
	}
	sort.Slice(e.progPts, func(i, j int) bool { return e.progPts[i].TProg < e.progPts[j].TProg })

	// ---- gas valve events -> sample-effective atmosphere ----
	e.buildGas()

	// ---- DTG within segments, then peaks + stages ----
	e.buildDTGStages()

	// ---- residue ----
	e.buildResidue()
}

const noiseTol = 0.02 // mg; below this an uptick is instrument jitter

func mapStatus(corr string, noisy bool) string {
	if noisy {
		return "noisy"
	}
	if corr != "ok" {
		return corr
	}
	return "ok"
}

func fieldUnit(r Row, f string) string {
	if v, ok := r.Fields[f]; ok {
		return v.Unit
	}
	return ""
}

func (e *engine) ruleNode(name, label string, slope, intercept float64, ok bool) Node {
	status := "ok"
	if !ok {
		status = "unavailable"
	}
	n := e.node("rule", label, "drift(t) = a*t + b; correction=0 when unavailable",
		slope, UnitMass+"/"+UnitTime, status,
		[]Ref{{Kind: "param", ID: "param:drift_model", Role: "model"}})
	_ = intercept
	return n
}

// linfit ordinary least squares y = a*x + b
func linfit(xs, ys []float64) (a, b float64) {
	var sx, sy, sxx, sxy float64
	n := float64(len(xs))
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
		sxx += xs[i] * xs[i]
		sxy += xs[i] * ys[i]
	}
	d := n*sxx - sx*sx
	if math.Abs(d) < 1e-12 {
		return 0, ys[0]
	}
	a = (n*sxy - sx*sy) / d
	b = (sy - a*sx) / n
	return a, b
}

func interp(x float64, xs, ys []float64) float64 {
	i := sort.Search(len(xs)-1, func(i int) bool { return xs[i+1] >= x })
	if i >= len(xs)-1 {
		return ys[len(ys)-1]
	}
	if xs[i+1] == xs[i] {
		return ys[i]
	}
	w := (x - xs[i]) / (xs[i+1] - xs[i])
	return ys[i] + w*(ys[i+1]-ys[i])
}

func f(x float64) string {
	// short deterministic formatting
	s := strconvF(x)
	return s
}
