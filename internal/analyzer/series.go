package analyzer

import (
	"math"

	"github.com/example/tga/internal/domain"
)

func fp(v float64) *float64 { return &v }

// interpProgram linearly interpolates furnace setpoint at time t. Outside
// the defined range the nearest endpoint is used (furnace program is fully
// specified; this is not a sample-data interpolation across a gap).
func (f *finder) interpProgram(t float64) *float64 {
	rows := f.snap.Furnace
	if len(rows) == 0 {
		return nil
	}
	if t <= rows[0].T {
		return fp(rows[0].Setpoint)
	}
	if t >= rows[len(rows)-1].T {
		return fp(rows[len(rows)-1].Setpoint)
	}
	for i := 0; i+1 < len(rows); i++ {
		if t >= rows[i].T && t <= rows[i+1].T {
			a, b := rows[i], rows[i+1]
			if b.T == a.T {
				return fp(b.Setpoint)
			}
			r := (t - a.T) / (b.T - a.T)
			return fp(a.Setpoint + r*(b.Setpoint-a.Setpoint))
		}
	}
	return nil
}

// blankAt evaluates the blank-curve correction at time t. Mode "blank" uses
// piecewise-linear blank mass; "linear" uses a least-squares slope anchored
// at the first blank point. Returns 0 when no blank exists.
func (f *finder) blankAt(t float64, slope, intercept float64) (float64, string) {
	mode := f.params.DriftMode
	bp := f.snap.Blank
	if len(bp) == 0 || mode == "none" {
		return 0, "no_blank_correction"
	}
	if mode == "blank" {
		if t <= bp[0].T {
			return bp[0].Mass, "blank:endpoint_clamp"
		}
		if t >= bp[len(bp)-1].T {
			return bp[len(bp)-1].Mass, "blank:endpoint_clamp"
		}
		for i := 0; i+1 < len(bp); i++ {
			if t >= bp[i].T && t <= bp[i+1].T {
				r := (t - bp[i].T) / (bp[i+1].T - bp[i].T)
				return bp[i].Mass + r*(bp[i+1].Mass-bp[i].Mass), "blank:linear_interp"
			}
		}
	}
	return slope*t + intercept, "blank:linear_fit"
}

func (f *finder) fitBlankLinear() (slope, intercept float64) {
	bp := f.snap.Blank
	if len(bp) < 2 {
		if len(bp) == 1 {
			return 0, bp[0].Mass
		}
		return 0, 0
	}
	var sx, sy, sxx, sxy float64
	for _, p := range bp {
		sx += p.T
		sy += p.Mass
		sxx += p.T * p.T
		sxy += p.T * p.Mass
	}
	n := float64(len(bp))
	den := n*sxx - sx*sx
	if den == 0 {
		return 0, sy / n
	}
	slope = (n*sxy - sx*sy) / den
	intercept = (sy - slope*sx) / n
	return slope, intercept
}

