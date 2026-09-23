package analysis

import (
	"fmt"
	"sort"

	"tga-review/internal/domain"
)

type Engine struct{}

func New() *Engine { return &Engine{} }

// Normalize splits valid points into gap-separated segments. Bad/unknown
// cells stay in place (status != ok) so plots show markers instead of
// silently dropping them.
func (e *Engine) Normalize(in Input) NormalizedPayload {
	var diags []domain.Diagnostic
	var segs []Segment
	var orphans []SeriesPoint
	cur := Segment{ID: segID(0), Index: 0}
	segIdx := 0
	flush := func() {
		if len(cur.Points) > 0 {
			segs = append(segs, cur)
			segIdx++
			cur = Segment{ID: segID(segIdx), Index: segIdx}
		}
	}
	for _, p := range in.Points {
		if p.Time.Status != domain.StatusOK {
			sp := SeriesPoint{Seq: p.Seq, Status: domain.StatusUnknown, NonMono: p.NonMono}
			if p.Temp.Status == domain.StatusOK {
				sp.TempC = p.Temp.Value
			}
			if p.Mass.Status == domain.StatusOK {
				sp.RawMassMg = p.Mass.Value
			}
			orphans = append(orphans, sp)
			diags = append(diags, domain.Diagnostic{Code: "POINT_TIME_UNREADABLE", Severity: domain.SevWarning,
				Ref: pointRef(in, p.Seq), Message: fmt.Sprintf("row %d kept outside segments (time unreadable)", p.Seq)})
			continue
		}
		sp := SeriesPoint{Seq: p.Seq, TimeS: p.Time.Value, GapAfter: p.GapAfter, NonMono: p.NonMono}
		if p.Temp.Status == domain.StatusOK {
			sp.TempC = p.Temp.Value
			sp.Status = domain.StatusOK
		} else {
			sp.Status = domain.StatusUnknown
			diags = append(diags, domain.Diagnostic{Code: "POINT_TEMP_UNREADABLE", Severity: domain.SevWarning,
				Ref: pointRef(in, p.Seq), Message: fmt.Sprintf("row %d temperature retained as unknown", p.Seq)})
		}
		if p.Mass.Status == domain.StatusOK {
			sp.RawMassMg = p.Mass.Value
		} else {
			sp.Status = domain.StatusUnknown
			diags = append(diags, domain.Diagnostic{Code: "POINT_MASS_UNREADABLE", Severity: domain.SevWarning,
				Ref: pointRef(in, p.Seq), Message: fmt.Sprintf("row %d mass retained as unknown", p.Seq)})
		}
		if len(cur.Points) > 0 {
			prev := cur.Points[len(cur.Points)-1]
			if sp.TimeS-prev.TimeS > domain.GapThresholdSec {
				flush()
			}
		}
		cur.Points = append(cur.Points, sp)
	}
	flush()
	if len(orphans) > 0 {
		diags = append(diags, domain.Diagnostic{Code: "ORPHAN_POINTS_KEPT", Severity: domain.SevInfo,
			Message: fmt.Sprintf("%d rows with unreadable time retained as orphans", len(orphans))})
	}
	if len(segs) > 1 {
		diags = append(diags, domain.Diagnostic{Code: "SEGMENTS_SPLIT_BY_GAP", Severity: domain.SevInfo,
			Message: fmt.Sprintf("series split into %d segments by sampling gaps", len(segs))})
	}
	return NormalizedPayload{Segments: segs, Orphans: orphans, Diagnostics: diags}
}

func segID(i int) string { return fmt.Sprintf("seg-%d", i) }

func pointRef(in Input, seq int) string {
	if s, ok := in.SourceIDs[domain.LayerSamples]; ok {
		return fmt.Sprintf("%s#p%d", s, seq)
	}
	return fmt.Sprintf("p%d", seq)
}

