package derive

import (
	"fmt"
	"sort"
)

// findCandidates detects DTG peaks within each continuous segment. Peaks are
// allowed to overlap in time; overlapping peaks are both kept and linked. A
// peak cut by a sampling gap becomes an "unknown" candidate with all legal
// boundary options retained.
func findCandidates(series *Series, st *State, atSeq int64) []Candidate {
	points := series.Points
	if len(points) == 0 {
		return nil
	}
	windows := gasWindows(st)

	segIDs := map[int]bool{}
	for _, p := range points {
		segIDs[p.SegmentID] = true
	}
	var segs []int
	for id := range segIDs {
		segs = append(segs, id)
	}
	sort.Ints(segs)

	var peaks []peak
	for _, sid := range segs {
		peaks = append(peaks, peaksInSegment(points, sid, st.Params)...)
	}
	sort.Slice(peaks, func(i, j int) bool { return peaks[i].at < peaks[j].at })

	candidates := make([]Candidate, 0, len(peaks))
	for i, pk := range peaks {
		c := Candidate{
			ID:          fmt.Sprintf("cand-%03d", i+1),
			PeakS:       pk.at,
			PeakC:       tempAt(points, pk.at),
			OnsetS:      pk.onset,
			OffsetS:     pk.offset,
			OnsetC:      tempAt(points, pk.onset),
			OffsetC:     tempAt(points, pk.offset),
			BoundsKnown: pk.boundsKnown,
			Status:      CandStatusProposed,
			Sources:     append(append([]string{}, series.Sources...), series.ID, st.Params.VersionID),
			ComputedSeq: atSeq,
		}
		if !pk.boundsKnown {
			c.Status = CandStatusUnknown
			c.Reasons = append(c.Reasons, pk.unknownReason)
		}
		if mOn, ok := massAt(points, c.OnsetS); ok {
			if mOff, ok2 := massAt(points, c.OffsetS); ok2 && c.BoundsKnown {
				c.LossMg = mOn - mOff
				if st.InitialMass != nil && st.InitialMass.MassMg != 0 {
					c.LossPct = c.LossMg / st.InitialMass.MassMg * 100
				}
			}
		}
		if win := windowForPeak(windows, c.PeakS); win != nil {
			w := *win
			c.GasWindow = &w
			c.Reasons = append(c.Reasons, fmt.Sprintf(
				"valve %s switches at %.0fs; plausible reaction window %.0f-%.0fs (valve time != reaction time)",
				win.ValveID, win.ValveAtS, win.ReactionStart, win.ReactionEnd))
		}
		candidates = append(candidates, c)
	}

	// Link overlaps: stages may overlap and are never forced to sum to 100%.
	for i := range candidates {
		for j := range candidates {
			if i == j {
				continue
			}
			a, b := candidates[i], candidates[j]
			if a.OnsetS < b.OffsetS && b.OnsetS < a.OffsetS {
				candidates[i].Overlaps = true
			}
		}
	}
	return candidates
}

type peak struct {
	at            float64
	onset         float64
	offset        float64
	boundsKnown   bool
	unknownReason string
}

func peaksInSegment(points []SPoint, sid int, p Parameters) []peak {
	var idx []int
	for i, pt := range points {
		if pt.SegmentID == sid && pt.DTGValid {
			idx = append(idx, i)
		}
	}
	if len(idx) < 3 {
		return nil
	}

	// Local maxima of downward-positive DTG.
	var maxima []int
	for k := 1; k < len(idx)-1; k++ {
		i := idx[k]
		if points[i].DTGPctPerS >= points[idx[k-1]].DTGPctPerS &&
			points[i].DTGPctPerS > points[idx[k+1]].DTGPctPerS &&
			points[i].DTGPctPerS >= p.ProminencePct {
			maxima = append(maxima, i)
		}
	}

	var out []peak
	segmentFirst := points[idx[0]].TS
	segmentLast := points[idx[len(idx)-1]].TS
	for _, mi := range maxima {
		pk := peak{at: points[mi].TS}

		// Left walk to valley or threshold.
		li := mi
		for li > 0 && points[li-1].SegmentID == sid && points[li-1].DTGValid {
			if points[li-1].DTGPctPerS < p.ProminencePct {
				break
			}
			if points[li-1].DTGPctPerS >= points[li].DTGPctPerS {
				break
			}
			li--
		}
		// Right walk.
		ri := mi
		for ri < len(points)-1 && points[ri+1].SegmentID == sid && points[ri+1].DTGValid {
			if points[ri+1].DTGPctPerS < p.ProminencePct {
				break
			}
			if points[ri+1].DTGPctPerS >= points[ri].DTGPctPerS {
				break
			}
			ri++
		}

		pk.onset = points[li].TS
		pk.offset = points[ri].TS
		pk.boundsKnown = true
		if points[li].TS <= segmentFirst+1e-9 {
			pk.boundsKnown = false
			pk.unknownReason = "onset touches segment edge; a sampling gap or run start hides the true boundary"
			pk.onset = segmentFirst
		}
		if points[ri].TS >= segmentLast-1e-9 {
			pk.boundsKnown = false
			pk.unknownReason = "offset touches segment edge; a sampling gap hides the true boundary"
			pk.offset = segmentLast
		}
		out = append(out, pk)
	}
	return out
}

// gasWindows turns valve times into plausible reaction windows using the
// parameterised temperature lag (earliest response) and gas delay (latest
// response). The valve switch time is explicitly not treated as the reaction
// time.
func gasWindows(st *State) []GasWindow {
	var out []GasWindow
	for _, v := range st.Valves {
		out = append(out, GasWindow{
			ValveID:       v.ID,
			ValveAtS:      v.AtS,
			ReactionStart: v.AtS + st.Params.TempLagS,
			ReactionEnd:   v.AtS + st.Params.GasDelayS,
			LagS:          st.Params.TempLagS,
			DelayS:        st.Params.GasDelayS,
			Note:          "reaction is only guaranteed inside this window; valve time precedes it",
		})
	}
	return out
}

func windowForPeak(ws []GasWindow, peakS float64) *GasWindow {
	best := -1
	for i := range ws {
		if peakS >= ws[i].ReactionStart && peakS <= ws[i].ReactionEnd {
			if best == -1 || ws[i].ValveAtS > ws[best].ValveAtS {
				best = i
			}
		}
	}
	if best >= 0 {
		return &ws[best]
	}
	// Closest valve within one full delay window either side.
	for i := range ws {
		if peakS >= ws[i].ValveAtS && peakS <= ws[i].ReactionEnd {
			return &ws[i]
		}
	}
	return nil
}
