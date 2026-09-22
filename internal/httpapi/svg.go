package httpapi

import (
	"fmt"
	"math"
	"net/http"
	"strings"

	"tgaaudit/internal/pipeline"
	"tgaaudit/internal/store"
)

const (
	svgW     = 980
	trackH   = 150
	leftPad  = 70
	rightPad = 30
	topPad   = 30
)

type series struct {
	xs, ys []float64
}

func (s *Server) chart(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	snap, err := s.Store.Snapshot(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	var svg string
	switch kind {
	case "combined.svg":
		svg = combinedSVG(snap)
	default:
		writeErr(w, 404, errStr("unknown chart: "+kind))
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	fmt.Fprint(w, svg)
}

func tRange(snap *store.DerivationSnapshot) (float64, float64) {
	tmin, tmax := math.MaxFloat64, 0.0
	if snap.Corrected != nil {
		for _, p := range snap.Corrected.Points {
			if p.TS < tmin {
				tmin = p.TS
			}
			if p.TS > tmax {
				tmax = p.TS
			}
		}
	}
	if tmin == math.MaxFloat64 {
		return 0, 1
	}
	return tmin, tmax
}

func xScale(t, tmin, tmax float64) float64 {
	plotW := float64(svgW - leftPad - rightPad)
	return leftPad + (t-tmin)/(tmax-tmin)*plotW
}

func yScale(v, vmin, vmax, top, h float64) float64 {
	return top + (vmax-v)/(vmax-vmin)*h
}

func polyline(xs, ys []float64, tmin, tmax, vmin, vmax, top, h float64,
	color string, width float64) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="%.1f" points="`,
		color, width)
	n := 0
	for i := range xs {
		if math.IsNaN(ys[i]) {
			continue
		}
		x := xScale(xs[i], tmin, tmax)
		y := yScale(ys[i], vmin, vmax, top, h)
		fmt.Fprintf(&b, "%.1f,%.1f ", x, y)
		n++
	}
	b.WriteString(`"/>`)
	return b.String()
}

func gapBands(gaps []pipeline.Gap, tmin, tmax, top, h float64) string {
	var b strings.Builder
	for _, g := range gaps {
		x0 := xScale(g.FromT, tmin, tmax)
		x1 := xScale(g.ToT, tmin, tmax)
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="#fde2e2" opacity="0.8"/>`,
			x0, top, x1-x0, h)
	}
	return b.String()
}

func frame(top, h float64, title, ylabel string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<rect x="%d" y="%.1f" width="%d" height="%.1f" fill="#fff" stroke="#bbb"/>`,
		leftPad, top, svgW-leftPad-rightPad, h)
	fmt.Fprintf(&b, `<text x="%d" y="%.1f" font-size="13" font-weight="bold">%s</text>`,
		10, top+16, title)
	fmt.Fprintf(&b, `<text x="12" y="%.1f" font-size="11">%s</text>`,
		top+h/2, ylabel)
	return b.String()
}

