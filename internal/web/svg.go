package web

import (
	"fmt"
	"strings"
)

// panel maps data coordinates into an SVG viewport.
type panel struct {
	W, H            int
	XMin, XMax      float64
	YMin, YMax      float64
	padL, padR      int
	padT, padB      int
}

func newPanel(w, h int, xMin, xMax, yMin, yMax float64) *panel {
	if yMax == yMin {
		yMax = yMin + 1
	}
	if xMax == xMin {
		xMax = xMin + 1
	}
	return &panel{W: w, H: h, XMin: xMin, XMax: xMax, YMin: yMin, YMax: yMax,
		padL: 58, padR: 12, padT: 22, padB: 26}
}

func (p *panel) x(v float64) float64 {
	w := float64(p.W - p.padL - p.padR)
	return float64(p.padL) + w*(v-p.XMin)/(p.XMax-p.XMin)
}

func (p *panel) y(v float64) float64 {
	h := float64(p.H - p.padT - p.padB)
	return float64(p.padT) + h*(1-(v-p.YMin)/(p.YMax-p.YMin))
}

func (p *panel) polyline(xs, ys []float64, color string, width float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="%.1f" points="`, color, width)
	n := len(xs)
	if len(ys) < n {
		n = len(ys)
	}
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "%.1f,%.1f ", p.x(xs[i]), p.y(ys[i]))
	}
	b.WriteString(`"/>`)
	return b.String()
}

func (p *panel) vline(x, y0, y1 float64, color string, dash bool, width float64) string {
	d := ""
	if dash {
		d = ` stroke-dasharray="4 3"`
	}
	return fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f"%s/>`,
		p.x(x), p.y(y0), p.x(x), p.y(y1), color, width, d)
}

func (p *panel) rect(x0, x1 float64, color string, opacity float64) string {
	return fmt.Sprintf(`<rect x="%.1f" y="%d" width="%.1f" height="%d" fill="%s" fill-opacity="%.2f"/>`,
		p.x(x0), p.padT, p.x(x1)-p.x(x0), p.H-p.padT-p.padB, color, opacity)
}

func (p *panel) text(x, y float64, anchor, color, s string) string {
	return fmt.Sprintf(`<text x="%.1f" y="%.1f" font-size="10" fill="%s" text-anchor="%s">%s</text>`,
		x, y, color, anchor, s)
}

// frame draws axes, ticks and title, returning the opening SVG tag + frame.
func (p *panel) frame(title, xLabel, yLabel string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" width="%d" height="%d" xmlns="http://www.w3.org/2000/svg" style="background:#fff;border:1px solid #ccc">`, p.W, p.H, p.W, p.H)
	// axes
	fmt.Fprintf(&b, `<rect x="%d" y="%d" width="%d" height="%d" fill="none" stroke="#888" stroke-width="0.6"/>`,
		p.padL, p.padT, p.W-p.padL-p.padR, p.H-p.padT-p.padB)
	for i := 0; i <= 5; i++ {
		xv := p.XMin + (p.XMax-p.XMin)*float64(i)/5
		yv := p.YMin + (p.YMax-p.YMin)*float64(i)/5
		b.WriteString(p.text(p.x(xv), float64(p.H-p.padB+14), "middle", "#555", fmt.Sprintf("%.0f", xv)))
		b.WriteString(p.text(float64(p.padL-5), p.y(yv)+3, "end", "#555", fmt.Sprintf("%.3g", yv)))
	}
	b.WriteString(p.text(float64(p.padL+4), float64(p.padT-8), "start", "#222", title))
	b.WriteString(p.text(float64(p.W-p.padR), float64(p.H-6), "end", "#555", xLabel))
	b.WriteString(p.text(8, float64(p.padT)+4, "start", "#555", yLabel))
	return b.String()
}
