package engine

import (
	"math"

	"tga-review/internal/ident"
	"tga-review/internal/types"
)

// computeDTG builds DTG (%/min vs sample temperature in degC) from corrected
// mass. Differences are never taken across a flagged sampling gap.
func (w *work) computeDTG(pts []corrPoint) types.Series {
	half := int(w.p["dtg_window_pts"])
	if half < 1 {
		half = 1
	}
	ref := w.initialRefGuess()
	s := types.Series{ID: w.run.ID + ":dtg", Name: "DTG",
		XUnit: units_c, YUnit: units_pctm}
	for i := range pts {
		l, r := i-half, i+half
		if l < 0 {
			l = i
		}
		if r > len(pts)-1 {
			r = i
		}
		var v float64
		bridged := false
		if r > l && ref > 0 && !gapBetween(pts, l, r) {
			dmPct := (pts[r].CorrMass - pts[l].CorrMass) / ref * 100
			dtMin := (pts[r].T - pts[l].T) / 60.0
			if dtMin > 0 {
				v = dmPct / dtMin
			}
		} else if r != l {
			bridged = true
		}
		flags := ""
		if bridged {
			flags = "gap:no_derivative"
		}
		if pts[i].NonMonotonic {
			flags = appendFlag(flags, "nonmonotonic")
		}
		s.Points = append(s.Points, types.Point{
			ID:    ident.PointID(w.run.ID, "dtg", i),
			T:     pts[i].T,
			X:     pts[i].Temp,
			Y:     v,
			Gap:   pts[i].Gap,
			Flags: flags,
		})
	}
	return s
}

func gapBetween(pts []corrPoint, l, r int) bool {
	for i := l + 1; i <= r; i++ {
		if pts[i].Gap {
			return true
		}
	}
	return false
}

// initialRefGuess is the pre-analysis reference: mean corrected mass of the
// first stable points. The official reference is decided in analyzeInitialMass.
func (w *work) initialRefGuess() float64 {
	if len(w.corr) == 0 {
		return 1
	}
	n := 3
	if n > len(w.corr) {
		n = len(w.corr)
	}
	var sum float64
	for i := 0; i < n; i++ {
		sum += w.corr[i].CorrMass
	}
	if sum/float64(n) <= 0 {
		return 1
	}
	return sum / float64(n)
}

// computeProgramSeries renders programmed temperature vs time and the
// lag-adjusted sample estimate as a separate curve.
func (w *work) computeProgramSeries() types.Series {
	prog := types.Series{ID: w.run.ID + ":program", Name: "furnace_program",
		XUnit: units_s, YUnit: units_c}
	sample := types.Series{ID: w.run.ID + ":sample_temp_est", Name: "sample_temperature_estimate",
		XUnit: units_s, YUnit: units_c}
	lag := w.p["temp_lag_s"]
	idx := 0
	for _, seg := range w.n.program {
		for _, f := range []float64{seg.T0, seg.T1} {
			tProg := f
			temp := programTempAt(w.n.program, tProg)
			prog.Points = append(prog.Points, types.Point{
				ID: ident.PointID(w.run.ID, "program", idx),
				T:  tProg, X: tProg, Y: temp,
			})
			sample.Points = append(sample.Points, types.Point{
				ID: ident.PointID(w.run.ID, "sample_temp_est", idx),
				T:  tProg, X: tProg, Y: programTempAt(w.n.program, math.Max(0, tProg-lag)),
			})
			idx++
		}
	}
	w.programSampleSeries = sample
	return prog
}

func programTempAt(segs []segment, t float64) float64 {
	if len(segs) == 0 {
		return 0
	}
	if t <= segs[0].T0 {
		return segs[0].Temp0
	}
	for _, s := range segs {
		if t >= s.T0 && t <= s.T1 {
			if s.T1 == s.T0 {
				return s.Temp1
			}
			f := (t - s.T0) / (s.T1 - s.T0)
			return s.Temp0 + f*(s.Temp1-s.Temp0)
		}
	}
	return segs[len(segs)-1].Temp1
}

// computeGasSeries renders valve times and the delay-adjusted times at which
// the new atmosphere actually reaches the sample.
func (w *work) computeGasSeries() types.Series {
	valve := types.Series{ID: w.run.ID + ":valve", Name: "valve_events",
		XUnit: units_s, YUnit: units_c}
	reach := types.Series{ID: w.run.ID + ":atmos_reach", Name: "atmosphere_reaches_sample",
		XUnit: units_s, YUnit: units_c}
	delay := w.p["atmos_delay_s"]
	for i, g := range w.n.gas {
		valve.Points = append(valve.Points, types.Point{
			ID: ident.PointID(w.run.ID, "valve", i), T: g.T, X: g.T, Y: 0,
		})
		reach.Points = append(reach.Points, types.Point{
			ID: ident.PointID(w.run.ID, "atmos_reach", i), T: g.T, X: g.T + delay, Y: 0,
		})
	}
	w.atmosReachSeries = reach
	return valve
}
