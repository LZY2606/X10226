package engine

import (
	"sort"

	"tga-lineage/internal/domain"
)

// AlgorithmVersion identifies the domain calculation implementation.
const AlgorithmVersion = "tga-stage/1.3.0"

// point is one sampled observation extracted from a fragment. Bad values
// remain present (ok=false); nothing is dropped for plotting.
type point struct {
	idx  int
	t    float64
	v    float64
	temp float64
	ok   bool
	tempOK bool
	rowDiags []domain.Diagnostic
}

// segment is a continuous valid stretch; gaps never get interpolated across.
type segment struct{ start, end int }

func pointsFrom(frag *domain.Fragment, needTemp bool) []point {
	pts := make([]point, 0, len(frag.Rows))
	for _, row := range frag.Rows {
		p := point{idx: row.Index, rowDiags: row.Diagnostics}
		if c, ok := row.Original["time"]; ok && c.ParseOK {
			p.t = c.Value
		}
		if c, ok := row.Original["mass"]; ok && c.ParseOK {
			p.v = c.Value
			p.ok = true
		}
		if needTemp {
			if c, ok := row.Original["temp"]; ok && c.ParseOK {
				p.temp = c.Value
				p.tempOK = true
			}
		} else {
			if c, ok := row.Original["setpoint"]; ok && c.ParseOK {
				p.temp = c.Value
				p.tempOK = true
			}
		}
		pts = append(pts, p)
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].t < pts[j].t })
	return pts
}

// validSegments groups contiguous indices whose points are usable.
func validSegments(pts []point, valid func(point) bool) []segment {
	var segs []segment
	i := 0
	for i < len(pts) {
		if !valid(pts[i]) {
			i++
			continue
		}
		s := segment{start: i}
		for i < len(pts) && valid(pts[i]) {
			s.end = i
			i++
		}
		segs = append(segs, s)
	}
	return segs
}

// gaps describes intervals not covered by valid points (bad values or missing
// samples), expressed in the time basis t_since_run_start [s].
type gap struct {
	startT, endT float64
	startRow, endRow int
	reason       string
}

func gapList(pts []point, segs []segment) []gap {
	var gaps []gap
	if len(segs) == 0 {
		return gaps
	}
	// leading
	if segs[0].start > 0 {
		gaps = append(gaps, gap{endT: pts[segs[0].start].t, endRow: pts[segs[0].start].idx, reason: "leading missing samples"})
	}
	for i := 0; i+1 < len(segs); i++ {
		a, b := segs[i].end, segs[i+1].start
		gaps = append(gaps, gap{
			startT: pts[a].t, endT: pts[b].t,
			startRow: pts[a].idx, endRow: pts[b].idx,
			reason: "missing/unconvertible samples",
		})
	}
	if segs[len(segs)-1].end < len(pts)-1 {
		a := segs[len(segs)-1].end
		gaps = append(gaps, gap{startT: pts[a].t, startRow: pts[a].idx, reason: "trailing missing samples"})
	}
	return gaps
}

// medianDelta estimates the sampling cadence.
func medianDelta(pts []point, segs []segment) float64 {
	var ds []float64
	for _, s := range segs {
		for i := s.start; i < s.end; i++ {
			ds = append(ds, pts[i+1].t-pts[i].t)
		}
	}
	sort.Float64s(ds)
	if len(ds) == 0 {
		return 0
	}
	return ds[len(ds)/2]
}

func latestVersion(kind domain.FragmentKind, frags map[domain.FragmentKind]*domain.Fragment) (int, bool) {
	f, ok := frags[kind]
	if !ok {
		return 0, false
	}
	return f.Version, true
}
