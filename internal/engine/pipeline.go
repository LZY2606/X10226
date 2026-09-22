package engine

import (
	"fmt"
	"math"
	"sort"

	"tga-review/internal/domain"
	"tga-review/internal/ident"
)

// differentiate builds the correction view and a smoothed DTG. DTG across a
// sampling gap is never produced by interpolation: the two points touching the
// gap stay present (for plotting raw data) but carry UNCERTAIN confidence and
// are excluded from peak/stage use.
func (e *Engine) differentiate(pts []*point, in Inputs, add findingAdder) ([]domain.CorrectionPoint, []domain.DTGPoint) {
	corr := make([]domain.CorrectionPoint, 0, len(pts))
	dtg := make([]domain.DTGPoint, 0, len(pts))
	usableIdx := []int{}
	for i, p := range pts {
		cp := domain.CorrectionPoint{
			ID:          "cor-" + ident.Short(fmt.Sprintf("%s|cor|%d", in.RunID, i)),
			SampleID:    p.id,
			T:           p.t,
			Temp:        p.temp,
			RawMass:     p.rawMass,
			BlankMass:   p.blankMass,
			DriftOffset: p.driftOff,
			CorrMass:    p.corrMass,
			AcrossGap:   p.gapBefore,
			Usable:      p.usable,
		}
		corr = append(corr, cp)
		if p.usable {
			usableIdx = append(usableIdx, i)
		}
	}
	// mark non-monotone perturbations from raw (positive steps in a lossy
	// neighbourhood). They remain in place; DTG confidence is downgraded.
	for k := 1; k+1 < len(usableIdx); k++ {
		a := pts[usableIdx[k-1]]
		b := pts[usableIdx[k]]
		c := pts[usableIdx[k+1]]
		if b.corrMass > a.corrMass+1e-9 && c.corrMass < b.corrMass {
			b.nonMono = true
		}
	}
	// raw DTG over adjacent usable points only (gap adjacency flagged)
	raw := make([]float64, len(usableIdx))
	gapTouch := make([]bool, len(usableIdx))
	for k, pi := range usableIdx {
		raw[k] = math.NaN()
		if k == 0 {
			continue
		}
		pa := pts[usableIdx[k-1]]
		pb := pts[pi]
		if pb.gapBefore {
			gapTouch[k-1], gapTouch[k] = true, true
			add("GAP_NO_INTERPOLATION", "warning",
				fmt.Sprintf("sampling gap %.0fs -> %.0fs: DTG not interpolated across it", pa.t, pb.t),
				"", pa.id, pb.id)
			continue
		}
		dtMin := (pb.t - pa.t) / 60.0
		if dtMin > 0 {
			raw[k] = (pb.corrMass - pa.corrMass) / dtMin
		}
	}
	w := in.Params.SmoothingWindow
	for k, pi := range usableIdx {
		p := pts[pi]
		var sum float64
		var n int
		from := k - w/2
		to := from + w - 1
		if from < 0 {
			to += -from
			from = 0
		}
		if to >= len(usableIdx) {
			from -= to - len(usableIdx) + 1
			to = len(usableIdx) - 1
		}
		if from < 0 {
			from = 0
		}
		for j := from; j <= to; j++ {
			if !math.IsNaN(raw[j]) && !gapTouch[j] {
				sum += raw[j]
				n++
			}
		}
		conf := "OK"
		if gapTouch[k] || p.nonMono {
			conf = "UNCERTAIN"
			if p.nonMono {
				add("NONMONOTONE_NOISE", "warning",
					fmt.Sprintf("non-monotone mass perturbation at t=%.0fs kept, DTG downgraded", p.t), p.id, p.id)
			}
		}
		d := math.NaN()
		if n > 0 {
			d = sum / float64(n)
		}
		p.dtg, p.confidence = d, conf
		dtg = append(dtg, domain.DTGPoint{
			ID:           "dtg-" + ident.Short(fmt.Sprintf("%s|dtg|%d", in.RunID, k)),
			CorrectionID: corr[pi].ID,
			T:            p.t,
			Temp:         p.temp,
			Mass:         p.corrMass,
			DTG:          d,
			Confidence:   conf,
			AcrossGap:    gapTouch[k],
		})
	}
	return corr, dtg
}

// detectPeaks finds confident DTG minima and decides whether each is uniquely
// attributable or sits in an overlapping window with another minimum.
func (e *Engine) detectPeaks(dtg []domain.DTGPoint, in Inputs, add findingAdder) []domain.Peak {
	var confident []domain.DTGPoint
	for _, d := range dtg {
		if d.Confidence == "OK" && !math.IsNaN(d.DTG) {
			confident = append(confident, d)
		}
	}
	var mins []domain.DTGPoint
	for i := 1; i+1 < len(confident); i++ {
		if confident[i].DTG < confident[i-1].DTG && confident[i].DTG < confident[i+1].DTG {
			mins = append(mins, confident[i])
		}
	}
	if len(confident) > 0 && len(mins) == 0 {
		j := 0
		for i := range confident {
			if confident[i].DTG < confident[j].DTG {
				j = i
			}
		}
		mins = []domain.DTGPoint{confident[j]}
	}
	peaks := make([]domain.Peak, 0, len(mins))
	for i, m := range mins {
		prom := prominence(confident, m)
		attr := "UNIQUE"
		overlap := false
		for j, o := range mins {
			if i == j {
				continue
			}
			dt := math.Abs(m.T-o.T)
			if dt < in.Params.OverlapWindowS {
				overlap = true
			}
			// ambiguous attribution: two comparable minima close together
			if dt < in.Params.OverlapWindowS*2 && math.Abs(m.DTG-o.DTG) < 0.6*math.Max(-m.DTG, -o.DTG) {
				attr = "AMBIGUOUS"
			}
		}
		statusNote := ""
		if prom < in.Params.PeakProminenceMgMin {
			statusNote = " (below prominence threshold)"
		}
		if statusNote != "" {
			add("WEAK_PEAK", "info", fmt.Sprintf("dtg minimum at %.0f degC%s", m.Temp, statusNote), "", m.ID)
		}
		peaks = append(peaks, domain.Peak{
			ID:          "pk-" + ident.Short(fmt.Sprintf("%s|pk|%d", in.RunID, i)),
			T:           m.T,
			Temp:        m.Temp,
			DTG:         m.DTG,
			Prominence:  prom,
			Overlap:     overlap,
			Attribution: attr,
		})
	}
	if len(peaks) >= 2 {
		add("OVERLAPPING_PEAKS", "info",
			fmt.Sprintf("%d DTG minima detected; overlapping legal candidates retained", len(peaks)), "")
	}
	sort.Slice(peaks, func(i, j int) bool { return peaks[i].T < peaks[j].T })
	return peaks
}

func prominence(curve []domain.DTGPoint, m domain.DTGPoint) float64 {
	left, right := 0.0, 0.0
	found := false
	for _, c := range curve {
		if c.T == m.T {
			found = true
			continue
		}
		if !found {
			if c.DTG > left {
				left = c.DTG
			}
		} else if c.DTG > right {
			right = c.DTG
		}
	}
	base := math.Max(left, right)
	return base - m.DTG
}
