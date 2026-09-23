package algo

import (
	"math"
	"sort"

	"tgalineage/internal/model"
)

// computeDTG fills DtgPctC as percent-of-initial-mass per degree C on the
// smoothed mass series. Gaps are never crossed (the endpoint after a gap gets
// 0 and is marked, rather than a fabricated slope).
func computeDTG(pts []Point, gaps []Gap, sm []float64, m0 float64) {
	smoothT := make([]float64, len(pts))
	copy(smoothT, sm)
	gapAfter := map[string]bool{}
	for _, g := range gaps {
		gapAfter[g.BeforeID] = true
	}
	for i := 1; i < len(pts)-1; i++ {
		if gapAfter[pts[i-1].ID] {
			pts[i].DtgPctC = 0
			continue
		}
		tL, tR := pts[i-1].TempC, pts[i+1].TempC
		mL, mR := smoothT[i-1], smoothT[i+1]
		if tR > tL {
			pts[i].DtgPctC = -((mR - mL) / m0) * 100 / (tR - tL)
			continue
		}
		// temperature channel missing: use time basis and flag
		sL, sR := pts[i-1].TS, pts[i+1].TS
		if sR > sL {
			pts[i].DtgPctC = -((mR - mL) / m0) * 100 / ((sR - sL) / 60.0) // %/min proxy
			pts[i].Flag = joinFlag(pts[i].Flag, "dtg-time-basis")
		}
	}
}

func joinFlag(a, b string) string {
	if a == "" {
		return b
	}
	return a + ";" + b
}

// furnaceTrajectory simulates the program: each ramp changes T at rate,
// isothermal holds T. Coordinate basis: seconds from run start, deg C.
func furnaceTrajectory(fp model.FurnaceProgram, endS float64) []FurnacePoint {
	if !fp.Initial.Parsable || endS <= 0 {
		return nil
	}
	t, temp := 0.0, fp.Initial.Value
	out := []FurnacePoint{{0, temp}}
	for _, seg := range fp.Segments {
		switch seg.Kind {
		case "ramp":
			if seg.Rate.Parsable && seg.Target.Parsable && seg.Rate.Value != 0 {
				dur := math.Abs(seg.Target.Value-temp) / math.Abs(seg.Rate.Value) * 60
				steps := 20
				for k := 1; k <= steps; k++ {
					tt := t + dur*float64(k)/float64(steps)
					tv := temp + (seg.Target.Value-temp)*float64(k)/float64(steps)
					out = append(out, FurnacePoint{tt, tv})
				}
				t += dur
				temp = seg.Target.Value
			}
		case "isothermal":
			if seg.Duration.Parsable {
				out = append(out, FurnacePoint{t + seg.Duration.Value, temp})
				t += seg.Duration.Value
			}
		}
	}
	if t < endS {
		out = append(out, FurnacePoint{endS, temp})
	}
	return out
}

// atmosphereWindows applies the domain rule: reaction onset at the sample is
// valve_time + transport delay. Thermal lag is represented by the (separately
// observable) DTG evidence; the window note makes the inequality explicit.
func atmosphereWindows(gas []model.GasEvent, peaks []Peak) []AtmosphereWindow {
	var out []AtmosphereWindow
	for _, ge := range gas {
		if !ge.ValveTime.Parsable {
			continue
		}
		w := AtmosphereWindow{
			EventID:      ge.ID,
			FromGas:      ge.FromGas,
			ToGas:        ge.ToGas,
			ValveTimeS:   ge.ValveTime.Value,
			DelayS:       ge.DelayS.Value,
			ReactionFrom: ge.ValveTime.Value + ge.DelayS.Value,
			Note:         "valve switch time is not reaction time; thermal lag can delay DTG onset further",
		}
		// attach observed peak evidence after the reaction-from instant
		for _, pk := range peaks {
			if pk.CenterS >= w.ReactionFrom && pk.CenterS-w.ReactionFrom < 600 {
				w.Note += "; nearest DTG center after reaction-from=" + f2(pk.CenterS) + "s"
				break
			}
		}
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ReactionFrom < out[j].ReactionFrom })
	return out
}

func f2(v float64) string {
	abs := math.Abs(v)
	if abs != 0 && (abs < 0.01 || abs >= 1e6) {
		return strconvF(v)
	}
	return strconvF(math.Round(v*100) / 100)
}

