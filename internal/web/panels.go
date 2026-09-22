package web

import (
	"fmt"
	"math"
	"strings"

	"tgareview/internal/engine"
)

func polyline(art *engine.Artifact, p plot, top float64, pick func(engine.Point) (float64, bool), color string, dash string) string {
	mn, mx := minMaxPts(art, pick)
	// pad range
	if mx-mn < 1e-9 {
		mx += 1
		mn -= 1
	}
	y := yscale(mn, mx, top)
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<text x="%.0f" y="%.1f" text-anchor="end" fill="%s">%.2f</text>`, padL-4, top+14, color, mx))
	b.WriteString(fmt.Sprintf(`<text x="%.0f" y="%.1f" text-anchor="end" fill="%s">%.2f</text>`, padL-4, top+rowH-6, color, mn))
	started := false
	b.WriteString(fmt.Sprintf(`<polyline fill="none" stroke="%s" stroke-width="1.5" %s points="`, color, dash))
	for _, q := range art.Points {
		v, ok := pick(q)
		if !ok {
			started = false
			continue
		}
		if started {
			b.WriteByte(' ')
		}
		b.WriteString(fmt.Sprintf("%.1f,%.2f", p.x(q.TimeS), y(v)))
		started = true
	}
	b.WriteString(`"/>`)
	return b.String()
}

// panelTG shows corrected mass % (TG) and DTG on a twin scale, with gaps shaded.
func panelTG(art *engine.Artifact, p plot, top float64) string {
	var b strings.Builder
	b.WriteString(frame(top, "TG / DTG", "质量 % / DTG %·min⁻¹"))
	for _, g := range art.Gaps {
		b.WriteString(fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.0f" fill="#000" opacity="0.06" data-gap="1"/>`,
			p.x(g.T1S), top, p.x(g.T2S)-p.x(g.T1S), rowH))
	}
	b.WriteString(polyline(art, p, top, func(q engine.Point) (float64, bool) {
		if q.MissingInput {
			return 0, false
		}
		return q.Pct, true
	}, "#1f6fb2", ""))
	// DTG rescaled into same row via own scaling
	mn, mx := minMaxPts(art, func(q engine.Point) (float64, bool) {
		if q.MissingInput {
			return 0, false
		}
		return q.DTGPctPerMin, true
	})
	y := yscale(mn, mx, top)
	b.WriteString(fmt.Sprintf(`<text x="%.0f" y="%.1f" text-anchor="start" fill="#c0392b">DTG %.2f..%.2f</text>`,
		padL+6, top+14, mn, mx))
	b.WriteString(`<polyline fill="none" stroke="#c0392b" stroke-width="1" points="`)
	started := false
	for _, q := range art.Points {
		if q.MissingInput {
			started = false
			continue
		}
		if started {
			b.WriteByte(' ')
		}
		b.WriteString(fmt.Sprintf("%.1f,%.2f", p.x(q.TimeS), y(q.DTGPctPerMin)))
		started = true
	}
	b.WriteString(`"/>`)
	// non-monotonic markers
	for _, q := range art.Points {
		if q.NonMonotonic {
			b.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.2f" r="2.5" fill="#e67e22" data-id="point-%d" data-label="非单调噪声点#%d"/>`,
				p.x(q.TimeS), top+18, q.Index, q.Index))
		}
	}
	return b.String()
}

