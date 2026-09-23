package engine

import (
	"math"

	"tga-lineage/internal/domain"
)

type buildCtx struct {
	ts        []float64
	sm        []float64
	ok        []bool
	segs      []segment
	gaps      []gap
	dtMed     float64
	gapFactor float64
	pks       []peak
	ovs       []overlapPair
	gasEvs    []gasEvent
	fpPts     []point
	sp        []point
	initialMass float64
	initialRow  int
	stageTags []string
	fp        string
	refs      map[string]domain.NodeRef
}

func seriesFromFloats(id, kind, label, unit, basis string, ts, vals []float64, ok []bool, refs []domain.NodeRef, tags []string) domain.Node {
	cp := make([]float64, len(vals))
	copy(cp, vals)
	// NaN values mark unplottable (missing/unconvertible) samples.
	n := domain.Node{ID: id, Kind: kind, Label: label, Unit: unit, Basis: basis,
		Times: append([]float64{}, ts...), Vals: cp, Direct: refs, SourceIDs: tags, Status: "current"}
	if ok != nil {
		good := false
		for _, b := range ok {
			if b {
				good = true
			}
		}
		if !good {
			n.Status = "empty"
		}
	}
	return n
}

func peaksNode(id string, pks []peak, ts []float64, ctx buildCtx) domain.Node {
	type pv struct {
		Index        int     `json:"index"`
		T            float64 `json:"t"`
		DTG          float64 `json:"dtg_mg_min"`
		Prominence   float64 `json:"prominence_mg_min"`
		StartT       float64 `json:"start_t"`
		EndT         float64 `json:"end_t"`
		Atmosphere   string  `json:"atmosphere_at_reaction_time"`
		SampleTempC  float64 `json:"sample_temp_c"`
		FurnaceSetC  float64 `json:"furnace_setpoint_c"`
	}
	arr := make([]pv, 0, len(pks))
	for _, p := range pks {
		atm := atmosphereAt(ctx.gasEvs, p.t)
		arr = append(arr, pv{
			Index: p.idx, T: p.t, DTG: p.height, Prominence: p.prom,
			StartT: ts[p.startI], EndT: ts[p.endI], Atmosphere: atm,
			SampleTempC: sampleTempAt(ctx.sp, p.t), FurnaceSetC: furnaceSetpointAt(ctx.fpPts, p.t),
		})
	}
	n := domain.Node{ID: id, Kind: "derived/peaks", Label: "DTG 峰（按样品反应时间归属气氛）",
		Basis: "t_since_run_start", Extra: map[string]any{"peaks": arr},
		Direct: []domain.NodeRef{ctx.refs["dtg"], ctx.refs["sampleTemp"], ctx.refs["furnace"], ctx.refs["gas"]},
		SourceIDs: ctx.stageTags, Status: "current"}
	if len(pks) == 0 {
		n.Status = "empty"
		n.Rejects = []domain.Rejection{{Code: "no_peak", Message: "未检测到超过显著性阈值的 DTG 峰"}}
	}
	return n
}

func (c *buildCtx) massAt(t float64) (float64, bool) {
	if math.IsNaN(t) {
		return 0, false
	}
	// nearest-interpolate within segments only
	idx := nearestInSegment(c.ts, c.ok, t)
	if idx < 0 {
		return 0, false
	}
	if math.IsNaN(c.sm[idx]) {
		return 0, false
	}
	return c.sm[idx], true
}

func nearestInSegment(ts []float64, ok []bool, t float64) int {
	best := -1
	bestD := math.Inf(1)
	for i := range ts {
		if ok != nil && !ok[i] {
			continue
		}
		d := math.Abs(ts[i] - t)
		if d < bestD {
			bestD, best = d, i
		}
	}
	return best
}

func gapsView(gs []gap) []map[string]any {
	out := make([]map[string]any, 0, len(gs))
	for _, g := range gs {
		out = append(out, map[string]any{
			"start_t": g.startT, "end_t": g.endT, "duration_s": g.endT - g.startT,
			"start_row": g.startRow, "end_row": g.endRow, "reason": g.reason,
		})
	}
	return out
}

func valveView(evs []gasEvent) []map[string]any {
	out := make([]map[string]any, 0, len(evs))
	for _, e := range evs {
		m := map[string]any{"valve_t": e.t, "state": e.state}
		if e.flowOK {
			m["flow_ml_min"] = e.flow
		}
		out = append(out, m)
	}
	return out
}

func reactionView(evs []gasEvent) []map[string]any {
	out := make([]map[string]any, 0, len(evs))
	for _, e := range evs {
		m := map[string]any{"reaction_t": e.reactionT, "state": e.state, "delay_s": e.reactionT - e.t}
		if e.flowOK {
			m["flow_ml_min"] = e.flow
		}
		out = append(out, m)
	}
	return out
}

func tareView(ls []tareLayer) []map[string]any {
	out := make([]map[string]any, 0, len(ls))
	for _, l := range ls {
		m := map[string]any{"row": l.row, "layer": l.layer, "applied": false}
		if l.ok {
			m["mass_mg"] = l.mass
		} else {
			m["mass_mg"] = nil
		}
		if l.basis != "" {
			m["basis"] = l.basis
		}
		out = append(out, m)
	}
	return out
}

func qualityNode(id string, frags map[domain.FragmentKind]*domain.Fragment, gaps []gap, nmCount int) domain.Node {
	type dRow struct {
		Fragment string `json:"fragment"`
		Version  int    `json:"version"`
		Filename string `json:"filename"`
		Imported bool   `json:"imported"`
		Rows     int    `json:"rows"`
		Diag     []domain.Diagnostic `json:"diagnostics"`
	}
	var rows []dRow
	status := "current"
	for _, k := range domain.AllFragmentKinds() {
		f, ok := frags[k]
		if !ok {
			continue
		}
		dr := dRow{Fragment: string(k), Version: f.Version, Filename: f.Filename, Imported: f.Imported, Rows: len(f.Rows)}
		for _, r := range f.Rows {
			dr.Diag = append(dr.Diag, r.Diagnostics...)
		}
		if !f.Imported {
			status = "partial"
		}
		rows = append(rows, dr)
	}
	n := domain.Node{ID: id, Kind: "derived/import_quality", Label: "导入质量与诊断（半失败、未转换值、缺口统计）",
		Extra: map[string]any{"fragments": rows, "gap_count": len(gaps), "nonmonotonic_points": nmCount},
		Status: status}
	return n
}

// driftFitPoints selects plateau points: near-zero |DTG| smoothed slope and
// only valid points. Falls back to all points when plateaus are insufficient.
func driftFitPoints(corr, ts []float64, segs []segment) []point {
	var out []point
	for _, s := range segs {
		for i := s.start + 1; i < s.end; i++ {
			if math.IsNaN(corr[i]) || math.IsNaN(corr[i-1]) || math.IsNaN(corr[i+1]) {
				continue
			}
			slope := math.Abs((corr[i+1]-corr[i-1])/(ts[i+1]-ts[i-1])) * 60
			if slope < 0.005 { // mg/min threshold for "flat"
				out = append(out, point{t: ts[i], v: corr[i], ok: true})
			}
		}
	}
	if len(out) < 4 {
		out = out[:0]
		for i := range corr {
			if !math.IsNaN(corr[i]) {
				out = append(out, point{t: ts[i], v: corr[i], ok: true})
			}
		}
	}
	return out
}
