package ingest

import (
	"strings"

	"tga-archive/internal/model"
)

type tempConv func(float64) float64

func fatalDiag(res *ParseResult, msg string) {
	res.Diags = append(res.Diags, diagon(res.Kind, 0, "-", "fatal", msg, "", model.SevError))
}

// unitOrDiag resolves a linear unit scale (time/mass). An unknown unit is a
// fatal diagnostic for that layer and yields scale 1 so downstream code can
// still run without panicking; the original representation is preserved.
func unitOrDiag(res *ParseResult, raw, defBase string, conv func(string) (float64, error), field string) (model.Unit, float64) {
	u := strings.TrimSpace(raw)
	if u == "" {
		u = defBase
	}
	s, err := conv(u)
	if err != nil {
		fatalDiag(res, err.Error())
		return mkUnit(raw, "?", 0), 1
	}
	return mkUnit(u, defBase, s), s
}

func tempUnitOrDiag(res *ParseResult, raw, defBase string) (model.Unit, tempConv) {
	u := strings.TrimSpace(raw)
	if u == "" {
		u = defBase
	}
	switch strings.ToLower(u) {
	case "k", "kelvin":
		return mkUnit(u, model.TempBase, 1), func(v float64) float64 { return v }
	case "c", "°c", "degc", "celsius":
		return mkUnit(u, model.TempBase, 1), func(v float64) float64 { return v + 273.15 }
	case "f", "°f", "degf", "fahrenheit":
		return mkUnit(u, model.TempBase, 1), func(v float64) float64 { return (v-32)*5/9 + 273.15 }
	}
	fatalDiag(res, "unknown temperature unit "+u)
	return mkUnit(raw, "?", 0), nil
}

func parseSamples(res *ParseResult, idx map[string]int, rows [][]string, h map[string]string) {
	if err := requireCols(idx, "t", "temp", "mass"); err != nil {
		fatalDiag(res, err.Error())
		return
	}
	tu, tscale := unitOrDiag(res, h[hTime], model.TimeBase, timeScale, "time_unit")
	xu, xconv := tempUnitOrDiag(res, h[hTemp], model.TempBase)
	mu, mscale := unitOrDiag(res, h[hMass], model.MassBase, massScale, "mass_unit")
	res.Samples = &model.Samples{TimeUnit: tu, TempUnit: xu, MassUnit: mu}
	for n, row := range rows {
		rowNo := n + 2
		if len(strings.TrimSpace(strings.Join(row, ""))) == 0 {
			continue
		}
		rt, _ := cell(row, idx, "t")
		rx, _ := cell(row, idx, "temp")
		rm, _ := cell(row, idx, "mass")
		p := model.Point{I: n, RawT: rt, RawTemp: rx, RawMass: rm}
		ok := true
		if tv, good := atof(rt); good {
			p.TSec = tv * tscale
			p.TOk = true
		} else {
			res.Diags = append(res.Diags, diagon(res.Kind, rowNo, "t", "unconvertible", "time could not be parsed/converted", rt, model.SevError))
			ok = false
		}
		if xv, good := atof(rx); good && xconv != nil {
			p.TK = xconv(xv)
			p.TempOk = true
		} else {
			res.Diags = append(res.Diags, diagon(res.Kind, rowNo, "temp", "unconvertible", "temperature could not be parsed/converted", rx, model.SevError))
			ok = false
		}
		if mv, good := atof(rm); good {
			p.MMg = mv * mscale
			p.MassOk = true
		} else {
			res.Diags = append(res.Diags, diagon(res.Kind, rowNo, "mass", "unconvertible", "mass could not be parsed/converted", rm, model.SevError))
			ok = false
		}
		res.Samples.Points = append(res.Samples.Points, p)
		if ok {
			res.AcceptedRows++
		} else {
			res.RejectedRows++
		}
	}
}

func parseBlank(res *ParseResult, idx map[string]int, rows [][]string, _ map[string]string) {
	if err := requireCols(idx, "t", "mass"); err != nil {
		fatalDiag(res, err.Error())
		return
	}
	_, tscale := unitOrDiag(res, res.Headers[hTime], model.TimeBase, timeScale, "time_unit")
	mu, mscale := unitOrDiag(res, res.Headers[hMass], model.MassBase, massScale, "mass_unit")
	res.Blank = &model.Blank{MassUnit: mu}
	for n, row := range rows {
		rt, _ := cell(row, idx, "t")
		rm, _ := cell(row, idx, "mass")
		tv, tok := atof(rt)
		mv, mok := atof(rm)
		if !tok || !mok {
			res.Diags = append(res.Diags, diagon(res.Kind, n+2, "-", "unconvertible", "blank row could not be parsed", rt+","+rm, model.SevError))
			res.RejectedRows++
			continue
		}
		res.Blank.Points = append(res.Blank.Points, model.BlankPoint{TSec: tv * tscale, MMg: mv * mscale, Raw: rt + "," + rm})
		res.AcceptedRows++
	}
}

