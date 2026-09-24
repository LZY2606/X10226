package tga

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// .tgab —— 失重谱系离线案例包文本格式（可复制的小型样例）。
//
//   # 注释 / 空行
//   [SECTION]
//   |col|col|           表头行（每层第一行）
//   |...|...|           数据行
//
// 数值可带单位（如 10.2mg、25 °C）；无单位标注时按列基准。
// 无法转换的单元格留在原位并附诊断，不会为画图而丢弃。

var layerHeader = map[string][]string{
	LayerSamples:    {"time_s", "temp_c", "mass_mg"},
	LayerCorrection: {"method", "a_mg", "b_mg_per_s"},
	LayerFurnace:    {"time_s", "target_temp_c"},
	LayerGas:        {"time_s", "valve", "gas"},
	LayerBlank:      {"time_s", "blank_mass_mg"},
	LayerInitial:    {"mass_mg", "basis", "note"},
}

// ParsedBundle 是解析结果：六层（出现与否）+ 包级诊断。
type ParsedBundle struct {
	Layers    map[string]*Layer
	Diags     []Diagnostic
	SubStatus string // ok | partial | failed
}

// ParseBundle 解析 .tgab 文本。origin 标记导入/修订/半失败来源。
func ParseBundle(projectID, submissionID string, seq int, text string) (*ParsedBundle, error) {
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	pb := &ParsedBundle{Layers: map[string]*Layer{}, SubStatus: StatusOK}
	current := ""
	rowSeq := map[string]int{}
	lineNo := 0

	flush := func() {}

	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			name = strings.ToLower(name)
			if _, ok := layerHeader[name]; !ok {
				return nil, fmt.Errorf("第 %d 行: 未知段 [%s]", lineNo, name)
			}
			current = name
			if _, exists := pb.Layers[current]; !exists {
				pb.Layers[current] = &Layer{Name: current, Status: StatusOK, Present: true, Header: layerHeader[current]}
			}
			rowSeq[current] = 0
			continue
		}
		if current == "" {
			return nil, fmt.Errorf("第 %d 行: 段外数据: %q", lineNo, firstN(line, 40))
		}
		if !strings.HasPrefix(line, "|") {
			pb.Layers[current].Diags = append(pb.Layers[current].Diags,
				Diagnostic{SevError, current, lineNo, "", "malformed_line", "缺少 | 分隔的行结构", firstN(line, 40)})
			pb.Layers[current].Status = StatusPartial
			pb.SubStatus = StatusPartial
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) >= 2 && fields[0] == "" {
			fields = fields[1:]
		}
		if len(fields) >= 1 && fields[len(fields)-1] == "" {
			fields = fields[:len(fields)-1]
		}
		// 表头行：全部非数字。
		if looksLikeHeader(fields, layerHeader[current]) {
			if join(fields) != join(layerHeader[current]) {
				return nil, fmt.Errorf("第 %d 行: [%s] 表头不匹配，期望 %v，得到 %v", lineNo, current, layerHeader[current], fields)
			}
			continue
		}
		header := layerHeader[current]
		if len(fields) != len(header) {
			pb.Layers[current].Diags = append(pb.Layers[current].Diags,
				Diagnostic{SevError, current, lineNo, "", "column_count",
					fmt.Sprintf("列数 %d 与表头 %d 不符", len(fields), len(header)), firstN(line, 40)})
			pb.Layers[current].Status = StatusPartial
			pb.SubStatus = StatusPartial
			continue
		}
		rowSeq[current]++
		rseq := rowSeq[current]
		row := Row{ID: stablePointID(current, rseq), Cells: map[string]Cell{}}
		layer := pb.Layers[current]
		rowBad := false
		for i, col := range header {
			raw := strings.TrimSpace(fields[i])
			var cell Cell
			if col == "method" || col == "valve" || col == "gas" || col == "basis" || col == "note" {
				cell = Cell{Q: Q{Original: raw}}
				if raw == "" && col != "note" {
					cell.Diags = []Diagnostic{{SevError, current, lineNo, col, "empty_cell", "分类字段为空", raw}}
					rowBad = true
				}
			} else {
				cell = ParseCell(current, col, raw, lineNo)
				if HasDiagLevel(cell.Diags, SevError) {
					rowBad = true
				}
			}
			row.Cells[col] = cell
		}
		layer.Rows = append(layer.Rows, row)
		if rowBad {
			layer.Status = StatusPartial
			pb.SubStatus = StatusPartial
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	flush()

	// 层级语义校验。
	validateLayers(pb)

	if pb.Layers[LayerSamples] == nil || len(pb.Layers[LayerSamples].Rows) < 3 {
		pb.Diags = append(pb.Diags, Diagnostic{SevError, LayerSamples, 0, "", "samples_required", "至少需要 3 个有效采样点"})
		pb.SubStatus = StatusFailed
	}
	if pb.Layers[LayerInitial] == nil {
		pb.Diags = append(pb.Diags, Diagnostic{SevError, LayerInitial, 0, "", "initial_required", "缺少样品初重层"})
		if pb.SubStatus != StatusFailed {
			pb.SubStatus = StatusPartial
		}
	}
	_ = seq
	return pb, nil
}

func validateLayers(pb *ParsedBundle) {
	if cor := pb.Layers[LayerCorrection]; cor != nil {
		for _, r := range cor.Rows {
			m := r.Cells["method"].Q.Original
			if m != "none" && m != "blank" && m != "linear" {
				cor.Diags = append(cor.Diags, Diagnostic{SevError, LayerCorrection, 0, "method", "method 必须为 none|blank|linear", m})
				cor.Status = StatusPartial
				pb.SubStatus = StatusPartial
			}
		}
		if len(cor.Rows) == 0 {
			cor.Diags = append(cor.Diags, Diagnostic{SevError, LayerCorrection, 0, "", "empty_layer", "校正层为空"})
			cor.Status = StatusFailed
			pb.SubStatus = StatusPartial
		}
	}
	if g := pb.Layers[LayerGas]; g != nil {
		for _, r := range g.Rows {
			v := r.Cells["valve"].Q.Original
			if v != "open" && v != "switch" && v != "close" {
				g.Diags = append(g.Diags, Diagnostic{SevWarning, LayerGas, 0, "valve", "阀动作非常见值 open|switch|close", v})
			}
		}
	}
	// 时间单调校验：非单调的时间点附诊断（非单调噪声只应出现在质量列，时间乱序属错误）。
	if sm := pb.Layers[LayerSamples]; sm != nil {
		var prev *float64
		for _, r := range sm.Rows {
			t := r.Cells["time_s"].Q.Normalized
			if t != nil && prev != nil && *t <= *prev {
				sm.Diags = append(sm.Diags, Diagnostic{SevError, LayerSamples, 0, "time_s", "采样时间非严格递增", r.Cells["time_s"].Q.Original})
				sm.Status = StatusPartial
				pb.SubStatus = StatusPartial
			}
			if t != nil {
				prev = t
			}
		}
	}
}

func looksLikeHeader(fields, header []string) bool {
	if len(fields) != len(header) {
		return false
	}
	for i, f := range fields {
		if strings.ToLower(strings.TrimSpace(f)) != header[i] {
			return false
		}
	}
	return true
}

func join(xs []string) string { return strings.Join(xs, "|") }
func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// stablePointID 原始点稳定身份：层名 + 层内行序（1 起）。导出/再导入不变。
func stablePointID(layer string, rowSeq int) string {
	return "P#" + layer + "#" + strconv.Itoa(rowSeq)
}