// detectPeaks finds local maxima of |DTG| above prominence and separated by
// min_separation_s. Overlapping peaks (shoulders inside one broad depression)
// are produced as an alternative group: multiple legal decompositions.
func detectPeaks(pts []Point, gaps []Gap, pr model.Params) []Peak {
	var peaks []Peak
	gapStart := map[string]bool{}
	for _, g := range gaps {
		gapStart[g.AfterID] = true
	}
	for i := 1; i < len(pts)-1; i++ {
		if gapStart[pts[i].ID] {
			continue
		}
		d := pts[i].DtgPctC
		if d < pr.Prominence {
			continue
		}
		if d >= pts[i-1].DtgPctC && d >= pts[i+1].DtgPctC {
			l, r := peakBounds(pts, i, gapStart)
			peaks = append(peaks, Peak{
				ID:       pkID(len(peaks)),
				CenterID: pts[i].ID, CenterS: pts[i].TS, CenterC: pts[i].TempC,
				Depth: d, LeftID: pts[l].ID, RightID: pts[r].ID,
				LeftS: pts[l].TS, RightS: pts[r].TS,
				Note: "zero-derivative shoulders on the smoothed DTG",
			})
		}
	}
	// merge too-close maxima, but remember them as overlap alternatives
	peaks = splitOverlaps(pts, peaks, pr)
	return peaks
}

func pkID(n int) string {
	return idf("pk_%03d", n+1)
}

// peakBounds walks out to the DTG shoulders (depth <= 10% of peak, or sign
// change) without crossing gaps.
func peakBounds(pts []Point, c int, gapStart map[string]bool) (int, int) {
	threshold := pts[c].DtgPctC * 0.1
	l := c
	for l > 0 && !gapStart[pts[l].ID] {
		if pts[l-1].DtgPctC <= threshold || pts[l-1].DtgPctC < 0 {
			break
		}
		l--
	}
	r := c
	for r < len(pts)-1 && !gapStart[pts[r+1].ID] {
		if pts[r+1].DtgPctC <= threshold || pts[r+1].DtgPctC < 0 {
			break
		}
		r++
	}
	return l, r
}

// splitOverlaps detects shoulder structure inside a single broad peak: if the
// envelope between bounds contains two local maxima above prominence (even
// without a full sign change), the single-peak and the two-peak readings are
// both retained in one alternative group.
func splitOverlaps(pts []Point, peaks []Peak, pr model.Params) []Peak {
	var out []Peak
	for _, pk := range peaks {
		var inner []int
		for i := range pts {
			if pts[i].TS > pk.LeftS && pts[i].TS < pk.RightS && pts[i].DtgPctC > pr.Prominence {
				if i > 0 && i < len(pts)-1 &&
					pts[i].DtgPctC >= pts[i-1].DtgPctC && pts[i].DtgPctC >= pts[i+1].DtgPctC {
					inner = append(inner, i)
				}
			}
		}
		if len(inner) >= 2 {
			group := "alt_" + pk.ID
			pk.AltGroup = group
			pk.Note = "overlapping peaks: single-envelope reading; alternatives listed"
			out = append(out, pk)
			for k, idx := range inner {
				l, r := peakBounds(pts, idx, map[string]bool{})
				out = append(out, Peak{
					ID:       idf("pk_%03d_%d", peakCounterStart(pk.ID), k+1),
					CenterID: pts[idx].ID, CenterS: pts[idx].TS, CenterC: pts[idx].TempC,
					Depth: pts[idx].DtgPctC, LeftID: pts[l].ID, RightID: pts[r].ID,
					LeftS: pts[l].TS, RightS: pts[r].TS, AltGroup: group,
					Note: "overlapping peaks: sub-peak reading",
				})
			}
		} else {
			out = append(out, pk)
		}
	}
	// enforce min separation among non-alternative maxima: keep stronger
	sort.SliceStable(out, func(i, j int) bool { return out[i].CenterS < out[j].CenterS })
	var kept []Peak
	for _, pk := range out {
		if len(kept) > 0 && pk.AltGroup == "" &&
			math.Abs(pk.CenterS-kept[len(kept)-1].CenterS) < pr.MinSeparationS {
			if pk.Depth > kept[len(kept)-1].Depth {
				kept[len(kept)-1] = pk
			}
			continue
		}
		kept = append(kept, pk)
	}
	return kept
}
