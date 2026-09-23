// Package parse ingests copy-pasteable "tgax bundle v1" text into immutable
// input fragments. Original tokens are retained; unconvertible values stay in
// place with diagnostics instead of being dropped for plotting.
package parse

import (
	"fmt"
	"strconv"
	"strings"

	"tgax/internal/model"
)

type Result struct {
	Kind        string
	Version     model.InputVersion
	Diagnostics []model.Diagnostic
}

const header = "# tgax bundle v1"

func addDiag(d *[]model.Diagnostic, scope string, line int, code, msg, raw string) {
	*d = append(*d, model.Diagnostic{Scope: scope, Line: line, Code: code, Message: msg, Raw: raw})
}

func qNum(scope string, line int, tok, wantUnit string, diags *[]model.Diagnostic) model.Quantity {
	q := model.Quantity{Raw: tok, RawUnit: wantUnit, Unit: wantUnit}
	num, unit, err := splitNumUnit(tok)
	if err != nil {
		// token may be a bare number with unit supplied by column header
		num = strings.TrimSpace(tok)
		unit = wantUnit
		if _, err2 := strconv.ParseFloat(num, 64); err2 != nil {
			q.Status = "unconvertible"
			q.Diagnostic = err.Error()
			addDiag(diags, scope, line, "bad_number", fmt.Sprintf("无法解析数值 %q", tok), tok)
			return q
		}
	}
	v, err := convert(num, unit, wantUnit)
	if err != nil {
		q.Status = "unconvertible"
		q.Diagnostic = err.Error()
		addDiag(diags, scope, line, "bad_unit", err.Error(), tok)
		return q
	}
	q.Value = &v
	q.Status = "ok"
	return q
}

func splitNumUnit(tok string) (num, unit string, err error) {
	tok = strings.TrimSpace(tok)
	i := strings.IndexAny(tok, "0123456789.+-eE")
	_ = i
	// find first non-numeric run after number
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if c >= '0' && c <= '9' || c == '.' || c == '+' || c == '-' || c == 'e' || c == 'E' {
			continue
		}
		n := strings.TrimSpace(tok[:i])
		u := strings.TrimSpace(tok[i:])
		if _, e := strconv.ParseFloat(n, 64); e != nil {
			return "", "", fmt.Errorf("无法解析数值/单位 %q", tok)
		}
		return n, u, nil
	}
	if _, e := strconv.ParseFloat(tok, 64); e == nil {
		return tok, "", nil
	}
	return "", "", fmt.Errorf("无法解析数值/单位 %q", tok)
}

func convert(num, unit, want string) (float64, error) {
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, err
	}
	if unit == "" {
		return v, nil // unit comes from column header; already canonical
	}
	switch want {
	case "s":
		switch strings.ToLower(unit) {
		case "s", "sec", "secs", "second", "seconds":
			return v, nil
		case "min", "mins", "minute", "minutes":
			return v * 60, nil
		case "h", "hr", "hrs", "hour", "hours":
			return v * 3600, nil
		}
	case "°C", "C", "degC":
		switch {
		case unit == "°C" || strings.EqualFold(unit, "C") || strings.EqualFold(unit, "degC"):
			return v, nil
		case unit == "K":
			return v - 273.15, nil
		case unit == "°F" || strings.EqualFold(unit, "F") || strings.EqualFold(unit, "degF"):
			return (v - 32) * 5 / 9, nil
		}
	case "mg":
		switch strings.ToLower(unit) {
		case "mg":
			return v, nil
		case "g":
			return v * 1000, nil
		case "ug", "µg":
			return v / 1000, nil
		case "kg":
			return v * 1e6, nil
		}
	case "mg/s":
		switch strings.ToLower(unit) {
		case "mg/s":
			return v, nil
		case "mg/min":
			return v / 60, nil
		case "g/s":
			return v * 1000, nil
		}
	}
	return 0, fmt.Errorf("无法把单位 %q 转换为 %q", unit, want)
}

// ParseBundle parses the six-layer text bundle. Missing or broken sections are
// reported; valid fragments are still returned (half-failure import).
func ParseBundle(kind string, text string) (map[string]model.InputVersion, []model.Diagnostic) {
	out := map[string]model.InputVersion{}
	var diags []model.Diagnostic
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	headerSeen := false
	var section string
	var rawRows []model.RawRow
	flush := func() {
		if section == "" || len(rawRows) == 0 {
			return
		}
		v, sd := parseSection(section, rawRows)
		diags = append(diags, sd...)
		if v != nil {
			out[section] = *v
		}
	}
	for n, line := range lines {
		ln := n + 1
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "# tgax bundle") {
			headerSeen = true
			continue
		}
		if strings.HasPrefix(trim, "## ") {
			flush()
			section = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(trim, "## ")))
			rawRows = nil
			continue
		}
		if !headerSeen {
			addDiag(&diags, "bundle", ln, "missing_header", "缺少 # tgax bundle v1 文件头", trim)
			headerSeen = true
		}
		if section == "" {
			addDiag(&diags, "bundle", ln, "outside_section", "数据行出现在任何 ## 节之前", trim)
			continue
		}
		if strings.HasPrefix(trim, "#") {
			// column-header / field-hint row: kept as raw, not parsed as data
			rawRows = append(rawRows, model.RawRow{Line: ln, Cells: map[string]string{"header": strings.TrimSpace(trim)}, Raw: line})
			continue
		}
		fields := strings.Fields(trim)
		cells := map[string]string{}
		for i, f := range fields {
			cells[strconv.Itoa(i)] = f
		}
		rawRows = append(rawRows, model.RawRow{Line: ln, Cells: cells, Raw: line})
	}
	flush()
	for _, k := range []string{"SAMPLES", "DRIFT", "FURNACE", "VALVES", "BLANK", "MASS0"} {
		if _, ok := out[k]; !ok {
			addDiag(&diags, k, 0, "missing_section", "缺少 "+k+" 分层", "")
		}
	}
	return out, diags
}

