// Package svg renders the four synchronized review charts server-side:
// TG/DTG, furnace program, atmosphere timeline and stage candidates.
package svg

import (
	"fmt"
	"math"
	"strings"

	"tga-archive/internal/app"
	"tga-archive/internal/derive"
	"tga-archive/internal/domain"
)

const (
	W      = 920.0
	H      = 210.0
	ml     = 58.0
	mr     = 24.0
	mt     = 22.0
	mb     = 30.0
	axisW  = W - ml - mr
	axisH  = H - mt - mb
)

type scale struct {
	t0, t1 float64
}

func (sc scale) x(t float64) float64 {
	if sc.t1 == sc.t0 {
		return ml
	}
	return ml + (t-sc.t0)/(sc.t1-sc.t0)*axisW
}

func yOf(v, v0, v1 float64) float64 {
	if v1 == v0 {
		return mt + axisH/2
	}
	return mt + axisH - (v-v0)/(v1-v0)*axisH
}

func timeRange(st *app.State) (float64, float64) {
	t0, t1 := math.Inf(1), math.Inf(-1)
	for _, p := range st.Report.Points {
		if p.Time.Valid {
			t0 = math.Min(t0, p.Time.Value)
			t1 = math.Max(t1, p.Time.Value)
		}
	}
	for _, w := range st.Report.Windows {
		if w.StartAt.Valid {
			t0 = math.Min(t0, w.StartAt.Value)
		}
		if w.EndAt.Valid && w.EndAt.Value < 1e17 {
			t1 = math.Max(t1, w.EndAt.Value)
		}
		if w.ValveAt.Valid {
			t0 = math.Min(t0, w.ValveAt.Value)
		}
	}
	for _, seg := range furnaceSegs(st) {
		if seg.StartTime.Valid {
			t0 = math.Min(t0, seg.StartTime.Value)
		}
		if seg.EndTime.Valid {
			t1 = math.Max(t1, seg.EndTime.Value)
		}
	}
	if math.IsInf(t0, 1) {
		return 0, 1
	}
	return t0, t1
}

func furnaceSegs(st *app.State) []domain.FurnaceSegment {
	if ls, ok := st.Layers[domain.LayerFurnace]; ok && ls.Current != nil {
		return ls.Current.Furnace
	}
	return nil
}

func valves(st *app.State) []domain.ValveEvent {
	if ls, ok := st.Layers[domain.LayerGas]; ok && ls.Current != nil {
		return ls.Current.Valves
	}
	return nil
}

func frame(title string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf(`<svg class="chart" viewBox="0 0 %.0f %.0f" preserveAspectRatio="xMidYMid meet" role="img" aria-label="%s">`, W, H, title))
	b.WriteString(fmt.Sprintf(`<text x="%.0f" y="15" class="chart-title">%s</text>`, ml, title))
	b.WriteString(fmt.Sprintf(`<rect x="%.0f" y="%.0f" width="%.0f" height="%.0f" class="plot-bg"/>`, ml, mt, axisW, axisH))
	return b.String()
}

func axes(sc scale, v0, v1 float64, yUnit, xLabel string) string {
	var b strings.Builder
	for i := 0; i <= 4; i++ {
		yy := mt + float64(i)*axisH/4
		v := v1 - float64(i)*(v1-v0)/4
		b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" class="grid"/>`, ml, yy, W-mr, yy))
		b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" class="tick" text-anchor="end">%s</text>`, ml-5, yy+3, f3(v)))
	}
	// x ticks (time in seconds)
	n := 6
	for i := 0; i <= n; i++ {
		t := sc.t0 + float64(i)*(sc.t1-sc.t0)/float64(n)
		xx := sc.x(t)
		b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" class="grid-v"/>`, xx, mt, xx, mt+axisH))
		b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" class="tick" text-anchor="middle">%s s</text>`, xx, mt+axisH+16, f1(t)))
	}
	b.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" class="axis-label" text-anchor="middle">%s</text>`, ml+axisW/2, H-3, xLabel))
	b.WriteString(fmt.Sprintf(`<text x="14" y="%.1f" class="axis-label" text-anchor="middle" transform="rotate(-90 14 %.1f)">%s</text>`, mt+axisH/2, mt+axisH/2, yUnit))
	return b.String()
}

