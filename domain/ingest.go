// Package domain: ingest 负责把可复制的小型文本样例解析为六个原始分层。
//
// 规则:
//   - 原始文本逐字保存进 Submission.RawText; 每个值保留原始表示(RawText)。
//   - 派生层才做规范化(mg/degC); 无法转换的值留在原位置(status=invalid)+诊断。
//   - 明确的格式错误不导致整体静默通过: 能解析的分层照样保存, 状态为 partial
//     (导入半失败); 整份为空则 rejected。
package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// 文本导入格式(行式, 可复制):
//
//	# 注释
//	title: 初版基线
//	## samples
//	sample  10.00 min  50.12 mg  30.2 C
//	## drift
//	drift   12.0  min  +0.03 mg  恒温段锚点
//	## furnace
//	ramp    0.0   120.0 min  30.0  800.0 C
//	## gas
//	gas     60.0  min  N2 -> O2
//	## blank
//	blank   10.0  min  -0.01 mg
//	## initial_mass
//	m0      50.00 mg  balance_reading  装样后天平读数
//	## params
//	param   atmoDelayMin = 2.0
//
// 时间/数值也可写作炉程序分钟数裸数字; 未识别分层/坏行会产生诊断。

type parser struct {
	lines   []string
	data    LayerData
	diags   []Diagnostic
	present map[Layer]bool
	section string
	seq     map[string]int
	title   string
}

// ParseImport 解析一份导入文本。它永不 panic: 格式错误以诊断返回,
// 已成功解析的分层保留。
func ParseImport(text string) (LayerData, map[Layer]bool, string, []Diagnostic) {
	p := &parser{
		lines:   strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n"),
		present: map[Layer]bool{},
		seq:     map[string]int{},
	}
	for i, raw := range p.lines {
		lineNo := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "##") {
			continue
		}
		if strings.HasPrefix(line, "##") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "##"))
			p.section = canonicalSection(name)
			if p.section == "" {
				p.diags = append(p.diags, Diagnostic{
					Code: "unknown_section", Line: lineNo,
					Message: fmt.Sprintf("未识别分层 %q, 其下内容将忽略", name),
				})
			}
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "title:") {
			p.title = strings.TrimSpace(line[len("title:"):])
			continue
		}
		if p.section == "" {
			// 允许 ## params 之外的 param 行
			if isParamLine(line) {
				p.section = "params"
			} else {
				p.diags = append(p.diags, Diagnostic{
					Code: "outside_section", Line: lineNo,
					Message: "该行不属于任何分层, 已忽略",
				})
				continue
			}
		}
		p.parseRow(lineNo, line)
	}
	return p.data, p.present, p.title, p.diags
}

func canonicalSection(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "samples", "sample", "tg":
		return "samples"
	case "drift", "drift anchors", "driftanchors":
		return "drift"
	case "furnace", "furnace program", "program":
		return "furnace"
	case "gas", "gas events", "gasvalve", "valve":
		return "gas"
	case "blank", "blank curve", "blankcurve":
		return "blank"
	case "initial_mass", "initial mass", "m0", "initialmass":
		return "initial_mass"
	case "params", "parameters", "param":
		return "params"
	}
	return ""
}

func isParamLine(line string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "param")
}

// nextSeq 为每个分层维护稳定的行内序号。
func (p *parser) nextSeq(section string) int {
	p.seq[section]++
	return p.seq[section]
}