func dataRows(rows []model.RawRow) []model.RawRow {
	var d []model.RawRow
	for _, r := range rows {
		if _, isHeader := r.Cells["header"]; !isHeader {
			d = append(d, r)
		}
	}
	return d
}

func parseSection(section string, rows []model.RawRow) (*model.InputVersion, []model.Diagnostic) {
	var diags []model.Diagnostic
	data := dataRows(rows)
	v := &model.InputVersion{Kind: sectionKey(section), RawRows: rows, Status: "ok"}
	switch section {
	case "SAMPLES":
		for i, r := range data {
			f := strings.Fields(r.Raw)
			sp := model.SamplePoint{Index: i, Line: r.Line}
			if len(f) < 3 {
				addDiag(&diags, "SAMPLES", r.Line, "bad_row", "采样点需要 t、温度、质量三列", r.Raw)
				sp.T.Status, sp.Temp.Status, sp.M.Status = "unconvertible", "unconvertible", "unconvertible"
			} else {
				sp.T = qNum("SAMPLES", r.Line, f[0], "s", &diags)
				sp.Temp = qNum("SAMPLES", r.Line, f[1], "°C", &diags)
				sp.M = qNum("SAMPLES", r.Line, f[2], "mg", &diags)
				sp.T.Basis, sp.Temp.Basis, sp.M.Basis = "furnace_time", "furnace_program", "balance_raw"
			}
			v.Samples = append(v.Samples, sp)
		}
	case "DRIFT":
		dm := &model.DriftModel{}
		for _, r := range data {
			f := strings.Fields(r.Raw)
			if len(f) < 3 {
				addDiag(&diags, "DRIFT", r.Line, "bad_row", "漂移行需要 字段 数值 单位", r.Raw)
				continue
			}
			switch strings.ToLower(f[0]) {
			case "t0":
				dm.T0 = qNum("DRIFT", r.Line, f[1]+" "+f[2], "s", &diags)
			case "a0":
				dm.A0 = qNum("DRIFT", r.Line, f[1]+" "+f[2], "mg", &diags)
			case "rate":
				dm.Rate = qNum("DRIFT", r.Line, f[1]+" "+f[2], "mg/s", &diags)
			default:
				addDiag(&diags, "DRIFT", r.Line, "unknown_field", "未知漂移字段 "+f[0], r.Raw)
			}
		}
		v.Drift = dm
	case "FURNACE":
		for _, r := range data {
			f := strings.Fields(r.Raw)
			fp := model.FurnacePoint{Line: r.Line}
			if len(f) < 2 {
				addDiag(&diags, "FURNACE", r.Line, "bad_row", "炉程序点需要 t、温度两列", r.Raw)
			} else {
				fp.T = qNum("FURNACE", r.Line, f[0], "s", &diags)
				fp.Temp = qNum("FURNACE", r.Line, f[1], "°C", &diags)
			}
			v.Furnace = append(v.Furnace, fp)
		}
	case "VALVES":
		for _, r := range data {
			f := strings.Fields(r.Raw)
			ve := model.ValveEvent{Line: r.Line, Raw: strings.TrimSpace(r.Raw)}
			if len(f) < 2 {
				addDiag(&diags, "VALVES", r.Line, "bad_row", "阀事件需要 时间、气体两列", r.Raw)
			} else {
				ve.At = qNum("VALVES", r.Line, f[0], "s", &diags)
				ve.Gas = strings.ToLower(f[1])
			}
			v.Valves = append(v.Valves, ve)
		}
	case "BLANK":
		for _, r := range data {
			f := strings.Fields(r.Raw)
			bp := model.BlankPoint{Line: r.Line}
			if len(f) < 2 {
				addDiag(&diags, "BLANK", r.Line, "bad_row", "空白曲线点需要 温度、质量两列", r.Raw)
			} else {
				bp.Temp = qNum("BLANK", r.Line, f[0], "°C", &diags)
				bp.M = qNum("BLANK", r.Line, f[1], "mg", &diags)
			}
			v.Blank = append(v.Blank, bp)
		}
	case "MASS0":
		m := &model.Mass0{}
		for i, r := range data {
			if i > 0 {
				addDiag(&diags, "MASS0", r.Line, "multiple_rows", "初重只允许一行，其余保留为诊断", r.Raw)
			}
			f := strings.Fields(r.Raw)
			m.Raw = strings.TrimSpace(r.Raw)
			if len(f) < 2 {
				addDiag(&diags, "MASS0", r.Line, "bad_row", "初重需要 数值、单位", r.Raw)
				continue
			}
			m.M = qNum("MASS0", r.Line, f[0]+" "+f[1], "mg", &diags)
			if len(f) > 2 {
				m.Note = strings.Join(f[2:], " ")
			}
		}
		v.Mass0 = m
	default:
		addDiag(&diags, section, 0, "unknown_section", "未知分层 "+section, "")
		return nil, diags
	}
	if len(diags) > 0 {
		v.Status = "partial"
	}
	v.Diagnostic = diags
	return v, diags
}

func sectionKey(s string) string {
	switch s {
	case "SAMPLES":
		return "samples"
	case "DRIFT":
		return "drift"
	case "FURNACE":
		return "furnace"
	case "VALVES":
		return "valves"
	case "BLANK":
		return "blank"
	case "MASS0":
		return "mass"
	}
	return strings.ToLower(s)
}