// buildSeries creates normalized points, assigns furnace/program and sample
// temperatures, corrects drift, and splits points into segments at gaps.
func (f *finder) buildSeries() ([]domain.Point, [][]int, int) {
	slope, intercept := f.fitBlankLinear()
	points := make([]domain.Point, 0, len(f.snap.Points))

	// Sample-temperature lag as a first-order response to program temperature
	// evaluated at each actual sample time (never at gap-filled times).
	var prevT, prevSample float64
	havePrev := false
	for i, rp := range f.snap.Points {
		pt := domain.Point{
			ID: idWith("pt", rp.T, i),
			FurnaceTimeS: rp.T,
			Segment: 0,
		}
		prog := f.interpProgram(rp.T)
		pt.ProgramTempC = prog
		if rp.HasTemp {
			pt.SampleTempC = fp(rp.SampleTemp)
		} else if prog != nil {
			// derive lagged sample temperature only from program values at
			// real sampling instants
			target := *prog
			if !havePrev {
				prevT = rp.T
				prevSample = target
			} else {
				dt := rp.T - prevT
				if dt > 0 {
					alpha := 1 - math.Exp(-dt/math.Max(f.params.ThermalTauS, 1e-9))
					prevSample = prevSample + alpha*(target-prevSample)
				}
				prevT = rp.T
			}
			havePrev = true
			pt.SampleTempC = fp(prevSample)
			pt.Notes = append(pt.Notes, "sample_temp:derived_from_program_lag")
		} else {
			havePrev = false
		}
		if rp.HasMass {
			raw := rp.Mass
			pt.RawMassMg = fp(raw)
			corr, mode := f.blankAt(rp.T, slope, intercept)
			corrected := raw - corr
			pt.CorrectedMassMg = fp(corrected)
			if mode != "no_blank_correction" {
				pt.Notes = append(pt.Notes, "drift:"+mode)
			}
		}
		if g := f.gasAt(rp.T); g != nil {
			pt.Gas = g.Gas
			pt.GasLabel = g.Label
		}
		points = append(points, pt)
	}

	// Segment splitting at gaps. Only adjacent real samples with a time
	// step greater than the configured threshold define a new segment.
	segments := [][]int{}
	gapCount := 0
	cur := []int{}
	for i := range points {
		if len(cur) > 0 {
			dt := points[i].FurnaceTimeS - points[i-1].FurnaceTimeS
			if dt > f.params.GapMaxStepS {
				points[i-1].GapAfter = true
				points[i-1].DTGBoundaryNote()
				gapCount++
				segments = append(segments, cur)
				cur = []int{}
			}
		}
		cur = append(cur, i)
	}
	if len(cur) > 0 {
		segments = append(segments, cur)
	}
	for si, idxs := range segments {
		for _, i := range idxs {
			points[i].Segment = si
		}
	}
	return points, segments, gapCount
}

// DTGBoundaryNote marks a point whose DTG must not be computed across the
// following gap.
func (p *domain.Point) DTGBoundaryNote() {
	p.Notes = append(p.Notes, "dtg:gap_boundary_one_sided")
}

func (f *finder) gasAt(sampleT float64) *domain.GasWindow {
	wins := f.gasWindows()
	for i := range wins {
		if sampleT >= wins[i].StartS && sampleT < wins[i].EndS {
			return &wins[i]
		}
	}
	return nil
}

// gasWindows shifts valve coordinates to sample-reaction coordinates by the
// configured gas transport delay.
func (f *finder) gasWindows() []domain.GasWindow {
	out := make([]domain.GasWindow, 0, len(f.snap.Valves))
	for i, v := range f.snap.Valves {
		gw := domain.GasWindow{
			ID:          idWith("gas", v.On, v.Off, v.Gas),
			Gas:         v.Gas,
			Label:       gasLabel(v.Gas),
			ValveStartS: v.On,
			ValveEndS:   v.Off,
			StartS:      v.On + f.params.GasDelayS,
			EndS:        v.Off + f.params.GasDelayS,
		}
		_ = i
		out = append(out, gw)
	}
// ensure ordered
	return out
}

func gasLabel(g string) string {
	switch g {
	case "N2", "n2", "nitrogen":
		return "N₂ inert"
	case "air", "Air":
		return "Air (oxidative)"
	case "O2", "o2":
		return "O₂ (oxidative)"
	default:
		return g
	}
}

// furnaceProgram turns furnace rows into ramp/hold program segments.
func (f *finder) furnaceProgram() []domain.ProgramSegment {
	rows := f.snap.Furnace
	if len(rows) == 0 {
		return nil
	}
	out := make([]domain.ProgramSegment, 0, len(rows)-1)
	for i := 0; i+1 < len(rows); i++ {
		a, b := rows[i], rows[i+1]
		kind := "ramp"
		if a.Setpoint == b.Setpoint {
			kind = "hold"
		}
		out = append(out, domain.ProgramSegment{
			ID:       idWith("prog", a.T, b.T),
			Kind:     kind,
			StartS:   a.T,
			EndS:     b.T,
			StartT:   a.Setpoint,
			EndT:     b.Setpoint,
		})
	}
	return out
}
