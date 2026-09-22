package core

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// ---------- Parsed material shapes ----------

// SamplePoint is one raw acquisition point; every field keeps its raw text.
type SamplePoint struct {
	Index int      `json:"index"`
	Time  Quantity `json:"time"`
	Temp  Quantity `json:"temp"`
	Mass  Quantity `json:"mass"`
}

type RawSamples struct {
	Points        []SamplePoint `json:"points"`
	TimeUnitRaw   string        `json:"time_unit_raw"`
	TempUnitRaw   string        `json:"temp_unit_raw"`
	MassUnitRaw   string        `json:"mass_unit_raw"`
	DeclaredGap   [][2]int      `json:"declared_gap_indexes,omitempty"`
	GapIntervalsS []GapInterval `json:"gap_intervals_s,omitempty"`
}

type GapInterval struct {
	FromT float64 `json:"from_t_s"`
	ToT   float64 `json:"to_t_s"`
	Reason string `json:"reason"`
}

type DriftPoint struct {
	T    Quantity `json:"t"`
	Mass Quantity `json:"mass"`
}

type DriftCorrection struct {
	Mode      string       `json:"mode"` // linear|points
	Slope     Quantity     `json:"slope,omitempty"`
	Intercept Quantity     `json:"intercept,omitempty"`
	Points    []DriftPoint `json:"points,omitempty"`
}

type FurnaceSegment struct {
	Index    int      `json:"index"`
	Type     string   `json:"type"` // ramp|hold
	StartT   Quantity `json:"start_temp"`
	EndT     Quantity `json:"end_temp"`
	Rate     Quantity `json:"rate,omitempty"` // degC/min
	Duration Quantity `json:"duration_s"`
	StartAtS Quantity `json:"start_at_s"`
}

type FurnaceProgram struct {
	Segments []FurnaceSegment `json:"segments"`
}

type GasEvent struct {
	Index   int      `json:"index"`
	Time    Quantity `json:"time"`
	FromGas string   `json:"from_gas"`
	ToGas   string   `json:"to_gas"`
	Flow    Quantity `json:"flow"`
}

type GasEvents struct {
	Events []GasEvent `json:"events"`
}

type BlankPoint struct {
	T    Quantity `json:"t"`
	Mass Quantity `json:"mass"`
}

type BlankCurve struct {
	Points []BlankPoint `json:"points"`
}

type InitialMass struct {
	Mass    Quantity `json:"mass"`
	Note    string   `json:"note"`
	Balance string   `json:"balance_id"`
}

// ---------- public parse entry ----------

// ParseLayer parses raw text for a layer kind. It never panics on bad input:
// structural failure returns status rejected with diagnostics; local value
// failures produce status ok/partial with per-value diagnostics.
func ParseLayer(kind, raw string, p Params) (parsed any, status string, diags []Diagnostic) {
	switch kind {
	case KindRawSamples:
		return parseRawSamples(raw, p)
	case KindDriftCorrection:
		return parseDrift(raw)
	case KindFurnaceProgram:
		return parseFurnace(raw)
	case KindGasEvents:
		return parseGas(raw)
	case KindBlankCurve:
		return parseBlank(raw)
	case KindInitialMass:
		return parseInitialMass(raw)
	default:
		return nil, StatusRejected, []Diagnostic{{Code: "UNKNOWN_KIND", Where: kind, Message: "未知分层类型"}}
	}
}

// ---------- small format helpers ----------

func trimAll(s []string) []string {
	out := make([]string, len(s))
	for i := range s {
		out[i] = strings.TrimSpace(s[i])
	}
	return out
}

// splitValueUnit accepts "12.3", "12.3 s", "12.3s", "12.3|s".
func splitValueUnit(cell string) (string, string) {
	cell = strings.TrimSpace(cell)
	if cell == "" {
		return "", ""
	}
	if i := strings.Index(cell, "|"); i >= 0 {
		return strings.TrimSpace(cell[:i]), strings.TrimSpace(cell[i+1:])
	}
	// number followed by space/unit
	i := 0
	for i < len(cell) && (cell[i] >= '0' && cell[i] <= '9' || cell[i] == '.' ||
		cell[i] == '-' || cell[i] == '+' || cell[i] == 'e' || cell[i] == 'E') {
		i++
	}
	if i == 0 {
		return cell, ""
	}
	return strings.TrimSpace(cell[:i]), strings.TrimSpace(cell[i:])
}