func (p *parser) parseRow(lineNo int, line string) {
	section := p.section
	if section == "params" {
		p.parseParam(lineNo, line)
		return
	}
	p.present[Layer(section)] = true
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}
	// 去掉行类型标记词(sample/ramp/gas/blank/drift/m0...)
	fields = fields[1:]
	diag := func(code, msg string) Diagnostic {
		return Diagnostic{Code: code, Line: lineNo, Layer: section, Message: msg}
	}
	switch section {
	case "samples":
		s := RawSample{Seq: p.nextSeq(section), Line: lineNo}
		if len(fields) < 5 {
			p.diags = append(p.diags, diag("malformed_sample",
				"采样点至少需要 时间 单位 质量 单位 温度 单位, 该行按缺口处理"))
			s.Time = badQTime(fields, lineNo)
			s.Mass = Quant{Status: StatusUnknown, RawText: joinOrEmpty(fields)}
			s.Temp = Quant{Status: StatusUnknown}
		} else {
			s.Time = parseQTime(fields[0], fields[1], lineNo)
			s.Mass = parseQuant(fields[2], fields[3], "mg", lineNo)
			s.Temp = parseQuant(fields[4], fields[5], "degC", lineNo)
		}
		p.data.Samples = append(p.data.Samples, s)
	case "drift":
		if len(fields) < 4 {
			p.diags = append(p.diags, diag("malformed_drift", "漂移锚点字段不足, 已丢弃该行"))
			return
		}
		a := DriftAnchor{
			Seq: p.nextSeq(section), Line: lineNo,
			Time:   parseQTime(fields[0], fields[1], lineNo),
			Offset: parseQuant(fields[2], fields[3], "mg", lineNo),
			Note:   strings.Join(fields[4:], " "),
		}
		p.data.DriftAnchors = append(p.data.DriftAnchors, a)
	case "furnace":
		if len(fields) < 5 {
			p.diags = append(p.diags, diag("malformed_furnace",
				"炉程序段格式: ramp <start> <end> <tunit> <from> <to> <tempUnit(可共享)>, 已丢弃该行"))
			return
		}
		seg := FurnaceSeg{
			Seq:   p.nextSeq(section),
			Line:  lineNo,
			Kind:  "ramp",
			Start: parseQTime(fields[0], fields[1], lineNo),
			End:   parseQTime(fields[2], fields[1], lineNo),
		}
		switch {
		case len(fields) >= 6 && isUnit(fields[5]):
			seg.From = parseQuant(fields[3], fields[5], "degC", lineNo)
			seg.To = parseQuant(fields[4], fields[5], "degC", lineNo)
		case len(fields) >= 6 && isUnit(fields[4]):
			// 30.0 C 800.0 C 形式
			seg.From = parseQuant(fields[3], fields[4], "degC", lineNo)
			seg.To = parseQuant(fields[5], fields[6], "degC", lineNo)
		default:
			seg.From = parseQuant(fields[3], "C", "degC", lineNo)
			seg.To = parseQuant(fields[4], "C", "degC", lineNo)
		}
		p.data.FurnaceSegs = append(p.data.FurnaceSegs, seg)
	case "gas":
		if len(fields) < 5 {
			p.diags = append(p.diags, diag("malformed_gas",
				"气体事件格式: <time> <unit> <from> -> <to>, 已丢弃该行"))
			return
		}
		arrow := -1
		for i, f := range fields {
			if f == "->" || f == "=>" {
				arrow = i
				break
			}
		}
		if arrow < 2 || arrow != len(fields)-2 {
			p.diags = append(p.diags, diag("malformed_gas", "缺少 '->' 或气体名称不完整, 已丢弃该行"))
			return
		}
		g := GasEvent{
			Seq:     p.nextSeq(section),
			Line:    lineNo,
			Time:    parseQTime(fields[0], fields[1], lineNo),
			FromGas: fields[arrow-1],
			ToGas:   fields[arrow+1],
			RawText: line,
		}
		p.data.GasEvents = append(p.data.GasEvents, g)
	case "blank":
		if len(fields) < 3 {
			p.diags = append(p.diags, diag("malformed_blank", "空白点至少需要 时间 单位 质量 单位, 已丢弃"))
			return
		}
		bp := BlankPoint{
			Seq:  p.nextSeq(section),
			Line: lineNo,
			Time: parseQTime(fields[0], fields[1], lineNo),
		}
		if len(fields) >= 4 {
			bp.Mass = parseQuant(fields[2], fields[3], "mg", lineNo)
		} else {
			bp.Mass = parseQuant(fields[2], "mg", "mg", lineNo)
		}
		p.data.Blank = append(p.data.Blank, bp)
	case "initial_mass":
		if len(fields) < 2 {
			p.diags = append(p.diags, diag("malformed_m0", "初重格式: <mass> <unit> [basis] [note...], 已丢弃"))
			return
		}
		im := InitialMass{Seq: p.nextSeq(section), Line: lineNo}
		if len(fields) >= 3 && isUnit(fields[1]) {
			im.Mass = parseQuant(fields[0], fields[1], "mg", lineNo)
			rest := fields[2:]
			if len(rest) > 0 {
				im.Basis = rest[0]
				im.Note = strings.Join(rest[1:], " ")
			}
		} else {
			im.Mass = parseQuant(fields[0], "mg", "mg", lineNo)
			rest := fields[1:]
			if len(rest) > 0 {
				im.Basis = rest[0]
				im.Note = strings.Join(rest[1:], " ")
			}
		}
		p.data.InitialMasses = append(p.data.InitialMasses, im)
	default:
		p.diags = append(p.diags, diag("unparsed_row", "未处理行"))
	}
}

