package engine

import (
	"sort"

	"tga-review/internal/domain"
)

// FindPeaks locates DTG minima (mass-loss maxima) inside each gap-bounded
// segment. Peaks closer in time than OverlapPeakWindowMin are recorded as
// mutually ambiguous: they become overlapping stage candidates rather than one
// forced decomposition.
func FindPeaks(projectID, batchID string, fp domain.Fingerprint, dtgNode *domain.DerivedNode, p domain.Params) (*domain.DerivedNode, error) {
	dp, ok := dtgNode.Payload.(domain.DTGPayload)
	if !ok {
		return fail("dtg node payload mismatch")
	}
	pts := dp.Points
	var peaks []domain.Peak
	for i := 1; i+1 < len(pts); i++ {
		cur := pts[i]
		if cur.SegmentID != pts[i-1].SegmentID || cur.SegmentID != pts[i+1].SegmentID {
			continue
		}
		if cur.DTGMgMin <= pts[i-1].DTGMgMin && cur.DTGMgMin <= pts[i+1].DTGMgMin && cur.DTGMgMin < -p.PeakAbsMgMin {
			prom := prominence(pts, i)
			if prom < p.PeakProminence {
				continue
			}
			id, _ := domain.ContentID(domain.PrefixCandidate, "peak", struct {
				DTG string
				Row int
				V   float64
			}{dtgNode.ID, cur.Row, cur.DTGMgMin})
			peaks = append(peaks, domain.Peak{
				ID: id, Row: cur.Row, SegmentID: cur.SegmentID,
				TMin: cur.TMin, TK: cur.TK, DTGMgMin: cur.DTGMgMin, ProminenceMg: prom,
			})
		}
	}
	sort.Slice(peaks, func(i, j int) bool { return peaks[i].TMin < peaks[j].TMin })
	for i := range peaks {
		for j := i + 1; j < len(peaks); j++ {
			if peaks[j].TMin-peaks[i].TMin <= p.OverlapPeakWindowMin {
				peaks[i].AmbiguousWith = append(peaks[i].AmbiguousWith, peaks[j].ID)
				peaks[j].AmbiguousWith = append(peaks[j].AmbiguousWith, peaks[i].ID)
			}
		}
	}
	payload := domain.PeaksPayload{Peaks: peaks}
	id, err := domain.ContentID(domain.PrefixDerived, string(domain.DKPeak), struct {
		In string
		FP domain.Fingerprint
		P  domain.PeaksPayload
	}{dtgNode.ID, fp, payload})
	if err != nil {
		return nil, err
	}
	return &domain.DerivedNode{
		ID: id, ProjectID: projectID, Kind: domain.DKPeak, BatchID: batchID,
		Fingerprint: fp, InputNodeIDs: []string{dtgNode.ID}, Payload: payload,
	}, nil
}

// prominence is the local drop from neighboring maxima to the trough, a small
// deterministic measure used only for thresholding.
func prominence(pts []domain.DTGPoint, i int) float64 {
	left := pts[i].DTGMgMin
	right := pts[i].DTGMgMin
	for j := i - 1; j >= 0 && pts[j].SegmentID == pts[i].SegmentID; j-- {
		if pts[j].DTGMgMin > left {
			left = pts[j].DTGMgMin
		}
		if pts[j].DTGMgMin >= 0 {
			break
		}
	}
	for j := i + 1; j < len(pts) && pts[j].SegmentID == pts[i].SegmentID; j++ {
		if pts[j].DTGMgMin > right {
			right = pts[j].DTGMgMin
		}
		if pts[j].DTGMgMin >= 0 {
			break
		}
	}
	base := (left + right) / 2
	return base - pts[i].DTGMgMin
}
