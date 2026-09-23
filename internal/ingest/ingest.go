// Package ingest 解析导入文本。原始表示逐字保留在源版本中；
// 解析只负责校验与单位规范化，无法转换的值留在原位置并附诊断。
package ingest

import (
	"fmt"
	"strconv"
	"strings"

	"tgagsb/internal/domain"
)

// Doc 是一个导入文档（批次中的一个条目）。
type Doc struct {
	Kind domain.SourceKind
	Raw  string // 原始表示，逐字保留
}

// Samples 是规范化后的原始采样点（s, °C, mg）。
type Samples struct {
	Time, Temp, Mass []float64
}

// Drift 是天平漂移校正（mg/s）。
type Drift struct {
	RateMgPerS float64
}

// FurnaceProgram 是炉程序（s, °C）。
type FurnaceProgram struct {
	Time, Setpoint []float64
}

// GasEvents 是气体阀事件（s, 气体名）。
type GasEvents struct {
	Time []float64
	Gas  []string
}

// BlankCurve 是空白曲线（°C, mg 表观质量）。
type BlankCurve struct {
	Temp, MassMg []float64
}

// InitialMass 是样品初重分层记录。
type InitialMass struct {
	Layer  string
	MassMg float64
}

// SplitBatch 把批次文本按 `---` 分隔为文档；每个文档首行声明 kind。
func SplitBatch(batch string) ([]Doc, error) {
	var docs []Doc
	for _, chunk := range strings.Split(batch, "\n---") {
		chunk = strings.TrimSpace(chunk)
		chunk = strings.TrimPrefix(chunk, "---")
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		lines := strings.SplitN(chunk, "\n", 2)
		head := strings.TrimSpace(lines[0])
		if !strings.HasPrefix(head, "kind:") {
			return nil, fmt.Errorf("文档缺少 kind 头: %q", head)
		}
		kind := domain.SourceKind(strings.TrimSpace(strings.TrimPrefix(head, "kind:")))
		ok := false
		for _, k := range domain.AllSourceKinds {
			if k == kind {
				ok = true
			}
		}
		if !ok {
			return nil, fmt.Errorf("未知源类别 %q", kind)
		}
		docs = append(docs, Doc{Kind: kind, Raw: chunk})
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("空批次")
	}
	return docs, nil
}

// Parse 解析并规范化一份源文档。返回解析结果、诊断与错误。
// 单位无法转换时返回错误与诊断；原始文本仍由调用方保留。
func Parse(kind domain.SourceKind, raw string) (any, []string, error) {
	lines := strings.Split(raw, "\n")
	var units map[string]string
	var header []string
	var rows [][]string
	kv := map[string]string{}
	for _, ln := range lines[1:] { // 跳过 kind 头
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		if strings.HasPrefix(ln, "units:") {
			units = map[string]string{}
			for _, pair := range strings.Split(strings.TrimPrefix(ln, "units:"), ",") {
				kv2 := strings.SplitN(strings.TrimSpace(pair), "=", 2)
				if len(kv2) == 2 {
					units[strings.TrimSpace(kv2[0])] = strings.TrimSpace(kv2[1])
				}
			}
			continue
		}
		if strings.Contains(ln, "=") && !strings.Contains(ln, ",") {
			kv2 := strings.SplitN(ln, "=", 2)
			kv[strings.TrimSpace(kv2[0])] = strings.TrimSpace(kv2[1])
			continue
		}
		if header == nil && strings.Contains(ln, ",") && !isNumber(strings.Split(ln, ",")[0]) {
			for _, h := range strings.Split(ln, ",") {
				header = append(header, strings.TrimSpace(h))
			}
			continue
		}
		if strings.Contains(ln, ",") {
			var cols []string
			for _, c := range strings.Split(ln, ",") {
				cols = append(cols, strings.TrimSpace(c))
			}
			rows = append(rows, cols)
		}
	}
	switch kind {
	case domain.SrcRawSamples:
		return parseSamples(units, header, rows)
	case domain.SrcDrift:
		return parseDrift(kv)
	case domain.SrcFurnaceProg:
		return parseFurnace(units, header, rows)
	case domain.SrcGasEvents:
		return parseGas(units, header, rows)
	case domain.SrcBlankCurve:
		return parseBlank(units, header, rows)
	case domain.SrcInitialMass:
		return parseInitialMass(kv)
	}
	return nil, nil, fmt.Errorf("未支持的类别 %q", kind)
}