// parseParam 解析 "param key = value [note...]" 行, 保留原始 value 文本。
func (p *parser) parseParam(lineNo int, line string) {
	body := line
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(body)), "param") {
		body = strings.TrimSpace(body)[len("param"):]
	}
	eq := strings.Index(body, "=")
	if eq < 0 {
		p.diags = append(p.diags, Diagnostic{
			Code: "malformed_param", Line: lineNo, Layer: "params",
			Message: "参数行需要 key = value 形式",
		})
		return
	}
	key := strings.TrimSpace(body[:eq])
	rest := strings.TrimSpace(body[eq+1:])
	note := ""
	if sp := strings.IndexAny(rest, " \t"); sp >= 0 {
		note = strings.TrimSpace(rest[sp:])
		rest = strings.TrimSpace(rest[:sp])
	}
	if key == "" {
		p.diags = append(p.diags, Diagnostic{
			Code: "malformed_param", Line: lineNo, Layer: "params",
			Message: "参数键为空",
		})
		return
	}
	p.data.Params = append(p.data.Params, RawParam{
		Line: lineNo, Key: key, RawText: rest, Note: note,
	})
}

func joinOrEmpty(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

func badQTime(fields []string, lineNo int) QTime {
	return QTime{Coord: CoordFurnaceMinute, RawText: joinOrEmpty(fields),
		Diagnostic: "时间无法解析, 该点形成采样缺口"}
}

// isUnit 判断 token 是否是已知单位。
func isUnit(tok string) bool {
	switch strings.ToLower(tok) {
	case "mg", "g", "ug", "µg", "c", "°c", "k", "f", "°f",
		"min", "s", "minute", "minutes", "sec", "secs":
		return true
	}
	return false
}

// toBaseMass 把带单位的质量数值规范化为 mg。
func toBaseMass(v float64, unit string) (float64, bool) {
	switch strings.ToLower(unit) {
	case "mg":
		return v, true
	case "g":
		return v * 1000, true
	case "ug", "µg":
		return v / 1000, true
	}
	return 0, false
}

// toDegC 把温度规范化为摄氏度。
func toDegC(v float64, unit string) (float64, bool) {
	switch strings.ToLower(unit) {
	case "c", "°c":
		return v, true
	case "k":
		return v - 273.15, true
	case "f", "°f":
		return (v - 32) * 5 / 9, true
	}
	return 0, false
}

func toMinutes(v float64, unit string) (float64, bool) {
	switch strings.ToLower(unit) {
	case "min", "minute", "minutes":
		return v, true
	case "s", "sec", "secs":
		return v / 60, true
	}
	return 0, false
}

// parseQuant 解析 "<number> <unit>", 规范化到目标基准单位(baseUnit: mg/degC)。
// 无法转换时返回 status=invalid 并保留原始表示; 空值返回 unknown。
func parseQuant(numTok, unitTok, baseUnit string, lineNo int) Quant {
	q := Quant{RawText: strings.TrimSpace(numTok + " " + unitTok), RawUnit: unitTok}
	if strings.TrimSpace(numTok) == "" || strings.EqualFold(numTok, "?") || strings.EqualFold(numTok, "na") {
		q.Status = StatusUnknown
		q.Unit = baseUnit
		q.Diagnostic = "值缺失"
		return q
	}
	v, err := strconv.ParseFloat(strings.TrimPrefix(numTok, "+"), 64)
	if err != nil {
		q.Status = StatusInvalid
		q.Unit = baseUnit
		q.Diagnostic = fmt.Sprintf("第 %d 行数值 %q 无法解析为数字", lineNo, numTok)
		return q
	}
	switch baseUnit {
	case "mg":
		if nv, ok := toBaseMass(v, unitTok); ok {
			q.Value, q.Unit, q.Status = nv, "mg", StatusOK
		} else {
			q.Status = StatusInvalid
			q.Unit = "mg"
			q.Diagnostic = fmt.Sprintf("第 %d 行质量单位 %q 无法转换为 mg", lineNo, unitTok)
		}
	case "degC":
		if nv, ok := toDegC(v, unitTok); ok {
			q.Value, q.Unit, q.Status = nv, "degC", StatusOK
		} else {
			q.Status = StatusInvalid
			q.Unit = "degC"
			q.Diagnostic = fmt.Sprintf("第 %d 行温度单位 %q 无法转换为 degC", lineNo, unitTok)
		}
	}
	return q
}

// parseQTime 解析炉时间并规范化为 furnace minute。
func parseQTime(numTok, unitTok string, lineNo int) QTime {
	qt := QTime{Coord: CoordFurnaceMinute, RawText: strings.TrimSpace(numTok + " " + unitTok)}
	v, err := strconv.ParseFloat(numTok, 64)
	if err != nil {
		qt.Diagnostic = fmt.Sprintf("第 %d 行时间 %q 无法解析, 形成采样缺口", lineNo, numTok)
		return qt
	}
	if nv, ok := toMinutes(v, unitTok); ok {
		qt.Minutes = nv
	} else {
		qt.Diagnostic = fmt.Sprintf("第 %d 行时间单位 %q 无法转换为 min", lineNo, unitTok)
	}
	return qt
}
