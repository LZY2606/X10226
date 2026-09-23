package tga

import "math"

// gapFactor: a missing interval longer than this factor times the median valid
// sampling interval is treated as a sampling gap.
const gapFactor = 5

func validPoints(rep *Report) []*Point {
	out := []*Point{}
	for i := range rep.Points {
		if rep.Points[i].Valid {
			out = append(out, &rep.Points[i])
		}
	}
	return out
}

// detectGaps finds sampling hiatuses. Everything downstream is segmented by
// gaps and never interpolates across them.
func detectGaps(rep *Report) {
	vp := validPoints(rep)
	if len(vp) < 3 {
		return
	}
	deltas := make([]float64, 0, len(vp)-1)
	for i := 1; i < len(vp); i++ {
		deltas = append(deltas, vp[i].TimeS-vp[i-1].TimeS)
	}
	med := median(deltas)
	thr := med * gapFactor
	for i := 1; i < len(vp); i++ {
		d := vp[i].TimeS - vp[i-1].TimeS
		if med > 0 && d > thr {
			gid := HashID("gap", map[string]any{"from": vp[i-1].ID, "to": vp[i].ID})
			rep.Gaps = append(rep.Gaps, Gap{
				ID: gid, FromS: vp[i-1].TimeS, ToS: vp[i].TimeS,
				FromPtID: vp[i-1].ID, ToPtID: vp[i].ID,
				Reason: "采样间隔超过中位数间隔的 5 倍",
			})
			vp[i-1].GapAfter = true
		}
	}
}

func median(xs []float64) float64 {
	cp := append([]float64{}, xs...)
	// simple insertion sort, input is small
	for i := 1; i < len(cp); i++ {
		for j := i; j > 0 && cp[j-1] > cp[j]; j-- {
			cp[j-1], cp[j] = cp[j], cp[j-1]
		}
	}
	n := len(cp)
	if n == 0 {
		return 0
	}
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

// segments returns valid points split at gaps.
func segments(rep *Report) [][]*Point {
	vp := validPoints(rep)
	seg := [][]*Point{}
	cur := []*Point{}
	for i, p := range vp {
		cur = append(cur, p)
		if p.GapAfter || i == len(vp)-1 {
			seg = append(seg, cur)
			cur = []*Point{}
		}
	}
	return seg
}
