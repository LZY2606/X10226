package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ParseError 是明确的格式错误；导入层据此拒绝该层但不影响其他层。
type ParseError struct {
	Kind    string
	Line    int
	Message string
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("%s: line %d: %s", e.Kind, e.Line, e.Message)
}

// splitCSVLines 切分文本行，跳过空行与 # 注释。
func splitCSVLines(raw string) []string {
	var out []string
	for _, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		out = append(out, ln)
	}
	return out
}

// parseHeaderUnit 解析形如 time_s / mass_mg / temp_C 的表头，返回字段名与单位。
func parseHeaderUnit(h string) (field, unit string, err error) {
	i := strings.Index(h, "_")
	if i <= 0 || i == len(h)-1 {
		return "", "", fmt.Errorf("header %q must carry a unit suffix like time_s or mass_mg", h)
	}
	return h[:i], h[i+1:], nil
}

// ParseRawPoints 解析原始采样点 CSV：表头必须携带单位，如 time_s,mass_mg,temp_C。
// 原始表示（行号、原始单位）全部保留；无法解析的行是格式错误。
func ParseRawPoints(kind, raw string) (*RawPointsDoc, error) {
	lines := splitCSVLines(raw)
	if len(lines) < 2 {
		return nil, &ParseError{Kind: kind, Line: 1, Message: "need header plus at least one data row"}
	}
	head := strings.Split(lines[0], ",")
	if len(head) != 3 {
		return nil, &ParseError{Kind: kind, Line: 1, Message: "expected 3 columns: time_<unit>,mass_<unit>,temp_<unit>"}
	}
	var fields, units [3]string
	for i, h := range head {
		f, u, err := parseHeaderUnit(strings.TrimSpace(h))
		if err != nil {
			return nil, &ParseError{Kind: kind, Line: 1, Message: err.Error()}
		}
		fields[i], units[i] = f, u
	}
	if fields[0] != "time" || fields[1] != "mass" || fields[2] != "temp" {
		return nil, &ParseError{Kind: kind, Line: 1, Message: "columns must be time,mass,temp in order"}
	}
	doc := &RawPointsDoc{}
	for i, ln := range lines[1:] {
		row := i + 2
		cols := strings.Split(ln, ",")
		if len(cols) != 3 {
			return nil, &ParseError{Kind: kind, Line: row, Message: "expected 3 columns"}
		}
		var vals [3]float64
		for c, s := range cols {
			v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return nil, &ParseError{Kind: kind, Line: row, Message: fmt.Sprintf("column %d: %v", c+1, err)}
			}
			vals[c] = v
		}
		doc.Points = append(doc.Points, RawPoint{
			Row:  row,
			Time: Quantity{Value: vals[0], Unit: units[0], Basis: "elapsed"},
			Mass: Quantity{Value: vals[1], Unit: units[1], Basis: "balance-readout"},
			Temp: Quantity{Value: vals[2], Unit: units[2], Basis: "sample-thermocouple"},
		})
	}
	return doc, nil
}

// ParseDrift 解析天平漂移校正 JSON。
func ParseDrift(kind, raw string) (*DriftCorrection, error) {
	var d DriftCorrection
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, &ParseError{Kind: kind, Line: 1, Message: err.Error()}
	}
	if d.Model != "linear" {
		return nil, &ParseError{Kind: kind, Line: 1, Message: fmt.Sprintf("unsupported drift model %q (want \"linear\")", d.Model)}
	}
	if d.Slope.Unit == "" || d.Intercept.Unit == "" {
		return nil, &ParseError{Kind: kind, Line: 1, Message: "slope and intercept must carry units"}
	}
	return &d, nil
}

// ParseFurnace 解析炉程序 CSV：start_<u>,end_<u>,starttemp_<u>,endtemp_<u>。
func ParseFurnace(kind, raw string) (*FurnaceProgram, error) {
	lines := splitCSVLines(raw)
	if len(lines) < 2 {
		return nil, &ParseError{Kind: kind, Line: 1, Message: "need header plus at least one segment"}
	}
	head := strings.Split(lines[0], ",")
	if len(head) != 4 {
		return nil, &ParseError{Kind: kind, Line: 1, Message: "expected 4 columns: start_<unit>,end_<unit>,starttemp_<unit>,endtemp_<unit>"}
	}
	var units [4]string
	for i, h := range head {
		_, u, err := parseHeaderUnit(strings.TrimSpace(h))
		if err != nil {
			return nil, &ParseError{Kind: kind, Line: 1, Message: err.Error()}
		}
		units[i] = u
	}
	fp := &FurnaceProgram{}
	for i, ln := range lines[1:] {
		row := i + 2
		cols := strings.Split(ln, ",")
		if len(cols) != 4 {
			return nil, &ParseError{Kind: kind, Line: row, Message: "expected 4 columns"}
		}
		var v [4]float64
		for c, s := range cols {
			x, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
			if err != nil {
				return nil, &ParseError{Kind: kind, Line: row, Message: fmt.Sprintf("column %d: %v", c+1, err)}
			}
			v[c] = x
		}
		fp.Segments = append(fp.Segments, FurnaceSegment{
			StartTime: Quantity{Value: v[0], Unit: units[0], Basis: "elapsed"},
			EndTime:   Quantity{Value: v[1], Unit: units[1], Basis: "elapsed"},
			StartTemp: Quantity{Value: v[2], Unit: units[2], Basis: "furnace-setpoint"},
			EndTemp:   Quantity{Value: v[3], Unit: units[3], Basis: "furnace-setpoint"},
		})
	}
	return fp, nil
}

