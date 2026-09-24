package tga

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type SamplePoint struct {
	T    Measure
	Mass Measure
	Temp Measure
}

type DriftPoint struct {
	T     Measure
	Drift Measure
}

type FurnacePoint struct {
	T        Measure
	Setpoint Measure
}

type GasEvent struct {
	T    Measure
	Gas  string
	Flow Measure
}

type BlankPoint struct {
	T    Measure
	Mass Measure
}

type Parsed struct {
	Samples   []SamplePoint
	Drift     []DriftPoint
	Furnace   []FurnacePoint
	Gas       []GasEvent
	Blank     []BlankPoint
	Initial   *Measure
	Diags     []Diag
	Fragments []Fragment
}

var unitBySuffix = map[string]Unit{
	"s":       UnitS,
	"min":     UnitMin,
	"mg":      UnitMg,
	"g":       UnitG,
	"C":       UnitC,
	"K":       UnitK,
	"pct":     UnitPct,
	"pct_min": UnitPctMin,
	"mL_min":  UnitMLMin,
}

type colDef struct {
	name  string
	unit  Unit
	text  bool
	known bool
}

func parseHeader(line string) ([]colDef, *Diag) {
	parts := strings.Split(line, "\t")
	defs := make([]colDef, len(parts))
	for i, p := range parts {
		p = strings.TrimSpace(p)
		idx := strings.Index(p, "_")
		if idx < 0 {
			defs[i] = colDef{name: p, text: true}
			continue
		}
		u, ok := unitBySuffix[p[idx+1:]]
		if !ok {
			return nil, &Diag{Code: "unit.unknown", Where: "header",
				Message: fmt.Sprintf("未知单位后缀 %q（列 %q）：整列保留原文，不进入派生层", p[idx+1:], p), Raw: p}
		}
		defs[i] = colDef{name: p[:idx], unit: u, known: true}
	}
	return defs, nil
}

func isNumChar(b byte) bool {
	return b >= '0' && b <= '9' || b == '.' || b == '-' || b == '+' || b == 'e' || b == 'E'
}

func parseMeasureCell(cell string, def colDef) (Measure, error) {
	cell = strings.TrimSpace(cell)
	if def.text {
		return Measure{Value: 0, Unit: UnitNone, Raw: cell}, nil
	}
	num := cell
	inline := ""
	i := len(cell)
	for i > 0 && !isNumChar(cell[i-1]) {
		i--
	}
	if i < len(cell) {
		num, inline = cell[:i], cell[i:]
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(num), 64)
	if err != nil {
		return Measure{}, fmt.Errorf("数值解析失败 %q", cell)
	}
	u := def.unit
	if inline != "" {
		iu, ok := unitBySuffix[inline]
		if !ok {
			return Measure{}, fmt.Errorf("未知单位 %q（保留原文 %q）", inline, cell)
		}
		u = iu
	}
	m := Measure{Value: v, Unit: u, Raw: cell}
	return Normalize(m)
}

