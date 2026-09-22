package engine

import (
	"math"

	"tga-review/internal/domain"
)

// BuildDTG computes dm/dt with central differences. Differences never cross gap
// boundaries (each segment is differentiated independently). A centered moving
// average may smooth DTG; non-monotonic bounces and isolated spikes survive as
// first-class flags on every point.
func BuildDTG(projectID, batchID string, fp domain.Fingerprint, corrected *domain.DerivedNode, gapNode *domain.DerivedNode, p domain.Params) (*domain.DerivedNode, error) {
	cp, ok := corrected.Payload.(domain.CorrectedPayload)
	if !ok {
		return fail("corrected node payload mismatch")
	}
	gp, ok := gapNode.Payload.(domain.GapMapPayload)
	if !ok {
		return fail("gap node payload mismatch")
	}
	pts := cp.Points
	n := len(pts)
	if n < 2 {
		return fail("need at least two points for DTG")
	}
	segs := GapSegments(toCanonical(pts), gp.Gaps)

	raw := make([]float64, n)
	segAt := make([]string, n)
	for i := 0; i < n; i++ {
		segAt[i] = segs[i]
	}
	for i := 0; i < n; i++ {
		switch {
		case i > 0 && i < n-1 && segAt[i-1] == segAt[i] && segAt[i] == segAt[i+1]:
			dt := pts[i+1].TMin - pts[i-1].TMin
			if dt > 0 {
				raw[i] = (pts[i+1].CorrectedMg - pts[i-1].CorrectedMg) / dt
			}
		case i > 0 && segAt[i-1] == segAt[i]:
			dt := pts[i].TMin - pts[i-1].TMin
			if dt > 0 {
				raw[i] = (pts[i].CorrectedMg - pts[i-1].CorrectedMg) / dt
			}
		case i < n-1 && segAt[i] == segAt[i+1]:
			dt := pts[i+1].TMin - pts[i].TMin
			if dt > 0 {
				raw[i] = (pts[i+1].CorrectedMg - pts[i].CorrectedMg) / dt
			}
		}
	}

	smoothed := movingAverage(raw, segAt, p.DTGWindowPoints)

	out := make([]domain.DTGPoint, n)
	for i := 0; i < n; i++ {
		v := smoothed[i]
		flag := ""
		nonMono := false
		// positive bounce: mass increased where neighbors lose mass (noise)
		if v > 0 {
			nonMono = true
			neigh := neighborLoss(raw, segAt, i)
			if neigh > 0 && v > neigh*p.NoiseRelThreshold {
				flag = "positive_bounce"
			} else {
				flag = "non_monotonic"
			}
		}
		// isolated spike: raw magnitude far above both same-segment neighbors
		if i > 0 && i < n-1 && segAt[i-1] == segAt[i] && segAt[i] == segAt[i+1] {
			a, b := math.Abs(raw[i-1]), math.Abs(raw[i+1])
			base := math.Max(a, b)
			if base > 0 && math.Abs(raw[i]) > base*p.SpikeRelThreshold && math.Abs(raw[i]) > math.Abs(v) {
				if flag == "" {
					flag = "isolated_spike"
				}
			}
		}
		out[i] = domain.DTGPoint{
			Row: pts[i].Row, TMin: pts[i].TMin, TK: pts[i].TK,
			DTGMgMin: round6(v), SegmentID: segAt[i], NonMonotonic: nonMono, NoiseFlag: flag,
		}
	}
	payload := domain.DTGPayload{Points: out, Window: p.DTGWindowPoints}
	id, err := domain.ContentID(domain.PrefixDerived, string(domain.DKDTGSeries), struct {
		In   string
		Gap  string
		FP   domain.Fingerprint
		P    domain.DTGPayload
	}{corrected.ID, gapNode.ID, fp, payload})
	if err != nil {
		return nil, err
	}
	return &domain.DerivedNode{
		ID: id, ProjectID: projectID, Kind: domain.DKDTGSeries, BatchID: batchID,
		Fingerprint: fp, InputNodeIDs: []string{corrected.ID, gapNode.ID}, Payload: payload,
	}, nil
}

func toCanonical(cps []domain.CorrectedPoint) []domain.CanonicalPoint {
	out := make([]domain.CanonicalPoint, len(cps))
	for i, c := range cps {
		out[i] = domain.CanonicalPoint{Row: c.Row, TMin: c.TMin, TK: c.TK, MassMg: c.CorrectedMg, Present: true}
	}
	return out
}

func movingAverage(xs []float64, segs []string, w int) []float64 {
	if w <= 1 {
		return append([]float64{}, xs...)
	}
	h := w / 2
	out := make([]float64, len(xs))
	for i := range xs {
		sum, cnt := 0.0, 0
		for j := i - h; j <= i+h; j++ {
			if j < 0 || j >= len(xs) || segs[j] != segs[i] {
				continue
			}
			sum += xs[j]
			cnt++
		}
		if cnt > 0 {
			out[i] = sum / float64(cnt)
		}
	}
	return out
}

func neighborLoss(raw []float64, segs []string, i int) float64 {
	best := 0.0
	for _, j := range []int{i - 1, i + 1} {
		if j >= 0 && j < len(raw) && segs[j] == segs[i] && raw[j] < 0 {
			if -raw[j] > best {
				best = -raw[j]
			}
		}
	}
	return best
}

func round6(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	return math.Round(v*1e6) / 1e6
}
