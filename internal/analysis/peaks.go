package analysis

import (
	"fmt"
	"sort"

	"tga-archive/internal/model"
)

type peak struct {
	idx     int
	tS      float64
	rate    float64 // negative %/min magnitude (negative number)
	prom    float64
	onset   *float64
	offset  *float64
	group   int
}

// findPeaks detects local minima of DTG (max rate of loss) with prominence
// above PeakPromPct. Peaks inside suspect (noise) intervals are kept but marked
// unknown.
func findPeaks(pts []workPoint, p model.Params) []peak {
	var peaks []peak
	for i := 1; i < len(pts)-1; i++ {
		if pts[i].dtg == nil || pts[i-1].dtg == nil || pts[i+1].dtg == nil {
			continue
		}
		v := *pts[i].dtg
		if v >= 0 {
			continue
		}
		if v <= *pts[i-1].dtg && v <= *pts[i+1].dtg && (v < *pts[i-1].dtg || v < *pts[i+1].dtg) {
			prom := prominence(pts, i)
			if prom >= p.PeakPromPct {
				on, off := bounds(pts, i)
				peaks = append(peaks, peak{idx: i, tS: pts[i].t, rate: v, prom: prom, onset: on, offset: off})
			}
		}
	}
	groupOverlaps(peaks, pts, p)
	return peaks
}

func prominence(pts []workPoint, i int) float64 {
	v := *pts[i].dtg
	// walk to a higher shoulder each side within the same segment
	left := v
	for j := i - 1; j >= 0 && pts[j].seg == pts[i].seg && pts[j].dtg != nil; j-- {
		if *pts[j].dtg > left {
			left = *pts[j].dtg
		}
		if *pts[j].dtg >= 0 {
			break
		}
	}
	right := v
	for j := i + 1; j < len(pts) && pts[j].seg == pts[i].seg && pts[j].dtg != nil; j++ {
		if *pts[j].dtg > right {
			right = *pts[j].dtg
		}
		if *pts[j].dtg >= 0 {
			break
		}
	}
	base := left
	if right > base {
		base = right
	}
	return base - v
}

// bounds estimates onset/offset as points where DTG first crosses 10% of the
// peak depth on each side, within the same segment (never crossing a gap).
func bounds(pts []workPoint, i int) (*float64, *float64) {
	v := *pts[i].dtg
	thr := 0.1 * v // v negative; threshold more negative than 0
	var on, off float64
	found := false
	for j := i - 1; j >= 0 && pts[j].seg == pts[i].seg && pts[j].dtg != nil; j-- {
		if *pts[j].dtg >= thr {
			on = pts[j+1].t
			found = true
			break
		}
	}
	if !found {
		for j := i - 1; j >= 0 && pts[j].seg == pts[i].seg; j-- {
			if pts[j].massOk {
				on = pts[j].t
				break
			}
		}
	}
	found = false
	for j := i + 1; j < len(pts) && pts[j].seg == pts[i].seg && pts[j].dtg != nil; j++ {
		if *pts[j].dtg >= thr {
			off = pts[j-1].t
			found = true
			break
		}
	}
	if !found {
		for j := i + 1; j < len(pts) && pts[j].seg == pts[i].seg; j++ {
			if pts[j].massOk {
				off = pts[j].t
				break
			}
		}
	}
	return &on, &off
}

// groupOverlaps assigns the same group id to peaks whose onset-offset windows
// overlap or whose separation is small relative to their width.
func groupOverlaps(peaks []peak, pts []workPoint, p model.Params) {
	sort.Slice(peaks, func(i, j int) bool { return peaks[i].tS < peaks[j].tS })
	g := 0
	for i := range peaks {
		if peaks[i].group == 0 {
			g++
			peaks[i].group = g
		}
		for j := i + 1; j < len(peaks); j++ {
			a, b := peaks[i], peaks[j]
			if a.offset != nil && b.onset != nil && *b.onset <= *a.offset {
				peaks[j].group = peaks[i].group
				continue
			}
			wa := width(a)
			wb := width(b)
			minw := wa
			if wb < minw {
				minw = wb
			}
			if minw > 0 && (b.tS-a.tS) < p.OverlapSplitPct*minw {
				peaks[j].group = peaks[i].group
			}
		}
	}
}

func width(p peak) float64 {
	if p.onset != nil && p.offset != nil && *p.offset > *p.onset {
		return *p.offset - *p.onset
	}
	return 0
}

func pctAt(pts []workPoint, t float64) (float64, bool) {
	for i := range pts {
		if pts[i].massOk && abs(pts[i].t-t) < 1e-9 {
			return pts[i].pct, true
		}
	}
	return 0, false
}

