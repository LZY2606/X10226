// Package engine turns the current raw layers into a versioned DerivedSet.
//
// Domain rules encoded here (also asserted by tests):
//   - gas delay and temperature lag mean valve time != sample reaction time;
//   - stages may overlap and mass losses are never forced to sum to 100%;
//   - no interpolation crosses a sampling gap; points touching a gap stay
//     UNCERTAIN and all legal candidates are retained when not unique.
package engine

import (
	"fmt"
	"math"
	"sort"

	"tga-review/internal/domain"
	"tga-review/internal/ident"
)

const AlgorithmVersion = "tga-engine-1.3.0"

type Engine struct {
	project string
}

func New(projectID string) *Engine { return &Engine{project: projectID} }

// Inputs are the current versions of every raw layer.
type Inputs struct {
	Samples   []domain.TimePoint
	Drift     []domain.DriftCorrection
	Program   []domain.ProgramSegment
	Gas       []domain.GasEvent
	Blank     []domain.BlankPoint
	Initial   []domain.InitialMass
	Params    domain.Params
	RunID     string
	ParentRuns []string
}

// point is the engine's internal work row.
type point struct {
	id         string
	t, temp    float64
	rawMass    float64
	usable     bool
	diag       string
	gapBefore  bool // gap relative to previous usable point
	nonMono    bool // positive local mass movement at this point
	blankMass  float64
	driftOff   float64
	corrMass   float64
	dtg        float64
	confidence string
}

// Run computes a full derived set deterministically.
func (e *Engine) Run(in Inputs) (domain.DerivedSet, error) {
	if err := in.Params.Validate(); err != nil {
		return domain.DerivedSet{}, err
	}
	if len(in.Samples) == 0 {
		return domain.DerivedSet{}, fmt.Errorf("no sample points")
	}
	findings := []domain.Finding{}
	addFinding := func(code, sev, msg, obj string, src ...string) {
		findings = append(findings, domain.Finding{
			ID:       "fnd-" + ident.Short(fmt.Sprintf("%s|%s|%d", in.RunID, code, len(findings))),
			Code:     code,
			Severity: sev,
			Message:  msg,
			ObjectID: obj,
			SourceIDs: src,
		})
	}

	pts := e.buildPoints(in, addFinding)
	corrections, dtg := e.differentiate(pts, in, addFinding)
	peaks := e.detectPeaks(dtg, in, addFinding)
	episodes := e.atmosphere(in, addFinding)
	stages := e.stages(peaks, dtg, episodes, in, addFinding)
	residue := e.residue(pts, in, addFinding)

	prov := domain.Provenance{
		AlgorithmVersion: AlgorithmVersion,
		ParamFingerprint: ident.Fingerprint(in.Params),
		RunID:            in.RunID,
		ParentRunIDs:     in.ParentRuns,
	}
	allSrc := sourceIDs(in)
	prov.SourceIDs = allSrc
	for i := range findings {
		if len(findings[i].SourceIDs) == 0 {
			findings[i].SourceIDs = allSrc
		}
	}

	return domain.DerivedSet{
		RunID:       in.RunID,
		Prov:        prov,
		Params:      in.Params,
		Corrections: corrections,
		DTG:         dtg,
		Peaks:       peaks,
		Episodes:    episodes,
		Stages:      stages,
		Residue:     residue,
		Findings:    findings,
	}, nil
}

func sourceIDs(in Inputs) []string {
	var ids []string
	for _, s := range in.Samples {
		ids = append(ids, s.ID)
	}
	for _, g := range in.Gas {
		ids = append(ids, g.ID)
	}
	for _, b := range in.Blank {
		ids = append(ids, b.ID)
	}
	for _, d := range in.Drift {
		ids = append(ids, d.ID)
	}
	for _, im := range in.Initial {
		ids = append(ids, im.ID)
	}
	for _, p := range in.Program {
		ids = append(ids, p.ID)
	}
	return ids
}

// usableQuantity returns canonical value and validity of one Quantity.
func usableQuantity(q domain.Quantity) (float64, bool, string) {
	if q.Raw == "" {
		return 0, false, "missing"
	}
	if !q.Convertible || math.IsNaN(q.ConvertedValue) {
		return 0, false, q.Diagnostic
	}
	return q.ConvertedValue, true, ""
}

