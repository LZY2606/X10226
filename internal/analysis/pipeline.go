package analysis

import (
	"math"
	"sort"

	"tga-archive/internal/model"
)

// Sources are the currently effective layers feeding one derivation.
type Sources struct {
	Samples *model.Layer
	Blank   *model.Layer
	Furnace *model.Layer
	Gases   *model.Layer
	Drift   *model.Layer
	Initial *model.Layer
}

func (s Sources) refs() []layerRef {
	var out []layerRef
	add := func(l *model.Layer) {
		if l != nil {
			out = append(out, layerRef{Kind: string(l.Kind), ID: l.ID, Version: l.Version, Hash: LayerContentHash(l)})
		}
	}
	add(s.Samples)
	add(s.Blank)
	add(s.Furnace)
	add(s.Gases)
	add(s.Drift)
	add(s.Initial)
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

type workPoint struct {
	t          float64
	tk         float64
	tempValid  bool
	rawMg      float64
	massOk     bool
	corrMg     float64
	pct        float64
	dtg        *float64
	suspect    bool
	seg        int
	rawT       string
	rawTemp    string
	rawMass    string
	dtgReason  string
}

// Compute runs the tga-stage-v1 pipeline. The result is deterministic in
// (sources, params): node/candidate/residue identities reproduce on replay and
// after delete + re-import.
func Compute(src Sources, p model.Params) *model.Derived {
	pf := ParamFingerprint(p)
	stateHash := StateHash(pf, src.refs())
	d := &model.Derived{StateHash: stateHash, Algo: p.AlgoVersion, ParamFP: pf, ByID: map[string]*model.Node{}}

	pts := buildPoints(src)
	applyBlank(pts, src)
	applyDrift(pts, src, d)
	m0, m0Status := resolveM0(pts, src, d)
	toPercent(pts, m0, m0Status)
	effectiveTemp(pts, p)
	gaps := segmentGaps(pts, p, d)
	d.Gaps = gaps
	markNoise(pts, p, d)
	smoothAndDTG(pts, p)
	peaks := findPeaks(pts, p)
	cands := buildCandidates(pts, peaks, src, p, stateHash, d)
	d.Candidates = cands
	d.Residues = buildResidues(pts, src, p, stateHash, d)
	d.Series = makeSeries(pts)
	d.SeriesID = nid(stateHash, "series")
	d.DTGID = nid(stateHash, "dtg")
	d.Root = buildChain(pts, src, p, stateHash, d, m0, m0Status)
	indexNodes(&d.Root, d.ByID)
	return d
}

func buildPoints(src Sources) []workPoint {
	var pts []workPoint
	if src.Samples == nil || src.Samples.Samples == nil {
		return pts
	}
	for _, q := range src.Samples.Samples.Points {
		pts = append(pts, workPoint{
			t: q.TSec, tk: q.TK, tempValid: q.TOk && q.TempOk,
			rawMg: q.MMg, massOk: q.MassOk,
			rawT: q.RawT, rawTemp: q.RawTemp, rawMass: q.RawMass,
			seg: -1,
		})
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].t < pts[j].t })
	return pts
}

// interpBlank returns the blank mass at time t by piecewise-linear
// interpolation across the blank curve (blank itself is complete).
func interpBlank(src Sources, t float64) (float64, bool) {
	if src.Blank == nil || src.Blank.Blank == nil || len(src.Blank.Blank.Points) == 0 {
		return 0, false
	}
	bp := src.Blank.Blank.Points
	if t <= bp[0].TSec {
		return bp[0].MMg, true
	}
	last := bp[len(bp)-1]
	if t >= last.TSec {
		return last.MMg, true
	}
	i := sort.Search(len(bp), func(i int) bool { return bp[i].TSec >= t })
	a, b := bp[i-1], bp[i]
	if b.TSec == a.TSec {
		return a.MMg, true
	}
	f := (t - a.TSec) / (b.TSec - a.TSec)
	return a.MMg + f*(b.MMg-a.MMg), true
}

func applyBlank(pts []workPoint, src Sources) {
	if src.Blank == nil {
		return
	}
	for i := range pts {
		if !pts[i].massOk {
			continue
		}
		if bm, ok := interpBlank(src, pts[i].t); ok {
			pts[i].corrMg = pts[i].rawMg - bm
		}
	}
}

func applyDrift(pts []workPoint, src Sources, d *model.Derived) {
	if src.Drift == nil || src.Drift.Drift == nil {
		for i := range pts {
			if pts[i].massOk && src.Blank != nil {
				pts[i].corrMg = pts[i].corrMg
			} else if pts[i].massOk {
				pts[i].corrMg = pts[i].rawMg
			}
		}
		return
	}
	a := src.Drift.Drift.AMgPerS
	b := src.Drift.Drift.BMg
	for i := range pts {
		if !pts[i].massOk {
			continue
		}
		base := pts[i].rawMg
		if src.Blank != nil {
			if bm, ok := interpBlank(src, pts[i].t); ok {
				base -= bm
			}
		}
		pts[i].corrMg = base - (a*pts[i].t + b)
	}
	d.Findings = append(d.Findings, model.Finding{
		Code: "balance_drift_corrected", Severity: model.SevWarning,
		Message: "linear balance baseline drift a*t+b subtracted",
		Refs:    []string{src.Drift.ID},
	})
}

func resolveM0(pts []workPoint, src Sources, d *model.Derived) (float64, model.Status) {
	if src.Initial != nil && src.Initial.Initial != nil {
		m0 := src.Initial.Initial.M0Mg
		if src.Blank != nil {
			if bm, ok := interpBlank(src, 0); ok {
				m0 -= bm
			}
		}
		return m0, model.StatusOK
	}
	// fall back to earliest valid corrected point, marked unknown
	var earliest *workPoint
	for i := range pts {
		if pts[i].massOk {
			earliest = &pts[i]
			break
		}
	}
	if earliest == nil {
		d.Findings = append(d.Findings, model.Finding{Code: "no_initial_mass", Severity: model.SevError, Message: "no initial mass layer and no valid sample mass"})
		return 0, model.StatusError
	}
	d.Findings = append(d.Findings, model.Finding{Code: "initial_mass_inferred", Severity: model.SevWarning, Message: "m0 inferred from first valid point; residue/percent are provisional", Refs: []string{src.Samples.ID}})
	return earliest.corrMg, model.StatusUnknown
}

func toPercent(pts []workPoint, m0 float64, m0Status model.Status) {
	if m0 == 0 {
		return
	}
	for i := range pts {
		if pts[i].massOk {
			pts[i].pct = 100 * pts[i].corrMg / m0
		}
	}
}

// effectiveTemp applies the temperature lag: sample- experienced temperature
// is furnace temperature minus lag. This is one of the reasons the valve switch
// time is not the reaction time.
func effectiveTemp(pts []workPoint, p model.Params) {
	if p.TempLagK == 0 {
		return
	}
	for i := range pts {
		if pts[i].tempValid {
			pts[i].tk -= p.TempLagK
		}
	}
}

func validF(pts []workPoint) []int {
	var idx []int
	for i := range pts {
		if pts[i].massOk {
			idx = append(idx, i)
		}
	}
	return idx
}

func qround(v float64) float64 { return math.Round(v*1e6) / 1e6 }
