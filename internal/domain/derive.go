package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

const AlgoVersion = "tga-decomp-1.3.0"

// Inputs is the resolved set of current raw layer versions fed to derivation.
type Inputs struct {
	Sample  *LayerVersion
	Drift   *LayerVersion
	Furnace *LayerVersion
	Gas     *LayerVersion
	Blank   *LayerVersion
	Initial *LayerVersion
}

func (in Inputs) Sorted() []struct{ Kind LayerKind; V *LayerVersion } {
	return []struct{ Kind LayerKind; V *LayerVersion }{
		{LayerSample, in.Sample}, {LayerDrift, in.Drift}, {LayerFurnace, in.Furnace},
		{LayerGas, in.Gas}, {LayerBlank, in.Blank}, {LayerInitial, in.Initial},
	}
}

func ParamFingerprint(p Params) string {
	h := sha256.Sum256(append([]byte(AlgoVersion+"|"), MarshalCanonical(p)...))
	return hex.EncodeToString(h[:10])
}

func sourceRefs(in Inputs) []SourceRef {
	var refs []SourceRef
	for _, kv := range in.Sorted() {
		if kv.V != nil {
			refs = append(refs, SourceRef{Kind: "layer", ID: kv.V.ID(), Seq: kv.V.Seq, Detail: string(kv.Kind)})
		}
	}
	return refs
}

// DependsOn reports whether derived kind k depends on the given raw layer.
// Local source changes only invalidate what depends on them.
func DependsOn(k NodeKind, l LayerKind) bool {
	switch k {
	case NodeTG:
		return l == LayerSample || l == LayerDrift || l == LayerBlank || l == LayerInitial
	case NodeStages:
		return l == LayerSample || l == LayerDrift || l == LayerBlank ||
			l == LayerGas || l == LayerFurnace || l == LayerInitial
	case NodeResidual:
		return l == LayerSample || l == LayerDrift || l == LayerBlank || l == LayerInitial
	}
	return false
}

type dpWork struct {
	point     DerivedPoint
	usable    bool   // mass convertible
	tUsable   bool   // temperature convertible
	rawMass   float64
	dtg       float64
	hasDTG    bool
	nonMono   bool
	gapAfter  bool
}