func f1(v float64) string {
	if math.Abs(v-math.Round(v)) < 0.05 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}
func f3(v float64) string {
	av := math.Abs(v)
	switch {
	case av >= 100:
		return fmt.Sprintf("%.0f", v)
	case av >= 10:
		return fmt.Sprintf("%.1f", v)
	default:
		return fmt.Sprintf("%.3g", v)
	}
}

// TGDTG renders corrected mass and DTG on one panel.
func TGDTG(st *app.State) string {
	t0, t1 := timeRange(st)
	sc := scale{t0, t1}
	pts := st.Report.Points
	var m0, m1, d0, d1 float64
	m0, m1, d0, d1 = math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for _, p := range pts {
		if p.MassCorr.Valid {
			m0, m1 = math.Min(m0, p.MassCorr.Value), math.Max(m1, p.MassCorr.Value)
		}
		if p.DTG.Valid {
			d0, d1 = math.Min(d0, p.DTG.Value), math.Max(d1, p.DTG.Value)
		}
	}
	if math.IsInf(m0, 1) {
		m0, m1 = 0, 1
	}
	if m1-m0 < 1e-6 {
		m1 = m0 + 1
	}
	if math.IsInf(d0, 1) {
		d0, d1 = -1, 1
	}
	if d1-d0 < 1e-9 {
		d1 = d0 + 1
	}

	var b strings.Builder
	b.WriteString(frame(`TG / DTG <tspan class="sub">(corrected mass mg, rate mg/s; gaps never interpolated)</tspan>`))
	b.WriteString(axes(sc, m0, m1, "mg / mg·s⁻¹", "furnace time (s)"))

	// gap shading
	for _, g := range st.Report.Gaps {
		if g.From.Valid && g.To.Valid {
			b.WriteString(fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" class="gap"/>`,
				sc.x(g.From.Value), mt, sc.x(g.To.Value)-sc.x(g.From.Value), axisH))
		}
	}
	// corrected mass polyline (break at gaps: segments)
	var seg strings.Builder
	pen := false
	for _, p := range pts {
		if !p.Time.Valid || !p.MassCorr.Valid || p.GapBefore {
			pen = false
			continue
		}
		xx, yy := sc.x(p.Time.Value), yOf(p.MassCorr.Value, m0, m1)
		if !pen {
			seg.WriteString(fmt.Sprintf("M%.1f,%.1f ", xx, yy))
			pen = true
		} else {
			seg.WriteString(fmt.Sprintf("L%.1f,%.1f ", xx, yy))
		}
	}
	b.WriteString(fmt.Sprintf(`<path d="%s" class="tg-line"/>`, seg.String()))
	// DTG as separate-segment thin line scaled to lower 40% of plot
	dBase := mt + axisH
	dH := axisH * 0.45
	dScale := func(v float64) float64 { return dBase - (v-d0)/(d1-d0)*dH }
	var dg strings.Builder
	pen = false
	for _, p := range pts {
		if !p.Time.Valid || !p.DTG.Valid || p.GapBefore {
			pen = false
			continue
		}
		xx := sc.x(p.Time.Value)
		yy := dScale(p.DTG.Value)
		if !pen {
			dg.WriteString(fmt.Sprintf("M%.1f,%.1f ", xx, yy))
			pen = true
		} else {
			dg.WriteString(fmt.Sprintf("L%.1f,%.1f ", xx, yy))
		}
	}
	b.WriteString(fmt.Sprintf(`<path d="%s" class="dtg-line"/>`, dg.String()))
	b.WriteString(fmt.Sprintf(`<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" class="zero-line"/>`, ml, dScale(0), W-mr, dScale(0)))

	// peaks
	for _, pk := range st.Report.Peaks {
		b.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="3.5" class="peak-dot"><title>DTG peak %s s, %s K, %s mg/s (cluster %d)</title></circle>`,
			sc.x(pk.Time.Value), dScale(pk.DTG.Value), f2(pk.Time.Value), f2(pk.Temp.Value-273.15), f4(pk.DTG.Value), pk.Cluster))
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func f2(v float64) string { return fmt.Sprintf("%.1f", v) }
func f4(v float64) string { return fmt.Sprintf("%.4f", v) }

var _ = derive.AlgorithmVersion