// panelFurnace draws programmed furnace temperature and the lagged sample temp.
func panelFurnace(art *engine.Artifact, p plot, top float64) string {
	var b strings.Builder
	b.WriteString(frame(top, "炉程序：炉温 vs 样品温度（一阶滞后）", "°C"))
	b.WriteString(polyline(art, p, top, func(q engine.Point) (float64, bool) {
		if q.MissingInput {
			return 0, false
		}
		return q.TFurnaceC, true
	}, "#888", `stroke-dasharray="4 2"`))
	b.WriteString(polyline(art, p, top, func(q engine.Point) (float64, bool) {
		if q.MissingInput {
			return 0, false
		}
		return q.TSampleC, true
	}, "#2c7a2c", ""))
	// mark lag restart after gaps
	for _, iv := range art.Intervals {
		q := art.Points[iv.FromIndex]
		b.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="2" fill="#2c7a2c"/>`, p.x(q.TimeS), top+rowH-14))
	}
	return b.String()
}

// panelGas draws the gas atmosphere row and distinguishes valve switch from the
// delayed reaction time at the sample.
func panelGas(art *engine.Artifact, p plot, top float64) string {
	var b strings.Builder
	b.WriteString(frame(top, "气氛时间线（阀切换 ≠ 样品反应时刻）", "gas"))
	cy := top + rowH/2
	// gas regions on the sample timeline (shifted by delay)
	prev := p.tMin
	curGas := "initial"
	type seg struct {
		t0, t1 float64
		gas    string
	}
	var segs []seg
	for _, v := range art.Valves {
		segs = append(segs, seg{prev, v.ReactionS, curGas})
		// valve marker (furnace clock)
		b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#333" stroke-width="1.4"/>`,
			p.x(v.TimeS), top+10, p.x(v.TimeS), top+rowH-10))
		b.WriteString(fmt.Sprintf(`<polygon points="%.1f,%.1f %.1f,%.1f %.1f,%.1f" fill="#333"/>`,
			p.x(v.TimeS)-3, top+8, p.x(v.TimeS)+3, top+8, p.x(v.TimeS), top+14))
		b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" text-anchor="middle" fill="#333">阀→%s</text>`,
			p.x(v.TimeS), top+26, v.ToGas))
		// delay arrow to reaction time
		b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#d35400" stroke-width="1" stroke-dasharray="3 2"/>`,
			p.x(v.TimeS), cy+10, p.x(v.ReactionS), cy+10))
		b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" text-anchor="middle" fill="#d35400">反应@%.0fs</text>`,
			p.x(v.ReactionS), cy+22, v.ReactionS))
		prev = v.ReactionS
		curGas = v.ToGas
	}
	segs = append(segs, seg{prev, p.tMax, curGas})
	colors := map[string]string{"air": "#f6d7a7", "N2": "#cfe3f7", "initial": "#e8e8e8", "": "#e8e8e8"}
	for _, sg := range segs {
		col := colors[sg.gas]
		if col == "" {
			col = "#e6f4d9"
		}
		b.WriteString(fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="20" fill="%s" stroke="#bbb"/>`,
			p.x(sg.t0), cy-10, math.Max(0, p.x(sg.t1)-p.x(sg.t0)), col))
		b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" text-anchor="middle">%s</text>`,
			(p.x(sg.t0)+p.x(sg.t1))/2, cy+4, sg.gas))
	}
	return b.String()
}

// panelStages draws candidate windows; overlapping options are stacked and color
// coded; no normalization to 100% is implied.
func panelStages(art *engine.Artifact, p plot, top float64) string {
	var b strings.Builder
	b.WriteString(frame(top, "阶段候选（可重叠；不强制总和 100%）", "stage"))
	lane := map[string]int{}
	laneOrder := []string{}
	yFor := func(group string) float64 {
		if _, ok := lane[group]; !ok {
			lane[group] = len(laneOrder)
			laneOrder = append(laneOrder, group)
		}
		return top + 20 + float64(lane[group]%4)*18
	}
	for _, c := range art.Candidates {
		y := yFor(c.GroupID)
		col := "#7fb3d5"
		switch c.Option {
		case "split_a":
			col = "#a9d18e"
		case "split_b":
			col = "#f4b183"
		case "merged":
			col = "#9dc3e6"
		}
		b.WriteString(fmt.Sprintf(`<rect class="cand" data-id="%s" x="%.1f" y="%.1f" width="%.1f" height="14" rx="3" fill="%s" stroke="#456"/>`,
			c.ID, p.x(c.StartS), y, math.Max(2, p.x(c.EndS)-p.x(c.StartS)), col))
		b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" text-anchor="middle" font-size="9">%s</text>`,
			(p.x(c.StartS)+p.x(c.EndS))/2, y+11, c.Option))
		if c.PeakS >= c.StartS && c.PeakS <= c.EndS {
			b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#000" stroke-width="0.8"/>`,
				p.x(c.PeakS), y-2, p.x(c.PeakS), y+16))
		}
	}
	return b.String()
}
