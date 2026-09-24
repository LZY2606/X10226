package engine

import (
	"sort"

	"tgaline/internal/domain"
)

type rawPeak struct {
	idx     int
	centerS float64
	height  float64
}

type peakFindingResult struct {
	peaks  []domain.Peak
	groups map[string][]string
}

func findPeaks(s *domain.Series, dtg []float64, dtgOK []bool, p map[string]float64) peakFindingResult {
	var res peakFindingResult
	res.groups = map[string][]string{}
	minProm := p["peak_prominence"]

	var raw []rawPeak
	for i := 1; i < len(dtg)-1; i++ {
		if !dtgOK[i] {
			continue
		}
		if dtg[i] >= dtg[i-1] && dtg[i] >= dtg[i+1] && dtg[i] >= minProm {
			if dtg[i] == dtg[i+1] {
				continue
			}
			raw = append(raw, rawPeak{i, s.TimeS[i], dtg[i]})
		}
	}

	minSep := p["peak_min_separation_s"]
	halfW := p["overlap_group_s"]

	// transitive clustering on distance <= minSep using union-find
	parent := make([]int, len(raw))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	for i := 0; i < len(raw); i++ {
		for j := i + 1; j < len(raw); j++ {
			if absf(raw[i].centerS-raw[j].centerS) <= minSep {
				union(i, j)
			}
		}
	}
	members := map[int][]rawPeak{}
	var roots []int
	for i, q := range raw {
		r := find(i)
		if _, ok := members[r]; !ok {
			roots = append(roots, r)
		}
		members[r] = append(members[r], q)
	}
	sort.Ints(roots)

	for _, r := range roots {
		items := members[r]
		sort.Slice(items, func(a, b int) bool { return items[a].centerS < items[b].centerS })
		minT := items[0].centerS
		maxT := items[len(items)-1].centerS
		gid := groupID(minT, maxT, len(items))
		var ids []string
		for k, q := range items {
			win := domain.Window{StartS: q.centerS - halfW, EndS: q.centerS + halfW}
			id := peakID(q.centerS, q.height, gid, k)
			ids = append(ids, id)
			res.peaks = append(res.peaks, domain.Peak{
				ID: id, CenterS: q.centerS, CenterTK: s.TK[q.idx],
				Height: q.height, Window: win, GroupID: gid,
			})
		}
		if len(items) > 1 {
			for _, id := range ids {
				for k := range res.peaks {
					if res.peaks[k].ID == id {
						for _, other := range ids {
							if other != id {
								res.peaks[k].Overlaps = append(res.peaks[k].Overlaps, other)
							}
						}
					}
				}
			}
		}
		res.groups[gid] = ids
	}
	sort.Slice(res.peaks, func(i, j int) bool { return res.peaks[i].CenterS < res.peaks[j].CenterS })
	return res
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
