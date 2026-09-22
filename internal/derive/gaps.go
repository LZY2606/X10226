package derive

import (
	"math"
	"sort"

	"tga/internal/domain"
)

// detectGaps flags consecutive valid samples whose spacing exceeds the larger
// of 3x median cadence and the configured threshold (seconds).
func detectGaps(times []float64, valid []bool, thresholdS float64) []domain.Gap {
	var dts []float64
	for i := 1; i < len(times); i++ {
		if valid[i] && valid[i-1] {
			dts = append(dts, times[i]-times[i-1])
		}
	}
	if len(dts) == 0 {
		return nil
	}
	sorted := append([]float64(nil), dts...)
	sort.Float64s(sorted)
	median := sorted[len(sorted)/2]
	limit := median * 3
	if thresholdS > limit {
		limit = thresholdS
	}
	var gaps []domain.Gap
	for i := 1; i < len(times); i++ {
		if !valid[i] || !valid[i-1] {
			// an unknown point itself also breaks continuity for DTG/plots
			if math.IsNaN(times[i]) || math.IsNaN(times[i-1]) {
				gaps = append(gaps, domain.Gap{
					StartIndex: i - 1, EndIndex: i,
					StartTime: safe(times, i-1), EndTime: safe(times, i),
					DurationS: math.NaN(), Reason: "unconvertible point: no interpolation across unknown value",
				})
			}
			continue
		}
		if d := times[i] - times[i-1]; d > limit {
			gaps = append(gaps, domain.Gap{
				StartIndex: i - 1, EndIndex: i,
				StartTime: times[i-1], EndTime: times[i], DurationS: d,
				Reason: "sample spacing exceeds gap threshold; values across are not interpolated",
			})
		}
	}
	return gaps
}

func safe(xs []float64, i int) float64 {
	if i >= 0 && i < len(xs) {
		return xs[i]
	}
	return math.NaN()
}

func isGapBetween(gaps []domain.Gap, a, b int) bool {
	for _, g := range gaps {
		if g.StartIndex == a && g.EndIndex == b {
			return true
		}
	}
	return false
}

func gapRule(gaps []domain.Gap) domain.RuleEvent {
	durs := make([]float64, 0, len(gaps))
	for _, g := range gaps {
		durs = append(durs, round(g.DurationS, 3))
	}
	return domain.RuleEvent{
		Code: domain.RuleGapNoInterpolation, Severity: "warn",
		Message:  "sampling gaps detected; interpolation, DTG and peak fitting never span a gap",
		Evidence: map[string]any{"count": len(gaps), "durations_s": durs},
	}
}

// withinRun reports whether two indices belong to the same contiguous valid
// run with no gap between them.
func withinRun(gaps []domain.Gap, a, b int) bool {
	if a > b {
		a, b = b, a
	}
	for _, g := range gaps {
		if g.StartIndex >= a && g.StartIndex < b {
			return false
		}
	}
	return true
}
