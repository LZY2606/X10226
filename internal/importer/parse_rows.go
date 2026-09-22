package importer

import (
	"fmt"

	"tga-review/internal/domain"
)

func (p *Parsed) parseSample(f []string, h map[string]int, lineNo int) string {
	ts := field(f, h, 0, "time")
	ms := field(f, h, 1, "mass")
	tmp := field(f, h, 2, "temp")
	tv, _, err := ParseSeconds(ts)
	if err != nil {
		p.Samples = append(p.Samples, domain.SamplePoint{
			UID: domain.StableID("pt", lineNo, ts, ms), Status: domain.RowUnconverted,
			RawMass: ParseMass(ms), RawTemp: ParseTemp(tmp), Diag: err.Error(),
		})
		return fmt.Sprintf("time: %v", err)
	}
	mq := ParseMass(ms)
	tq := ParseTemp(tmp)
	pt := domain.SamplePoint{
		UID:     domain.StableID("pt", lineNo, ts, ms),
		T:       tv,
		RawMass: mq,
		RawTemp: tq,
		Status:  domain.RowOK,
	}
	bad := ""
	if !mq.OK {
		pt.Status = domain.RowUnconverted
		bad = "mass: " + mq.Diag
	}
	if !tq.OK {
		pt.Status = domain.RowUnconverted
		if bad != "" {
			bad += "; "
		}
		bad += "temp: " + tq.Diag
	}
	if bad == "" {
		pt.Mass, pt.Temp = mq.Value, tq.Value
	} else {
		pt.Diag = bad
	}
	p.Samples = append(p.Samples, pt)
	return bad
}

func (p *Parsed) parseDrift(f []string, h map[string]int, lineNo int) string {
	ts := field(f, h, 0, "time")
	os := field(f, h, 1, "offset")
	tv, _, err := ParseSeconds(ts)
	oq := ParseMass(os)
	dp := domain.DriftPoint{UID: domain.StableID("dp", lineNo, ts, os), T: tv, Offset: oq, Status: domain.RowOK}
	bad := ""
	if err != nil {
		bad = "time: " + err.Error()
	}
	if !oq.OK {
		if bad != "" {
			bad += "; "
		}
		bad += "offset: " + oq.Diag
	}
	if bad != "" {
		dp.Status, dp.Diag = domain.RowUnconverted, bad
	}
	p.Drift = append(p.Drift, dp)
	return bad
}

func (p *Parsed) parseFurnace(f []string, h map[string]int, lineNo int) string {
	st := field(f, h, 0, "start_time")
	et := field(f, h, 1, "end_time")
	t0 := field(f, h, 2, "t0")
	t1 := field(f, h, 3, "t1")
	s0, _, e1 := ParseSeconds(st)
	e0, _, e2 := ParseSeconds(et)
	q0, q1 := ParseTemp(t0), ParseTemp(t1)
	seg := domain.FurnaceSegment{
		UID: domain.StableID("fs", lineNo, st, et, t0, t1),
		StartT: s0, EndT: e0, T0: q0.Value, T1: q1.Value,
	}
	bad := joinDiag("start_time", e1, "end_time", e2, q0, q1)
	if bad != "" {
		seg.UID = domain.StableID("fsbad", lineNo, st, et, t0, t1)
	}
	p.Furnace = append(p.Furnace, seg)
	return bad
}

func (p *Parsed) parseValves(f []string, h map[string]int, lineNo int) string {
	ts := field(f, h, 0, "time")
	from := field(f, h, 1, "from_gas")
	to := field(f, h, 2, "to_gas")
	note := field(f, h, 3, "note")
	tv, _, err := ParseSeconds(ts)
	ev := domain.ValveEvent{
		UID: domain.StableID("ve", lineNo, ts, from, to),
		T: tv, FromGas: from, ToGas: to, Note: note,
	}
	bad := ""
	if err != nil {
		bad = "time: " + err.Error()
	}
	if to == "" {
		if bad != "" {
			bad += "; "
		}
		bad += "to_gas: missing"
	}
	_ = bad
	p.Valves = append(p.Valves, ev)
	return bad
}

func (p *Parsed) parseBlank(f []string, h map[string]int, lineNo int) string {
	ts := field(f, h, 0, "time")
	ms := field(f, h, 1, "mass")
	tmp := field(f, h, 2, "temp")
	tv, _, err := ParseSeconds(ts)
	mq, tq := ParseMass(ms), ParseTemp(tmp)
	bp := domain.BlankPoint{
		UID: domain.StableID("bp", lineNo, ts, ms), T: tv, Mass: mq, Temp: tq, Status: domain.RowOK,
	}
	bad := ""
	if err != nil {
		bad = "time: " + err.Error()
	}
	if !mq.OK {
		if bad != "" {
			bad += "; "
		}
		bad += "mass: " + mq.Diag
	}
	if bad != "" {
		bp.Status, bp.Diag = domain.RowUnconverted, bad
	}
	p.Blank = append(p.Blank, bp)
	return bad
}

func (p *Parsed) parseM0(f []string, h map[string]int, lineNo int) string {
	layer := field(f, h, 0, "layer")
	at := field(f, h, 1, "at_time")
	ms := field(f, h, 2, "mass")
	tv, _, err := ParseSeconds(at)
	mq := ParseMass(ms)
	l := domain.M0Layer{
		UID: domain.StableID("m0", lineNo, layer, at, ms),
		Layer: layer, At: tv, Mass: mq, Status: domain.RowOK,
	}
	bad := ""
	if err != nil {
		bad = "at_time: " + err.Error()
	}
	if !mq.OK {
		if bad != "" {
			bad += "; "
		}
		bad += "mass: " + mq.Diag
	}
	if layer == "" {
		if bad != "" {
			bad += "; "
		}
		bad += "layer: missing"
	}
	if bad != "" {
		l.Status, l.Diag = domain.RowUnconverted, bad
	}
	p.M0 = append(p.M0, l)
	return bad
}

func joinDiag(n1 string, e1 error, n2 string, e2 error, qs ...domain.Q) string {
	bad := ""
	if e1 != nil {
		bad = n1 + ": " + e1.Error()
	}
	if e2 != nil {
		if bad != "" {
			bad += "; "
		}
		bad += n2 + ": " + e2.Error()
	}
	for i, q := range qs {
		if !q.OK {
			if bad != "" {
				bad += "; "
			}
			bad += fmt.Sprintf("temp%d: %s", i, q.Diag)
		}
	}
	return bad
}