// ParseGasEvents 解析气体阀事件 CSV：time_<unit>,from_gas,to_gas,flow_<unit>。
func ParseGasEvents(kind, raw string) (*GasEventsDoc, error) {
	lines := splitCSVLines(raw)
	if len(lines) < 1 {
		return nil, &ParseError{Kind: kind, Line: 1, Message: "empty gas events"}
	}
	head := strings.Split(lines[0], ",")
	if len(head) != 4 {
		return nil, &ParseError{Kind: kind, Line: 1, Message: "expected 4 columns: time_<unit>,from_gas,to_gas,flow_<unit>"}
	}
	_, tu, err := parseHeaderUnit(strings.TrimSpace(head[0]))
	if err != nil {
		return nil, &ParseError{Kind: kind, Line: 1, Message: err.Error()}
	}
	_, fu, err := parseHeaderUnit(strings.TrimSpace(head[3]))
	if err != nil {
		return nil, &ParseError{Kind: kind, Line: 1, Message: err.Error()}
	}
	doc := &GasEventsDoc{}
	for i, ln := range lines[1:] {
		row := i + 2
		cols := strings.Split(ln, ",")
		if len(cols) != 4 {
			return nil, &ParseError{Kind: kind, Line: row, Message: "expected 4 columns"}
		}
		t, err := strconv.ParseFloat(strings.TrimSpace(cols[0]), 64)
		if err != nil {
			return nil, &ParseError{Kind: kind, Line: row, Message: err.Error()}
		}
		fl, err := strconv.ParseFloat(strings.TrimSpace(cols[3]), 64)
		if err != nil {
			return nil, &ParseError{Kind: kind, Line: row, Message: err.Error()}
		}
		doc.Events = append(doc.Events, GasEvent{
			Time:    Quantity{Value: t, Unit: tu, Basis: "valve-clock"},
			FromGas: strings.TrimSpace(cols[1]),
			ToGas:   strings.TrimSpace(cols[2]),
			Flow:    Quantity{Value: fl, Unit: fu, Basis: "mfc-setpoint"},
		})
	}
	return doc, nil
}

// ParseBlank 解析空白曲线 CSV：time_<unit>,mass_<unit>。
func ParseBlank(kind, raw string) (*BlankCurve, error) {
	lines := splitCSVLines(raw)
	if len(lines) < 2 {
		return nil, &ParseError{Kind: kind, Line: 1, Message: "need header plus at least one row"}
	}
	head := strings.Split(lines[0], ",")
	if len(head) != 2 {
		return nil, &ParseError{Kind: kind, Line: 1, Message: "expected 2 columns: time_<unit>,mass_<unit>"}
	}
	_, tu, err := parseHeaderUnit(strings.TrimSpace(head[0]))
	if err != nil {
		return nil, &ParseError{Kind: kind, Line: 1, Message: err.Error()}
	}
	_, mu, err := parseHeaderUnit(strings.TrimSpace(head[1]))
	if err != nil {
		return nil, &ParseError{Kind: kind, Line: 1, Message: err.Error()}
	}
	bc := &BlankCurve{}
	for i, ln := range lines[1:] {
		row := i + 2
		cols := strings.Split(ln, ",")
		if len(cols) != 2 {
			return nil, &ParseError{Kind: kind, Line: row, Message: "expected 2 columns"}
		}
		t, err := strconv.ParseFloat(strings.TrimSpace(cols[0]), 64)
		if err != nil {
			return nil, &ParseError{Kind: kind, Line: row, Message: err.Error()}
		}
		m, err := strconv.ParseFloat(strings.TrimSpace(cols[1]), 64)
		if err != nil {
			return nil, &ParseError{Kind: kind, Line: row, Message: err.Error()}
		}
		bc.Points = append(bc.Points, BlankPoint{
			Time: Quantity{Value: t, Unit: tu, Basis: "elapsed"},
			Mass: Quantity{Value: m, Unit: mu, Basis: "blank-pan"},
		})
	}
	return bc, nil
}

// ParseInitialMass 解析样品初重 JSON。
func ParseInitialMass(kind, raw string) (*InitialMass, error) {
	var m InitialMass
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, &ParseError{Kind: kind, Line: 1, Message: err.Error()}
	}
	if m.Mass.Unit == "" {
		return nil, &ParseError{Kind: kind, Line: 1, Message: "mass must carry a unit"}
	}
	if m.Basis == "" {
		return nil, &ParseError{Kind: kind, Line: 1, Message: "mass must carry a weighing basis (e.g. as-received, dry)"}
	}
	return &m, nil
}

// Validate 按层类型解析并校验原始文本；原始字节始终由调用方另行保存。
func Validate(kind, raw string) error {
	var err error
	switch kind {
	case InputRawPoints:
		_, err = ParseRawPoints(kind, raw)
	case InputDrift:
		_, err = ParseDrift(kind, raw)
	case InputFurnace:
		_, err = ParseFurnace(kind, raw)
	case InputGasEvents:
		_, err = ParseGasEvents(kind, raw)
	case InputBlank:
		_, err = ParseBlank(kind, raw)
	case InputInitialMass:
		_, err = ParseInitialMass(kind, raw)
	default:
		err = &ParseError{Kind: kind, Line: 0, Message: "unknown input kind"}
	}
	return err
}