// unitFromHeader pulls a unit from a header like "time(s)" or "temperature [°C]".
func unitFromHeader(h string) string {
	h = strings.TrimSpace(h)
	for _, pair := range [][2]string{{"(", ")"}, {"[", "]"}, {"{", "}"}} {
		a := strings.Index(h, pair[0])
		b := strings.Index(h, pair[1])
		if a >= 0 && b > a {
			return strings.TrimSpace(h[a+1 : b])
		}
	}
	return ""
}

func readCSVOrReject(raw, wantCols int, where string) ([][]string, []Diagnostic) {
	rd := csv.NewReader(strings.NewReader(strings.TrimSpace(raw)))
	rd.FieldsPerRecord = -1
	rd.TrimLeadingSpace = true
	recs, err := rd.ReadAll()
	if err != nil {
		return nil, []Diagnostic{{Code: "CSV_PARSE", Where: where,
			Message: "CSV 解析失败:" + err.Error()}}
	}
	if len(recs) < 2 {
		return nil, []Diagnostic{{Code: "CSV_EMPTY", Where: where,
			Message: "至少需要表头和一行数据"}}
	}
	for i := range recs {
		recs[i] = trimAll(recs[i])
	}
	hdr := recs[0]
	if len(hdr) < wantCols {
		return nil, []Diagnostic{{Code: "CSV_HEADER", Where: where,
			Message: fmt.Sprintf("表头需要 %d 列,实际 %d 列", wantCols, len(hdr))}}
	}
	return recs, nil
}

// colIndex finds a header column by any of the supplied name fragments.
func colIndex(hdr []string, names ...string) int {
	for i, h := range hdr {
		hl := strings.ToLower(h)
		for _, n := range names {
			if strings.Contains(hl, n) {
				return i
			}
		}
	}
	return -1
}

func parseRawSamples(raw string, p Params) (any, string, []Diagnostic) {
	recs, diags := readCSVOrReject(raw, 3, KindRawSamples)
	if diags != nil {
		return RawSamples{}, StatusRejected, diags
	}
	hdr := recs[0]
	iT := colIndex(hdr, "time", "t/")
	if iT < 0 {
		iT = 0
	}
	iTemp := colIndex(hdr, "temp", "temperature")
	if iTemp < 0 {
		iTemp = 1
	}
	iM := colIndex(hdr, "mass", "m/")
	if iM < 0 {
		iM = 2
	}
	iGap := colIndex(hdr, "gap", "missing")
	tU, tempU, mU := unitFromHeader(hdr[iT]), unitFromHeader(hdr[iTemp]), unitFromHeader(hdr[iM])
	rs := RawSamples{TimeUnitRaw: tU, TempUnitRaw: tempU, MassUnitRaw: mU}
	for r := 1; r < len(recs); r++ {
		row := recs[r]
		pt := SamplePoint{Index: r - 1}
		if len(row) <= max3(iT, iTemp, iM) {
			diags = append(diags, Diagnostic{"ROW_SHORT", fmt.Sprintf("row:%d", r), "列数不足,整行保留在诊断中"})
			continue
		}
		tv, tu := splitValueUnit(row[iT]); if tu == "" { tu = tU }
		xv, xu := splitValueUnit(row[iTemp]); if xu == "" { xu = tempU }
		mv, mu := splitValueUnit(row[iM]); if mu == "" { mu = mU }
		pt.Time = ParseQuantity(tv, tu, "time")
		pt.Temp = ParseQuantity(xv, xu, "temp")
		pt.Mass = ParseQuantity(mv, mu, "mass")
		if !pt.Time.OK {
			diags = append(diags, Diagnostic{Code: "BAD_TIME", Where: fmt.Sprintf("point:%d", pt.Index),
				Message: "时间无法转换:" + pt.Time.Diag})
		}
		if !pt.Temp.OK {
			diags = append(diags, Diagnostic{Code: "BAD_TEMP", Where: fmt.Sprintf("point:%d", pt.Index),
				Message: "温度无法转换:" + pt.Temp.Diag})
		}
		if !pt.Mass.OK {
			diags = append(diags, Diagnostic{Code: "BAD_MASS", Where: fmt.Sprintf("point:%d", pt.Index),
				Message: "质量无法转换,留在原位置:" + pt.Mass.Diag})
		}
		rs.Points = append(rs.Points, pt)
		if iGap >= 0 && len(row) > iGap && strings.EqualFold(row[iGap], "gap") {
			rs.DeclaredGap = append(rs.DeclaredGap, [2]int{pt.Index - 1, pt.Index})
		}
	}
	if len(rs.Points) == 0 {
		return rs, StatusRejected, append(diags, Diagnostic{"NO_POINTS", "raw_samples", "没有任何采样点"})
	}
	rs = detectGaps(rs, p)
	status := StatusOK
	if len(diags) > 0 {
		status = StatusPartial
	}
	return rs, status, diags
}

