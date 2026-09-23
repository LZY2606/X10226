// Package importer 解析可复制的文本样例包。
// 一个包可含多份文档；每份独立解析，单份失败不影响其它（半失败）。
// 数值问题不拒绝文档：原值留在原位置并附诊断，规范化留给派生层。
package importer

import (
	"fmt"
	"strings"

	"tga-review/internal/core"
	"tga-review/internal/quantity"
)

// ParsedDoc 是解析成功的一份文档。
type ParsedDoc struct {
	Kind        core.DocKind
	Meta        map[string]string
	Columns     []string
	Units       map[string]quantity.Unit
	Rows        [][]string // 原始文本单元格，逐字保留
	Diagnostics []core.Diagnostic
}

// DocFailure 是解析失败的一份文档。
type DocFailure struct {
	Index       int
	Kind        string
	Excerpt     string
	Diagnostics []core.Diagnostic
}

// ParseReport 是整包解析结果（成功与失败并列，即半失败报告）。
type ParseReport struct {
	Docs     []ParsedDoc
	Failures []DocFailure
}

// OK 表示至少有一份文档解析成功。
func (r ParseReport) OK() bool { return len(r.Docs) > 0 }

// docSchema 描述一种文档的必需列与可选列。
type docSchema struct {
	required []string
	optional []string
}

var schemas = map[core.DocKind]docSchema{
	core.DocRawSamples:      {required: []string{"t", "temp", "mass"}},
	core.DocDriftCorrection: {required: []string{"t", "offset"}},
	core.DocFurnaceProgram:  {required: []string{"t", "temp"}},
	core.DocGasEvents:       {required: []string{"t", "gas"}, optional: []string{"flow"}},
	core.DocBlankCurve:      {required: []string{"t", "mass"}},
	core.DocInitialWeight:   {required: []string{"mass"}},
}

const docMarker = "=== tga-doc v1 ==="

// ParseBundle 解析整包文本。结构错误（未知种类、缺列、列数不齐）
// 使该文档进入 Failures；数值问题只产生诊断。
func ParseBundle(text string) ParseReport {
	var rep ParseReport
	blocks := splitBlocks(text)
	for i, block := range blocks {
		pd, diags := parseDoc(block)
		if len(diags) > 0 {
			kind := ""
			if pd != nil {
				kind = string(pd.Kind)
			}
			rep.Failures = append(rep.Failures, DocFailure{
				Index:       i,
				Kind:        kind,
				Excerpt:     excerpt(block),
				Diagnostics: diags,
			})
			continue
		}
		rep.Docs = append(rep.Docs, *pd)
	}
	return rep
}

func excerpt(block string) string {
	lines := strings.Split(block, "\n")
	if len(lines) > 4 {
		lines = lines[:4]
	}
	return strings.Join(lines, "\n")
}

// splitBlocks 按文档标记切分；标记之外的内容被忽略（允许注释性文字）。
func splitBlocks(text string) []string {
	var blocks []string
	var cur []string
	in := false
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == docMarker {
			if in && len(strings.TrimSpace(strings.Join(cur, ""))) > 0 {
				blocks = append(blocks, strings.Join(cur, "\n"))
			}
			cur = nil
			in = true
			continue
		}
		if in {
			cur = append(cur, line)
		}
	}
	if in && len(strings.TrimSpace(strings.Join(cur, ""))) > 0 {
		blocks = append(blocks, strings.Join(cur, "\n"))
	}
	return blocks
}

func parseDoc(block string) (*ParsedDoc, []core.Diagnostic) {
	pd := &ParsedDoc{Meta: map[string]string{}, Units: map[string]quantity.Unit{}}
	lines := strings.Split(block, "\n")
	var body []string
	inBody := false
	lineNo := 0
	for _, raw := range lines {
		lineNo++
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !inBody {
			if line == "---" {
				inBody = true
				continue
			}
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				return nil, []core.Diagnostic{{Severity: "error", Code: "bad_meta", Line: lineNo,
					Message: fmt.Sprintf("元数据行缺少冒号: %q", line)}}
			}
			pd.Meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
			continue
		}
		body = append(body, line)
	}
	kindStr := pd.Meta["kind"]
	if kindStr == "" {
		return nil, []core.Diagnostic{{Severity: "error", Code: "missing_kind", Message: "缺少 kind 元数据"}}
	}
	kind := core.DocKind(kindStr)
	schema, known := schemas[kind]
	if !known {
		return nil, []core.Diagnostic{{Severity: "error", Code: "unknown_kind",
			Message: fmt.Sprintf("未知文档种类: %q", kindStr)}}
	}
	pd.Kind = kind
	// 解析 units 元数据：units: t=s temp=C mass=mg
	unitsRaw := pd.Meta["units"]
	if unitsRaw == "" {
		return nil, []core.Diagnostic{{Severity: "error", Code: "missing_units",
			Message: "缺少 units 元数据（所有领域量必须携带单位）"}}
	}
	for _, pair := range strings.Fields(unitsRaw) {
		col, u, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, []core.Diagnostic{{Severity: "error", Code: "bad_units",
				Message: fmt.Sprintf("units 项缺少 '=': %q", pair)}}
		}
		pd.Units[col] = quantity.Unit(u)
	}
	if len(body) == 0 {
		return nil, []core.Diagnostic{{Severity: "error", Code: "empty_body", Message: "文档没有数据体"}}
	}
	pd.Columns = splitCSV(body[0])
	for _, req := range schema.required {
		if !contains(pd.Columns, req) {
			return nil, []core.Diagnostic{{Severity: "error", Code: "missing_column",
				Message: fmt.Sprintf("缺少必需列 %q（表头: %s）", req, body[0])}}
		}
		if _, ok := pd.Units[req]; !ok && req != "gas" {
			return nil, []core.Diagnostic{{Severity: "error", Code: "missing_column_unit",
				Message: fmt.Sprintf("列 %q 缺少单位声明", req)}}
		}
	}
	for i, row := range body[1:] {
		cells := splitCSV(row)
		if len(cells) != len(pd.Columns) {
			return nil, []core.Diagnostic{{Severity: "error", Code: "bad_column_count", Line: i + 2,
				Message: fmt.Sprintf("第 %d 行列数 %d 与表头 %d 不一致", i+2, len(cells), len(pd.Columns))}}
		}
		pd.Rows = append(pd.Rows, cells)
	}
	if len(pd.Rows) == 0 {
		return nil, []core.Diagnostic{{Severity: "error", Code: "no_rows", Message: "文档没有数据行"}}
	}
	if kind == core.DocInitialWeight && len(pd.Rows) != 1 {
		return nil, []core.Diagnostic{{Severity: "error", Code: "bad_rows",
			Message: "initial_weight 必须恰好一行"}}
	}
	return pd, nil
}

func splitCSV(line string) []string {
	parts := strings.Split(line, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Cell 取一行中某列的原始文本；不存在返回空串。
func (d *ParsedDoc) Cell(row int, col string) string {
	for i, c := range d.Columns {
		if c == col {
			if row < len(d.Rows) && i < len(d.Rows[row]) {
				return d.Rows[row][i]
			}
		}
	}
	return ""
}

// Value 把一行中某列解析为带单位的量（保留原始表示）。
func (d *ParsedDoc) Value(row int, col string) quantity.Value {
	return quantity.Parse(d.Cell(row, col), d.Units[col])
}
