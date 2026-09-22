// Package web renders the synchronized review figure (TG/DTG, furnace
// program, gas timeline and stage candidates) as inline SVG using only the
// standard library. All four panels share one furnace-time x axis so a valve
// switch, a peak and a stage line up vertically.
package web

import (
	"fmt"
	"math"
	"strings"

	"tgareview/internal/engine"
)

const (
	svgW  = 980.0
	padL  = 56.0
	padR  = 24.0
	plotW = svgW - padL - padR
	rowH  = 120.0
	gap   = 14.0
	axisH = 26.0
)

type plot struct {
	tMin, tMax float64
	x          func(t float64) float64
}

// Figure renders an artifact's synchronized figure.
func Figure(art *engine.Artifact) string {
	if art == nil || len(art.Points) == 0 {
		return `<svg xmlns="http://www.w3.org/2000/svg" width="980" height="120"><text x="20" y="60">no data</text></svg>`
	}
	tMin, tMax := art.Points[0].TimeS, art.Points[len(art.Points)-1].TimeS
	p := plot{tMin: tMin, tMax: tMax, x: func(t float64) float64 {
		return padL + (t-tMin)/(tMax-tMin)*plotW
	}}
	panels := 4
	h := axisH + float64(panels)*rowH + float64(panels-1)*gap + 10
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg class="fig" data-art="%s" xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f" font-family="sans-serif" font-size="10">`,
		art.ID, svgW, h, svgW, h))
	// gridlines for 100s
	for t := math.Ceil(tMin/100) * 100; t <= tMax; t += 100 {
		x := p.x(t)
		b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="8" x2="%.1f" y2="%.1f" stroke="#eee"/>`, x, x, h-axisH))
	}
	top := axisH
	b.WriteString(panelTG(art, p, top))
	b.WriteString(panelFurnace(art, p, top+rowH+gap))
	b.WriteString(panelGas(art, p, top+2*(rowH+gap)))
	b.WriteString(panelStages(art, p, top+3*(rowH+gap)))
	// shared x axis
	y0 := h - 8
	for t := math.Ceil(tMin/100) * 100; t <= tMax; t += 100 {
		x := p.x(t)
		b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#999"/>`, x, y0-4, x, y0+2))
		b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" text-anchor="middle" fill="#666">%.0fs</text>`, x, y0+14, t))
	}
	b.WriteString(fmt.Sprintf(`<text x="%.0f" y="%.1f" text-anchor="middle" fill="#333">炉时间 (furnace elapsed time, s)</text>`, padL+plotW/2, h-0))
	// cursor overlay driven by JS
	b.WriteString(fmt.Sprintf(`<line id="cursor" x1="0" y1="8" x2="0" y2="%.1f" stroke="#c33" stroke-width="1" opacity="0"/>`, h-axisH))
	b.WriteString(`</svg>`)
	return b.String()
}

func frame(top float64, title, ylabel string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<rect x="%.0f" y="%.1f" width="%.0f" height="%.0f" fill="#fafafa" stroke="#ddd"/>`, padL, top, plotW, rowH))
	b.WriteString(fmt.Sprintf(`<text x="8" y="%.1f" font-weight="bold">%s</text>`, top+14, title))
	b.WriteString(fmt.Sprintf(`<text x="8" y="%.1f" fill="#666">%s</text>`, top+28, ylabel))
	return b.String()
}

func yscale(min, max, top float64) func(float64) float64 {
	return func(v float64) float64 {
		if max == min {
			return top + rowH/2
		}
		return top + 8 + (1-(v-min)/(max-min))*(rowH-16)
	}
}

func minMaxPts(art *engine.Artifact, pick func(engine.Point) (float64, bool)) (float64, float64) {
	mn, mx := math.Inf(1), math.Inf(-1)
	for _, q := range art.Points {
		v, ok := pick(q)
		if !ok {
			continue
		}
		mn = math.Min(mn, v)
		mx = math.Max(mx, v)
	}
	if math.IsInf(mn, 1) {
		return 0, 1
	}
	return mn, mx
}
