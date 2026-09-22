// Package svg renders the synchronized review charts as native SVG. Each
// plotted element carries a stable data-id tying the visible mark to a derived
// node or source row, enabling result->input traceability from the page.
package svg

import (
	"fmt"
	"math"
	"strings"

	"tga-review/internal/domain"
)

const (
	chartW = 780
	chartH = 250
	mL     = 60
	mR     = 18
	mT     = 24
	mB     = 40
)

func sc(v, lo, hi, dlo, dhi float64) float64 {
	if hi == lo {
		return (dlo + dhi) / 2
	}
	return dlo + (v-lo)/(hi-lo)*(dhi-dlo)
}

func ticks(lo, hi float64, n int, conv func(float64) string) []string {
	out := []string{}
	for i := 0; i <= n; i++ {
		v := lo + (hi-lo)*float64(i)/float64(n)
		x := sc(v, lo, hi, mL, chartW-mR)
		out = append(out, fmt.Sprintf(`<line x1="%.1f" y1="%d" x2="%.1f" y2="%d" stroke="#e2e8f0"/>`, x, mT, x, chartH-mB))
		out = append(out, fmt.Sprintf(`<text x="%.1f" y="%d" font-size="10" text-anchor="middle" fill="#64748b">%s</text>`, x, chartH-mB+14, conv(v)))
	}
	return out
}

func yticks(lo, hi float64, n int, conv func(float64) string) []string {
	out := []string{}
	for i := 0; i <= n; i++ {
		v := lo + (hi-lo)*float64(i)/float64(n)
		y := sc(v, lo, hi, chartH-mB, mT)
		out = append(out, fmt.Sprintf(`<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#e2e8f0"/>`, mL, y, chartW-mR, y))
		out = append(out, fmt.Sprintf(`<text x="%d" y="%.1f" font-size="10" text-anchor="end" fill="#64748b">%s</text>`, mL-6, y+3, conv(v)))
	}
	return out
}

