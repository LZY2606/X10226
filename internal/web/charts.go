package web

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"tga-lineage/internal/model"
	"tga-lineage/internal/project"
)

// ChartSpec describes the four synchronized panels. All panels share the
// furnace-time x-axis so a valve switch, a DTG peak and a stage candidate at
// the same time line up vertically.
type ChartSpec struct {
	Width  float64
	Height float64
	T0, T1 float64
}

func chartBounds(st *project.State) (t0, t1 float64) {
	first := true
	for _, a := range st.CurrentArtifacts() {
		if a.Kind != model.ArtCorrectedTG {
			continue
		}
		for _, p := range a.Series {
			if first {
				t0, t1, first = p.TMin, p.TMin, false
			}
			t0 = math.Min(t0, p.TMin)
			t1 = math.Max(t1, p.TMin)
		}
	}
	if first {
		return 0, 1
	}
	if t1-t0 < 1e-9 {
		t1 = t0 + 1
	}
	pad := (t1 - t0) * 0.03
	return t0 - pad, t1 + pad
}

type scale struct{ x0, x1, y0, y1, t0, t1 float64 }

func (s scale) X(t float64) float64 { return s.x0 + (t-s.t0)/(s.t1-s.t0)*(s.x1-s.x0) }
func (s scale) Y(v float64) float64 { return s.y1 - (v-s.y0)/(s.y1-s.y0)*(s.y1-s.y0) }

func yRange(series []model.Point) (float64, float64) {
	if len(series) == 0 {
		return 0, 1
	}
	lo, hi := series[0].Y, series[0].Y
	for _, p := range series {
		lo = math.Min(lo, p.Y)
		hi = math.Max(hi, p.Y)
	}
	if hi-lo < 1e-12 {
		hi = lo + 1
	}
	pad := (hi - lo) * 0.08
	return lo - pad, hi + pad
}

const (
	cTG       = "#1f6feb"
	cDTG      = "#bf3989"
	cProgram  = "#bc4c00"
	cSampleT  = "#8250df"
	cStage    = "rgba(63,122,77,0.18)"
	cStageBd  = "#2da44e"
	cGap      = "#f85149"
	cValve    = "#0969da"
	cEffValve = "#fb8f44"
	cGrid     = "#e6e8eb"
	cAxis     = "#57606a"
	cText     = "#24292f"
)

// RenderCharts produces one SVG per panel plus a shared atmosphere background.
func RenderCharts(st *project.State) (tgSVG, dtgSVG, furnaceSVG, timelineSVG string) {
	t0, t1 := chartBounds(st)
	W := 760.0
	tgH, dtgH, fH, tlH := 210.0, 170.0, 140.0, 90.0

	var tg *model.Artifact
	var dtg *model.Artifact
	var stages *model.Artifact
	var valves *model.Artifact
	var program *model.Artifact
	for _, a := range st.CurrentArtifacts() {
		switch a.Kind {
		case model.ArtCorrectedTG:
			tg = a
		case model.ArtDTG:
			dtg = a
		case model.ArtStages:
			stages = a
		case model.ArtValveEff:
			valves = a
		case model.ArtProgramT:
			program = a
		}
	}
	tgSVG = renderPanel(W, tgH, t0, t1, func(b *strings.Builder, sc scale) {
		drawGrid(b, sc)
		if stages != nil {
			drawStageBands(b, sc, *stages)
		}
		if tg != nil {
			drawSeries(b, sc, tg.Series, cTG, 1.6)
			fmt.Fprintf(b, `<text x="10" y="12" font-size="10" fill="%s">corrected mass [mg]</text>`, cTG)
		}
	})
	dtgSVG = renderPanel(W, dtgH, t0, t1, func(b *strings.Builder, sc scale) {
		drawGrid(b, sc)
		if stages != nil {
			drawStageBands(b, sc, *stages)
		}
		if dtg != nil {
			zc := sc
			zc.y0, zc.y1 = yRange(dtg.Series)
			drawSeries(b, sc, dtg.Series, cDTG, 1.4)
			drawZero(b, zc)
			fmt.Fprintf(b, `<text x="10" y="12" font-size="10" fill="%s">dM/dt [mg/min]</text>`, cDTG)
		}
	})
	furnaceSVG = renderPanel(W, fH, t0, t1, func(b *strings.Builder, sc scale) {
		drawGrid(b, sc)
		if program != nil {
			// first half of series = setpoint, remainder = sample sensor
			drawProgram(b, sc, *program)
		}
	})
	timelineSVG = renderPanel(W, tlH, t0, t1, func(b *strings.Builder, sc scale) {
		drawTimeline(b, sc, valves, stages, st)
	})
	return
}

