package ingest

import (
	"strings"

	"tgaline/internal/domain"
)

func parseSampleRows(res *ParseResult, header []string, records [][]string, units map[string]string) {
	ti := colIndex(header, "time", "t", "furnace_time", "furnace_time_s")
	mi := colIndex(header, "mass", "m", "weight")
	pyi := colIndex(header, "temp", "temperature", "t_c", "sample_temp")
	if ti < 0 || mi < 0 {
		res.Fatal = errf("csv samples: header must contain time and mass columns, got %v", header)
		return
	}
	var diags []diag
	for r := range records {
		rowNo := r + 2
		sp := domain.SamplePoint{Row: rowNo}
		sp.TimeRaw = cell(records, r, ti)
		sp.MassRaw = cell(records, r, mi)
		if pyi >= 0 {
			sp.TempRaw = cell(records, r, pyi)
		}
		if v, ok := parseNumeric(sp.TimeRaw); ok {
			if cv, ok2 := convertTime(v, units["time"]); ok2 {
				sp.TimeS = cv
			} else {
				diags = append(diags, badCell(rowNo, "time", sp.TimeRaw, "unknown_time_unit", "time unit not convertible: "+units["time"]))
			}
		} else {
			diags = append(diags, badCell(rowNo, "time", sp.TimeRaw, "bad_number", "time is not numeric"))
		}
		if v, ok := parseNumeric(sp.MassRaw); ok {
			if cv, ok2 := convertMass(v, units["mass"]); ok2 {
				sp.MassMg = cv
			} else {
				diags = append(diags, badCell(rowNo, "mass", sp.MassRaw, "unknown_mass_unit", "mass unit not convertible: "+units["mass"]))
			}
		} else {
			diags = append(diags, badCell(rowNo, "mass", sp.MassRaw, "bad_number", "mass is not numeric"))
		}
		if pyi >= 0 && sp.TempRaw != "" {
			if v, ok := parseNumeric(sp.TempRaw); ok {
				if cv, ok2 := convertTemp(v, units["temp"]); ok2 {
					sp.TempC = cv
				} else {
					diags = append(diags, badCell(rowNo, "temp", sp.TempRaw, "unknown_temp_unit", "temp unit not convertible: "+units["temp"]))
				}
			} else {
				diags = append(diags, badCell(rowNo, "temp", sp.TempRaw, "bad_number", "temp is not numeric"))
			}
		}
		res.Samples = append(res.Samples, sp)
	}
	res.Diagnostics = append(res.Diagnostics, toDomainDiags(diags)...)
}

func parseCurveRows(res *ParseResult, kind domain.Kind, header []string, records [][]string, units map[string]string) {
	ti := colIndex(header, "time", "t")
	vi := colIndex(header, "value", "mass", "temp", "temperature", "correction", "m")
	if ti < 0 || vi < 0 {
		res.Fatal = errf("csv %s: header must contain time and value columns, got %v", kind, header)
		return
	}
	vunit := units["value"]
	if kind == domain.KindFurnace {
		vunit = units["temp"]
	}
	if (kind == domain.KindDrift || kind == domain.KindBlank) && vunit == "" {
		vunit = units["mass"]
	}
	var diags []diag
	for r := range records {
		rowNo := r + 2
		cp := domain.CurvePoint{Row: rowNo}
		cp.TimeRaw = cell(records, r, ti)
		cp.ValueRaw = cell(records, r, vi)
		if v, ok := parseNumeric(cp.TimeRaw); ok {
			if cv, ok2 := convertTime(v, units["time"]); ok2 {
				cp.TimeS = cv
			} else {
				diags = append(diags, badCell(rowNo, "time", cp.TimeRaw, "unknown_time_unit", "time unit not convertible"))
			}
		} else {
			diags = append(diags, badCell(rowNo, "time", cp.TimeRaw, "bad_number", "time is not numeric"))
		}
		if v, ok := parseNumeric(cp.ValueRaw); ok {
			var cv float64
			var ok2 bool
			switch kind {
			case domain.KindFurnace:
				cv, ok2 = convertTemp(v, vunit)
				if !ok2 {
					diags = append(diags, badCell(rowNo, "value", cp.ValueRaw, "unknown_temp_unit", "temp unit not convertible"))
				}
			default:
				cv, ok2 = convertMass(v, vunit)
				if !ok2 {
					diags = append(diags, badCell(rowNo, "value", cp.ValueRaw, "unknown_mass_unit", "mass unit not convertible"))
				}
			}
			if ok2 {
				cp.Value = cv
			}
		} else {
			diags = append(diags, badCell(rowNo, "value", cp.ValueRaw, "bad_number", "value is not numeric"))
		}
		res.Curve = append(res.Curve, cp)
	}
	res.Diagnostics = append(res.Diagnostics, toDomainDiags(diags)...)
}

var _ = strings.TrimSpace
