package engine

import (
	"fmt"
	"math"

	"tga-lineage/internal/domain"
)

type peak struct {
	idx         int
	t           float64
	prom        float64 // prominence in mg/min
	height      float64
	startI, endI int
}

// detectPeaks finds DTG minima (mass-loss maxima) within each segment.
// Prominence = height - deepest adjacent "valley floor" on either side.
func detectPeaks(ts, dtgVals []float64, segs []segment, minProm float64) []peak {
	var peaks []peak
	for _, s := range segs {
		// candidate local maxima of -dm/dt
		var cand []int
		for i := s.start + 1; i < s.end; i++ {
			if math.IsNaN(dtgVals[i]) {
				continue
			}
			if dtgVals[i] >= dtgVals[i-1] && dtgVals[i] >= dtgVals[i+1] && dtgVals[i] > 0 {
				cand = append(cand, i)
			}
		}
		for _, ci := range cand {
			h := dtgVals[ci]
			// floor left
			fl := 0.0
			for j := ci - 1; j >= s.start; j-- {
				if math.IsNaN(dtgVals[j]) {
					break
				}
				if dtgVals[j] < fl {
					fl = dtgVals[j]
				}
				if dtgVals[j] <= 0 {
					break
				}
			}
			fr := 0.0
			for j := ci + 1; j <= s.end; j++ {
				if math.IsNaN(dtgVals[j]) {
					break
				}
				if dtgVals[j] < fr {
					fr = dtgVals[j]
				}
				if dtgVals[j] <= 0 {
					break
				}
			}
			base := math.Max(fl, fr)
			prom := h - base
			if prom < minProm {
				continue
			}
			// width: expand while dtg above noise floor (half prominence)
			floor := base + prom*0.5
			li := ci
			for li-1 >= s.start && !math.IsNaN(dtgVals[li-1]) && dtgVals[li-1] > floor {
				li--
			}
			ri := ci
			for ri+1 <= s.end && !math.IsNaN(dtgVals[ri+1]) && dtgVals[ri+1] > floor {
				ri++
			}
			peaks = append(peaks, peak{idx: ci, t: ts[ci], prom: prom, height: h, startI: li, endI: ri})
		}
	}
	return peaks
}

// overlapPair describes two peaks whose windows overlap (a non-unique
// decomposition: either one merged stage or two split stages is legal).
type overlapPair struct {
	a, b peak
}

func findOverlaps(pks []peak) []overlapPair {
	var out []overlapPair
	for i := 0; i < len(pks); i++ {
		for j := i + 1; j < len(pks); j++ {
			a, b := pks[i], pks[j]
			if a.endI >= b.startI && b.endI >= a.startI {
				// saddle between the two peaks is shallow -> genuinely ambiguous
				out = append(out, overlapPair{a: a, b: b})
			}
		}
	}
	return out
}

// stageSpec is one resolved/alternative stage candidate (times in seconds).
type stageSpec struct {
	key          string
	group        string
	label        string
	startT, endT float64
	peakT        float64
	peakI        int
	merged       bool
	gapTouched   bool
	gapRange     [2]float64
	peaks        []int
	rejects      []domain.Rejection
}

func (s stageSpec) stageID(srcTags []string, fp string) string {
	rangeKey := fmt.Sprintf("%.6g_%.6g", s.startT, s.endT)
	if s.merged {
		return nodeID("stage/merged", s.key+"/"+rangeKey, srcTags, fp)
	}
	return nodeID("stage/peak", s.key+"/"+rangeKey, srcTags, fp)
}
