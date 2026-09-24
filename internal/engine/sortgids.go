package engine

import (
	"sort"

	"tgaline/internal/domain"
)

func sortGidsByTime(gids []string, groups map[string][]string, peaks []domain.Peak) {
	firstTime := func(gid string) float64 {
		pids := groups[gid]
		best := 0.0
		got := false
		for _, id := range pids {
			for k := range peaks {
				if peaks[k].ID == id {
					if !got || peaks[k].CenterS < best {
						best = peaks[k].CenterS
						got = true
					}
				}
			}
		}
		return best
	}
	sort.Slice(gids, func(i, j int) bool {
		a, b := firstTime(gids[i]), firstTime(gids[j])
		if a == b {
			return gids[i] < gids[j]
		}
		return a < b
	})
}
