package domain

import (
	"encoding/json"
	"sort"
)

// Sources is the set of current source versions fed to the engine.
type Sources struct {
	Sample   *Source
	Drift    *Source
	Furnace  *Source
	Gas      *Source
	Blank    *Source
	InitMass *Source
}

// ComputeResult is the deterministic output for one set of current sources.
type ComputeResult struct {
	Nodes     []Node
	Judgments []Judgment
}

type engine struct {
	proj string
	src  Sources
	refs map[string][]Ref
}

// Compute turns current sources into derived nodes + machine-readable judgments.
// It is pure: same sources + same AlgoVersion always yield identical node IDs.
func Compute(projectID string, s Sources) ComputeResult {
	e := &engine{proj: projectID, src: s, refs: map[string][]Ref{}}
	out := ComputeResult{}
	if s.Sample == nil {
		return out
	}
	e.ref(NodeCleanedSeries, e.sourceRefs(KindSamplePoints, KindDriftCorrection, KindBlankCurve)...)

	cleaned := e.cleanedSeries(&out)
	e.dtgSeries(cleaned, &out)
	e.atmosphereStages(cleaned, &out)
	e.dtgStages(cleaned, &out)
	e.residue(cleaned, &out)
	e.standingJudgments(cleaned, &out)
	sort.SliceStable(out.Nodes, func(i, j int) bool {
		ni, nj := out.Nodes[i], out.Nodes[j]
		if ni.Kind != nj.Kind {
			return ni.Kind < nj.Kind
		}
		return ni.GroupKey < nj.GroupKey
	})
	return out
}

func (e *engine) sourceRef(k string) (Ref, bool) {
	switch k {
	case KindSamplePoints:
		if e.src.Sample != nil {
			return Ref{Kind: "source", ID: e.src.Sample.ID, Role: k}, true
		}
	case KindDriftCorrection:
		if e.src.Drift != nil {
			return Ref{Kind: "source", ID: e.src.Drift.ID, Role: k}, true
		}
	case KindFurnaceProgram:
		if e.src.Furnace != nil {
			return Ref{Kind: "source", ID: e.src.Furnace.ID, Role: k}, true
		}
	case KindGasValve:
		if e.src.Gas != nil {
			return Ref{Kind: "source", ID: e.src.Gas.ID, Role: k}, true
		}
	case KindBlankCurve:
		if e.src.Blank != nil {
			return Ref{Kind: "source", ID: e.src.Blank.ID, Role: k}, true
		}
	case KindInitialMass:
		if e.src.InitMass != nil {
			return Ref{Kind: "source", ID: e.src.InitMass.ID, Role: k}, true
		}
	}
	return Ref{}, false
}

func (e *engine) sourceRefs(ks ...string) []Ref {
	var rs []Ref
	for _, k := range ks {
		if r, ok := e.sourceRef(k); ok {
			rs = append(rs, r)
		}
	}
	return rs
}

func (e *engine) ref(kind string, rs ...Ref) { e.refs[kind] = rs }

func (e *engine) emit(out *ComputeResult, kind, group string, params interface{}, payload interface{}, refs ...Ref) Node {
	pb, _ := json.Marshal(payload)
	id := StableID(e.proj, kind, group, refs, params, pb)
	n := Node{
		ID:                id,
		ProjectID:         e.proj,
		Kind:              kind,
		GroupKey:          group,
		AlgoVersion:       AlgoVersion,
		ParamsFingerprint: fingerprint(e.proj, kind, group, refs, params),
		DirectSources:     refs,
		Payload:           pb,
	}
	out.Nodes = append(out.Nodes, n)
	return n
}

func (e *engine) sample() SamplePointsPayload {
	var p SamplePointsPayload
	if e.src.Sample != nil {
		_ = jsonUnmarshal(e.src.Sample.Payload, &p)
	}
	return p
}

func (e *engine) drift() DriftPayload {
	var p DriftPayload
	if e.src.Drift != nil {
		_ = json.Unmarshal(e.src.Drift.Payload, &p)
	}
	return p
}

func (e *engine) furnace() FurnacePayload {
	var p FurnacePayload
	if e.src.Furnace != nil {
		_ = json.Unmarshal(e.src.Furnace.Payload, &p)
	}
	return p
}

func (e *engine) gas() GasValvePayload {
	var p GasValvePayload
	if e.src.Gas != nil {
		_ = json.Unmarshal(e.src.Gas.Payload, &p)
	}
	return p
}

func (e *engine) blank() BlankCurvePayload {
	var p BlankCurvePayload
	if e.src.Blank != nil {
		_ = json.Unmarshal(e.src.Blank.Payload, &p)
	}
	return p
}

func (e *engine) initMass() (InitialMassPayload, bool) {
	if e.src.InitMass == nil {
		return InitialMassPayload{}, false
	}
	var p InitialMassPayload
	_ = json.Unmarshal(e.src.InitMass.Payload, &p)
	return p, true
}

type pt struct {
	idx        int
	t, tc, raw float64
	interval   int
}

