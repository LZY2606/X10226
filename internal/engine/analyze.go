package engine

import (
	"math"
	"sort"

	"tgareview/internal/domain"
	"tgareview/internal/ids"
)

// Inputs binds each layer kind to its parsed layer and the revision id that
// supplied it (for provenance edges).
type Inputs struct {
	ProjectID string
	Params    Params
	Layers    map[domain.LayerKind]*domain.Layer
	Revisions map[domain.LayerKind]string
}

// Analyze runs the deterministic decomposition. Missing optional layers yield
// diagnostics, never a crash; the engine never edits raw layers.
func Analyze(in Inputs) *Artifact {
	if in.Revisions == nil {
		in.Revisions = map[domain.LayerKind]string{}
	}
	p := in.Params
	basis := Basis{}
	for _, k := range domain.AllLayers() {
		if rev := in.Revisions[k]; rev != "" {
			basis[string(k)] = rev
		}
	}
	basisHash := ids.Hash(joinBasis(basis))
	artID := ids.Of("art", in.ProjectID, AlgorithmVersion+"|"+p.Fingerprint()+"|"+basisHash)

	a := &Artifact{
		ID:                artID,
		AlgorithmVersion:  AlgorithmVersion,
		ParamsFingerprint: p.Fingerprint(),
		Params:            p,
		Basis:             basis,
	}

	sampleLayer := in.Layers[domain.SamplePoints]
	if sampleLayer == nil {
		a.Diagnostics = append(a.Diagnostics, "sample_points layer absent: analysis empty")
		a.Residue = &ResidueChain{ID: ids.Of("res", artID, "final"), Status: "unknown",
			Nodes: []ChainNode{{ID: ids.Of("node", artID, "nosamples"), Label: "sample points", Status: "unknown", Diagnostics: []string{"no sample_points layer in basis"}}}}
		a.addStandardRules()
		return a
	}

	// ---- initial mass ----
	var m0 *float64
	m0NodeSrc := ""
	if im := in.Layers[domain.InitialMass]; im != nil && im.InitialMass != nil {
		m0NodeSrc = in.Revisions[domain.InitialMass]
		if im.InitialMass.MassMg.OK() {
			v := *im.InitialMass.MassMg.Value
			m0 = &v
		} else {
			a.Diagnostics = append(a.Diagnostics, "initial mass unconvertible; percentages unavailable")
		}
	} else {
		a.Diagnostics = append(a.Diagnostics, "initial_mass layer absent; percentages unavailable")
	}

	// ---- drift model (derived layer only) ----
	driftRate := p.BaselineDriftRateMgPerMin
	driftRef := 0.0
	if dc := in.Layers[domain.DriftLayer]; dc != nil && dc.Drift != nil {
		if driftRate < 0 && dc.Drift.RateMgPerMin.OK() {
			driftRate = *dc.Drift.RateMgPerMin.Value
		}
		if dc.Drift.ReferenceTimeS.OK() {
			driftRef = *dc.Drift.ReferenceTimeS.Value
		}
	}
	if driftRate < 0 {
		driftRate = 0
		a.Diagnostics = append(a.Diagnostics, "no usable balance drift rate; drift correction set to zero")
	}

	// ---- build point series ----
	blankLayer := in.Layers[domain.BlankCurve]
	for _, s := range sampleLayer.Samples {
		pt := Point{Index: s.Index}
		ok := s.TimeS.OK() && s.TempC.OK() && s.MassMg.OK()
		if !ok {
			pt.MissingInput = true
			if s.TimeS.OK() {
				pt.TimeS = *s.TimeS.Value
			}
			a.Points = append(a.Points, pt)
			continue
		}
		pt.TimeS = *s.TimeS.Value
		pt.TFurnaceC = *s.TempC.Value
		pt.MRawMg = *s.MassMg.Value
		pt.DriftMg = driftRate * (pt.TimeS - driftRef) / 60.0
		pt.BlankMg = blankAt(blankLayer, pt.TimeS, a)
		pt.MCorrMg = pt.MRawMg - pt.DriftMg - pt.BlankMg
		if m0 != nil && *m0 != 0 {
			pt.Pct = 100.0 * pt.MCorrMg / *m0
		}
		a.Points = append(a.Points, pt)
	}
	markNonMonotonic(a.Points, p.NoiseBlipRelTol)

	// ---- intervals and gaps ----
	buildIntervals(a, p)

	// ---- sample temperature: first-order lag, re-initialized after gaps ----
	estimateSampleTemp(a, p)

	// ---- DTG, smoothed only inside an interval (never across a gap) ----
	computeDTG(a, p)

	// ---- peak groups and stage candidates ----
	findStages(a, p)

	// ---- atmosphere attribution (valve switch != reaction time) ----
	attributeAtmosphere(a, in)

	// ---- residue chain ----
	buildResidue(a, in, m0, m0NodeSrc, driftRate, driftRef)

	a.addStandardRules()
	return a
}

