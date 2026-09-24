package derive

import (
	"fmt"
	"math"
	"sort"

	"tga-review/internal/domain"
)

// FindPeaks detects negative DTG excursions, groups overlapped peaks and
// retains both merged and split interpretations as legal candidates.
func (e *Engine) FindPeaks(s Series) PeaksResult {
	prom := e.pNum("peak_prominence_frac") // fraction/s (SI)
	type ext struct{ idx int }
	var exts []ext
	for i := 2; i < len(s.DTG)-2; i++ {
		if s.DTG[i].InGap {
			continue
		}
		if s.DTG[i].Value < s.DTG[i-1].Value && s.DTG[i].Value < s.DTG[i+1].Value && s.DTG[i].Value < -prom {
			exts = append(exts, ext{i})
		}
	}
	var peaks []Peak
	src := append([]string{}, s.Sources...)
	for gi, ex := range exts {
		i := ex.idx
		left, right := i-1, i+1
		for left > 0 && s.DTG[left].Value < s.DTG[left-1].Value && !s.DTG[left].InGap {
			left--
		}
		for right < len(s.DTG)-1 && s.DTG[right].Value < s.DTG[right+1].Value && !s.DTG[right].InGap {
			right++
		}
		// prominence relative to highest shoulder
		shoulder := math.Max(s.DTG[left].Value, s.DTG[right].Value)
		prominence := shoulder - s.DTG[i].Value
		crosses := inAnyGap(s.Gaps, s.Points[left].T, s.Points[right].T)
		pk := Peak{
			T: s.DTG[i].T, TempK: s.Points[i].TempK, Depth: s.DTG[i].Value,
			Prominence: prominence, LeftT: s.Points[left].T, RightT: s.Points[right].T,
			CrossesGap: crosses, MergeGroup: gi + 1, Sources: src,
		}
		pk.ID = e.derID("peak", map[string]any{
			"t": math.Round(pk.T*100) / 100, "depth": math.Round(pk.Depth*1e9) / 1e9,
			"left": pk.LeftT, "right": pk.RightT, "gap": crosses,
		})
		if prominence < prom {
			pk.RejectReason = fmt.Sprintf("prominence %.3g below threshold %.3g fraction/s", prominence, prom)
		}
		peaks = append(peaks, pk)
	}

	// Overlap detection: basins that overlap in time are grouped; we emit a
	// merged candidate plus the split candidates. depth limits recursion.
	accepted := peaks[:0]
	var rejected []Peak
	for _, p := range peaks {
		if p.RejectReason != "" {
			rejected = append(rejected, p)
			continue
		}
		accepted = append(accepted, p)
	}

	// annotate overlapping clusters
	depth := int(e.pNum("overlap_split_depth"))
	groups := overlapClusters(accepted)
	if len(groups) > 0 {
		var extra []Peak
		for _, grp := range groups {
			if len(grp) < 2 {
				continue
			}
			mergedT := 0.0
			var depthSum float64
			for _, idx := range grp {
				mergedT += accepted[idx].T
				depthSum += accepted[idx].Depth
			}
			mergedT /= float64(len(grp))
			leftT := accepted[grp[0]].LeftT
			rightT := accepted[grp[len(grp)-1]].RightT
			crosses := inAnyGap(s.Gaps, leftT, rightT)
			mp := Peak{
				T: mergedT, Depth: depthSum, Prominence: depthSum,
				LeftT: leftT, RightT: rightT, CrossesGap: crosses,
				Sources: src,
			}
			mp.ID = e.derID("peak-merged", map[string]any{
				"members": memberIDs(accepted, grp), "left": leftT, "right": rightT,
			})
			extra = append(extra, mp)
			if depth >= 2 {
				for _, idx := range grp {
					accepted[idx].MergeGroup = grp[0] + 1
				}
			}
		}
		accepted = append(accepted, extra...)
	}
	sort.Slice(accepted, func(i, j int) bool { return accepted[i].T < accepted[j].T })
	return PeaksResult{Peaks: accepted, Rejected: rejected, Sources: src}
}

func memberIDs(peaks []Peak, idxs []int) []string {
	var out []string
	for _, i := range idxs {
		out = append(out, peaks[i].ID)
	}
	sort.Strings(out)
	return out
}

// overlapClusters groups indices whose [LeftT,RightT] basins intersect.
func overlapClusters(peaks []Peak) [][]int {
	idx := make([]int, len(peaks))
	for i := range peaks {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return peaks[idx[a]].LeftT < peaks[idx[b]].LeftT })
	var groups [][]int
	var cur []int
	curRight := -1.0
	for k, i := range idx {
		if k == 0 || peaks[i].LeftT > curRight {
			if len(cur) > 0 {
				groups = append(groups, cur)
			}
			cur = []int{i}
			curRight = peaks[i].RightT
		} else {
			cur = append(cur, i)
			if peaks[i].RightT > curRight {
				curRight = peaks[i].RightT
			}
		}
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}
	// keep groups in time order
	sort.Slice(groups, func(i, j int) bool {
		return peaks[groups[i][0]].LeftT < peaks[groups[j][0]].LeftT
	})
	return groups
}
