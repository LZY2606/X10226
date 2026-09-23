package calc

import (
	"sort"

	"gotga/internal/domain"
)

// detectPeaks finds negative DTG extrema (mass loss). Overlapping peaks that
// sit closer than PeakMinSepS are kept as an ambiguity group: every legal
// interpretation remains a first-class candidate instead of one forced answer.
func detectPeaks(segs [][]domain.Point, p domain.Params, fp string, seq int64, in *Inputs) []domain.Peak {
	var all []domain.Peak
	promFloor := 0.0
	if in.Mass0.Valid {
		promFloor = p.PeakMinProm * in.Mass0.MG
	}
	for _, pts := range segs {
		var raw []domain.Peak
		for i := 1; i < len(pts)-1; i++ {
			if pts[i].D < 0 && pts[i].D <= pts[i-1].D && pts[i].D <= pts[i+1].D {
				pk := domain.Peak{
					ID:         domain.HashID("peak", fp, pts[i].T, round4(pts[i].D)),
					T:          pts[i].T,
					TempC:      pts[i].Tmp,
					Rate:       pts[i].D,
					Prominence: peakProminence(pts, i),
				}
				pk.Time = formatWindow(pk.T)
				if in.Mass0.Valid && pk.Prominence < promFloor {
					continue
				}
				raw = append(raw, pk)
			}
		}
		all = append(all, raw...)
	}
	if len(all) == 0 {
		return all
	}
	// Group peaks closer than the separation threshold -> overlap ambiguity.
	sort.Slice(all, func(i, j int) bool { return all[i].T < all[j].T })
	groups := map[string][]int{}
	groupOf := make([]int, len(all))
	for i := range all {
		groupOf[i] = -1
	}
	gid := 0
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if all[j].T-all[i].T <= p.PeakMinSepS {
				if groupOf[i] == -1 && groupOf[j] == -1 {
					groupOf[i], groupOf[j] = gid, gid
					gid++
				} else if groupOf[i] != -1 {
					groupOf[j] = groupOf[i]
				} else if groupOf[j] != -1 {
					groupOf[i] = groupOf[j]
				}
			}
		}
	}
	for i := range all {
		g := groupOf[i]
		if g == -1 {
			g = gid + i
		}
		groupID := domain.HashID("peakgroup", fp, g)
		all[i].GroupID = groupID
		groups[groupID] = append(groups[groupID], i)
	}
	for _, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}
		// Mark alternatives; each is legal until human/evidence resolves.
		lead := all[idxs[0]].ID
		for k, ix := range idxs {
			if k > 0 {
				all[ix].AltIDs = []string{}
				for _, jx := range idxs {
					if jx != ix {
						all[ix].AltIDs = append(all[ix].AltIDs, all[jx].ID)
					}
				}
				all[ix].AltOf = lead
			} else {
				for _, jx := range idxs[1:] {
					all[ix].AltIDs = append(all[ix].AltIDs, all[jx].ID)
				}
			}
		}
	}
	return all
}

func peakProminence(pts []domain.Point, i int) float64 {
	// Local prominence proxy in mg: excursion from a nearby shoulder baseline.
	left, right := i, i
	for left > 0 && pts[left-1].D > pts[left].D {
		left--
	}
	for right < len(pts)-1 && pts[right+1].D > pts[right].D {
		right++
	}
	shoulder := 0.0
	if pts[left].D > shoulder {
		shoulder = pts[left].D
	}
	if pts[right].D > shoulder {
		shoulder = pts[right].D
	}
	return shoulder - pts[i].D
}

func round4(v float64) float64 { return float64(int(v*1e4)) / 1e4 }
