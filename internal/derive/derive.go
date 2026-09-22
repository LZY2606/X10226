package derive

import (
	"math"
	"sort"

	"tga-archive/internal/domain"
)

// Run performs every derivation from current inputs. It never fails silently:
// missing layers produce diagnostics and "unknown" residues instead of zeroes.
func Run(in Inputs, p domain.Params, paramVersion string) Report {
	r := Report{
		AlgorithmVersion: AlgorithmVersion,
		ParamVersion:     paramVersion,
		Params:           p,
		SourceFingerprints: map[string]string{},
	}
	remember := func(layer string, sp *domain.SourcePayload) {
		if sp != nil {
			r.SourceFingerprints[layer] = sp.SourceID
		}
	}
	remember(domain.LayerSamples, in.Samples)
	remember(domain.LayerDrift, in.Drift)
	remember(domain.LayerFurnace, in.Furnace)
	remember(domain.LayerGas, in.Valves)
	remember(domain.LayerBlank, in.Blank)
	remember(domain.LayerInitialMass, in.InitialMass)

	if in.Samples == nil || len(in.Samples.Samples) == 0 {
			r.Diagnostics = append(r.Diagnostics, domain.Diagnostic{Severity: domain.SevError, Code: "no_samples", Layer: domain.LayerSamples, Message: "cannot derive without raw samples"})
			r.Residues = append(r.Residues, unknownFinalResidue(nil))
		return r
	}

	basis := resolveBasis(in, &r)
	r.Gaps = append(r.Gaps, in.Samples.Gaps...)

	blank := buildBlank(in.Blank, p.BlankExtrapolate, &r)
	drift := in.Drift
	r.Points = correctSeries(in.Samples.Samples, in.Samples.Gaps, drift, blank, p, &r)
	r.Peaks = findPeaks(r.Points, p)
	r.Candidates = buildCandidates(r.Points, r.Peaks, in, p, basis, paramVersion)
	r.Windows = buildWindows(in.Valves, in.Furnace, r.Points, p)
	r.Residues = buildResidues(r.Points, basis, r.Candidates, in, p)
	return r
}

func resolveBasis(in Inputs, r *Report) Basis {
	if in.InitialMass != nil && in.InitialMass.InitialMass != nil {
		im := in.InitialMass.InitialMass
		return Basis{Mass: im.Mass, At: im.At, Note: "revised initial mass " + in.InitialMass.SourceID}
	}
	// Fall back to the first valid sample mass (and record the assumption).
	for _, s := range in.Samples.Samples {
		if s.Mass.Valid && s.Time.Valid {
			r.Diagnostics = append(r.Diagnostics, domain.Diagnostic{Severity: domain.SevWarning, Code: "assumed_initial_mass", Layer: domain.LayerInitialMass,
				Message: "no initial_mass layer; using first valid sample mass as basis"})
			return Basis{Mass: s.Mass, At: s.Time, Note: "first valid sample mass"}
		}
	}
	r.Diagnostics = append(r.Diagnostics, domain.Diagnostic{Severity: domain.SevError, Code: "no_basis", Layer: domain.LayerInitialMass, Message: "no usable initial mass"})
	return Basis{Mass: invalid(domain.UnitMg), At: invalid(domain.UnitSecond), Note: "unknown"}
}

func invalid(unit string) domain.Measure {
	return domain.Measure{Value: math.NaN(), Unit: unit, Valid: false}
}

