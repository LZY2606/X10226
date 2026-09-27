package compute

import (
	"sort"
)

// detectPeaks finds every local minimum of DTG (mass loss rate). Candidates
// below the prominence threshold are kept with status=rejected. Peaks closer
// than the overlap tolerance are grouped: both are retained (status=ambiguous)
// instead of forcing one decomposition, and each references the other.
func detectPeaks(p Params, r *Result) []PeakCandidate {
	var peaks []PeakCandidate
	for _, seg := range r.Segments {
		for i := 1; i+1 < len(seg.Points); i++ {
			c := seg.Points[i]
			if c.DTG < seg.Points[i-1].DTG && c.DTG < seg.Points[i+1].DTG && c.DTG < 0 {
				pk := PeakCandidate{
					SegmentID:    seg.ID,
					AtT:          c.T,
					AtTk:         c.Tk,
					DTG:          round(c.DTG, 6),
					ProminenceOK: -c.DTG >= p.PeakProminence,
					Status:       "proposed",
					Sources:      appendSource(r, seg.ID),
				}
				pk.ID = ID("peak", seg.ID, ftoa(c.T), ftoa(c.DTG))
				if !pk.ProminenceOK {
					pk.Status = "rejected"
					pk.RejectReason = "below prominence threshold"
				}
				peaks = append(peaks, pk)
			}
		}
	}
	groupOverlaps(peaks, p)
	sort.SliceStable(peaks, func(i, j int) bool {
		if peaks[i].SegmentID == peaks[j].SegmentID {
			return peaks[i].AtT < peaks[j].AtT
		}
		return peaks[i].SegmentID < peaks[j].SegmentID
	})
	return peaks
}

// groupOverlaps assigns deterministic overlap groups. In a group of two
// prominent peaks both are ambiguous legal candidates; no single winner.
func groupOverlaps(peaks []PeakCandidate, p Params) {
	var active []*PeakCandidate
	for i := range peaks {
		if !peaks[i].ProminenceOK {
			continue
		}
		cur := &peaks[i]
		var mate *PeakCandidate
		for j := range active {
			cand := active[j]
			if cand.SegmentID != cur.SegmentID {
				continue
			}
			dT := cur.AtTk - cand.AtTk
			if dT < 0 {
				dT = -dT
			}
			if dT <= p.OverlapTolerance {
				mate = cand
				break
			}
		}
		if mate != nil {
			gid := ID("ogroup", mate.ID, cur.ID)
			mate.OverlapGroup = gid
			cur.OverlapGroup = gid
			mate.Status, cur.Status = "ambiguous", "ambiguous"
			mate.AlternativeTo, cur.AlternativeTo = cur.ID, mate.ID
		} else {
			active = append(active, cur)
		}
	}
}

func appendSource(r *Result, ids ...string) []string {
	out := append([]string{}, r.Provenance.DirectSources...)
	return append(out, ids...)
}
