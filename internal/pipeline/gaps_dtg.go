package pipeline

import (
	"math"

	"tgaaudit/internal/domain"
)

// buildGaps detects regions that must never be bridged: consecutive missing
// sample rows, or time jumps wider than the configured threshold. Valid
// points are numbered into segments; DTG is computed per segment.
func (d *Derivation) buildGaps(p domain.Params) {
	pts := d.Corrected.Points
	segOf := make([]int, len(pts))
	for i := range segOf {
		segOf[i] = -1
	}
	var gaps []Gap
	segID := -1
	prevValid := -1
	missingRun := 0
	for i := range pts {
		if pts[i].Missing {
			missingRun++
			continue
		}
		newSegment := prevValid < 0
		if prevValid >= 0 {
			switch {
			case missingRun > 0:
				gaps = append(gaps, Gap{Index: len(gaps),
					FromT: pts[prevValid].TS, ToT: pts[i].TS,
					Reason: "missing_sample_rows", SpansMissingRows: missingRun})
				newSegment = true
			case pts[i].TS-pts[prevValid].TS > p.GapThresholdS:
				gaps = append(gaps, Gap{Index: len(gaps),
					FromT: pts[prevValid].TS, ToT: pts[i].TS,
					Reason: "sampling_time_jump"})
				newSegment = true
			}
		}
		if newSegment {
			segID++
		}
		segOf[i] = segID
		prevValid = i
		missingRun = 0
	}
	if missingRun > 0 && prevValid >= 0 {
		gaps = append(gaps, Gap{Index: len(gaps), FromT: pts[prevValid].TS,
			ToT:    pts[len(pts)-1].TS,
			Reason: "missing_sample_rows_trailing", SpansMissingRows: missingRun})
	}
	d.Gaps = gaps
	d.segOf = segOf
}

// buildDtg computes central-difference and time-weighted smoothed DTG inside
// each segment. Rates stay null across gaps; nothing is interpolated.
func (d *Derivation) buildDtg(p domain.Params) {
	pts := d.Corrected.Points
	raw := make([]*float64, len(pts))
	smooth := make([]*float64, len(pts))
	for i := range pts {
		if pts[i].Missing || pts[i].MassFraction == nil {
			continue
		}
		l := d.nearestValid(i, -1)
		r := d.nearestValid(i, +1)
		if l >= 0 && r >= 0 {
			dt := pts[r].TS - pts[l].TS
			if dt > 0 {
				v := (*pts[r].MassFraction - *pts[l].MassFraction) / dt
				raw[i] = &v
			}
		}
	}
	hw := p.SmoothWindowS / 2
	for i := range pts {
		if raw[i] == nil {
			continue
		}
		var sum, wsum float64
		for j := range pts {
			if d.segOf[j] != d.segOf[i] || raw[j] == nil {
				continue
			}
			dt := math.Abs(pts[j].TS - pts[i].TS)
			if dt <= hw {
				w := 1 - dt/(hw+1e-9)
				sum += w * *raw[j]
				wsum += w
			}
		}
		if wsum > 0 {
			v := sum / wsum
			smooth[i] = &v
		}
	}
	out := make([]DtgPoint, 0, len(pts))
	for i := range pts {
		out = append(out, DtgPoint{I: i, TS: pts[i].TS, Smooth: smooth[i],
			Raw: raw[i], Segment: d.segOf[i]})
	}
	d.dtgPoints = out
}

func (d *Derivation) nearestValid(i, dir int) int {
	seg := d.segOf[i]
	pts := d.Corrected.Points
	for j := i + dir; j >= 0 && j < len(pts); j += dir {
		if d.segOf[j] != seg {
			return -1
		}
		if !pts[j].Missing && pts[j].MassFraction != nil {
			return j
		}
	}
	return -1
}