func openSVG(title string) *strings.Builder {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %d %d" class="chart" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="%s">`, chartW, chartH, title)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#ffffff"/>`, chartW, chartH)
	fmt.Fprintf(&b, `<text x="%d" y="16" font-size="12" font-weight="700" fill="#0f172a">%s</text>`, mL, title)
	return &b
}

func closeSVG(b *strings.Builder) string {
	b.WriteString(`</svg>`)
	return b.String()
}

type mark struct {
	i    int
	kind string // noise | peak
	id   string
}

type line struct {
	color string
	width float64
}

type ptData struct {
	x, y  float64
	id    string
	mark  string
}

func fmtTime(v float64) string  { return fmt.Sprintf("%.0f", v) }
func fmtMg(v float64) string    { return fmt.Sprintf("%.1f", v) }
func fmtRate(v float64) string  { return fmt.Sprintf("%.2f", v) }
func fmtC(k float64) string     { return fmt.Sprintf("%.0f", k-273.15) }

// TGChart renders corrected mass (mg) against furnace time; gaps break the line.
func TGChart(corr domain.CorrectedPayload, gaps []domain.Gap, current bool) string {
	b := openSVG("TG 校正质量-炉时间")
	if len(corr.Points) == 0 {
		b.WriteString(`<text x="120" y="120" fill="#94a3b8">无数据</text>`)
		return closeSVG(b)
	}
	tmin, tmax := corr.Points[0].TMin, corr.Points[len(corr.Points)-1].TMin
	mmin, mmax := corr.Points[0].CorrectedMg, corr.Points[0].CorrectedMg
	for _, p := range corr.Points {
		mmin = math.Min(mmin, p.CorrectedMg)
		mmax = math.Max(mmax, p.CorrectedMg)
	}
	pad := (mmax - mmin) * 0.1
	mmin -= pad
	mmax += pad
	writeGrid(b, tmin, tmax, mmin, mmax, fmtTime, fmtMg)

	breakAt := map[[2]int]bool{}
	for _, g := range gaps {
		breakAt[[2]int{g.StartRow, g.EndRow}] = true
	}
	stroke := "#1d4ed8"
	if !current {
		stroke = "#94a3b8"
	}
	var seg strings.Builder
	started := false
	prevRow := -1
	for _, p := range corr.Points {
		x := sc(p.TMin, tmin, tmax, mL, chartW-mR)
		y := sc(p.CorrectedMg, mmin, mmax, chartH-mB, mT)
		if started && breakAt[[2]int{prevRow, p.Row}] {
			started = false
		}
		op := "L"
		if !started {
			op = "M"
			started = true
		}
		fmt.Fprintf(&seg, "%s%.1f %.1f ", op, x, y)
		prevRow = p.Row
	}
	fmt.Fprintf(b, `<path d="%s" fill="none" stroke="%s" stroke-width="1.8"/>`, seg.String(), stroke)
	// gap shading
	for _, g := range gaps {
		x0 := sc(g.T0Min, tmin, tmax, mL, chartW-mR)
		x1 := sc(g.T1Min, tmin, tmax, mL, chartW-mR)
		fmt.Fprintf(b, `<rect x="%.1f" y="%d" width="%.1f" height="%d" fill="#fecaca" opacity="0.5"/>`, x0, mT, math.Max(2, x1-x0), chartH-mB-mT)
		fmt.Fprintf(b, `<text x="%.1f" y="%d" font-size="9" fill="#b91c1c" text-anchor="middle">缺口</text>`, (x0+x1)/2, mT+10)
	}
	fmt.Fprintf(b, `<text x="%d" y="%d" font-size="11" fill="#334155">炉时间 (min)</text>`, chartW/2-30, chartH-6)
	return closeSVG(b)
}

func writeGrid(b *strings.Builder, xmin, xmax, ymin, ymax float64, xconv, yconv func(float64) string) {
	for _, s := range ticks(xmin, xmax, 6, xconv) {
		b.WriteString(s)
	}
	for _, s := range yticks(ymin, ymax, 5, yconv) {
		b.WriteString(s)
	}
	fmt.Fprintf(b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#94a3b8"/>`, mL, chartH-mB, chartW-mR, chartH-mB)
	fmt.Fprintf(b, `<line x1="%d" y1="%d" x2="%d" y2="%d" stroke="#94a3b8"/>`, mL, mT, mL, chartH-mB)
}

