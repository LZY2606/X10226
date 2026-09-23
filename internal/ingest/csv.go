package ingest

import (
	"encoding/csv"
	"io"
	"strconv"
	"strings"

	"tgasys/internal/domain"
)

// fieldSpec declares one column of a layer schema.
type fieldSpec struct {
	name     string // canonical field
	unit     string // canonical coordinate reference
	aliases  []string
	optional bool
}

type layerSchema struct {
	kind   string
	fields []fieldSpec
}

var schemas = map[string]layerSchema{
	domain.LayerPoints: {kind: domain.LayerPoints, fields: []fieldSpec{
		{name: "time_s", unit: "s", aliases: []string{"time_s", "time", "t", "炉时间", "时间", "time(s)"}},
		{name: "temp_c", unit: "C", aliases: []string{"temp_c", "temperature", "temp", "t_c", "炉温", "温度", "temperature(c)"}},
		{name: "mass_mg", unit: "mg", aliases: []string{"mass_mg", "mass", "weight", "m", "质量", "样品质量", "mass(mg)"}},
	}},
	domain.LayerDrift: {kind: domain.LayerDrift, fields: []fieldSpec{
		{name: "valid_from", unit: "", aliases: []string{"valid_from", "from", "since", "生效自", "版本"}, optional: true},
		{name: "slope_mg_per_s", unit: "mg/s", aliases: []string{"slope_mg_per_s", "slope", "drift_slope", "漂移斜率", "slope(mg/s)"}},
		{name: "intercept_mg", unit: "mg", aliases: []string{"intercept_mg", "intercept", "b", "截距", "intercept(mg)"}},
		{name: "note", unit: "", aliases: []string{"note", "备注", "说明"}, optional: true},
	}},
	domain.LayerProgram: {kind: domain.LayerProgram, fields: []fieldSpec{
		{name: "start_s", unit: "s", aliases: []string{"start_s", "start", "from_s", "起始时间", "start(s)"}},
		{name: "end_s", unit: "s", aliases: []string{"end_s", "end", "to_s", "结束时间", "end(s)"}},
		{name: "target_temp_c", unit: "C", aliases: []string{"target_temp_c", "target", "setpoint", "目标温度", "target(c)"}},
		{name: "rate_c_per_min", unit: "C/min", aliases: []string{"rate_c_per_min", "rate", "ramp", "升温速率", "rate(c/min)"}},
	}},
	domain.LayerGas: {kind: domain.LayerGas, fields: []fieldSpec{
		{name: "switch_s", unit: "s", aliases: []string{"switch_s", "switch", "at_s", "阀切换时间", "switch(s)"}},
		{name: "gas", unit: "", aliases: []string{"gas", "atmosphere", "气氛", "气体"}},
		{name: "flow_mlpm", unit: "", aliases: []string{"flow_mlpm", "flow", "流量", "flow(ml/min)"}, optional: true},
	}},
	domain.LayerBlank: {kind: domain.LayerBlank, fields: []fieldSpec{
		{name: "time_s", unit: "s", aliases: []string{"time_s", "time", "t", "炉时间", "time(s)"}},
		{name: "blank_mass_mg", unit: "mg", aliases: []string{"blank_mass_mg", "blank_mass", "blank", "空白质量", "blank(mg)"}},
	}},
	domain.LayerInitial: {kind: domain.LayerInitial, fields: []fieldSpec{
		{name: "m0_mg", unit: "mg", aliases: []string{"m0_mg", "m0", "initial_mass", "初重", "初始质量", "m0(mg)"}},
		{name: "weighed_at_s", unit: "s", aliases: []string{"weighed_at_s", "at_s", "称量时刻", "weighed_at(s)"}, optional: true},
		{name: "reason", unit: "", aliases: []string{"reason", "修订原因", "备注"}, optional: true},
	}},
}

func resolveHeader(h string) (string, string) {
	h = strings.TrimSpace(h)
	// explicit unit suffix in brackets: "time [s]" or "质量(mg)"
	name := h
	unitTok := ""
	if i := strings.IndexAny(h, "[("); i >= 0 {
		j := strings.IndexAny(h[i:], "])")
		if j > 0 {
			unitTok = strings.TrimSpace(h[i+1 : i+j])
			name = strings.TrimSpace(h[:i])
		}
	}
	key := strings.ToLower(name)
	return key, unitTok
}