func isNumber(s string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return err == nil
}

func convTime(v float64, unit string) (float64, error) {
	switch unit {
	case "", "s":
		return v, nil
	case "min":
		return v * 60, nil
	}
	return 0, fmt.Errorf("无法转换的时间单位 %q", unit)
}

func convTemp(v float64, unit string) (float64, error) {
	switch unit {
	case "", "C":
		return v, nil
	case "K":
		return v - 273.15, nil
	}
	return 0, fmt.Errorf("无法转换的温度单位 %q", unit)
}

func convMass(v float64, unit string) (float64, error) {
	switch unit {
	case "", "mg":
		return v, nil
	case "g":
		return v * 1000, nil
	}
	return 0, fmt.Errorf("无法转换的质量单位 %q", unit)
}

func colIdx(header []string, name string) int {
	for i, h := range header {
		if h == name {
			return i
		}
	}
	return -1
}

func parseSamples(units map[string]string, header []string, rows [][]string) (any, []string, error) {
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("raw_samples 无数据行")
	}
	ti, te, mi := colIdx(header, "time"), colIdx(header, "temp"), colIdx(header, "mass")
	if ti < 0 || te < 0 || mi < 0 {
		return nil, nil, fmt.Errorf("raw_samples 表头需含 time,temp,mass")
	}
	s := &Samples{}
	for i, r := range rows {
		if len(r) <= ti || len(r) <= te || len(r) <= mi {
			return nil, nil, fmt.Errorf("第 %d 行列数不足", i+1)
		}
		tv, err := strconv.ParseFloat(r[ti], 64)
		if err != nil {
			return nil, nil, fmt.Errorf("第 %d 行 time 非数值: %w", i+1, err)
		}
		tv, err = convTime(tv, units["time"])
		if err != nil {
			return nil, []string{err.Error()}, err
		}
		xv, err := strconv.ParseFloat(r[te], 64)
		if err != nil {
			return nil, nil, fmt.Errorf("第 %d 行 temp 非数值: %w", i+1, err)
		}
		xv, err = convTemp(xv, units["temp"])
		if err != nil {
			return nil, []string{err.Error()}, err
		}
		mv, err := strconv.ParseFloat(r[mi], 64)
		if err != nil {
			return nil, nil, fmt.Errorf("第 %d 行 mass 非数值: %w", i+1, err)
		}
		mv, err = convMass(mv, units["mass"])
		if err != nil {
			return nil, []string{err.Error()}, err
		}
		s.Time = append(s.Time, tv)
		s.Temp = append(s.Temp, xv)
		s.Mass = append(s.Mass, mv)
	}
	return s, nil, nil
}

func parseDrift(kv map[string]string) (any, []string, error) {
	raw, ok := kv["drift_rate"]
	if !ok {
		return nil, nil, fmt.Errorf("drift_correction 缺少 drift_rate")
	}
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return nil, nil, fmt.Errorf("drift_rate 为空")
	}
	v, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return nil, nil, fmt.Errorf("drift_rate 非数值: %w", err)
	}
	unit := "mg/min"
	if len(parts) > 1 {
		unit = parts[1]
	}
	switch unit {
	case "mg/min":
		return &Drift{RateMgPerS: v / 60}, nil, nil
	case "mg/s":
		return &Drift{RateMgPerS: v}, nil, nil
	}
	err = fmt.Errorf("无法转换的漂移单位 %q", unit)
	return nil, []string{err.Error()}, err
}