// DeriveTG performs drift correction, TG% normalization, gap detection,
// non-monotonic noise flagging and numerical DTG. Gaps are never interpolated.
func DeriveTG(in Inputs, p Params) ([]DerivedPoint, []Diagnostic, *ResidualChain) {
	var diags []Diagnostic
	if in.Sample == nil || len(in.Sample.Points) == 0 {
		return nil, []Diagnostic{{Code: "missing", Message: "no sample points"}}, nil
	}
	var dm *DriftModel
	if in.Drift != nil {
		dm = in.Drift.Drift
	}
	blank := mapTBlank(in.Blank)
	pts := in.Sample.Points
	initialMg := initialMass(in)

	work := make([]dpWork, len(pts))
	// gaps: first pass on raw time intervals
	gapRanges := [][2]float64{}
	for i := range pts {
		if i > 0 && pts[i].T-pts[i-1].T > p.GapThresholdS {
			gapRanges = append(gapRanges, [2]float64{pts[i-1].T, pts[i].T})
			work[i-1].gapAfter = true
		}
	}
	for i, sp := range pts {
		wp := dpWork{}
		wp.point = DerivedPoint{PID: fmt.Sprintf("pt#%d", sp.Idx), T: sp.T}
		if sp.Mass.Diagnostic != "" {
			wp.usable = false
			wp.point.Diagnostic = sp.Mass.Diagnostic
			diags = append(diags, Diagnostic{Code: "unconvertible", Layer: "sample", Ref: fmt.Sprintf("row:%d", sp.Idx), Message: sp.Mass.Diagnostic})
		} else {
			wp.usable = true
			wp.rawMass = sp.Mass.Value
			mc := sp.Mass.Value
			if dm != nil {
				mc -= dm.B0.Value + dm.B1.Value*sp.T/60.0
			}
			if dm != nil && dm.UseBlank && len(blank) > 0 {
				mc -= blankAt(blank, sp.T)
			}
			wp.point.Mass = mc
			if initialMg != nil {
				v := 100.0 * mc / *initialMg
				wp.point.TGPct = &v
			}
		}
		if c, ok := TempC(sp.Temp); ok {
			wp.tUsable = true
			_ = c
		} else if sp.Temp.Diagnostic != "" {
			wp.point.Diagnostic = joinDiag(wp.point.Diagnostic, sp.Temp.Diagnostic)
		}
		// non-monotonic noise: short positive mass excursions beyond threshold
		if i > 0 && i < len(pts)-1 && wp.usable && work[i-1].usable {
			prev := work[i-1].point.Mass
			nextRaw := pts[i+1].Mass
			cur := wp.point.Mass
			if nextRaw.Diagnostic == "" {
				next := nextRaw.Value
				if dm != nil {
					next -= dm.B0.Value + dm.B1.Value*pts[i+1].T/60.0
				}
				if cur > prev && cur > next && (cur-prev) > p.NonMonotonicMg {
					wp.nonMono = true
					wp.point.NonMonotonic = true
				}
			}
		}
		work[i] = wp
	}
	// DTG via central difference, never crossing a gap and never using unusable neighbors
	for i := range work {
		if !work[i].usable {
			continue
		}
		if i > 0 && i < len(work)-1 && work[i-1].usable && work[i+1].usable &&
			!work[i-1].gapAfter && !work[i].gapAfter {
			dm := (work[i+1].point.Mass - work[i-1].point.Mass) / ((work[i+1].point.T - work[i-1].point.T) / 60.0)
			work[i].dtg, work[i].hasDTG = dm, true
			work[i].point.DTG = &dm
		}
	}
	out := make([]DerivedPoint, len(work))
	for i := range work {
		work[i].point.GapAfter = work[i].gapAfter
		out[i] = work[i].point
	}
	for _, g := range gapRanges {
		diags = append(diags, Diagnostic{Code: "gap", Message: fmt.Sprintf("sampling gap %.0fs..%.0fs left without interpolation", g[0], g[1])})
	}
	residual := buildResidual(in, out, initialMg, gapRanges, diags)
	return out, diags, residual
}

func joinDiag(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

func mapTBlank(b *LayerVersion) []SamplePoint {
	if b == nil {
		return nil
	}
	return b.Blank
}

// blankAt returns the pointwise blank drift at t using linear interpolation
// strictly inside the blank coverage (no extrapolation).
func blankAt(blank []SamplePoint, t float64) float64 {
	useful := []SamplePoint{}
	for _, p := range blank {
		if p.Mass.Diagnostic == "" {
			useful = append(useful, p)
		}
	}
	sort.SliceStable(useful, func(i, j int) bool { return useful[i].T < useful[j].T })
	if len(useful) == 0 || t < useful[0].T || t > useful[len(useful)-1].T {
		return 0
	}
	for i := 1; i < len(useful); i++ {
		if t <= useful[i].T {
			a, b := useful[i-1], useful[i]
			if b.T == a.T {
				return a.Mass.Value
			}
			f := (t - a.T) / (b.T - a.T)
			return a.Mass.Value + f*(b.Mass.Value-a.Mass.Value)
		}
	}
	return 0
}

func initialMass(in Inputs) *float64 {
	if in.Initial != nil && in.Initial.Initial != nil && in.Initial.Initial.Mass.Diagnostic == "" {
		v := in.Initial.Initial.Mass.Value
		return &v
	}
	// fall back to the first usable sample mass at/after the anchor if no
	// dedicated initial layer exists (initial import before revision).
	if in.Sample != nil {
		for _, sp := range in.Sample.Points {
			if sp.Mass.Diagnostic == "" {
				v := sp.Mass.Value
				return &v
			}
		}
	}
	return nil
}