func pctNear(pts []workPoint, t float64) (float64, bool) {
	best := -1
	bestd := 1e18
	for i := range pts {
		if !pts[i].massOk {
			continue
		}
		d := abs(pts[i].t - t)
		if d < bestd {
			bestd, best = d, i
		}
	}
	if best < 0 {
		return 0, false
	}
	return pts[best].pct, true
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func fp(v float64) *float64 { x := qround(v); return &x }

// buildCandidates turns peaks into stage candidates. Overlapping groups yield
// BOTH a merged candidate and split (per-peak) candidates, plus an explicit
// rejected naive "single stage" alternative: when it cannot be uniquely
// decided, all legal candidates are retained.
func buildCandidates(pts []workPoint, peaks []peak, src Sources, p model.Params, stateHash string, d *model.Derived) []model.Candidate {
	var out []model.Candidate
	groupPeaks := map[int][]int{}
	for i := range peaks {
		groupPeaks[peaks[i].group] = append(groupPeaks[peaks[i].group], i)
	}
	groups := make([]int, 0, len(groupPeaks))
	for g := range groupPeaks {
		groups = append(groups, g)
	}
	sort.Ints(groups)
	cidx := 0
	for _, g := range groups {
		idxs := groupPeaks[g]
		overlap := len(idxs) > 1
		// split candidates (one per peak)
		var splitIDs []string
		for _, pi := range idxs {
			pk := peaks[pi]
			c := makeCandidate(pts, pk, stateHash, cidx, "split")
			cidx++
			if overlap {
				c.Group = fmt.Sprintf("group%d", g)
				c.Rejected = append(c.Rejected, "") // placeholder removed below
				c.Rejected = nil
			}
			attachGasHint(&c, src, p)
			splitIDs = append(splitIDs, c.Node.ID)
			out = append(out, c)
		}
		if overlap {
			// merged candidate across the whole overlapping group
			first, last := peaks[idxs[0]], peaks[idxs[len(idxs)-1]]
			mergedPeak := peak{tS: first.tS, onset: first.onset, offset: last.offset, rate: first.rate, prom: first.prom, group: g}
			mc := makeCandidate(pts, mergedPeak, stateHash, cidx, "merged")
			cidx++
			mc.Group = fmt.Sprintf("group%d", g)
			mc.Overlap = splitIDs
			attachGasHint(&mc, src, p)
			for _, id := range splitIDs {
				for i := range out {
					if out[i].Node.ID == id {
						out[i].Overlap = append(out[i].Overlap, mc.Node.ID)
					}
				}
			}
			out = append(out, mc)
			// rejected naive alternative + finding
			rej := makeCandidate(pts, mergedPeak, stateHash, cidx, "naive-single")
			cidx++
			rej.Group = mc.Group
			rej.Status = model.StatusError
			rej.Rejected = []string{"overlapping DTG peaks: single-stage attribution would hide a shoulder peak; rejected under " + p.AlgoVersion}
			attachGasHint(&rej, src, p)
			out = append(out, rej)
			d.Findings = append(d.Findings, model.Finding{
				Code: "overlapping_peaks", Severity: model.SevWarning,
				Message: fmt.Sprintf("%d overlapping DTG peaks at t~%.0fs: merged + split candidates retained; stages may overlap and losses are not forced to sum to 100%%", len(idxs), first.tS),
				Refs:    append([]string{mc.Node.ID}, splitIDs...),
			})
		}
	}
	// Gas switch without a matching reaction window: record delay/lag finding.
	recordGasDelayFindings(peaks, src, p, d, out)
	return out
}

func makeCandidate(pts []workPoint, pk peak, stateHash string, ord int, shape string) model.Candidate {
	c := model.Candidate{}
	c.Node = model.Node{
		ID:      nid(stateHash, "cand", ord),
		Label:   fmt.Sprintf("stage %d (%s)", ord+1, shape),
		Kind:    "candidate",
		Status:  model.StatusOK,
		Algo:    model.AlgoV1,
		ParamFP: "",
		Meta:    map[string]any{"shape": shape, "peakRatePctPerMin": qround(pk.rate), "prominencePctPerMin": qround(pk.prom)},
	}
	c.PeakS = qround(pk.tS)
	c.OnsetS = fpOrNil(pk.onset)
	c.OffsetS = fpOrNil(pk.offset)
	if pk.onset != nil && pk.offset != nil {
		if po, ok := pctNear(pts, *pk.onset); ok {
			if pf2, ok2 := pctNear(pts, *pk.offset); ok2 {
				loss := po - pf2
				c.LossPct = fp(loss)
				c.Value = fmt.Sprintf("%.2f%%", loss)
				c.Unit = model.MassPctUnit
				c.Direct = []string{nid(stateHash, "series")}
			}
		}
	}
	if c.Value == "" {
		c.Status = model.StatusUnknown
		c.Reason = "onset/offset not both inside a non-gap segment; loss cannot be uniquely computed"
	}
	return c
}

func fpOrNil(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return fp(*v)
}
