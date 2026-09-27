package compute

import (
	"fmt"
	"math"

	"tga-review/internal/domain"
)

// splitSegments cuts the accepted trace into gap-free segments. A gap is
// declared whenever the time spacing exceeds the threshold; the missing region
// is recorded explicitly and is never bridged by interpolation.
func splitSegments(raw []rawPoint, p Params) ([]Segment, []Gap) {
	var segments []Segment
	var gaps []Gap
	if len(raw) == 0 {
		return segments, gaps
	}
	start := 0
	idx := 0
	for i := 1; i < len(raw); i++ {
		dt := raw[i].T - raw[i-1].T
		if dt > p.GapThreshold {
			segments = append(segments, makeSegment(idx, raw[start:i]))
			gid := ID("gap", ftoa(raw[i-1].T), ftoa(raw[i].T))
			gaps = append(gaps, Gap{
				ID: gid, FromT: raw[i-1].T, ToT: raw[i].T,
				Missing: int(math.Round(dt/p.medianSpacing(raw[start:i]))) - 1,
				Reason:  fmt.Sprintf("spacing %vg min exceeds gap threshold %vg min", round(dt, 4), p.GapThreshold),
			})
			idx++
			start = i
		}
	}
	segments = append(segments, makeSegment(idx, raw[start:]))
	return segments, gaps
}

func (p Params) medianSpacing(pts []rawPoint) float64 {
	if len(pts) < 2 {
		return 1
	}
	var ds []float64
	for i := 1; i < len(pts); i++ {
		ds = append(ds, pts[i].T-pts[i-1].T)
	}
	for i := 0; i < len(ds); i++ {
		for j := i + 1; j < len(ds); j++ {
			if ds[j] < ds[i] {
				ds[i], ds[j] = ds[j], ds[i]
			}
		}
	}
	m := ds[len(ds)/2]
	if m <= 0 {
		return 1
	}
	return m
}

func makeSegment(index int, pts []rawPoint) Segment {
	seg := Segment{Index: index, Points: make([]Point, 0, len(pts))}
	for _, rp := range pts {
		seg.Points = append(seg.Points, Point{
			T: rp.T, Tk: rp.Tk, MassRaw: rp.Mass, MassCorrect: rp.Mass,
		})
	}
	seg.ID = ID("seg", ftoa(index), ftoa(pts[0].T), ftoa(pts[len(pts)-1].T))
	seg.TStart, seg.TEnd = pts[0].T, pts[len(pts)-1].T
	seg.TStartK, seg.TEndK = pts[0].Tk, pts[len(pts)-1].Tk
	return seg
}

// correctedMass applies drift correction and blank subtraction. Blank lookup
// is segment-local and returns the raw value (with a diagnostic handled by the
// caller) if interpolation would cross a blank gap.
func correctedMass(t, raw float64, p Params, b Bundle) float64 {
	v := raw
	if p.DriftCorrection && b.Drift != nil {
		v -= driftOffset(t, b.Drift)
	}
	if p.BlankCorrection && len(b.Blank) > 0 {
		if bl, ok := blankAt(t, b.Blank); ok {
			v -= bl
		}
	}
	return v
}

func driftOffset(t float64, d *domain.DriftModel) float64 {
	switch d.Kind {
	case "linear":
		if d.SlopePerMin.Normalised {
			anchor := 0.0
			if d.AnchorTime.Normalised {
				anchor = d.AnchorTime.Canonical
			}
			off := d.SlopePerMin.Canonical * (t - anchor)
			if d.Intercept.Normalised {
				off += d.Intercept.Canonical
			}
			return off
		}
	case "points":
		var xs, ys []float64
		for _, pt := range d.Points {
			if pt.Time.Normalised && pt.Mass.Normalised {
				xs = append(xs, pt.Time.Canonical)
				ys = append(ys, pt.Mass.Canonical)
			}
		}
		if len(xs) >= 2 && t >= xs[0] && t <= xs[len(xs)-1] {
			return interp1(xs, ys, t)
		}
	}
	return 0
}

func blankAt(t float64, pts []domain.BlankPoint) (float64, bool) {
	var xs, ys []float64
	for _, pt := range pts {
		if pt.Time.Normalised && pt.Mass.Normalised {
			xs = append(xs, pt.Time.Canonical)
			ys = append(ys, pt.Mass.Canonical)
		}
	}
	if len(xs) < 2 || t < xs[0] || t > xs[len(xs)-1] {
		return 0, false
	}
	// Refuse interpolation across a blank gap larger than 2x local spacing.
	for i := 1; i < len(xs); i++ {
		if t >= xs[i-1] && t <= xs[i] {
			local := 1.0
			if i >= 2 {
				local = xs[i-1] - xs[i-2]
			}
			if xs[i]-xs[i-1] > 3*local {
				return 0, false
			}
		}
	}
	return interp1(xs, ys, t), true
}

func markNonMonotonic(seg *Segment) {
	for i := 1; i < len(seg.Points); i++ {
		if seg.Points[i].MassCorrect > seg.Points[i-1].MassCorrect+1e-9 {
			seg.Points[i].NonMonotonic = true
		}
	}
}

// assignTG sets mass percent relative to the stated initial mass when known,
// otherwise the first point of the first segment.
func assignTG(seg *Segment, r *Result) {
	var basis float64
	if r.InitialMassKnown {
		basis = r.InitialMassMG
	} else if len(seg.Points) > 0 {
		basis = seg.Points[0].MassCorrect
	}
	for i := range seg.Points {
		if basis != 0 {
			seg.Points[i].TG = seg.Points[i].MassCorrect / basis * 100
		}
	}
}

// assignDTG uses a centred derivative inside the segment. No derivative is
// produced at segment edges or across gaps.
func assignDTG(seg *Segment, _ *Result) {
	for i := range seg.Points {
		if i == 0 || i == len(seg.Points)-1 {
			continue
		}
		a, c := seg.Points[i-1], seg.Points[i+1]
		dt := c.T - a.T
		if dt > 0 {
			seg.Points[i].DTG = (c.TG - a.TG) / dt
		}
	}
}

func collectInputDiagnostics(b Bundle, r *Result) {
	note := func(scope, sev, msg string) {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{Scope: scope, Severity: sev, Message: msg})
	}
	for i, s := range b.Samples {
		if !s.Time.Normalised || !s.Temperature.Normalised || !s.Mass.Normalised {
			note(fmt.Sprintf("samples[%d]", i), "error", "point retained but excluded from derivation: missing normalisation")
		}
	}
	if b.Drift != nil {
		if b.Drift.Kind == "linear" && !b.Drift.SlopePerMin.Normalised {
			note("drift", "warning", "linear drift slope not normalised; correction skipped for it")
		}
	}
	if !r.InitialMassKnown {
		note("initial_mass", "warning", "residue basis falls back to first accepted point")
	}
}