func renderPanel(W, H, t0, t1 float64, body func(*strings.Builder, scale)) string {
	margin := scale{x0: 54, x1: W - 14, y0: 14, y1: H - 26, t0: t0, t1: t1}
	margin.y0, margin.y1 = 14, H-26
	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %.0f %.0f" class="panel" xmlns="http://www.w3.org/2000/svg">`, W, H)
	b.WriteString(`<rect width="100%" height="100%" fill="white"/>`)
	body(&b, margin)
	drawXAxis(&b, margin)
	b.WriteString(`</svg>`)
	return b.String()
}

func drawGrid(b *strings.Builder, sc scale) {
	for i := 0; i <= 6; i++ {
		t := sc.t0 + float64(i)/6*(sc.t1-sc.t0)
		x := sc.X(t)
		fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s"/>`,
			x, sc.y0, x, sc.y1, cGrid)
	}
}

func drawXAxis(b *strings.Builder, sc scale) {
	fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s"/>`,
		sc.x0, sc.y1, sc.x1, sc.y1, cAxis)
	for i := 0; i <= 6; i++ {
		t := sc.t0 + float64(i)/6*(sc.t1-sc.t0)
		x := sc.X(t)
		fmt.Fprintf(b, `<text x="%.1f" y="%.1f" font-size="10" fill="%s" text-anchor="middle">%.1f</text>`,
			x, sc.y1+14, cText, t)
	}
	fmt.Fprintf(b, `<text x="%.1f" y="%.1f" font-size="10" fill="%s">furnace time [min]</text>`,
		(sc.x0+sc.x1)/2, sc.y1+24, cAxis)
}

func polylinePoints(sc scale, series []model.Point) string {
	// split by segment so gaps are visible as breaks
	var sb strings.Builder
	lastSeg := -99
	for _, p := range series {
		if p.Seg != lastSeg {
			lastSeg = p.Seg
		}
		fmt.Fprintf(&sb, "%.1f,%.1f ", sc.X(p.TMin), sc.Y(p.Y))
	}
	return sb.String()
}

func drawSeries(b *strings.Builder, sc scale, series []model.Point, color string, width float64) {
	sc.y0, sc.y1 = yRange(series)
	drawYAxis(b, sc, "")
	seg := -1
	var pts []string
	flush := func() {
		if len(pts) < 2 {
			pts = pts[:0]
			return
		}
		fmt.Fprintf(b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="%.2f"/>`,
			strings.Join(pts, " "), color, width)
		pts = pts[:0]
	}
	// order by segment then time
	var ordered = append([]model.Point(nil), series...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Seg != ordered[j].Seg {
			return ordered[i].Seg < ordered[j].Seg
		}
		return ordered[i].TMin < ordered[j].TMin
	})
	for _, p := range ordered {
		if p.Seg != seg {
			flush()
			seg = p.Seg
		}
		pts = append(pts, fmt.Sprintf("%.1f,%.1f", sc.X(p.TMin), sc.Y(p.Y)))
		// flag non-monotonic points
		flagged := false
		for _, f := range p.Flags {
			if f == "noisy" {
				flagged = true
			}
		}
		if flagged {
			fmt.Fprintf(b, `<circle cx="%.1f" cy="%.1f" r="2.2" fill="%s"/>`,
				sc.X(p.TMin), sc.Y(p.Y), cGap)
		}
	}
	flush()
}

func drawZero(b *strings.Builder, sc scale) {
	if sc.y0 <= 0 && sc.y1 >= 0 {
		y := sc.Y(0)
		fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#999" stroke-dasharray="4 3"/>`,
			sc.x0, y, sc.x1, y)
	}
}