func parseFurnace(res *ParseResult, idx map[string]int, rows [][]string, _ map[string]string) {
	if err := requireCols(idx, "start", "end", "t0", "t1"); err != nil {
		fatalDiag(res, err.Error())
		return
	}
	_, tscale := unitOrDiag(res, res.Headers[hTime], model.TimeBase, timeScale, "time_unit")
	_, xconv := tempUnitOrDiag(res, res.Headers[hTemp], model.TempBase)
	res.Furnace = &model.Furnace{}
	for n, row := range rows {
		rs, _ := cell(row, idx, "start")
		re, _ := cell(row, idx, "end")
		r0, _ := cell(row, idx, "t0")
		r1, _ := cell(row, idx, "t1")
		s, s1 := atof(rs)
		e, e1 := atof(re)
		t0, t0ok := atof(r0)
		t1, t1ok := atof(r1)
		if !s1 || !e1 || !t0ok || !t1ok || xconv == nil {
			res.Diags = append(res.Diags, diagon(res.Kind, n+2, "-", "unconvertible", "furnace segment could not be parsed", strings.Join(row, ","), model.SevError))
			res.RejectedRows++
			continue
		}
		res.Furnace.Segments = append(res.Furnace.Segments, model.FurnaceSeg{
			StartS: s * tscale, EndS: e * tscale, T0K: xconv(t0), T1K: xconv(t1),
		})
		res.AcceptedRows++
	}
}

func parseGases(res *ParseResult, idx map[string]int, rows [][]string, _ map[string]string) {
	if err := requireCols(idx, "t", "action", "gas") {
		fatalDiag(res, err.Error())
		return
	}
	_, tscale := unitOrDiag(res, res.Headers[hTime], model.TimeBase, timeScale, "time_unit")
	res.Gases = &model.Gases{}
	for n, row := range rows {
		rt, _ := cell(row, idx, "t")
		act, _ := cell(row, idx, "action")
		gas, _ := cell(row, idx, "gas")
		flow, _ := cell(row, idx, "flow_sccm")
		tv, tok := atof(rt)
		a := model.GasAction(strings.ToLower(act))
		if !tok || (a != model.GasOn && a != model.GasOff) {
			res.Diags = append(res.Diags, diagon(res.Kind, n+2, "-", "unconvertible", "gas event could not be parsed", strings.Join(row, ","), model.SevError))
			res.RejectedRows++
			continue
		}
		f, _ := atof(flow)
		res.Gases.Events = append(res.Gases.Events, model.GasEvent{
			TSec: tv * tscale, Action: a, Gas: gas, FlowSccm: f, RawTime: rt,
		})
		res.AcceptedRows++
	}
}

func parseDrift(res *ParseResult, idx map[string]int, rows [][]string, _ map[string]string) {
	if err := requireCols(idx, "a_mg_per_s", "b_mg") {
		fatalDiag(res, err.Error())
		return
	}
	res.Drift = &model.Drift{}
	for n, row := range rows {
		ra, _ := cell(row, idx, "a_mg_per_s")
		rb, _ := cell(row, idx, "b_mg")
		basis, _ := cell(row, idx, "basis")
		a, aok := atof(ra)
		b, bok := atof(rb)
		if !aok || !bok {
			res.Diags = append(res.Diags, diagon(res.Kind, n+2, "-", "unconvertible", "drift row could not be parsed", strings.Join(row, ","), model.SevError))
			res.RejectedRows++
			continue
		}
		res.Drift.AMgPerS = a
		res.Drift.BMg = b
		res.Drift.Basis = basis
		res.Drift.Raw = strings.Join(row, ",")
		res.AcceptedRows++
	}
}

func parseInitial(res *ParseResult, idx map[string]int, rows [][]string, _ map[string]string) {
	if err := requireCols(idx, "m0") {
		fatalDiag(res, err.Error())
		return
	}
	mu, mscale := unitOrDiag(res, res.Headers[hMass], model.MassBase, massScale, "mass_unit")
	res.Initial = &model.Initial{MassUnit: mu}
	for n, row := range rows {
		rm, _ := cell(row, idx, "m0")
		m, ok := atof(rm)
		if !ok {
			res.Diags = append(res.Diags, diagon(res.Kind, n+2, "m0", "unconvertible", "initial mass could not be parsed", rm, model.SevError))
			res.RejectedRows++
			continue
		}
		res.Initial.M0Mg = m * mscale
		res.Initial.RawMass = rm
		res.AcceptedRows++
	}
}