func joinBasis(b Basis) string {
	keys := make([]string, 0, len(b))
	for k := range b {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for _, k := range keys {
		out += k + "=" + b[k] + ";"
	}
	return out
}

func blankAt(bl *domain.Layer, t float64, a *Artifact) float64 {
	if bl == nil {
		return 0
	}
	type bp struct{ tt, mm float64 }
	pts := make([]bp, 0, len(bl.Blank))
	for _, q := range bl.Blank {
		if q.TimeS.OK() && q.MassMg.OK() {
			pts = append(pts, bp{*q.TimeS.Value, *q.MassMg.Value})
		}
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].tt < pts[j].tt })
	if len(pts) == 0 || t < pts[0].tt || t > pts[len(pts)-1].tt {
		a.Diagnostics = append(a.Diagnostics, "blank curve does not bracket a sample time; blank contribution 0 (no extrapolation)")
		return 0
	}
	for i := 0; i+1 < len(pts); i++ {
		if t >= pts[i].tt && t <= pts[i+1].tt {
			if pts[i+1].tt == pts[i].tt {
				return pts[i].mm
			}
			f := (t - pts[i].tt) / (pts[i+1].tt - pts[i].tt)
			return pts[i].mm + f*(pts[i+1].mm-pts[i].mm)
		}
	}
	return 0
}

// markNonMonotonic flags small local upward excursions ("blips") inside the
// overall loss: a valid point that is a strict local maximum relative to its
// valid neighbours, with an excursion within relTol, is retained and flagged
// as suspected acquisition noise rather than being deleted or smoothed away.
func markNonMonotonic(pts []Point, relTol float64) {
	valid := make([]int, 0, len(pts))
	for i := range pts {
		if !pts[i].MissingInput {
			valid = append(valid, i)
		}
	}
	for k, i := range valid {
		if k == 0 || k == len(valid)-1 {
			continue
		}
		cur, prev, next := pts[i].MCorrMg, pts[valid[k-1]].MCorrMg, pts[valid[k+1]].MCorrMg
		if cur > prev && cur > next {
			up := math.Min(cur-prev, cur-next)
			scale := math.Abs(prev)
			if scale == 0 {
				scale = 1
			}
			if up/scale <= relTol {
				pts[i].NonMonotonic = true
			}
		}
	}
}

func buildIntervals(a *Artifact, p Params) {
	ivIdx := 0
	start := -1
	var prev Point
	prevValid := false
	flush := func(endIdx int, endT float64) {
		id := ids.Of("iv", a.ID, itoa(ivIdx))
		iv := Interval{ID: id, FromIndex: start, ToIndex: endIdx, StartS: a.Points[start].TimeS, EndS: endT}
		a.Intervals = append(a.Intervals, iv)
		for k := start; k <= endIdx; k++ {
			a.Points[k].IntervalID = id
		}
		ivIdx++
	}
	for i := range a.Points {
		pt := a.Points[i]
		if pt.MissingInput {
			if start >= 0 {
				flush(prev.Index, prev.TimeS)
				start = -1
			}
			prevValid = false
			continue
		}
		if start < 0 {
			start = i
		} else if pt.TimeS-prev.TimeS > p.GapMaxS {
			a.Gaps = append(a.Gaps, Gap{AfterIndex: prev.Index, T1S: prev.TimeS, T2S: pt.TimeS, DtS: pt.TimeS - prev.TimeS})
			flush(prev.Index, prev.TimeS)
			start = i
		}
		prev = pt
		prevValid = true
	}
	if start >= 0 && prevValid {
		flush(prev.Index, prev.TimeS)
	}
}

// estimateSampleTemp applies T_sample = low-pass(T_furnace); it is restarted at
// the first point of each interval rather than interpolated across a gap.
func estimateSampleTemp(a *Artifact, p Params) {
	for _, iv := range a.Intervals {
		first := true
		prevT := 0.0
		for i := iv.FromIndex; i <= iv.ToIndex; i++ {
			pt := &a.Points[i]
			if first {
				pt.TSampleC = pt.TFurnaceC
				pt.TEstimated = true // lag re-initialized after boundary/gap
				prevT = pt.TimeS
				first = false
				continue
			}
			dt := pt.TimeS - prevT
			alpha := 1.0 - math.Exp(-dt/math.Max(p.ThermalLagTauS, 1e-9))
			prevS := a.Points[i-1].TSampleC
			pt.TSampleC = prevS + alpha*(pt.TFurnaceC-prevS)
			prevT = pt.TimeS
		}
	}
}
