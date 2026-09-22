package tga

import (
	"fmt"
	"math"
	"sort"
)

// ---- sampling gaps --------------------------------------------------------

// Segment is a contiguous run of samples; gaps are never interpolated across.
type Segment struct {
	Start int `json:"start"` // inclusive index into Series
	End   int `json:"end"`   // exclusive index into Series
}

// GapJudgment is the machine-readable record that a gap was found and
// NOT interpolated across.
type GapJudgment struct {
	Rule        string  `json:"rule"` // "gap.no_interpolate"
	GapStartMin float64 `json:"gap_start_min"`
	GapEndMin   float64 `json:"gap_end_min"`
	WidthMin    float64 `json:"width_min"`
}

// SplitSegments splits the series wherever the time step exceeds
// gapThresholdMin. One judgment per gap is returned.
func SplitSegments(s *Series, gapThresholdMin float64) ([]Segment, []GapJudgment) {
	var segs []Segment
	var gaps []GapJudgment
	start := 0
	for i := 1; i < len(s.Time); i++ {
		if s.Time[i]-s.Time[i-1] > gapThresholdMin {
			segs = append(segs, Segment{Start: start, End: i})
			gaps = append(gaps, GapJudgment{
				Rule:        "gap.no_interpolate",
				GapStartMin: s.Time[i-1],
				GapEndMin:   s.Time[i],
				WidthMin:    s.Time[i] - s.Time[i-1],
			})
			start = i
		}
	}
	segs = append(segs, Segment{Start: start, End: len(s.Time)})
	return segs, gaps
}

// ---- balance drift & blank ------------------------------------------------

// ApplyDrift subtracts a linear balance drift (rate mg/min) from mass.
// Returns a corrected copy; the input series is untouched.
func ApplyDrift(s *Series, rateMgPerMin float64) *Series {
	out := &Series{Time: s.Time, Temp: s.Temp, Diags: s.Diags}
	out.Mass = make([]float64, len(s.Mass))
	for i := range s.Mass {
		out.Mass[i] = s.Mass[i] - rateMgPerMin*s.Time[i]
	}
	return out
}

// SubtractBlank interpolates the blank curve (temp °C, mass mg) onto the
// sample temperatures and subtracts it, segment by segment. Sample points
// outside the blank temperature range keep their drift-corrected mass and
// earn a diagnostic; nothing is extrapolated.
func SubtractBlank(s *Series, blankTemp, blankMass []float64, segs []Segment) (*Series, []Diagnostic) {
	out := &Series{Time: s.Time, Temp: s.Temp, Diags: s.Diags}
	out.Mass = make([]float64, len(s.Mass))
	copy(out.Mass, s.Mass)
	var diags []Diagnostic
	if len(blankTemp) < 2 {
		return out, append(diags, Diagnostic{Where: "blank", Message: "blank curve has <2 points; no blank subtraction applied"})
	}
	for _, sg := range segs {
		for i := sg.Start; i < sg.End; i++ {
			t := s.Temp[i]
			if t < blankTemp[0] || t > blankTemp[len(blankTemp)-1] {
				diags = append(diags, Diagnostic{
					Where:   fmt.Sprintf("sample[%d]", i),
					Message: fmt.Sprintf("temp %.2f C outside blank range [%.2f, %.2f]; blank not extrapolated", t, blankTemp[0], blankTemp[len(blankTemp)-1]),
				})
				continue
			}
			out.Mass[i] -= interp(blankTemp, blankMass, t)
		}
	}
	return out, diags
}

// interp linearly interpolates y at x within xs/ys (xs ascending).
func interp(xs, ys []float64, x float64) float64 {
	j := sort.Search(len(xs), func(i int) bool { return xs[i] >= x })
	if j == 0 {
		return ys[0]
	}
	if j >= len(xs) {
		return ys[len(ys)-1]
	}
	x0, x1 := xs[j-1], xs[j]
	y0, y1 := ys[j-1], ys[j]
	if x1 == x0 {
		return y0
	}
	return y0 + (y1-y0)*(x-x0)/(x1-x0)
}

// ---- non-monotonic noise ---------------------------------------------------

// NoiseJudgment records non-monotonic mass behaviour (mass gain during a
// loss experiment) that was preserved rather than smoothed away.
type NoiseJudgment struct {
	Rule      string  `json:"rule"` // "noise.non_monotonic_kept"
	Index     int     `json:"index"`
	TimeMin   float64 `json:"time_min"`
	DeltaMg   float64 `json:"delta_mg"`
	NoiseBand float64 `json:"noise_band_mg"`
}

// DetectNonMonotonic returns judgments for every mass increase larger than
// noiseBandMg. The series itself is never modified.
func DetectNonMonotonic(s *Series, segs []Segment, noiseBandMg float64) []NoiseJudgment {
	var out []NoiseJudgment
	for _, sg := range segs {
		for i := sg.Start + 1; i < sg.End; i++ {
			d := s.Mass[i] - s.Mass[i-1]
			if d > noiseBandMg {
				out = append(out, NoiseJudgment{
					Rule:      "noise.non_monotonic_kept",
					Index:     i,
					TimeMin:   s.Time[i],
					DeltaMg:   d,
					NoiseBand: noiseBandMg,
				})
			}
		}
	}
	return out
}

// ---- DTG --------------------------------------------------------------------

// DTG is the derivative trace, computed per segment (never across gaps).
type DTG struct {
	Temp      []float64 `json:"temp_C"`
	RateMgMin []float64 `json:"dm_dt_mg_min"` // negative during mass loss
	Segments  []Segment `json:"segments"`
}

// ComputeDTG smooths mass with a moving average of half-width win points
// and differentiates against time, segment by segment.
func ComputeDTG(s *Series, segs []Segment, win int) *DTG {
	if win < 1 {
		win = 1
	}
	d := &DTG{Temp: s.Temp, Segments: segs}
	d.RateMgMin = make([]float64, len(s.Time))
	for _, sg := range segs {
		sm := make([]float64, sg.End-sg.Start)
		for i := sg.Start; i < sg.End; i++ {
			lo := max(i-win, sg.Start)
			hi := min(i+win, sg.End-1)
			sum := 0.0
			for k := lo; k <= hi; k++ {
				sum += s.Mass[k]
			}
			sm[i-sg.Start] = sum / float64(hi-lo+1)
		}
		for i := sg.Start + 1; i < sg.End-1; i++ {
			dt := s.Time[i+1] - s.Time[i-1]
			if dt <= 0 {
				continue
			}
			d.RateMgMin[i] = (sm[i+1-sg.Start] - sm[i-1-sg.Start]) / dt
		}
		if sg.End-sg.Start >= 2 {
			d.RateMgMin[sg.Start] = d.RateMgMin[sg.Start+1]
			d.RateMgMin[sg.End-1] = d.RateMgMin[sg.End-2]
		}
	}
	return d
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = math.Abs