// Parse turns raw CSV bytes into a versioned ParsedLayer. The original cell
// text is always preserved; conversion failures never drop the record.
func Parse(projectID, kind string, version int, parentID, blobID, filename string, raw []byte) (*domain.ParsedLayer, []domain.Diagnostic) {
	sch, ok := schemas[kind]
	var fatal []domain.Diagnostic
	if !ok {
		return nil, []domain.Diagnostic{{Code: "unknown_layer", Message: "未知分层类型: " + kind, Layer: kind, Severity: "error"}}
	}
	r := csv.NewReader(strings.NewReader(string(raw)))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	all, err := r.ReadAll()
	if err != nil {
		return nil, []domain.Diagnostic{{Code: "csv_malformed", Message: "CSV 无法解析: " + err.Error(), Layer: kind, Severity: "error"}}
	}
	if len(all) == 0 {
		return nil, []domain.Diagnostic{{Code: "empty_file", Message: "文件为空，缺少表头", Layer: kind, Severity: "error"}}
	}

	headerRow := all[0]
	type colMap struct {
		spec    fieldSpec
		col     int
		unitTok string
	}
	cols := make([]colMap, 0)
	headers := make([]string, len(headerRow))
	seen := map[string]bool{}
	for ci, h := range headerRow {
		headers[ci] = strings.TrimSpace(h)
		key, unitTok := resolveHeader(h)
		for _, f := range sch.fields {
			if seen[f.name] {
				continue
			}
			match := false
			for _, a := range f.aliases {
				if strings.ToLower(a) == key {
					match = true
					break
				}
			}
			if match {
				cols = append(cols, colMap{spec: f, col: ci, unitTok: unitTok})
				seen[f.name] = true
			}
		}
	}
	missing := make([]string, 0)
	for _, f := range sch.fields {
		if f.optional || seen[f.name] {
			continue
		}
		missing = append(missing, f.name)
	}
	if len(missing) > 0 {
		fatal = append(fatal, domain.Diagnostic{Code: "missing_columns", Layer: kind, Severity: "error",
			Message: "缺少必需列: " + strings.Join(missing, ", ")})
	}

	layer := &domain.ParsedLayer{
		ProjectID: projectID, Kind: kind, Version: version, ParentID: parentID,
		BlobID: blobID, SchemaRev: SchemaRevision, MediaType: "text/csv",
		Filename: filename, Headers: headers,
	}
	dataRows := all[1:]
	if len(dataRows) == 0 {
		fatal = append(fatal, domain.Diagnostic{Code: "no_data_rows", Layer: kind, Severity: "error", Message: "只有表头没有数据行"})
	}

	for ri, rec := range dataRows {
		sourceRow := ri + 2 // 1-based incl header
		row := domain.ParsedRow{Index: ri, Fields: map[string]domain.Quantity{}, Valid: true}
		nonEmpty := false
		for _, c := range cols {
			tok := ""
			if c.col < len(rec) {
				tok = rec[c.col]
			}
			if strings.TrimSpace(tok) != "" {
				nonEmpty = true
			}
			// non-numeric textual fields are kept verbatim without conversion.
			if c.spec.unit == "" {
				row.Fields[c.spec.name] = domain.Quantity{Original: strings.TrimSpace(tok), SourceUnit: c.unitTok}
				continue
			}
			q := parseQuantity(tok, c.unitTok, c.spec.unit)
			if strings.TrimSpace(tok) != "" && q.Value == nil {
				d := errDiag(kind, c.spec.name, sourceRow, "unconvertible_value",
					"值 \""+tok+"\" 无法转换为 "+c.spec.unit+"，原始值保留在原位")
				row.Diagnostics = append(row.Diagnostics, d)
				layer.Diagnostics = append(layer.Diagnostics, d)
				row.Valid = false
			}
			row.Fields[c.spec.name] = q
		}
		// structural: ragged row
		if len(rec) != len(headerRow) {
			d := errDiag(kind, "", sourceRow, "ragged_row",
				"列数 "+strconv.Itoa(len(rec))+" 与表头 "+strconv.Itoa(len(headerRow))+" 不一致；该行字段按列位保留")
			row.Diagnostics = append(row.Diagnostics, d)
			layer.Diagnostics = append(layer.Diagnostics, d)
			row.Valid = false
		}
		if nonEmpty {
			layer.Rows = append(layer.Rows, row)
		} else {
			d := warnDiag(kind, "", sourceRow, "blank_row", "空行已跳过")
			layer.Diagnostics = append(layer.Diagnostics, d)
		}
	}
	layer.Accepted = len(fatal) == 0
	layer.Partial = layer.Accepted && hasInvalidRow(layer.Rows)
	layer.ID = layerIdentity(layer)
	return layer, fatal
}

func hasInvalidRow(rows []domain.ParsedRow) bool {
	for _, r := range rows {
		if !r.Valid {
			return true
		}
	}
	return false
}

// layerIdentity is content addressed by kind/version/parent/blob/schema so
// re-import of identical material yields the same id.
func layerIdentity(l *domain.ParsedLayer) string {
	return domain.NewCID("ly_", []any{l.ProjectID, l.Kind, l.Version, l.ParentID, l.BlobID, l.SchemaRev})
}

var _ = io.EOF