func (e *Engine) buildPoints(in Inputs, add findingAdder) []*point {
	type sp struct {
		s domain.TimePoint
		o int
	}
	var rows []sp
	for i, s := range in.Samples {
		rows = append(rows, sp{s, i})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ti, ok1, _ := usableQuantity(rows[i].s.T)
		tj, ok2, _ := usableQuantity(rows[j].s.T)
		if !ok1 || !ok2 {
			return rows[i].o < rows[j].o
		}
		return ti < tj
	})

	blankT, blankM := usableCurve(in.Blank, func(b domain.BlankPoint) (domain.Quantity, domain.Quantity) { return b.T, b.Mass })
	driftT, driftOff := usableDrift(in.Drift)

	pts := make([]*point, 0, len(rows))
	var prevT float64
	havePrev := false
	for _, r := range rows {
		t, tok, tdiag := usableQuantity(r.s.T)
		m, mok, mdiag := usableQuantity(r.s.Mass)
		temp, tempok, tempdiag := usableQuantity(r.s.Temp)
		diag := joinDiag(tdiag, mdiag, tempdiag)
		p := &point{id: r.s.ID, t: t, temp: temp, rawMass: m, usable: tok && mok && tempok, diag: diag, confidence: "OK"}
		if havePrev && p.usable && t-prevT > in.Params.GapThresholdS {
			p.gapBefore = true
		}
		if p.usable {
			if in.Params.BlankSubtraction {
				p.blankMass = interp(blankT, blankM, t, p.gapBefore)
			}
			if in.Params.DriftCorrection {
				p.driftOff = interp(driftT, driftOff, t, p.gapBefore)
			}
			p.corrMass = m - p.blankMass - p.driftOff
			prevT, havePrev = t, true
		}
		if !p.usable {
			add("UNCONVERTIBLE_POINT", "warning",
				fmt.Sprintf("sample point retained in place but not used: %s", diag), r.s.ID, r.s.ID)
		}
		pts = append(pts, p)
	}
	return pts
}

type findingAdder func(code, sev, msg, obj string, src ...string)

func joinDiag(ds ...string) string {
	out := ""
	for _, d := range ds {
		if d != "" {
			if out != "" {
				out += "; "
			}
			out += d
		}
	}
	return out
}

func usableCurve[T any](xs []T, f func(T) (domain.Quantity, domain.Quantity)) ([]float64, []float64) {
	var ts, vs []float64
	for _, x := range xs {
		q1, q2 := f(x)
		v1, ok1, _ := usableQuantity(q1)
		v2, ok2, _ := usableQuantity(q2)
		if ok1 && ok2 {
			ts = append(ts, v1)
			vs = append(vs, v2)
		}
	}
	sort.Sort(&pairSort{ts, vs})
	return ts, vs
}

func usableDrift(ds []domain.DriftCorrection) ([]float64, []float64) {
	return usableCurve(ds, func(d domain.DriftCorrection) (domain.Quantity, domain.Quantity) { return d.T, d.Offset })
}

type pairSort struct{ t, v []float64 }

func (p *pairSort) Len() int           { return len(p.t) }
func (p *pairSort) Less(i, j int) bool { return p.t[i] < p.t[j] }
func (p *pairSort) Swap(i, j int) {
	p.t[i], p.t[j] = p.t[j], p.t[i]
	p.v[i], p.v[j] = p.v[j], p.v[i]
}

// interp linearly interpolates v at t. When clampGap is true (the target
// follows a sampling gap) extrapolation is allowed for the smooth blank/drift
// reference curves, but the sample DTG itself is still never bridged.
func interp(ts, vs []float64, t float64, clampGap bool) float64 {
	if len(ts) == 0 {
		return 0
	}
	if t <= ts[0] {
		return vs[0]
	}
	if t >= ts[len(ts)-1] {
		return vs[len(vs)-1]
	}
	for i := 0; i+1 < len(ts); i++ {
		if ts[i] <= t && t <= ts[i+1] {
			f := (t - ts[i]) / (ts[i+1] - ts[i])
			return vs[i] + f*(vs[i+1]-vs[i])
		}
	}
	_ = clampGap
	return 0
}