func xAxis(top, h, tmin, tmax float64) string {
	var b strings.Builder
	y := top + h
	for t := math.Ceil(tmin/100) * 100; t <= tmax; t += 100 {
		x := xScale(t, tmin, tmax)
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#ddd"/>`,
			x, top, x, y)
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-size="10" text-anchor="middle">%.0f</text>`,
			x, y+12, t)
	}
	return b.String()
}

func combinedSVG(snap *store.DerivationSnapshot) string {
	tmin, tmax := tRange(snap)
	var b strings.Builder
	tracks := 4
	totalH := topPad + tracks*(trackH+10) + 70
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" font-family="sans-serif">`,
		svgW, totalH, svgW, totalH)
	b.WriteString(`<rect width="100%" height="100%" fill="#fafafa"/>`)

	// ---- Track 1: TG (corrected mass fraction) ----
	top := float64(topPad)
	b.WriteString(frame(top, trackH, "TG corrected (m/m0)", "fraction"))
	if snap.Corrected != nil {
		b.WriteString(gapBands(snap.Gaps, tmin, tmax, top, trackH))
		xs, ys := []float64{}, []float64{}
		var ymin, ymax = 1.0, 0.0
		for _, p := range snap.Corrected.Points {
			xs = append(xs, p.TS)
			if p.MassFraction != nil {
				ys = append(ys, *p.MassFraction)
				if *p.MassFraction < ymin {
					ymin = *p.MassFraction
				}
				if *p.MassFraction > ymax {
					ymax = *p.MassFraction
				}
			} else {
				ys = append(ys, math.NaN())
			}
		}
		if ymax > ymin {
			b.WriteString(polyline(xs, ys, tmin, tmax, ymin-0.01, ymax+0.01,
				top, trackH, "#1d4e89", 1.6))
		}
	}
	b.WriteString(xAxis(top, trackH, tmin, tmax))

	// ---- Track 2: DTG ----
	top2 := top + trackH + 10
	b.WriteString(frame(top2, trackH, "DTG smoothed (1/s)", "rate"))
	b.WriteString(gapBands(snap.Gaps, tmin, tmax, top2, trackH))
	if snap.Dtg != nil {
		xs, ys := []float64{}, []float64{}
		ymin, ymax := 0.0, 0.0
		for _, q := range snap.Dtg.Points {
			xs = append(xs, q.TS)
			if q.Smooth != nil {
				ys = append(ys, *q.Smooth)
				if *q.Smooth < ymin {
					ymin = *q.Smooth
				}
				if *q.Smooth > ymax {
					ymax = *q.Smooth
				}
			} else {
				ys = append(ys, math.NaN())
			}
		}
		b.WriteString(polyline(xs, ys, tmin, tmax, ymin*1.1, ymax*1.1+1e-9,
			top2, trackH, "#b5364b", 1.4))
	}
	b.WriteString(xAxis(top2, trackH, tmin, tmax))

	// ---- Track 3: Furnace program ----
	top3 := top2 + trackH + 10
	b.WriteString(frame(top3, trackH, "Furnace program (temperature)", "K"))
	fxs, fys := furnaceSeries(snap)
	if len(fxs) > 1 {
		vmin, vmax := minSlice(fys), maxSlice(fys)
		b.WriteString(polyline(fxs, fys, tmin, tmax, vmin-10, vmax+10,
			top3, trackH, "#6a4c93", 1.6))
	}
	b.WriteString(xAxis(top3, trackH, tmin, tmax))

	// ---- Track 4: Atmosphere timeline + reaction windows + peak markers ----
	top4 := top3 + trackH + 10
	b.WriteString(frame(top4, trackH, "Gas valve timeline, reaction windows, stage candidates", "gas"))
	drawAtmosphere(&b, snap, tmin, tmax, top4, trackH)

	// ---- Stage candidate strip ----
	top5 := top4 + trackH + 10
	fmt.Fprintf(&b, `<text x="10" y="%.1f" font-size="13" font-weight="bold">Stage candidates (h0 permissive / h1 conservative; overlaps allowed)</text>`,
		top5+14)
	drawStages(&b, snap, tmin, tmax, top5+22, 36)

	// x label
	fmt.Fprintf(&b, `<text x="%d" y="%d" font-size="11">furnace time (s)</text>`,
		svgW/2-40, totalH-8)
	b.WriteString(`</svg>`)
	return b.String()
}

func furnaceSeries(snap *store.DerivationSnapshot) ([]float64, []float64) {
	xs, ys := []float64{}, []float64{}
	if snap.Corrected == nil {
		return xs, ys
	}
	for _, p := range snap.Corrected.Points {
		if p.FurnaceTempK != nil {
			xs = append(xs, p.TS)
			ys = append(ys, *p.FurnaceTempK)
		} else if p.SampleTempK != nil {
			xs = append(xs, p.TS)
			ys = append(ys, *p.SampleTempK)
		}
	}
	return xs, ys
}