// correctSeries applies drift correction then blank subtraction, and computes
// DTG only over contiguous valid segments (never across a declared gap).
func correctSeries(samples []domain.RawSample, gaps []domain.Gap, drift, blankSP *domain.SourcePayload, p domain.Params, r *Report) []Point {
	blankPoints := blankCurve(blankSP)
	pts := make([]Point, 0, len(samples))
	prevValid := -1
	for i, s := range samples {
		pt := Point{
			Time:    s.Time,
			Temp:    s.Temp,
			MassRaw: s.Mass,
			DTG:     invalid(domain.UnitMgPerS),
			Flags:   s.Flags,
		}
		if !s.Time.Valid || !s.Mass.Valid {
			// keep invalid measures in place; never invent a value
			pt.MassCorr = invalid(domain.UnitMg)
			pt.GapBefore = prevValid >= 0
			pts = append(pts, pt)
			prevValid = -1
			continue
		}
		t := s.Time.Value
		gapBefore := i > 0 && isGapBoundary(gaps, samples, i, t)
		if gapBefore {
			pt.GapBefore = true
		}
		corr := s.Mass.Value
		chain := []string{}
		if drift != nil && drift.Drift != nil {
			anchor := 0.0
			if drift.Drift.AnchorAt.Valid {
				anchor = drift.Drift.AnchorAt.Value
			}
			d := drift.Drift.Rate.Value * (t - anchor)
			corr -= d
			chain = append(chain, "drift")
		}
		if b, ok := blankAt(blankPoints, t, p.BlankExtrapolate); ok {
			corr -= b
			chain = append(chain, "blank")
		}
		pt.MassCorr = domain.Measure{Value: corr, Unit: domain.UnitMg, Valid: true}
		if len(chain) > 0 && pt.Flags == nil {
			pt.Flags = append(pt.Flags, "corrected:"+joinComma(chain))
		} else if len(chain) > 0 {
			pt.Flags = append(pt.Flags, "corrected:"+joinComma(chain))
		}
		// DTG from the previous valid contiguous neighbour
		if !gapBefore && prevValid >= 0 {
			pv := pts[prevValid]
			dt := t - pv.Time.Value
			if dt > 0 {
				slope := (corr - pv.MassCorr.Value) / dt
				if math.Abs(slope) < p.NoiseThresholdMgS && nonMonotoneNeighbourhood(pts, prevValid, corr, t) {
					// non-monotonic noise inside the noise band: keep slope
					// visible but flagged so candidates do not promote it
					pt.Flags = append(pt.Flags, "nonmonotonic_noise")
				}
				pt.DTG = domain.Measure{Value: slope, Unit: domain.UnitMgPerS, Valid: true}
			}
		}
		pts = append(pts, pt)
		prevValid = i
	}
	return pts
}

func nonMonotoneNeighbourhood(pts []Point, prev int, curMass, curTime float64) bool {
	if prev < 1 {
		return false
	}
	a := pts[prev-1].MassCorr.Value
	b := pts[prev].MassCorr.Value
	if !(a > b && b < curMass) && !(a < b && b > curMass) {
		return false
	}
	return true
}

func isGapBoundary(gaps []domain.Gap, samples []domain.RawSample, i int, t float64) bool {
	if i == 0 {
		return false
	}
	for _, g := range gaps {
		if !g.From.Valid || !g.To.Valid {
			continue
		}
		prev := samples[i-1].Time.Value
		if prev <= g.From.Value+1e-9 && t >= g.To.Value-1e-9 {
			return true
		}
	}
	return false
}

func joinComma(xs []string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}

// findPeaks detects DTG minima (mass-loss peaks are negative) and clusters
// peaks closer than MinPeakSeparationS (overlapping / shoulder peaks).
func findPeaks(pts []Point, p domain.Params) []Peak {
	var peaks []Peak
	w := p.DTGHalfWindow
	for i := 1; i < len(pts)-1; i++ {
		pt := pts[i]
		if !pt.DTG.Valid || !pt.Time.Valid || !pt.Temp.Valid {
			continue
		}
		// smoothed value via symmetric window over contiguous valid points
		sv, ok := smoothDTG(pts, i, w)
		if !ok {
			continue
		}
		if sv > -p.PeakProminenceMgS {
			continue
		}
		if sv > smoothOrInf(pts, i-1, w) || sv > smoothOrInf(pts, i+1, w) {
			continue
		}
		peaks = append(peaks, Peak{At: i, Time: pt.Time, Temp: pt.Temp, DTG: measure(sv, domain.UnitMgPerS)})
	}
	// cluster close peaks
	sort.Slice(peaks, func(a, b int) bool { return peaks[a].Time.Value < peaks[b].Time.Value })
	cluster := -1
	var lastT float64
	for i := range peaks {
		if cluster < 0 || peaks[i].Time.Value-lastT > p.MinPeakSeparationS {
			cluster++
		}
		peaks[i].Cluster = cluster
		lastT = peaks[i].Time.Value
	}
	return peaks
}

func smoothDTG(pts []Point, i, w int) (float64, bool) {
	sum, n := 0.0, 0
	for j := i - w; j <= i+w; j++ {
		if j < 0 || j >= len(pts) || !pts[j].DTG.Valid {
			return 0, false
		}
		sum += pts[j].DTG.Value
		n++
	}
	return sum / float64(n), true
}

func smoothOrInf(pts []Point, i, w int) float64 {
	v, ok := smoothDTG(pts, i, w)
	if !ok {
		return math.Inf(1)
	}
	return v
}

func measure(v float64, unit string) domain.Measure {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return invalid(unit)
	}
	return domain.Measure{Value: v, Unit: unit, Valid: true}
}
