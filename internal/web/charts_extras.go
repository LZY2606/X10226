package web

import (
	"fmt"
	"strings"

	"tga-lineage/internal/model"
	"tga-lineage/internal/project"
)

func drawStageBands(b *strings.Builder, sc scale, arts model.Artifact) {
	for _, sg := range arts.Stages {
		x0 := sc.X(sg.StartMin)
		x1 := sc.X(sg.EndMin)
		stroke := cStageBd
		if sg.Certain != "certain" {
			stroke = cEffValve
		}
		fmt.Fprintf(b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" stroke="%s" stroke-dasharray="4 3" data-uid="%s"/>`,
			x0, sc.y0, x1-x0, sc.y1-sc.y0, cStage, stroke, sg.UID)
		// peak marker
		px := sc.X(sg.PeakMin)
		fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-dasharray="2 3" data-uid="%s"/>`,
			px, sc.y0, px, sc.y1, stroke, sg.UID)
	}
}

func drawProgram(b *strings.Builder, sc scale, art model.Artifact) {
	// setpoint series (RawRef empty for sample rows? both carry refs; split
	// by YUnit label: setpoint rows were appended first with XLabel
	// furnace_elapsed and sample rows with XLabel reaction_zone_min).
	var setpoint, sample []model.Point
	for _, p := range art.Series {
		if p.XLabel == "reaction_zone_min" {
			sample = append(sample, p)
		} else {
			setpoint = append(setpoint, p)
		}
	}
	// setpoint y-range
	all := append(append([]model.Point{}, setpoint...), sample...)
	if len(all) == 0 {
		return
	}
	lo, hi := yRange(all)
	sc.y0, sc.y1 = lo, hi
	if len(setpoint) > 1 {
		var pts []string
		for _, p := range setpoint {
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", sc.X(p.TMin), sc.Y(p.Y)))
		}
		fmt.Fprintf(b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="1.8"/>`,
			strings.Join(pts, " "), cProgram)
	}
	if len(sample) > 1 {
		var pts []string
		for _, p := range sample {
			pts = append(pts, fmt.Sprintf("%.1f,%.1f", sc.X(p.TMin), sc.Y(p.Y)))
		}
		fmt.Fprintf(b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="1.2" stroke-dasharray="5 3"/>`,
			strings.Join(pts, " "), cSampleT)
	}
	fmt.Fprintf(b, `<text x="8" y="14" font-size="10" fill="%s">program setpoint</text>`, cProgram)
	fmt.Fprintf(b, `<text x="8" y="26" font-size="10" fill="%s">sample sensor (+lag)</text>`, cSampleT)
	fmt.Fprintf(b, `<text x="%.1f" y="12" font-size="10" fill="%s" text-anchor="end">T [°C]</text>`,
		sc.x1, cAxis)
}

func drawTimeline(b *strings.Builder, sc scale, valves, stages *model.Artifact, st *project.State) {
	midY := (sc.y0 + sc.y1) / 2
	// base line
	fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#bbb" stroke-width="3"/>`,
		sc.x0, midY, sc.x1, midY)
	if valves != nil {
		// gas region shading: from each effective switch until the next
		colors := map[string]string{
			"N2": "#ddf4ff", "Ar": "#f0e6ff", "Air": "#fff1e5", "O2": "#ffebe9", "CO2": "#eaeef2",
		}
		for i, w := range valves.Valves {
			xStart := sc.X(w.EffAtMin)
			xEnd := sc.x1
			if i+1 < len(valves.Valves) {
				xEnd = sc.X(valves.Valves[i+1].EffAtMin)
			}
			fill := colors[w.Gas]
			if fill == "" {
				fill = "#eef3f8"
			}
			fmt.Fprintf(b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" opacity="0.65"/>`,
				xStart, sc.y0+2, xEnd-xStart, sc.y1-sc.y0-4, fill)
			// valve switch (operator time)
			xv := sc.X(w.ValveAtMin)
			fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="2"/>`,
				xv, sc.y0+1, xv, sc.y1-1, cValve)
			// effective (reaction time)
			xe := sc.X(w.EffAtMin)
			fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="2" stroke-dasharray="3 2"/>`,
				xe, sc.y0+1, xe, sc.y1-1, cEffValve)
			fmt.Fprintf(b, `<text x="%.1f" y="%.1f" font-size="10" fill="%s" text-anchor="middle">%s</text>`,
				(xStart+xEnd)/2, sc.y1-4, cText, w.Gas)
			// delay connector
			if w.ValveAtMin != w.EffAtMin {
				fmt.Fprintf(b, `<path d="M%.1f %.1f L%.1f %.1f" stroke="%s" stroke-width="1" marker-end="url(#arrow)"/>`,
					xv, sc.y0+2, xe, sc.y0+2, cEffValve)
			}
		}
	}
	// stage candidate ticks (top)
	if stages != nil {
		for _, sg := range stages.Stages {
			x0 := sc.X(sg.StartMin)
			x1 := sc.X(sg.EndMin)
			color := cStageBd
			if sg.Certain != "certain" {
				color = cEffValve
			}
			fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="3" data-uid="%s"/>`,
				x0, sc.y0+1, x1, sc.y0+1, color, sg.UID)
		}
	}
	fmt.Fprintf(b, `<text x="8" y="12" font-size="10" fill="%s">solid = valve time; dashed = reaction time (+delay)</text>`, cValve)
}

// renderEmptySVG is used before any sample has been imported.
func renderEmptySVG() string {
	return `<svg viewBox="0 0 760 120" class="panel" xmlns="http://www.w3.org/2000/svg">
<rect width="100%" height="100%" fill="white"/>
<text x="380" y="60" font-size="13" fill="#57606a" text-anchor="middle">导入六层数据后，这里同步显示 TG / DTG / 炉程序 / 气氛时间线</text></svg>`
}

func drawYAxis(b *strings.Builder, sc scale, unit string) {
	fmt.Fprintf(b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s"/>`,
		sc.x0, sc.y0, sc.x0, sc.y1, cAxis)
	for i := 0; i <= 3; i++ {
		v := sc.y0 + float64(i)/3*(sc.y1-sc.y0)
		y := sc.Y(v)
		fmt.Fprintf(b, `<text x="%.1f" y="%.1f" font-size="9" fill="%s" text-anchor="end">%.3g</text>`,
			sc.x0-4, y+3, cText, v)
	}
	if unit != "" {
		fmt.Fprintf(b, `<text x="10" y="12" font-size="10" fill="%s">%s</text>`, cAxis, unit)
	}
}
