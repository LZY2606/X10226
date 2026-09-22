package derive

import (
	"fmt"
	"math"

	"tga/internal/domain"
)

// findPeaks locates negative DTG extrema (mass loss). Overlapping peaks within
// a prominence-dependent proximity are linked rather than merged, because
// decomposition stages may overlap.
func findPeaks(p domain.Params, s domain.NormalizedSeries) ([]domain.Peak, []domain.RuleEvent) {
	var peaks []domain.Peak
	for i := 1; i < len(s.DTG)-1; i++ {
		if math.IsNaN(s.DTG[i]) {
			continue
		}
		if s.DTG[i] < s.DTG[i-1] && s.DTG[i] <= s.DTG[i+1] && s.DTG[i] < 0 {
			prom := peakProminence(s.DTG, i)
			if prom < p.Prominence {
				continue
			}
			peaks = append(peaks, domain.Peak{
				ID:         fmt.Sprintf("pk:%d", len(peaks)),
				Time:       round(s.Times[i], 6),
				TempC:      round(s.TempC[i], 3),
				DTG:        round(s.DTG[i], 6),
				Prominence: round(prom, 6),
			})
		}
	}
	// Link overlapping peaks: peaks whose local basins overlap (distance less
	// than the larger peak's half-width proxy = 5 cadences) stay separate but
	// cross-reference each other.
	if len(peaks) > 1 {
		idxByID := map[string]int{}
		times := make([]float64, len(peaks))
		for i := range peaks {
			idxByID[peaks[i].ID] = i
			times[i] = peaks[i].Time
		}
		med := medianCadence(s.Times)
		width := med * 5
		var overlapEv bool
		for i := 0; i < len(peaks); i++ {
			for j := i + 1; j < len(peaks); j++ {
				if math.Abs(times[j]-times[i]) <= width {
					peaks[i].OverlapIDs = append(peaks[i].OverlapIDs, peaks[j].ID)
					peaks[j].OverlapIDs = append(peaks[j].OverlapIDs, peaks[i].ID)
					overlapEv = true
				}
			}
		}
		if overlapEv {
			return peaks, []domain.RuleEvent{{
				Code: domain.RuleOverlapPeaks, Severity: "info",
				Message:  "overlapping DTG peaks kept as separate linked peaks; no forced single-phase merge",
				Evidence: map[string]any{"peak_count": len(peaks)},
			}}
		}
	}
	return peaks, nil
}

func peakProminence(dtg []float64, i int) float64 {
	// prominence relative to the higher of the two nearest shoulders (values
	// closer to zero), bounded to contiguous non-NaN neighborhood.
	left := dtg[i]
	for j := i - 1; j >= 0 && !math.IsNaN(dtg[j]); j-- {
		if dtg[j] > left {
			left = dtg[j]
		}
		if dtg[j] >= 0 {
			break
		}
	}
	right := dtg[i]
	for j := i + 1; j < len(dtg) && !math.IsNaN(dtg[j]); j++ {
		if dtg[j] > right {
			right = dtg[j]
		}
		if dtg[j] >= 0 {
			break
		}
	}
	shoulder := math.Max(left, right)
	return math.Abs(dtg[i] - shoulder)
}

func medianCadence(times []float64) float64 {
	var d []float64
	for i := 1; i < len(times); i++ {
		if !math.IsNaN(times[i]) && !math.IsNaN(times[i-1]) {
			d = append(d, times[i]-times[i-1])
		}
	}
	if len(d) == 0 {
		return 1
	}
	for i := 0; i < len(d); i++ {
		for j := i + 1; j < len(d); j++ {
			if d[j] < d[i] {
				d[i], d[j] = d[j], d[i]
			}
		}
	}
	return d[len(d)/2]
}
