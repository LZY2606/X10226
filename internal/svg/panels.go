package svg

import (
	"fmt"
	"strings"

	"tga-archive/internal/app"
	"tga-archive/internal/domain"
)

// Furnace renders the temperature program with measured sample temperature.
func Furnace(st *app.State) string {
	t0, t1 := timeRange(st)
	sc := scale{t0, t1}
	var p0, p1 float64 = 1e9, -1e9
	segs := furnaceSegs(st)
	for _, sg := range segs {
		if sg.StartTemp.Valid {
			p0 = minF(p0, sg.StartTemp.Value-273.15)
		}
		if sg.EndTemp.Valid {
			p1 = maxF(p1, sg.EndTemp.Value-273.15)
		}
	}
	for _, p := range st.Report.Points {
		if p.Temp.Valid {
			p0 = minF(p0, p.Temp.Value-273.15)
			p1 = maxF(p1, p.Temp.Value-273.15)
		}
	}
	if p0 > p1 {
		p0, p1 = 0, 100
	}
	pad := (p1 - p0) * 0.08
	p0 -= pad
	p1 += pad

	b := frame("Furnace program vs measured sample temperature (°C)")
	b += axes(sc, p0, p1, "°C", "furnace time (s)")
	if len(segs) > 0 {
		var path strings.Builder
		for i, sg := range segs {
			x0, x1 := sc.x(sg.StartTime.Value), sc.x(sg.EndTime.Value)
			y0 := yOf(sg.StartTemp.Value-273.15, p0, p1)
			y1 := yOf(sg.EndTemp.Value-273.15, p0, p1)
			cmd := "L"
			if i == 0 {
				cmd = "M"
			}
			path.WriteString(fmt.Sprintf("%s%.1f,%.1f L%.1f,%.1f ", cmd, x0, y0, x1, y1))
		}
		b += fmt.Sprintf(`<path d="%s" class="furnace-line"/>`, path.String())
	}
	// measured sample temperature (shows lag vs reference)
	var mp strings.Builder
	pen := false
	for _, p := range st.Report.Points {
		if !p.Time.Valid || !p.Temp.Valid || p.GapBefore {
			pen = false
			continue
		}
		xx, yy := sc.x(p.Time.Value), yOf(p.Temp.Value-273.15, p0, p1)
		if !pen {
			mp.WriteString(fmt.Sprintf("M%.1f,%.1f ", xx, yy))
			pen = true
		} else {
			mp.WriteString(fmt.Sprintf("L%.1f,%.1f ", xx, yy))
		}
	}
	b += fmt.Sprintf(`<path d="%s" class="sample-temp-line"/>`, mp.String())
	b += "</svg>"
	return b
}

// Atmosphere renders valve switch markers and the delayed reaction windows.
func Atmosphere(st *app.State) string {
	t0, t1 := timeRange(st)
	sc := scale{t0, t1}
	b := frame("Atmosphere timeline: valve switches vs sample-experienced reaction windows")
	b += axes(sc, 0, 3, "gas band", "furnace time (s)")

	colors := map[string]string{"N2": "gas-n2", "air": "gas-air", "O2": "gas-air", "Ar": "gas-n2"}
	// reaction windows (what the sample actually experiences)
	yTop, yBot := mt+18, mt+axisH-34
	for _, w := range st.Report.Windows {
		x0 := sc.x(w.StartAt.Value)
		x1 := sc.x(w.EndAt.Value)
		cls := colors[w.Gas]
		if cls == "" {
			cls = "gas-other"
		}
		b += fmt.Sprintf(`<rect x="%.1f" y="%.0f" width="%.1f" height="%.0f" rx="4" class="%s"><title>%s experienced by sample: %s s -> %s s</title></rect>`,
			x0, yTop+14, maxF(2, x1-x0), 26.0, cls, esc(w.Gas), f2(w.StartAt.Value), f2(minF(w.EndAt.Value, t1)))
		b += fmt.Sprintf(`<text x="%.1f" y="%.0f" class="gas-label">%s</text>`, x0+6, yTop+31, esc(w.Gas))
		// valve marker at the actual switch time
		vx := sc.x(w.ValveAt.Value)
		b += fmt.Sprintf(`<line x1="%.1f" y1="%.0f" x2="%.1f" y2="%.0f" class="valve-mark"><title>valve switched at %s s; sample reacts %s s later (lag %s K)</title></line>`,
			vx, yTop, vx, yBot, f2(w.ValveAt.Value), f2(st.Params.ValveDelayS), lagText(w.MeasuredLagK))
		b += fmt.Sprintf(`<polygon points="%.1f,%.0f %.1f,%.0f %.1f,%.0f" class="valve-arrow"/>`, vx-5, yTop+2, vx+5, yTop+2, vx, yTop+10)
	}
	b += fmt.Sprintf(`<text x="%.0f" y="%.0f" class="legend">▼ valve switch time　▬ delayed reaction window (%s s transport delay)</text>`, ml, H-8, f2(st.Params.ValveDelayS))
	b += "</svg>"
	return b
}

// Stages renders every candidate (including overlapping alternatives).
func Stages(st *app.State) string {
	t0, t1 := timeRange(st)
	sc := scale{t0, t1}
	b := frame("Stage candidates: overlaps allowed, percentages not forced to 100%")
	b += axes(sc, 0, 3, "", "furnace time (s)")

	rowH := 26.0
	for i, c := range st.Report.Candidates {
		y := mt + 12 + float64(i%7)*rowH
		x0, x1 := sc.x(c.StartTime.Value), sc.x(c.EndTime.Value)
		cls := "cand-" + c.Status
		b += fmt.Sprintf(`<g class="cand" data-key="%s">`, esc(c.Key))
		b += fmt.Sprintf(`<rect x="%.1f" y="%.0f" width="%.1f" height="18" rx="4" class="%s"><title>%s (%s): loss %s mg / %s%%</title></rect>`,
			x0, y, maxF(3, x1-x0), cls, esc(c.Key), esc(c.Kind), f4(c.LossMg.Value), f2(percent(c.LossPct)))
		b += fmt.Sprintf(`<text x="%.1f" y="%.0f" class="cand-label">%s · %s%%</text>`, x0+5, y+13, esc(c.Key), f2(percent(c.LossPct)))
		if c.PeakTime.Valid {
			b += fmt.Sprintf(`<line x1="%.1f" y1="%.0f" x2="%.1f" y2="%.0f" class="cand-peak"/>`, sc.x(c.PeakTime.Value), y-3, sc.x(c.PeakTime.Value), y+18)
		}
		b += `</g>`
	}
	b += "</svg>"
	return b
}

func percent(m domain.Measure) float64 {
	if m.Valid {
		return m.Value
	}
	return 0
}

func lagText(m domain.Measure) string {
	if !m.Valid {
		return "?"
	}
	return f2(m.Value)
}

func esc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