// ParseSource parses one immutable raw layer. Good rows are normalized for
// the derived layer while every original cell stays in Raw. Bad rows never
// abort the whole import: they become diagnostics plus retained fragments.
func ParseSource(kind SourceKind, sourceID, raw string) *Parsed {
	p := &Parsed{}
	addDiag := func(d Diag, fragText string) {
		p.Diags = append(p.Diags, d)
		p.Fragments = append(p.Fragments, Fragment{SourceID: sourceID, Where: d.Where, Raw: fragText, Reason: d.Message})
	}

	var defs []colDef
	headerSeen := false
	rowNo := 0
	for li, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if !headerSeen {
			d, derr := parseHeader(trim)
			if derr != nil {
				addDiag(*derr, trim)
				return p
			}
			defs = d
			headerSeen = true
			continue
		}
		rowNo++
		cells := strings.Split(line, "\t")
		where := fmt.Sprintf("row %d (line %d)", rowNo, li+1)
		if len(cells) != len(defs) {
			addDiag(Diag{Code: "row.parse", Where: where, Message: fmt.Sprintf("字段数 %d 与表头 %d 不一致，该行保留原文", len(cells), len(defs))}, line)
			continue
		}
		ms := make([]Measure, len(cells))
		rowOK := true
		for ci := range cells {
			if defs[ci].text {
				ms[ci] = Measure{Raw: strings.TrimSpace(cells[ci])}
				continue
			}
			m, err := parseMeasureCell(cells[ci], defs[ci])
			if err != nil {
				code := "value.parse"
				if strings.Contains(err.Error(), "未知单位") {
					code = "unit.unknown"
				}
				addDiag(Diag{Code: code, Where: where, Message: err.Error()}, line)
				rowOK = false
				break
			}
			ms[ci] = m
		}
		if !rowOK {
			continue
		}
		col := func(name string) (Measure, int) {
			for i, d := range defs {
				if d.name == name {
					return ms[i], i
				}
			}
			return Measure{}, -1
		}
		switch kind {
		case SrcSamples:
			t, it := col("t")
			m, im := col("mass")
			tmp, ix := col("temp")
			if it < 0 || im < 0 || ix < 0 {
				addDiag(Diag{Code: "schema.missing-column", Where: "header", Message: "samples 需要 t/mass/temp 列"}, trim)
				return p
			}
			if t.Value < 0 || m.Value < 0 {
				addDiag(Diag{Code: "value.domain", Where: where, Message: "时间或质量不能为负，该行保留原文"}, line)
				continue
			}
			p.Samples = append(p.Samples, SamplePoint{T: t, Mass: m, Temp: tmp})
		case SrcDrift:
			t, it := col("t")
			d, id := col("drift")
			if it < 0 || id < 0 {
				addDiag(Diag{Code: "schema.missing-column", Where: "header", Message: "driftcorr 需要 t/drift 列"}, trim)
				return p
			}
			if t.Value < 0 {
				addDiag(Diag{Code: "value.domain", Where: where, Message: "时间不能为负，该行保留原文"}, line)
				continue
			}
			p.Drift = append(p.Drift, DriftPoint{T: t, Drift: d})
		case SrcFurnace:
			t, it := col("t")
			sp, isp := col("setpoint")
			if it < 0 || isp < 0 {
				addDiag(Diag{Code: "schema.missing-column", Where: "header", Message: "furnace 需要 t/setpoint 列"}, trim)
				return p
			}
			if t.Value < 0 {
				addDiag(Diag{Code: "value.domain", Where: where, Message: "时间不能为负，该行保留原文"}, line)
				continue
			}
			p.Furnace = append(p.Furnace, FurnacePoint{T: t, Setpoint: sp})
		case SrcGas:
			t, it := col("t")
			g, ig := col("gas")
			fl, ifl := col("flow")
			if it < 0 || ig < 0 || ifl < 0 {
				addDiag(Diag{Code: "schema.missing-column", Where: "header", Message: "gas 需要 t/gas/flow 列"}, trim)
				return p
			}
			if t.Value < 0 {
				addDiag(Diag{Code: "value.domain", Where: where, Message: "时间不能为负，该行保留原文"}, line)
				continue
			}
			p.Gas = append(p.Gas, GasEvent{T: t, Gas: strings.TrimSpace(g.Raw), Flow: fl})
		case SrcBlank:
			t, it := col("t")
			m, im := col("mass")
			if it < 0 || im < 0 {
				addDiag(Diag{Code: "schema.missing-column", Where: "header", Message: "blank 需要 t/mass 列"}, trim)
				return p
			}
			if t.Value < 0 || m.Value < 0 {
				addDiag(Diag{Code: "value.domain", Where: where, Message: "时间或质量不能为负，该行保留原文"}, line)
				continue
			}
			p.Blank = append(p.Blank, BlankPoint{T: t, Mass: m})
		case SrcInitial:
			m, im := col("mass")
			if im < 0 {
				addDiag(Diag{Code: "schema.missing-column", Where: "header", Message: "initial 需要 mass 列"}, trim)
				return p
			}
			if m.Value <= 0 {
				addDiag(Diag{Code: "value.domain", Where: where, Message: "样品初重必须为正，该行保留原文"}, line)
				continue
			}
			if p.Initial == nil {
				mm := m
				p.Initial = &mm
			}
		}
	}
	if !headerSeen {
		addDiag(Diag{Code: "row.parse", Where: "header", Message: "未找到表头行，导入半失败：原始文本已保存，无可用数据行"}, raw)
		return p
	}
	order := func(n int, tAt func(int) float64) bool {
		bad := false
		for i := 1; i < n; i++ {
			if tAt(i) < tAt(i-1) {
				bad = true
			}
		}
		return bad
	}
	switch kind {
	case SrcSamples:
		if len(p.Samples) > 1 && order(len(p.Samples), func(i int) float64 { return p.Samples[i].T.Value }) {
			p.Diags = append(p.Diags, Diag{Code: "row.reordered", Where: "samples", Message: "采样点时间非单调，已按时间重排并保留诊断"})
			sort.SliceStable(p.Samples, func(i, j int) bool { return p.Samples[i].T.Value < p.Samples[j].T.Value })
		}
	case SrcDrift:
		if len(p.Drift) > 1 && order(len(p.Drift), func(i int) float64 { return p.Drift[i].T.Value }) {
			p.Diags = append(p.Diags, Diag{Code: "row.reordered", Where: "driftcorr", Message: "漂移点时间非单调，已按时间重排"})
			sort.SliceStable(p.Drift, func(i, j int) bool { return p.Drift[i].T.Value < p.Drift[j].T.Value })
		}
	case SrcFurnace:
		if len(p.Furnace) > 1 && order(len(p.Furnace), func(i int) float64 { return p.Furnace[i].T.Value }) {
			p.Diags = append(p.Diags, Diag{Code: "row.reordered", Where: "furnace", Message: "炉程序点时间非单调，已按时间重排"})
			sort.SliceStable(p.Furnace, func(i, j int) bool { return p.Furnace[i].T.Value < p.Furnace[j].T.Value })
		}
	case SrcGas:
		if len(p.Gas) > 1 && order(len(p.Gas), func(i int) float64 { return p.Gas[i].T.Value }) {
			p.Diags = append(p.Diags, Diag{Code: "row.reordered", Where: "gas", Message: "阀事件时间非单调，已按时间重排"})
			sort.SliceStable(p.Gas, func(i, j int) bool { return p.Gas[i].T.Value < p.Gas[j].T.Value })
		}
	case SrcBlank:
		if len(p.Blank) > 1 && order(len(p.Blank), func(i int) float64 { return p.Blank[i].T.Value }) {
			p.Diags = append(p.Diags, Diag{Code: "row.reordered", Where: "blank", Message: "空白点时间非单调，已按时间重排"})
			sort.SliceStable(p.Blank, func(i, j int) bool { return p.Blank[i].T.Value < p.Blank[j].T.Value })
		}
	}
	return p
}