// Correct applies the balance baseline drift correction. Blank subtraction
// interpolates ONLY within the blank's own point span (never across the
// sample's gaps; sample gaps are preserved on output).
func (e *Engine) Correct(in Input, n NormalizedPayload) CorrectedPayload {
	var diags []domain.Diagnostic
	method := "none"
	if in.Drift != nil {
		method = in.Drift.Model
	}
	out := CorrectedPayload{Method: method}
	blankAt := blankInterpolator(in.Blank, &diags)
	for _, seg := range n.Segments {
		cs := CorrSegment{ID: seg.ID, Index: seg.Index}
		for _, p := range seg.Points {
			cp := CorrectedPoint{Seq: p.Seq, TimeS: p.TimeS, TempC: p.TempC,
				RawMassMg: p.RawMassMg, Status: p.Status, NonMono: p.NonMono, GapAfter: p.GapAfter}
			if p.Status == domain.StatusOK {
				var corr float64
				switch method {
				case "linear":
					if in.Drift != nil {
						corr = in.Drift.RateMgMin * (p.TimeS / 60.0)
					}
				case "blank_subtract":
					if v, ok := blankAt(p.TimeS); ok {
						corr = v
					} else {
						cp.Status = domain.StatusUnknown
						diags = append(diags, domain.Diagnostic{Code: "BLANK_OUTSIDE_SPAN", Severity: domain.SevWarning,
							Ref: pointRef(in, p.Seq),
							Message: fmt.Sprintf("row %d at %.1fs outside blank span; mass left uncorrected", p.Seq, p.TimeS)})
					}
				}
				cp.CorrectionMg = corr
				cp.CorrMassMg = p.RawMassMg - corr
			}
			cs.Points = append(cs.Points, cp)
		}
		out.Segments = append(out.Segments, cs)
	}
	if method == "blank_subtract" && len(in.Blank) == 0 {
		diags = append(diags, domain.Diagnostic{Code: "BLANK_MISSING", Severity: domain.SevError,
			Message: "blank_subtract selected but no blank curve present; no correction applied"})
	}
	out.Diagnostics = diags
	return out
}

func blankInterpolator(blank []domain.BlankPoint, diags *[]domain.Diagnostic) func(float64) (float64, bool) {
	if len(blank) == 0 {
		return func(float64) (float64, bool) { return 0, false }
	}
	sorted := make([]domain.BlankPoint, len(blank))
	copy(sorted, blank)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].TimeSec < sorted[j].TimeSec })
	return func(t float64) (float64, bool) {
		if t < sorted[0].TimeSec || t > sorted[len(sorted)-1].TimeSec {
			return 0, false
		}
		// find bracketing points; piecewise-linear within blank span only
		for i := 1; i < len(sorted); i++ {
			if t <= sorted[i].TimeSec {
				a, b := sorted[i-1], sorted[i]
				if b.TimeSec == a.TimeSec {
					return b.MassMg, true
				}
				f := (t - a.TimeSec) / (b.TimeSec - a.TimeSec)
				return a.MassMg + f*(b.MassMg-a.MassMg), true
			}
		}
		return sorted[len(sorted)-1].MassMg, true
	}
}

// DTG computes forward-difference derivative within each segment. It never
// spans a gap. Unknown-mass points yield no derivative.
func (e *Engine) DTG(n NormalizedPayload, c CorrectedPayload) DTGPayload {
	var diags []domain.Diagnostic
	out := DTGPayload{}
	for _, seg := range c.Segments {
		ds := DTGSegment{ID: seg.ID, Index: seg.Index}
		for i := 0; i+1 < len(seg.Points); i++ {
			a, b := seg.Points[i], seg.Points[i+1]
			if a.Status != domain.StatusOK || b.Status != domain.StatusOK {
				continue
			}
			dt := b.TimeS - a.TimeS
			if dt <= 0 || dt > domain.GapThresholdSec {
				continue // never derive across a gap
			}
			ds.Points = append(ds.Points, DTGPoint{
				Seq: a.Seq, StartS: a.TimeS, EndS: b.TimeS,
				TimeS: (a.TimeS + b.TimeS) / 2, TempC: (a.TempC + b.TempC) / 2,
				MGperMin: (b.CorrMassMg - a.CorrMassMg) / (dt / 60.0),
			})
		}
		if len(ds.Points) == 0 {
			diags = append(diags, domain.Diagnostic{Code: "DTG_EMPTY_SEGMENT", Severity: domain.SevWarning,
				Ref: seg.ID, Message: fmt.Sprintf("segment %s yields no DTG points", seg.ID)})
		}
		out.Segments = append(out.Segments, ds)
	}
	out.Diagnostics = diags
	return out
}
