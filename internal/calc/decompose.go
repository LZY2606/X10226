package calc

import (
	"fmt"
	"math"
	"sort"

	"tgasys/internal/domain"
)

// DTGPoint holds -dm/dt (positive for loss) and quality flags.
type DTGPoint struct {
	T             float64
	Rate          float64
	NonMonotonic  bool
	WithinSegment bool
}

// Result is the full derived output of a recomputation.
type Result struct {
	DTG           []DTGPoint
	Stages        []*domain.StageCandidate
	Residue       *domain.Residue
	InitialMassMg float64
	InitialFrom   string
	Notes         []string
	// Evidence coordinates (with units / bases) for rule events.
	EffectiveGas  []EffectiveGas
	NonMonotonicT []float64
	GapTimes      []float64
	DriftApplied  bool
	Ambiguous     bool
}

// EffectiveGas is a valve event shifted to the sample frame.
type EffectiveGas struct {
	ValveS       float64
	EffectiveS   float64
	FromGas      string
	ToGas        string
	EffectiveTempC float64
}

func safe(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// Compute performs the full derivation for a model.
func Compute(m *Model) *Result {
	r := &Result{}

	// Initial mass: latest (by time) preserved initial-mass reading.
	if len(m.Initial) > 0 {
		in := m.Initial[len(m.Initial)-1]
		for _, x := range m.Initial {
			if x.T > in.T {
				in = x
			}
		}
		r.InitialMassMg = in.Mass
		r.InitialFrom = fmt.Sprintf("initial_mass@%gs=%gmg(raw %q)", in.T, in.Mass, in.Raw)
	} else if len(m.Corrected) > 0 {
		first := m.Corrected[0]
		r.InitialMassMg = first.Mass
		r.InitialFrom = fmt.Sprintf("first_sample_point@%gs=%gmg", first.T, first.Mass)
	}

	// Drift evidence.
	if m.Params.DriftMode != "none" && (m.Drift.HasLinear || len(m.Blank) > 0) {
		r.DriftApplied = true
	}

	// DTG per segment; gaps are never crossed.
	r.DTG = r.computeDTG(m)
	for _, d := range r.DTG {
		if d.NonMonotonic {
			r.NonMonotonicT = append(r.NonMonotonicT, d.T)
		}
	}
	for _, sg := range m.Segments {
		if sg.GapAfterS > 0 {
			r.GapTimes = append(r.GapTimes, sg.GapAfterS)
		}
	}

	// Effective gas events: valve time != sample reaction time.
	for _, g := range m.Gas {
		eff := g.At + m.Params.AtmosDelayS
		temp := m.sampleTempAt(eff)
		r.EffectiveGas = append(r.EffectiveGas, EffectiveGas{
			ValveS: g.At, EffectiveS: eff, FromGas: g.FromGas, ToGas: g.ToGas,
			EffectiveTempC: temp,
		})
	}

	r.Stages = r.findStages(m)
	r.Residue = r.computeResidue(m)
	if r.Ambiguous {
		r.Notes = append(r.Notes, "multiple legal stage decompositions retained")
	}
	return r
}

func (r *Result) computeDTG(m *Model) []DTGPoint {
	var out []DTGPoint
	for _, seg := range m.Segments {
		pts := seg.Points
		for i := 1; i+1 < len(pts); i++ {
			a, b := pts[i-1], pts[i+1]
			if b.T <= a.T {
				continue
			}
			rate := safe(-(b.Mass - a.Mass) / (b.T - a.T))
			// non-monotonic: a corrected mass increase beyond noise tolerance.
			nonMono := b.Mass-a.Mass > m.Params.NoiseTolMg && b.Mass > a.Mass
			_ = i
			cur := pts[i]
			if cur.Mass > a.Mass+m.Params.NoiseTolMg {
				nonMono = true
			}
			out = append(out, DTGPoint{T: cur.T, Rate: rate, NonMonotonic: nonMono, WithinSegment: true})
		}
	}
	if w := m.Params.SmoothingWindowN; w > 1 && len(out) > w {
		sm := make([]DTGPoint, len(out))
		copy(sm, out)
		half := w / 2
		for i := range out {
			sum, n := 0.0, 0
			for k := i - half; k <= i+half; k++ {
				if k >= 0 && k < len(out) {
					sum += out[k].Rate
					n++
				}
			}
			sm[i].Rate = sum / float64(n)
		}
		out = sm
	}
	return out
}

func (r *Result) findStages(m *Model) []*domain.StageCandidate {
	var cands []*domain.StageCandidate
	for _, seg := range m.Segments {
		dtg := r.dtgForSegment(seg)
		if len(dtg) < 3 {
			continue
		}
		var peaks []int
		for i := 1; i+1 < len(dtg); i++ {
			if dtg[i].Rate > dtg[i-1].Rate && dtg[i].Rate >= dtg[i+1].Rate &&
				dtg[i].Rate >= m.Params.MinProminence {
				peaks = append(peaks, i)
			}
		}
		for _, pi := range peaks {
			startIdx := pi
			for startIdx > 0 && dtg[startIdx-1].Rate >= dtg[startIdx].Rate*0.1 {
				startIdx--
			}
			endIdx := pi
			for endIdx < len(dtg)-1 && dtg[endIdx+1].Rate >= dtg[endIdx].Rate*0.1 {
				endIdx++
			}
			peakT := dtg[pi].T
			startT := dtg[startIdx].T
			endT := dtg[endIdx].T
			mStart, ok1 := r.massAt(m, startT)
			mEnd, ok2 := r.massAt(m, endT)
			loss := 0.0
			if ok1 && ok2 {
				loss = mStart - mEnd
			}
			c := &domain.StageCandidate{
				ID:         stageID(seg.Index, peakT),
				PeakS:      round3(peakT),
				PeakTempC:  round2(m.sampleTempAt(peakT)),
				StartS:     round3(startT),
				EndS:       round3(endT),
				MassLossMg: round4(loss),
				Prominence: round6(dtg[pi].Rate),
				AtmosLabel: r.atmosAt(m, peakT),
				Status:     domain.CandPending,
			}
			cands = append(cands, c)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].PeakS < cands[j].PeakS })
	// Overlap detection: stages may overlap and are NOT forced to sum to 100%.
	groups := map[string]bool{}
	for i := range cands {
		for j := i + 1; j < len(cands); j++ {
			a, b := cands[i], cands[j]
			if a.StartS <= b.EndS+m.Params.OverlapTolS && b.StartS <= a.EndS+m.Params.OverlapTolS {
				a.OverlapsWith = append(a.OverlapsWith, b.ID)
				b.OverlapsWith = append(b.OverlapsWith, a.ID)
				gid := overlapGroupID(a, b)
				a.GroupID, b.GroupID = gid, gid
				groups[gid] = true
			}
		}
	}
	for gid := range groups {
		var members []string
		for _, c := range cands {
			if c.GroupID == gid {
				members = append(members, c.ID)
			}
		}
		if len(members) > 1 {
			r.Ambiguous = true
			for _, c := range cands {
				if c.GroupID == gid {
					c.Status = domain.CandAlternative
					c.RejectReason = "overlapping peak: decomposition not unique; all legal alternatives retained"
				}
			}
		}
	}
	return cands
}