func max3(a, b, c int) int {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}

// detectGaps marks sampling gaps (declared or dt > gapFactor * median dt).
func detectGaps(rs RawSamples, p Params) RawSamples {
	dts := []float64{}
	for i := 1; i < len(rs.Points); i++ {
		if rs.Points[i].Time.OK && rs.Points[i-1].Time.OK {
			dts = append(dts, rs.Points[i].Time.Value-rs.Points[i-1].Time.Value)
		}
	}
	sort.Float64s(dts)
	med := 1.0
	if len(dts) > 0 {
		med = dts[len(dts)/2]
	}
	threshold := p.GapFactor * med
	for _, g := range rs.DeclaredGap {
		if g[0] >= 0 && g[0] < len(rs.Points) && g[1] < len(rs.Points) {
			rs.GapIntervalsS = append(rs.GapIntervalsS, GapInterval{
				FromT: rs.Points[g[0]].Time.Value, ToT: rs.Points[g[1]].Time.Value, Reason: "declared"})
		}
	}
	declared := map[[2]int]bool{}
	for _, g := range rs.DeclaredGap {
		declared[g] = true
	}
	for i := 1; i < len(rs.Points); i++ {
		a, b := rs.Points[i-1], rs.Points[i]
		if a.Time.OK && b.Time.OK && b.Time.Value-a.Time.Value > threshold &&
			!math.IsNaN(threshold) && threshold > 0 && !declared[[2]int{a.Index, b.Index}] {
			rs.GapIntervalsS = append(rs.GapIntervalsS, GapInterval{
				FromT: a.Time.Value, ToT: b.Time.Value, Reason: fmt.Sprintf("dt=%.3g>%.3g*median(%.3g)", b.Time.Value-a.Time.Value, p.GapFactor, med)})
		}
	}
	return rs
}

func parseDrift(raw string) (any, string, []Diagnostic) {
	var diags []Diagnostic
	t := strings.TrimSpace(raw)
	if strings.HasPrefix(t, "{") {
		var d DriftCorrection
		if err := json.Unmarshal([]byte(t), &d); err != nil {
			return DriftCorrection{}, StatusRejected,
				[]Diagnostic{{Code: "JSON_PARSE", Where: KindDriftCorrection, Message: err.Error()}}
		}
		if d.Mode != "linear" && d.Mode != "points" {
			diags = append(diags, Diagnostic{"BAD_MODE", "drift_correction.mode",
				"mode 必须是 linear 或 points"})
		}
		if d.Mode == "linear" {
			d.Slope = ParseQuantity(d.Slope.Raw, d.Slope.UnitRaw, "rate_mg_s")
			d.Intercept = ParseQuantity(d.Intercept.Raw, d.Intercept.UnitRaw, "mass")
			if !d.Slope.OK {
				diags = append(diags, Diagnostic{"BAD_SLOPE", "drift_correction.slope",
					"漂移斜率无法转换:" + d.Slope.Diag})
			}
		}
		for i := range d.Points {
			d.Points[i].T = ParseQuantity(d.Points[i].T.Raw, d.Points[i].T.UnitRaw, "temp")
			d.Points[i].Mass = ParseQuantity(d.Points[i].Mass.Raw, d.Points[i].Mass.UnitRaw, "mass")
		}
		if d.Mode == "points" && len(d.Points) < 2 {
			diags = append(diags, Diagnostic{"NO_POINTS", "drift_correction.points", "points 模式至少需要 2 点"})
		}
		status := StatusOK
		if len(diags) > 0 {
			status = StatusPartial
		}
		return d, status, diags
	}
	// CSV: t_C | mass_mg
	recs, dd := readCSVOrReject(raw, 2, KindDriftCorrection)
	if dd != nil {
		return DriftCorrection{}, StatusRejected, dd
	}
	hdr := recs[0]
	iT := colIndex(hdr, "t", "temp")
	if iT < 0 {
		iT = 0
	}
	iM := colIndex(hdr, "mass")
	if iM < 0 {
		iM = 1
	}
	d := DriftCorrection{Mode: "points"}
	for r := 1; r < len(recs); r++ {
		row := recs[r]
		tv, tu := splitValueUnit(row[iT])
		if tu == "" {
			tu = unitFromHeader(hdr[iT])
		}
		mv, mu := splitValueUnit(row[iM])
		if mu == "" {
			mu = unitFromHeader(hdr[iM])
		}
		tq := ParseQuantity(tv, tu, "temp")
		mq := ParseQuantity(mv, mu, "mass")
		if !tq.OK {
			diags = append(diags, Diagnostic{"BAD_TEMP", fmt.Sprintf("drift:row:%d", r), tq.Diag})
		}
		if !mq.OK {
			diags = append(diags, Diagnostic{"BAD_MASS", fmt.Sprintf("drift:row:%d", r), mq.Diag})
		}
		d.Points = append(d.Points, DriftPoint{T: tq, Mass: mq})
	}
	status := StatusOK
	if len(diags) > 0 {
		status = StatusPartial
	}
	return d, status, diags
}