func drawAtmosphere(b *strings.Builder, snap *store.DerivationSnapshot,
	tmin, tmax, top, h float64) {
	// valve switches + gas name bands are encoded through reaction windows
	// and gas labels on corrected points.
	if snap.Corrected != nil {
		prevGas := ""
		segStart := tmin
		flush := func(end float64, gas string) {
			if gas == "" {
				return
			}
			x0 := xScale(segStart, tmin, tmax)
			x1 := xScale(end, tmin, tmax)
			col := "#e8f0fe"
			if strings.Contains(strings.ToLower(gas), "air") {
				col = "#fde8d7"
			}
			fmt.Fprintf(b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`,
				x0, top+30, x1-x0, 26, col)
			fmt.Fprintf(b, `<text x="%.1f" y="%.1f" font-size="10">%s</text>`,
				x0+4, top+47, gas)
		}
		for _, p := range snap.Corrected.Points {
			if p.Gas != prevGas {
				flush(p.TS, prevGas)
				prevGas = p.Gas
				segStart = p.TS
			}
		}
		flush(tmax, prevGas)
	}
	// reaction windows (delay+lag) and observed peak markers
	mid := top + 85
	for _, r := range snap.Reactions {
		if r.GasTo == "" || (!strings.Contains(strings.ToLower(r.GasTo), "air")) {
			continue
		}
		xv := xScale(r.ValveT, tmin, tmax)
		xlo := xScale(r.ReactionTLo, tmin, tmax)
		xhi := xScale(r.ReactionTHi, tmin, tmax)
		xp := xScale(r.ObservedPeakT, tmin, tmax)
		fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#888" stroke-width="3"/>`,
			xv, mid, xlo, mid)
		fmt.Fprintf(b, `<rect x="%.1f" y="%.1f" width="%.1f" height="10" fill="#2a9d8f" opacity="0.6"/>`,
			xlo, mid-5, xhi-xlo)
		col := "#2a9d8f"
		if !r.PeakMatches {
			col = "#c1121f"
		}
		fmt.Fprintf(b, `<circle cx="%.1f" cy="%.1f" r="3.5" fill="%s"/>`, xp, mid, col)
		fmt.Fprintf(b, `<text x="%.1f" y="%.1f" font-size="9" text-anchor="middle">valve</text>`,
			xv, top+18)
		fmt.Fprintf(b, `<text x="%.1f" y="%.1f" font-size="9" text-anchor="middle">peak</text>`,
			xp, top+118)
	}
}

func drawStages(b *strings.Builder, snap *store.DerivationSnapshot,
	tmin, tmax, top, h float64) {
	rowFor := map[string]float64{"h0": top, "h1": top + h}
	for hyp, stages := range snap.Stages {
		y := rowFor[hyp]
		fmt.Fprintf(b, `<text x="10" y="%.1f" font-size="11">%s</text>`, y+15, hyp)
		for _, st := range stages {
			x0 := xScale(st.OnsetT, tmin, tmax)
			x1 := xScale(st.EndT, tmin, tmax)
			col := "#2a9d8f"
			switch st.Status {
			case "unknown":
				col = "#999"
			case "rejected":
				col = "#c1121f"
			}
			fmt.Fprintf(b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="4" fill="%s" opacity="0.55" stroke="#333"/>`,
				x0, y, x1-x0, h-12, col)
			label := fmt.Sprintf("#%d", st.Index)
			if st.MassLossPct != nil {
				label += fmt.Sprintf(" %.2f%%", *st.MassLossPct)
			}
			fmt.Fprintf(b, `<text x="%.1f" y="%.1f" font-size="10">%s</text>`,
				x0+3, y+15, label)
		}
	}
}

func minSlice(xs []float64) float64 {
	m := xs[0]
	for _, x := range xs[1:] {
		if x < m {
			m = x
		}
	}
	return m
}
func maxSlice(xs []float64) float64 {
	m := xs[0]
	for _, x := range xs[1:] {
		if x > m {
			m = x
		}
	}
	return m
}
