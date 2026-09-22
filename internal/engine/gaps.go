package engine

import (
	"sort"

	"tga-review/internal/domain"
)

// DetectGaps flags acquisition intervals that must never be interpolated across.
// A gap is:
//   - explicit_gap: successive records skip rows (missing sample points),
//   - time_anomaly: non-increasing furnace time,
//   - oversized_interval: dt exceeds both absolute and relative thresholds.
func DetectGaps(projectID, batchID string, fp domain.Fingerprint, norm *domain.DerivedNode, p domain.Params) (*domain.DerivedNode, error) {
	series, ok := norm.Payload.(domain.SeriesPayload)
	if !ok {
		return fail("normalized node payload mismatch")
	}
	pts := series.Points
	if len(pts) == 0 {
		return fail("cannot detect gaps on empty series")
	}
	dts := make([]float64, 0, len(pts)-1)
	for i := 1; i < len(pts); i++ {
		if d := pts[i].TMin - pts[i-1].TMin; d > 0 {
			dts = append(dts, d)
		}
	}
	sort.Float64s(dts)
	median := 1.0
	if len(dts) > 0 {
		median = dts[len(dts)/2]
		if median <= 0 {
			median = 1
		}
	}

	var gaps []domain.Gap
	for i := 1; i < len(pts); i++ {
		prev, cur := pts[i-1], pts[i]
		dt := cur.TMin - prev.TMin
		switch {
		case cur.Row > prev.Row+1:
			gaps = append(gaps, domain.Gap{StartRow: prev.Row, EndRow: cur.Row, T0Min: prev.TMin, T1Min: cur.TMin, Reason: "explicit_gap"})
		case dt <= 0:
			gaps = append(gaps, domain.Gap{StartRow: prev.Row, EndRow: cur.Row, T0Min: prev.TMin, T1Min: cur.TMin, Reason: "time_anomaly"})
		case dt > p.GapMaxIntervalMin && dt > median*p.GapMaxDTMin:
			gaps = append(gaps, domain.Gap{StartRow: prev.Row, EndRow: cur.Row, T0Min: prev.TMin, T1Min: cur.TMin, Reason: "oversized_interval"})
		}
	}
	payload := domain.GapMapPayload{Gaps: gaps}
	id, err := domain.ContentID(domain.PrefixDerived, string(domain.DKGapMap), struct {
		In string
		FP domain.Fingerprint
		P  domain.GapMapPayload
	}{norm.ID, fp, payload})
	if err != nil {
		return nil, err
	}
	return &domain.DerivedNode{
		ID: id, ProjectID: projectID, Kind: domain.DKGapMap, BatchID: batchID,
		Fingerprint: fp, InputNodeIDs: []string{norm.ID}, Payload: payload,
	}, nil
}

// GapSegments labels every point with a gap-bounded segment id.
func GapSegments(pts []domain.CanonicalPoint, gaps []domain.Gap) []string {
	between := map[[2]int]bool{}
	for _, g := range gaps {
		between[[2]int{g.StartRow, g.EndRow}] = true
	}
	out := make([]string, len(pts))
	seg := 0
	for i := range pts {
		if i > 0 && between[[2]int{pts[i-1].Row, pts[i].Row}] {
			seg++
		}
		out[i] = "seg" + itoa(seg)
	}
	return out
}