func parseFurnace(raw string) (any, string, []Diagnostic) {
	var f FurnaceProgram
	var diags []Diagnostic
	if err := json.Unmarshal([]byte(raw), &f); err == nil && strings.Contains(raw, "segments") {
		// raw/unit preserved; normalise into the derived coordinate base here,
		// and recompute segment start times.
		t0 := 0.0
		for i := range f.Segments {
			f.Segments[i].Index = i
			f.Segments[i].StartAtS = ParseQuantity(fmt.Sprintf("%g", t0), "s", "time")
			f.Segments[i].StartT = ParseQuantity(f.Segments[i].StartT.Raw, fallbackUnit(f.Segments[i].StartT.UnitRaw, "C"), "temp")
			f.Segments[i].EndT = ParseQuantity(f.Segments[i].EndT.Raw, fallbackUnit(f.Segments[i].EndT.UnitRaw, "C"), "temp")
			f.Segments[i].Rate = ParseQuantity(f.Segments[i].Rate.Raw, fallbackUnit(f.Segments[i].Rate.UnitRaw, "C/min"), "rate_c_min")
			f.Segments[i].Duration = ParseQuantity(f.Segments[i].Duration.Raw, fallbackUnit(f.Segments[i].Duration.UnitRaw, "s"), "time")
			if f.Segments[i].Duration.OK {
				t0 += f.Segments[i].Duration.Value
			}
			if !f.Segments[i].StartT.OK {
				diags = append(diags, Diagnostic{"BAD_TEMP", fmt.Sprintf("segment:%d", i), "段起始温度无法转换"})
			}
			if !f.Segments[i].Duration.OK {
				diags = append(diags, Diagnostic{"BAD_DURATION", fmt.Sprintf("segment:%d", i), "段时长无法转换"})
			}
			if f.Segments[i].Type != "ramp" && f.Segments[i].Type != "hold" {
				diags = append(diags, Diagnostic{"BAD_TYPE", fmt.Sprintf("segment:%d", i), "段类型必须是 ramp 或 hold"})
			}
		}
		status := StatusOK
		if len(diags) > 0 {
			status = StatusPartial
		}
		if len(f.Segments) == 0 {
			return f, StatusRejected, []Diagnostic{{"NO_SEGMENTS", "furnace_program", "炉程序没有段"}}
		}
		return f, status, diags
	}
	// CSV: type,start_temp_C,end_temp_C,rate_C_per_min,duration_s
	recs, dd := readCSVOrReject(raw, 4, KindFurnaceProgram)
	if dd != nil {
		return FurnaceProgram{}, StatusRejected, dd
	}
	hdr := recs[0]
	iType := colIndex(hdr, "type", "mode")
	iSt := colIndex(hdr, "start")
	iEn := colIndex(hdr, "end")
	iRate := colIndex(hdr, "rate")
	iDur := colIndex(hdr, "duration", "hold")
	if iType < 0 {
		iType = 0
	}
	t0 := 0.0
	for r := 1; r < len(recs); r++ {
		row := recs[r]
		get := func(i int) string {
			if i >= 0 && i < len(row) {
				return row[i]
			}
			return ""
		}
		seg := FurnaceSegment{Index: r - 1, Type: strings.ToLower(get(iType))}
		sv, su := splitValueUnit(get(iSt))
		seg.StartT = ParseQuantity(sv, fallbackUnit(su, "C"), "temp")
		ev, eu := splitValueUnit(get(iEn))
		seg.EndT = ParseQuantity(ev, fallbackUnit(eu, "C"), "temp")
		rv, ru := splitValueUnit(get(iRate))
		seg.Rate = ParseQuantity(rv, fallbackUnit(ru, "C/min"), "rate_c_min")
		dv, du := splitValueUnit(get(iDur))
		seg.Duration = ParseQuantity(dv, fallbackUnit(du, "s"), "time")
		seg.StartAtS = ParseQuantity(fmt.Sprintf("%g", t0), "s", "time")
		if seg.Duration.OK {
			t0 += seg.Duration.Value
		}
		f.Segments = append(f.Segments, seg)
	}
	return f, StatusOK, nil
}

