package compute

import (
	"tgareview/internal/domain"
)

// findPeaks detects DTG mass-loss extrema. Overlapping peaks inside the merge
// window are kept separate (each is a legal candidate); sub-threshold extrema
// are retained with an explicit rejection reason.
func findPeaks(dtg []domain.DTGPoint, corr []domain.CorrPoint, p domain.Params, prov domain.Provenance) []domain.Peak {
	var peaks []domain.Peak
	if len(dtg) < 3 {
		return peaks
	}
	type min struct {
		idx  int
		prom float64
	}
	var mins []min
	for i := 1; i+1 < len(dtg); i++ {
		if dtg[i].Spanning {
			continue
		}
		v := dtg[i].DMgPerS
		if v < 0 && v <= dtg[i-1].DMgPerS && v <= dtg[i+1].DMgPerS {
			left, right := i, i
			for left > 0 && dtg[left-1].DMgPerS < 0 {
				left--
			}
			for right < len(dtg)-1 && dtg[right+1].DMgPerS < 0 {
				right++
			}
			base := 0.0
			if dl, dr := -dtg[left].DMgPerS, -dtg[right].DMgPerS; dl > dr {
				base = dr
			} else {
				base = dl
			}
			mins = append(mins, min{idx: i, prom: -v - base})
		}
	}
	for k, m := range mins {
		t := dtg[m.idx].TimeS
		id := domain.ShortID("peak", f64(t), f64(dtg[m.idx].DMgPerS))
		pk := domain.Peak{
			ID: id, TimeS: t, ReactionTempC: tempAt(corr, t),
			DMgPerS: dtg[m.idx].DMgPerS, Prominence: m.prom,
			Status: domain.StatusCurrent,
			Prov:   withSources(prov, domain.Source{Kind: "derived", ID: id, Detail: "dtg_minimum"}),
		}
		for j, other := range mins {
			if j != k {
				d := dtg[m.idx].TimeS - dtg[other.idx].TimeS
				if d < 0 {
					d = -d
				}
				if d < p.PeakMergeWindowS {
					pk.Prov.NoteCodes = append(pk.Prov.NoteCodes, codeMergedPeaks)
				}
			}
		}
		if m.prom < p.PeakProminenceMgS {
			pk.Rejected = &domain.Rejection{
				Code:    "below_prominence",
				Message: "DTG prominence below configured threshold",
			}
		}
		peaks = append(peaks, pk)
	}
	return peaks
}