// DTGChart renders dm/dt with noise flags and peak markers.
func DTGChart(dtg domain.DTGPayload, peaks domain.PeaksPayload, gaps []domain.Gap, current bool) string {
	b := openSVG("DTG dm/dt-炉时间（峰/噪声/缺口）")
	if len(dtg.Points) == 0 {
		return closeSVG(b)
	}
	tmin, tmax := dtg.Points[0].TMin, dtg.Points[len(dtg.Points)-1].TMin
	vmin, vmax := 0.0, 0.0
	for _, p := range dtg.Points {
		vmin = math.Min(vmin, p.DTGMgMin)
		vmax = math.Max(vmax, p.DTGMgMin)
	}
	pad := math.Abs(vmin)*0.1 + 0.001
	vmin -= pad
	vmax += pad
	writeGrid(b, tmin, tmax, vmin, vmax, fmtTime, fmtRate)
	zeroY := sc(0, vmin, vmax, chartH-mB, mT)
	fmt.Fprintf(b, `<line x1="%d" y1="%.1f" x2="%d" y2="%.1f" stroke="#f59e0b" stroke-dasharray="4 3"/>`, mL, zeroY, chartW-mR, zeroY)

	breakAt := map[[2]int]bool{}
	for _, g := range gaps {
		breakAt[[2]int{g.StartRow, g.EndRow}] = true
	}
	stroke := "#7c3aed"
	if !current {
		stroke = "#94a3b8"
	}
	var seg strings.Builder
	started := false
	prevRow := -1
	pointXY := map[int][2]float64{}
	for _, p := range dtg.Points {
		x := sc(p.TMin, tmin, tmax, mL, chartW-mR)
		y := sc(p.DTGMgMin, vmin, vmax, chartH-mB, mT)
		pointXY[p.Row] = [2]float64{x, y}
		if started && breakAt[[2]int{prevRow, p.Row}] {
			started = false
		}
		op := "L"
		if !started {
			op = "M"
			started = true
		}
		fmt.Fprintf(&seg, "%s%.1f %.1f ", op, x, y)
		prevRow = p.Row
	}
	fmt.Fprintf(b, `<path d="%s" fill="none" stroke="%s" stroke-width="1.8"/>`, seg.String(), stroke)
	for _, p := range dtg.Points {
		xy := pointXY[p.Row]
		if p.NoiseFlag != "" {
			fmt.Fprintf(b, `<circle data-id="dtgrow-%d" cx="%.1f" cy="%.1f" r="3" fill="#f59e0b"><title>row %d: %s</title></circle>`, p.Row, xy[0], xy[1], p.Row, p.NoiseFlag)
		}
	}
	for _, pk := range peaks {
		xy, ok := pointXY[pk.Row]
		if !ok {
			continue
		}
		fmt.Fprintf(b, `<circle data-id="%s" cx="%.1f" cy="%.1f" r="4" fill="#dc2626"><title>峰 t=%.1f min %.3f mg/min</title></circle>`, pk.ID, pk.TMin, xy[1], pk.TMin, pk.DTGMgMin)
	}
	fmt.Fprintf(b, `<text x="%d" y="%d" font-size="11" fill="#334155">炉时间 (min) · mg/min</text>`, chartW/2-60, chartH-6)
	return closeSVG(b)
}

// ProgramChart renders furnace set temperature and reaction-window sample
// temperature (which lags the program).
func ProgramChart(program []domain.ProgramSegment, wins domain.ReactionWindowsPayload) string {
	b := openSVG("炉程序温度 与 气氛到达样品温度")
	if len(program) == 0 {
		return closeSVG(b)
	}
	type seg struct{ t0, t1, p0, p1 float64; iso bool }
	var segs []seg
	tmin, tmax := 1e18, -1e18
	pmin, pmax := 1e18, -1e18
	for _, ps := range program {
		q0, _ := ps.StartTime.Normalize()
		q1, _ := ps.EndTime.Normalize()
		r0, _ := ps.StartTemp.Normalize()
		r1, _ := ps.EndTemp.Normalize()
		t0, t1, p0, p1 := q0.Value, q1.Value, r0.Value, r1.Value
		segs = append(segs, seg{t0, t1, p0, p1, ps.Kind == "isothermal"})
		tmin = math.Min(tmin, t0)
		tmax = math.Max(tmax, t1)
		pmin = math.Min(math.Min(pmin, p0), p1)
		pmax = math.Max(math.Max(pmax, p0), p1)
	}
	for _, w := range wins.Windows {
		tmin = math.Min(tmin, w.ValveTMin)
		tmax = math.Max(tmax, w.ReachTMin)
		if w.SampleTK > 0 {
			pmin = math.Min(pmin, w.SampleTK)
			pmax = math.Max(pmax, w.ProgramTK)
		}
	}
	pad := (pmax - pmin) * 0.08
	pmin -= pad
	pmax += pad
	writeGrid(b, tmin, tmax, pmin, pmax, fmtTime, fmtC)
	for _, s := range segs {
		x0 := sc(s.t0, tmin, tmax, mL, chartW-mR)
		x1 := sc(s.t1, tmin, tmax, mL, chartW-mR)
		y0 := sc(s.p0, pmin, pmax, chartH-mB, mT)
		y1 := sc(s.p1, pmin, pmax, chartH-mB, mT)
		fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#0f766e" stroke-width="2"/>`, x0, y0, x1, y1)
	}
	// gas windows: valve marker (open circle) -> reach marker (filled)
	for _, w := range wins.Windows {
		xv := sc(w.ValveTMin, tmin, tmax, mL, chartW-mR)
		xr := sc(w.ReachTMin, tmin, tmax, mL, chartW-mR)
		fmt.Fprintf(b, `<line x1="%.1f" y1="%d" x2="%.1f" y2="%d" stroke="#ea580c" stroke-dasharray="3 3"/>`, xv, mT+4, xr, mT+4)
		fmt.Fprintf(b, `<circle cx="%.1f" cy="%d" r="3.5" fill="#ffffff" stroke="#ea580c"><title>阀切换 %.2f min %s</title></circle>`, xv, mT+4, w.ValveTMin, w.Gas)
		fmt.Fprintf(b, `<circle data-id="winrow-%d" cx="%.1f" cy="%d" r="4" fill="#ea580c"><title>到达样品 %.2f min (延迟 %.1f)</title></circle>`, w.GasEventRow, xr, mT+4, w.ReachTMin, w.DelayMin)
	}
	fmt.Fprintf(b, `<text x="%d" y="%d" font-size="11" fill="#334155">炉时间 (min) · 温度 (°C)</text>`, chartW/2-60, chartH-6)
	return closeSVG(b)
}