func fallbackUnit(u, d string) string {
	if strings.TrimSpace(u) == "" {
		return d
	}
	return u
}

func parseGas(raw string) (any, string, []Diagnostic) {
	recs, dd := readCSVOrReject(raw, 4, KindGasEvents)
	if dd != nil {
		return GasEvents{}, StatusRejected, dd
	}
	hdr := recs[0]
	iTime := colIndex(hdr, "time", "t")
	iFrom := colIndex(hdr, "from")
	iTo := colIndex(hdr, "to")
	iFlow := colIndex(hdr, "flow")
	if iTime < 0 {
		iTime = 0
	}
	if iTo < 0 {
		iTo = 2
	}
	g := GasEvents{}
	var diags []Diagnostic
	lastT := math.Inf(-1)
	for r := 1; r < len(recs); r++ {
		row := recs[r]
		get := func(i int) string {
			if i >= 0 && i < len(row) {
				return row[i]
			}
			return ""
		}
		ev := GasEvent{Index: r - 1}
		tv, tu := splitValueUnit(get(iTime))
		ev.Time = ParseQuantity(tv, fallbackUnit(tu, "s"), "time")
		ev.FromGas = get(iFrom)
		ev.ToGas = get(iTo)
		fv, fu := splitValueUnit(get(iFlow))
		ev.Flow = ParseQuantity(fv, fallbackUnit(fu, "mL/min"), "flow")
		if !ev.Time.OK {
			diags = append(diags, Diagnostic{"BAD_TIME", fmt.Sprintf("gas:%d", ev.Index), ev.Time.Diag})
		} else if ev.Time.Value < lastT {
			diags = append(diags, Diagnostic{"TIME_NOT_MONOTONIC", fmt.Sprintf("gas:%d", ev.Index),
				"阀事件时间早于上一事件"})
		} else {
			lastT = ev.Time.Value
		}
		if ev.ToGas == "" {
			diags = append(diags, Diagnostic{"NO_TARGET_GAS", fmt.Sprintf("gas:%d", ev.Index), "缺少目标气氛"})
		}
		g.Events = append(g.Events, ev)
	}
	status := StatusOK
	if len(diags) > 0 {
		status = StatusPartial
	}
	return g, status, diags
}

func parseBlank(raw string) (any, string, []Diagnostic) {
	recs, dd := readCSVOrReject(raw, 2, KindBlankCurve)
	if dd != nil {
		return BlankCurve{}, StatusRejected, dd
	}
	hdr := recs[0]
	iT := colIndex(hdr, "t", "temp")
	if iT < 0 {
		iT = 0
	}
	iM := colIndex(hdr, "mass")
	if iM < 0 {
		iM = 1
	}
	b := BlankCurve{}
	var diags []Diagnostic
	for r := 1; r < len(recs); r++ {
		row := recs[r]
		tv, tu := splitValueUnit(row[iT])
		mv, mu := splitValueUnit(row[iM])
		tq := ParseQuantity(tv, fallbackUnit(tu, unitFromHeader(hdr[iT])), "temp")
		mq := ParseQuantity(mv, fallbackUnit(mu, unitFromHeader(hdr[iM])), "mass")
		if !tq.OK || !mq.OK {
			diags = append(diags, Diagnostic{"BAD_POINT", fmt.Sprintf("blank:%d", r-1), "空白曲线点无法转换,保留原始值"})
		}
		b.Points = append(b.Points, BlankPoint{T: tq, Mass: mq})
	}
	status := StatusOK
	if len(diags) > 0 {
		status = StatusPartial
	}
	return b, status, diags
}

func parseInitialMass(raw string) (any, string, []Diagnostic) {
	var m struct {
		Raw       string `json:"raw"`
		Unit      string `json:"unit"`
		Note      string `json:"note"`
		BalanceID string `json:"balance_id"`
	}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return InitialMass{}, StatusRejected,
			[]Diagnostic{{Code: "JSON_PARSE", Where: KindInitialMass, Message: err.Error()}}
	}
	q := ParseQuantity(m.Raw, m.Unit, "mass")
	im := InitialMass{Mass: q, Note: m.Note, Balance: m.BalanceID}
	if !q.OK {
		return im, StatusPartial, []Diagnostic{{Code: "BAD_MASS", Where: "initial_mass", Message: q.Diag}}
	}
	return im, StatusOK, nil
}