func (e *engine) cleanedSeries(out *ComputeResult) []pt {
	sp := e.sample()
	dr := e.drift()
	bl := e.blank()

	type rd struct {
		t, m float64
		ok   bool
	}
	var rows []rd
	dropped := []int{}
	diags := []string{}
	for _, rp := range sp.Points {
		if !rp.TimeS.Valid() || !rp.MassMg.Valid() {
			dropped = append(dropped, rp.Index)
			if rp.MassMg.Diagnostic != "" || rp.TimeS.Diagnostic != "" {
				diags = append(diags, rp.TimeS.Diagnostic, rp.MassMg.Diagnostic)
			}
			continue
		}
		rows = append(rows, rd{t: *rp.TimeS.Value, m: *rp.MassMg.Value, ok: rp.TempC.Valid()})
		if !rp.TempC.Valid() && rp.TempC.Diagnostic != "" {
			diags = append(diags, rp.TempC.Diagnostic)
		}
	}
	// gaps from median interval
	gaps := []GapInfo{}
	if len(rows) > 2 {
		dts := make([]float64, 0, len(rows)-1)
		for i := 1; i < len(rows); i++ {
			dts = append(dts, rows[i].t-rows[i-1].t)
		}
		sorted := append([]float64{}, dts...)
		sort.Float64s(sorted)
		med := sorted[len(sorted)/2]
		for i := 1; i < len(rows); i++ {
			if med > 0 && rows[i].t-rows[i-1].t > GapFactor*med {
				gaps = append(gaps, GapInfo{
					AfterIndex: sp.Points[i-1].Index,
					BeforeT:    rows[i-1].t,
					AfterT:     rows[i].t,
					WidthS:     rows[i].t - rows[i-1].t,
				})
			}
		}
	}
	gapAfter := map[int]bool{}
	for _, g := range gaps {
		gapAfter[g.AfterIndex] = true
	}

	// temperature lookup (original temperature stratum, no interpolation across gaps)
	tempAt := func(t float64) (float64, bool) {
		for i := 1; i < len(sp.Points); i++ {
			a, b := sp.Points[i-1], sp.Points[i]
			if !a.TempC.Valid() || !b.TempC.Valid() || !a.TimeS.Valid() || !b.TimeS.Valid() {
				continue
			}
			if gapAfter[a.Index] {
				continue
			}
			if t >= *a.TimeS.Value && t <= *b.TimeS.Value {
				if *b.TimeS.Value == *a.TimeS.Value {
					return *a.TempC.Value, true
				}
				f := (t - *a.TimeS.Value) / (*b.TimeS.Value - *a.TimeS.Value)
				return *a.TempC.Value + f*(*b.TempC.Value-*a.TempC.Value), true
			}
		}
		return 0, false
	}

	blankAt := func(t float64) (float64, bool) {
		if len(bl.Points) < 2 {
			return 0, false
		}
		for i := 1; i < len(bl.Points); i++ {
			a, b := bl.Points[i-1], bl.Points[i]
			if t >= a.TimeS && t <= b.TimeS {
				if b.TimeS == a.TimeS {
					return a.MassMg, true
				}
				f := (t - a.TimeS) / (b.TimeS - a.TimeS)
				return a.MassMg + f*(b.MassMg-a.MassMg), true
			}
		}
		return 0, false
	}
	t0 := 0.0
	if len(rows) > 0 {
		t0 = rows[0].t
	}

	cps := make([]CleanPoint, 0, len(rows))
	pts := make([]pt, 0, len(rows))
	interval := 0
	nonmono := []int{}
	for i, r := range rows {
		if i > 0 && gapAfter[sp.Points[i-1].Index] {
			interval++
		}
		corr := r.m
		switch dr.Method {
		case "linear":
			corr = r.m - dr.RateMgPerMin*((r.t-t0)/60.0)
		case "blank_linked":
			if bm, ok := blankAt(r.t); ok {
				corr = r.m - bm
			}
		}
		tc := 0.0
		if sp.Points[i].TempC.Valid() {
			tc = *sp.Points[i].TempC.Value
		} else if v, ok := tempAt(r.t); ok {
			tc = v
		}
		cp := CleanPoint{Index: sp.Points[i].Index, TimeS: r.t, TempC: tc,
			MassRaw: r.m, MassCor: corr, Interval: interval}
		cps = append(cps, cp)
		pts = append(pts, pt{idx: cp.Index, t: r.t, tc: tc, raw: corr, interval: interval})
		if i > 0 && interval == pts[i-1].interval && corr > pts[i-1].raw {
			nonmono = append(nonmono, cp.Index)
		}
	}
	payload := CleanedSeriesPayload{
		TimeBasis:      sp.CoordinateBasis,
		Points:         cps,
		Gaps:           gaps,
		NonMonotonic:   nonmono,
		DriftMethod:    dr.Method,
		DroppedRawRows: dropped,
	}
	if len(diags) > 0 {
		payload.Diagnostics = diags
	}
	params := map[string]interface{}{
		"drift_method": dr.Method, "rate_mg_per_min": dr.RateMgPerMin,
		"gap_factor": GapFactor, "blank_linked": len(bl.Points) > 0,
	}
	e.emit(out, NodeCleanedSeries, "series", params, payload, e.refs[NodeCleanedSeries]...)
	return pts
}