// GasTimeline renders valve events and their delayed reaction windows on a
// shared time axis with the stages.
func GasTimeline(wins domain.ReactionWindowsPayload, stages domain.StagesPayload, tmin, tmax float64) string {
	b := openSVG("气氛时间线 与 阶段候选（共享时间轴）")
	if tmax <= tmin {
		tmax = tmin + 1
	}
	writeGrid(b, tmin, tmax, 0, 10, fmtTime, func(float64) string { return "" })
	// stages band
	for _, st := range stages.Stages {
		x0 := sc(st.StartTMin, tmin, tmax, mL, chartW-mR)
		x1 := sc(st.EndTMin, tmin, tmax, mL, chartW-mR)
		col := "#60a5fa"
		if st.Status == "rejected" {
			col = "#cbd5e1"
		} else if st.Status == "accepted" {
			col = "#22c55e"
		}
		fmt.Fprintf(b, `<rect data-id="%s" x="%.1f" y="%d" width="%.1f" height="22" rx="4" fill="%s" opacity="0.75"><title>%.1f-%.1f min %.2f%%</title></rect>`, st.ID, x0, 60, math.Max(3, x1-x0), col, st.StartTMin, st.EndTMin, st.MassLossPct)
	}
	// gas events
	y := 120
	gasColor := map[string]string{"N2": "#475569", "air": "#ea580c", "O2": "#dc2626", "Ar": "#0891b2"}
	for _, w := range wins.Windows {
		xv := sc(w.ValveTMin, tmin, tmax, mL, chartW-mR)
		xr := sc(w.ReachTMin, tmin, tmax, mL, chartW-mR)
		col := gasColor[w.Gas]
		if col == "" {
			col = "#7c3aed"
		}
		fmt.Fprintf(b, `<line x1="%.1f" y1="%d" x2="%.1f" y2="%d" stroke="%s" stroke-width="2"/>`, xv, y, xr, y, col)
		fmt.Fprintf(b, `<circle cx="%.1f" cy="%d" r="4" fill="#fff" stroke="%s"/>`, xv, y, col)
		fmt.Fprintf(b, `<circle data-id="winrow-%d" cx="%.1f" cy="%d" r="5" fill="%s"><title>%s 到达样品 %.2f min</title></circle>`, w.GasEventRow, xr, y, col, w.Gas, w.ReachTMin)
		fmt.Fprintf(b, `<text x="%.1f" y="%d" font-size="10" fill="%s">%s</text>`, xr+6, y+3, col, w.Gas)
	}
	fmt.Fprintf(b, `<text x="%d" y="%d" font-size="11" fill="#334155">炉时间 (min) · 阶段(上) / 气氛(下)</text>`, chartW/2-90, chartH-6)
	return closeSVG(b)
}