func (r *Result) dtgForSegment(seg Segment) []DTGPoint {
	var out []DTGPoint
	for _, d := range r.DTG {
		if d.T >= seg.Points[0].T && d.T <= seg.Points[len(seg.Points)-1].T {
			out = append(out, d)
		}
	}
	return out
}

func (r *Result) massAt(m *Model, t float64) (float64, bool) {
	for _, seg := range m.Segments {
		pts := seg.Points
		if t < pts[0].T || t > pts[len(pts)-1].T {
			continue
		}
		for i := 0; i+1 < len(pts); i++ {
			if t >= pts[i].T && t <= pts[i+1].T {
				a, b := pts[i], pts[i+1]
				f := 0.0
				if b.T > a.T {
					f = (t - a.T) / (b.T - a.T)
				}
				return a.Mass + f*(b.Mass-a.Mass), true
			}
		}
	}
	return 0, false
}

func (r *Result) atmosAt(m *Model, sampleT float64) string {
	cur := "initial"
	for _, g := range r.EffectiveGas {
		if sampleT >= g.EffectiveS {
			cur = g.ToGas
		}
	}
	return cur
}

func (r *Result) computeResidue(m *Model) *domain.Residue {
	if len(m.Corrected) == 0 || r.InitialMassMg == 0 {
		return nil
	}
	// final point = last usable corrected point (gaps end segments, so we use
	// the globally last valid point).
	last := Point{}
	for _, p := range m.Corrected {
		if p.MassOK {
			last = p
		}
	}
	if !last.MassOK {
		return nil
	}
	pct := safe(last.Mass / r.InitialMassMg * 100)
	res := &domain.Residue{
		ID:        residueID(last.T, last.Mass, r.InitialMassMg),
		AtS:       round3(last.T),
		Percent:   round4(pct),
		FinalMg:   round4(last.Mass),
		InitialMg: round4(r.InitialMassMg),
	}
	res.Chain = []domain.ChainStep{
		{Label: "preserved initial mass", Value: fmt.Sprintf("%g", round4(r.InitialMassMg)), Unit: "mg", Note: r.InitialFrom},
		{Label: "drift/blank correction", Value: m.Params.DriftMode, Unit: "mode", Note: r.driftNote(m)},
		{Label: "last valid corrected mass (gaps not interpolated)", Value: fmt.Sprintf("%g", round4(last.Mass)), Unit: "mg", From: []string{"layer:" + string(domain.LayerSample), "layer:" + string(domain.LayerBlank)}},
		{Label: "residue rate", Value: fmt.Sprintf("%g", round4(pct)), Unit: "%", Note: "= final/initial*100; stages are not forced to sum to 100%"},
	}
	return res
}

