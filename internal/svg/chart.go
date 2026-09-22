// Package svg renders the synchronized TGA review chart.
//
// One SVG contains four time-aligned panels (TG, DTG, furnace program and the
// atmosphere timeline) so a vertical line always connects the same sample
// time. Every interactive element carries the stable artifact identity.
package svg

import (
	"fmt"
	"html/template"
	"math"
	"strings"

	"tga/internal/engine"
	"tga/internal/model"
)

// Plot carries a fully derived state to the template.
type Plot struct {
	State   *engine.State
	Width   int
	Panels  []Panel
	AtSeq   int64
	HeadSeq int64
}

type Panel struct {
	ID    string
	Title string
}

type pt struct{ X, Y float64 }

// Render builds the overview SVG.
func Render(st *engine.State, headSeq int64) (string, error) {
	const W, H = 980, 760
	left, right := 60.0, W-20.0
	plotW := right - left

	rows := st.CorrectedRows()
	tMin, tMax := 0.0, 2400.0
	if len(rows) > 0 {
		tMin = rows[0].TimeSec
		tMax = rows[len(rows)-1].TimeSec
	}
	mMin, mMax := 0.0, 10.0
	if len(rows) > 0 {
		for _, r := range rows {
			if r.Valid {
				mMin = math.Min(mMin, r.CorrMg)
				mMax = math.Max(mMax, r.CorrMg)
			}
		}
	}
	if mMax-mMin < 0.1 {
		mMax = mMin + 1
	}
	mMin -= 0.1
	mMax += 0.1

	x := func(t float64) float64 { return left + (t-tMin)/(tMax-tMin)*plotW }

	panels := []struct {
		Top, Height float64
	}{
		{40, 180},  // TG
		{245, 150}, // DTG
		{420, 110}, // furnace
		{560, 90},  // atmosphere
	}

	d := &viewData{
		W: W, H: H, Left: left, Right: right,
		TMin: tMin, TMax: tMax, MMin: mMin, MMax: mMax,
		HeadSeq: headSeq, AtSeq: st.AtSeq,
	}

	// TG polyline + bad rows
	tgTop, tgH := panels[0].Top, panels[0].Height
	yM := func(m float64) float64 { return tgTop + (mMax-m)/(mMax-mMin)*tgH }
	var segments []string
	var cur []pt
	var badDots []dot
	for _, r := range rows {
		if !r.Valid {
			if len(cur) > 1 {
				segments = append(segments, polyline(cur))
			}
			cur = nil
			badDots = append(badDots, dot{X: x(r.TimeSec), Y: tgTop + tgH/2, Title: fmt.Sprintf("坏点 t=%.0f", r.TimeSec)})
			continue
		}
		cur = append(cur, pt{x(r.TimeSec), yM(r.CorrMg)})
	}
	if len(cur) > 1 {
		segments = append(segments, polyline(cur))
	}
	d.TG = tgView{Top: tgTop, H: tgH, Segments: segments, Bad: badDots,
		YLabels: axisLabels(mMin, mMax, 5, "%.2f")}

	// DTG
	dtgTop, dtgH := panels[1].Top, panels[1].Height
	var dtgPts []engine.DTGPoint
	for _, p := range st.DTGPoints() {
		dtgPts = append(dtgPts, p)
	}
	dMin, dMax := -0.02, 0.002
	for _, p := range dtgPts {
		if p.State == "ok" {
			dMin = math.Min(dMin, p.DtgMgPerSec)
			dMax = math.Max(dMax, p.DtgMgPerSec)
		}
	}
	yD := func(v float64) float64 { return dtgTop + (dMax-v)/(dMax-dMin)*dtgH }
	var ds []string
	var curD []pt
	for _, p := range dtgPts {
		if p.State != "ok" {
			if len(curD) > 1 {
				ds = append(ds, polyline(curD))
			}
			curD = nil
			continue
		}
		curD = append(curD, pt{x(p.TimeSec), yD(p.DtgMgPerSec)})
	}
	if len(curD) > 1 {
		ds = append(ds, polyline(curD))
	}
	var peakMarks []mark
	var stageMarks []stageMark
	for _, a := range st.ArtifactIDs {
		art := st.Artifacts[a]
		if art.Status != "current" {
			continue
		}
		switch art.Kind {
		case "dtg_peak":
			t := art.Data["time_sec"].(float64)
			T := art.Data["temp_c"].(float64)
			rate := art.Data["rate"].(float64)
			peakMarks = append(peakMarks, mark{X: x(t), Y: yD(rate), ID: art.ID,
				Label: fmt.Sprintf("P %.0fs %.0f°C", t, T)})
		case "data_gap":
			t0 := art.Data["t0_sec"].(float64)
			t1 := art.Data["t1_sec"].(float64)
			d.Gaps = append(d.Gaps, band{X0: x(t0), X1: x(t1), ID: art.ID})
		case "atmosphere_window":
			t0 := art.Data["reaction_t0_sec"].(float64)
			t1 := art.Data["reaction_t1_sec"].(float64)
			d.AtmBands = append(d.AtmBands, band{X0: x(t0), X1: x(t1), ID: art.ID,
				Label: fmt.Sprintf("%v→%v", art.Data["from"], art.Data["to"])})
		}
	}
	d.DTG = dtgView{Top: dtgTop, H: dtgH, Segments: ds, Peaks: peakMarks,
		YLabels: axisLabels(dMin, dMax, 5, "%.4f")}

	// furnace program
	fTop, fH := panels[2].Top, panels[2].Height
	var furnacePts []pt
	if len(rows) > 0 {
		for _, r := range rows {
			if r.Valid {
				tt := r.TempC
				yy := fTop + fH - (tt-20)/360*fH
				furnacePts = append(furnacePts, pt{x(r.TimeSec), yy})
			}
		}
	}
	d.Furnace = furnaceView{Top: fTop, H: fH, Polyline: polyline(furnacePts)}

	// atmosphere timeline: valve events as ticks, reaction windows as bands
	aTop, aH := panels[3].Top, panels[3].Height
	var valveTicks []mark
	gasSub := st.ActiveSubmission(model.LayerGas)
	if gasSub != nil {
		for _, g := range gasSub.Gas {
			if g.TimeOK {
				valveTicks = append(valveTicks, mark{X: x(g.TimeSec), Y: aTop + aH/2,
					Label: fmt.Sprintf("阀 %v→%v @%.0fs", g.FromGas, g.ToGas, g.TimeSec),
					ID:    fmt.Sprintf("valve-%.0f", g.TimeSec)})
			}
		}
	}
	d.Atmosphere = atmView{Top: aTop, H: aH, Valves: valveTicks}

	// stage boundary marks on TG
	for _, a := range st.ArtifactIDs {
		art := st.Artifacts[a]
		if art.Status != "current" || art.Kind != "stage_candidate" {
			continue
		}
		if art.Data["combo_index"].(int) != 0 {
			continue // primary combo only on chart
		}
		t0 := art.Data["t0_sec"].(float64)
		t1 := art.Data["t1_sec"].(float64)
		idx := int(art.Data["stage_index"].(int))
		stageMarks = append(stageMarks, stageMark{X0: x(t0), X1: x(t1), ID: art.ID,
			Label: fmt.Sprintf("S%d", idx+1)})
	}
	d.TG.StageBands = stageMarks

	d.XTicks = timeTicks(tMin, tMax, x)
	var buf strings.Builder
	if err := svgTmpl.Execute(&buf, d); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type viewData struct {
	W, H                                int
	Left, Right, TMin, TMax, MMin, MMax float64
	TG                                  tgView
	DTG                                 dtgView
	Furnace                             furnaceView
	Atmosphere                          atmView
	Gaps, AtmBands                      []band
	XTicks                              []tick
	HeadSeq, AtSeq                      int64
}

type tgView struct {
	Top, H     float64
	Segments   []string
	Bad        []dot
	StageBands []stageMark
	YLabels    []ylabel
}
type dtgView struct {
	Top, H   float64
	Segments []string
	Peaks    []mark
	YLabels  []ylabel
}
type furnaceView struct {
	Top, H   float64
	Polyline string
}
type atmView struct {
	Top, H float64
	Valves []mark
}
type band struct {
	X0, X1 float64
	ID     string
	Label  string
}
type mark struct {
	X, Y  float64
	ID    string
	Label string
}
type stageMark struct {
	X0, X1 float64
	ID     string
	Label  string
}
type dot struct {
	X, Y  float64
	Title string
}
type tick struct {
	X     float64
	Label string
}
type ylabel struct {
	Y     float64
	Label string
}

func polyline(ps []pt) string {
	var b strings.Builder
	for i, p := range ps {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.1f,%.1f", p.X, p.Y)
	}
	return b.String()
}

func axisLabels(min, max float64, n int, format string) []ylabel {
	var out []ylabel
	for i := 0; i <= n; i++ {
		v := min + (max-min)*float64(i)/float64(n)
		out = append(out, ylabel{Y: 0, Label: fmt.Sprintf(format, v)})
	}
	return out
}

func timeTicks(min, max float64, x func(float64) float64) []tick {
	var out []tick
	for t := min; t <= max+1; t += 300 {
		out = append(out, tick{X: x(t), Label: fmt.Sprintf("%.0fs", t)})
	}
	return out
}

var svgTmpl = template.Must(template.New("svg").Funcs(template.FuncMap{
	"subtract": func(a, b float64) float64 { return a - b },
	"add":      func(a, b float64) float64 { return a + b },
	"tgzero":   func(top, h float64) float64 { return top + h },
	"dtgzero":  func(top, h float64) float64 { return top + h*0.909 },
}).Parse(svgHTML))
