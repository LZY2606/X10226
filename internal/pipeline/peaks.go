package pipeline

import "tgaaudit/internal/domain"

func (d *Derivation) detectPeaks(p domain.Params) {
	pts := d.Corrected.Points
	val := func(i int) float64 {
		for _, q := range d.dtgPoints {
			if q.I == i && q.Smooth != nil {
				return *q.Smooth
			}
		}
		return 0
	}
	smoothAt := map[int]float64{}
	for _, q := range d.dtgPoints {
		if q.Smooth != nil {
			smoothAt[q.I] = *q.Smooth
		}
	}
	_ = val

	find := func(minSepS, minProm float64) PeakSet {
		var minima []Peak
		for i := range pts {
			v, ok := smoothAt[i]
			if !ok || v > -p.MinPeakRate {
				continue
			}
			l := d.nearestValid(i, -1)
			r := d.nearestValid(i, +1)
			if l < 0 || r < 0 {
				continue
			}
			if v >= smoothAt[l] || v >= smoothAt[r] {
				continue
			}
			// prominence: lowest of the two enclosing shoulders minus the
			// minimum, measured within +/- 3*minSep window.
			leftMax, rightMax := v, v
			for j := i; j >= 0 && d.segOf[j] == d.segOf[i]; j-- {
				if w, ok2 := smoothAt[j]; ok2 {
					if w > leftMax {
						leftMax = w
					}
					if pts[i].TS-pts[j].TS > 3*minSepS {
						break
					}
					if w > v+p.MinPeakRate {
						break
					}
				}
			}
			for j := i; j < len(pts) && d.segOf[j] == d.segOf[i]; j++ {
				if w, ok2 := smoothAt[j]; ok2 {
					if w > rightMax {
						rightMax = w
					}
					if pts[j].TS-pts[i].TS > 3*minSepS {
						break
					}
					if w > v+p.MinPeakRate {
						break
					}
				}
			}
			prom := minF(leftMax, rightMax) - v
			if prom < minProm {
				continue
			}
			leftT, rightT := d.peakBounds(i, v)
			temp := 0.0
			if pts[i].SampleTempK != nil {
				temp = *pts[i].SampleTempK
			} else if pts[i].FurnaceTempK != nil {
				temp = *pts[i].FurnaceTempK
			}
			minima = append(minima, Peak{Index: i, TS: pts[i].TS, Rate: v,
				SampleTempK: temp, Prominence: prom, LeftT: leftT, RightT: rightT})
		}
		// enforce minimum separation, dropping the weaker peak of a close pair
		kept := mergeSeparation(minima, pts, minSepS)
		return PeakSet{Peaks: kept}
	}

	h0 := find(p.PeakMinSepS, p.PeakProminence)
	h0.Hypothesis = "h0"
	h0.Label = "permissive_shoulders"
	h1 := find(2*p.PeakMinSepS, 2*p.PeakProminence)
	h1.Hypothesis = "h1"
	h1.Label = "conservative_merged"
	if len(h1.Peaks) < len(h0.Peaks) {
		h1.MergedNote = "close shoulder peaks merged under conservative thresholds; alternatives retained in h0"
	}
	d.PeakSets = []PeakSet{h0, h1}
}

// peakBounds walks out from the minimum until the smoothed rate crosses back
// above -MinPeakRate (or the segment ends). Bounds never cross gaps.
func (d *Derivation) peakBounds(i int, vmin float64) (float64, float64) {
	pts := d.Corrected.Points
	threshold := -0.0 // crossing zero
	_ = threshold
	smoothAt := map[int]float64{}
	for _, q := range d.dtgPoints {
		if q.Smooth != nil {
			smoothAt[q.I] = *q.Smooth
		}
	}
	leftT, rightT := pts[i].TS, pts[i].TS
	for j := i; j >= 0 && d.segOf[j] == d.segOf[i]; j-- {
		if w, ok := smoothAt[j]; ok && w >= 0 {
			leftT = pts[j].TS
			break
		}
		leftT = pts[j].TS
	}
	for j := i; j < len(pts) && d.segOf[j] == d.segOf[i]; j++ {
		if w, ok := smoothAt[j]; ok && w >= 0 {
			rightT = pts[j].TS
			break
		}
		rightT = pts[j].TS
	}
	return leftT, rightT
}

func mergeSeparation(peaks []Peak, pts []Point, minSepS float64) []Peak {
	// sort by time then greedily keep higher prominence when too close
	out := append([]Peak(nil), peaks...)
	// peaks discovered in index order already, but sort for safety
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if absF(out[j].TS-out[i].TS) < minSepS {
				if out[j].Prominence > out[i].Prominence {
					out[i] = out[j]
				}
				out = append(out[:j], out[j+1:]...)
				j--
			}
		}
	}
	return out
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func absF(a float64) float64 {
	if a < 0 {
		return -a
	}
	return a
}

var _ = domain.StatusUnknown