func parseFurnace(units map[string]string, header []string, rows [][]string) (any, []string, error) {
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("furnace_program 无数据行")
	}
	ti, si := colIdx(header, "time"), colIdx(header, "setpoint")
	if ti < 0 || si < 0 {
		return nil, nil, fmt.Errorf("furnace_program 表头需含 time,setpoint")
	}
	f := &FurnaceProgram{}
	for i, r := range rows {
		tv, err := strconv.ParseFloat(r[ti], 64)
		if err != nil {
			return nil, nil, fmt.Errorf("第 %d 行 time 非数值: %w", i+1, err)
		}
		tv, err = convTime(tv, units["time"])
		if err != nil {
			return nil, []string{err.Error()}, err
		}
		sv, err := strconv.ParseFloat(r[si], 64)
		if err != nil {
			return nil, nil, fmt.Errorf("第 %d 行 setpoint 非数值: %w", i+1, err)
		}
		sv, err = convTemp(sv, units["temp"])
		if err != nil {
			return nil, []string{err.Error()}, err
		}
		f.Time = append(f.Time, tv)
		f.Setpoint = append(f.Setpoint, sv)
	}
	return f, nil, nil
}

func parseGas(units map[string]string, header []string, rows [][]string) (any, []string, error) {
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("gas_events 无数据行")
	}
	ti, gi := colIdx(header, "time"), colIdx(header, "gas")
	if ti < 0 || gi < 0 {
		return nil, nil, fmt.Errorf("gas_events 表头需含 time,gas")
	}
	g := &GasEvents{}
	for i, r := range rows {
		tv, err := strconv.ParseFloat(r[ti], 64)
		if err != nil {
			return nil, nil, fmt.Errorf("第 %d 行 time 非数值: %w", i+1, err)
		}
		tv, err = convTime(tv, units["time"])
		if err != nil {
			return nil, []string{err.Error()}, err
		}
		g.Time = append(g.Time, tv)
		g.Gas = append(g.Gas, r[gi])
	}
	return g, nil, nil
}

func parseBlank(units map[string]string, header []string, rows [][]string) (any, []string, error) {
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("blank_curve 无数据行")
	}
	ti, mi := colIdx(header, "temp"), colIdx(header, "mass")
	if ti < 0 || mi < 0 {
		return nil, nil, fmt.Errorf("blank_curve 表头需含 temp,mass")
	}
	b := &BlankCurve{}
	for i, r := range rows {
		tv, err := strconv.ParseFloat(r[ti], 64)
		if err != nil {
			return nil, nil, fmt.Errorf("第 %d 行 temp 非数值: %w", i+1, err)
		}
		tv, err = convTemp(tv, units["temp"])
		if err != nil {
			return nil, []string{err.Error()}, err
		}
		mv, err := strconv.ParseFloat(r[mi], 64)
		if err != nil {
			return nil, nil, fmt.Errorf("第 %d 行 mass 非数值: %w", i+1, err)
		}
		mv, err = convMass(mv, units["mass"])
		if err != nil {
			return nil, []string{err.Error()}, err
		}
		b.Temp = append(b.Temp, tv)
		b.MassMg = append(b.MassMg, mv)
	}
	return b, nil, nil
}

func parseInitialMass(kv map[string]string) (any, []string, error) {
	raw, ok := kv["mass"]
	if !ok {
		return nil, nil, fmt.Errorf("initial_mass 缺少 mass")
	}
	parts := strings.Fields(raw)
	v, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return nil, nil, fmt.Errorf("initial_mass mass 非数值: %w", err)
	}
	unit := "mg"
	if len(parts) > 1 {
		unit = parts[1]
	}
	v, err = convMass(v, unit)
	if err != nil {
		return nil, []string{err.Error()}, err
	}
	layer := kv["layer"]
	if layer == "" {
		layer = "as_received"
	}
	return &InitialMass{Layer: layer, MassMg: v}, nil, nil
}