func (r *Result) driftNote(m *Model) string {
	switch {
	case len(m.Blank) > 0 && m.Params.DriftMode == "blank":
		return fmt.Sprintf("blank curve %d points, subtracted only within its measured range", len(m.Blank))
	case m.Drift.HasLinear:
		return fmt.Sprintf("linear slope=%g mg/s intercept=%g mg", m.Drift.Slope, m.Drift.Intercept)
	default:
		return "no correction data"
	}
}

// sampleTempAt estimates the sample temperature by lag-shifting the furnace
// program and interpolating measured points.
func (m *Model) sampleTempAt(sampleT float64) float64 {
	// Prefer measured sample temperature at the lag-shifted time.
	if t, ok := m.measuredTempAt(sampleT); ok {
		return t
	}
	return m.programTempAt(sampleT)
}

func (m *Model) measuredTempAt(t float64) (float64, bool) {
	var a, b *Point
	for i := range m.Corrected {
		p := &m.Corrected[i]
		if !p.TempOK {
			continue
		}
		if p.T <= t {
			x := *p
			a = &x
		}
		if p.T >= t && b == nil {
			x := *p
			b = &x
			break
		}
	}
	if a != nil && b != nil && b.T > a.T {
		f := (t - a.T) / (b.T - a.T)
		return a.Temp + f*(b.T-a.Temp), true
	}
	if a != nil {
		return a.Temp, true
	}
	if b != nil {
		return b.Temp, true
	}
	return 0, false
}

func (m *Model) programTempAt(sampleT float64) float64 {
	furnaceT := sampleT - m.Params.TempLagS
	for _, st := range m.Program {
		if furnaceT >= st.StartS && furnaceT <= st.EndS {
			durMin := (st.EndS - st.StartS) / 60
			f := 0.0
			if durMin > 0 {
				f = (furnaceT - st.StartS) / (st.EndS - st.StartS)
			}
			startTemp := st.Target - st.Rate*durMin
			return startTemp + f*st.Rate*durMin
		}
	}
	return 0
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }
