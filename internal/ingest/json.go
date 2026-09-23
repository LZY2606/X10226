package ingest

import (
	"encoding/json"
	"fmt"

	"tgaline/internal/domain"
)

type jDoc struct {
	Kind  string         `json:"kind"`
	Units map[string]string `json:"units"`
	Name  string         `json:"name"`
	Rows  []map[string]any `json:"rows"`
}

func parseJSON(res *ParseResult, data []byte) {
	var jd jDoc
	if err := json.Unmarshal(data, &jd); err != nil {
		res.Fatal = fmt.Errorf("json: invalid document: %w", err)
		return
	}
	if jd.Kind == "" {
		res.Fatal = fmt.Errorf("json: missing \"kind\"")
		return
	}
	kind := domain.Kind(jd.Kind)
	switch kind {
	case domain.KindSamples, domain.KindDrift, domain.KindFurnace,
		domain.KindGas, domain.KindBlank, domain.KindInitialMass:
	default:
		res.Fatal = fmt.Errorf("json: unknown kind %q", jd.Kind)
		return
	}
	res.Kind = kind
	if jd.Units != nil {
		res.Units = jd.Units
	}
	var ds []diag
	for i, row := range jd.Rows {
		rowNo := i + 1
		switch kind {
		case domain.KindSamples:
			sp := domain.SamplePoint{Row: rowNo}
			sp.TimeRaw = asString(row["time"])
			sp.MassRaw = asString(row["mass"])
			sp.TempRaw = asString(row["temp"])
			fillTime(&sp.TimeS, sp.TimeRaw, jd.Units["time"], rowNo, &ds)
			fillMass(&sp.MassMg, sp.MassRaw, jd.Units["mass"], rowNo, &ds)
			if sp.TempRaw != "" {
				fillTemp(&sp.TempC, sp.TempRaw, jd.Units["temp"], rowNo, &ds)
			}
			res.Samples = append(res.Samples, sp)
		case domain.KindDrift, domain.KindFurnace, domain.KindBlank:
			cp := domain.CurvePoint{Row: rowNo}
			cp.TimeRaw = asString(row["time"])
			cp.ValueRaw = firstString(row, "value", "mass", "temp")
			fillTime(&cp.TimeS, cp.TimeRaw, jd.Units["time"], rowNo, &ds)
			if kind == domain.KindFurnace {
				fillTemp(&cp.Value, cp.ValueRaw, unitOr(jd.Units, "temp", "value"), rowNo, &ds)
			} else {
				fillMass(&cp.Value, cp.ValueRaw, unitOr(jd.Units, "mass", "value"), rowNo, &ds)
			}
			res.Curve = append(res.Curve, cp)
		case domain.KindGas:
			ge := domain.GasEvent{Row: rowNo}
			ge.TimeRaw = asString(row["time"])
			ge.Atmosphere = asString(row["atmosphere"])
			ge.Action = asString(row["action"])
			if ge.Action == "" {
				ge.Action = "switch"
			}
			fillTime(&ge.TimeS, ge.TimeRaw, jd.Units["time"], rowNo, &ds)
			res.Gas = append(res.Gas, ge)
		case domain.KindInitialMass:
			im := domain.InitialMass{Row: rowNo, Basis: "net"}
			im.MassRaw = asString(row["mass"])
			if b := asString(row["basis"]); b != "" {
				im.Basis = b
			}
			im.TimeRaw = asString(row["time"])
			fillMass(&im.MassMg, im.MassRaw, jd.Units["mass"], rowNo, &ds)
			if im.TimeRaw != "" {
				fillTime(&im.TimeS, im.TimeRaw, jd.Units["time"], rowNo, &ds)
			}
			res.Masses = append(res.Masses, im)
		}
	}
	res.Diagnostics = toDomainDiags(ds)
}

func fillTime(dst *float64, raw, unit string, row int, ds *[]diag) {
	v, ok := parseNumeric(raw)
	if !ok {
		*ds = append(*ds, badCell(row, "time", raw, "bad_number", "time is not numeric"))
		return
	}
	cv, ok := convertTime(v, unit)
	if !ok {
		*ds = append(*ds, badCell(row, "time", raw, "unknown_time_unit", "time unit not convertible: "+unit))
		return
	}
	*dst = cv
}

func fillMass(dst *float64, raw, unit string, row int, ds *[]diag) {
	v, ok := parseNumeric(raw)
	if !ok {
		*ds = append(*ds, badCell(row, "mass", raw, "bad_number", "mass is not numeric"))
		return
	}
	cv, ok := convertMass(v, unit)
	if !ok {
		*ds = append(*ds, badCell(row, "mass", raw, "unknown_mass_unit", "mass unit not convertible: "+unit))
		return
	}
	*dst = cv
}

func fillTemp(dst *float64, raw, unit string, row int, ds *[]diag) {
	v, ok := parseNumeric(raw)
	if !ok {
		*ds = append(*ds, badCell(row, "temp", raw, "bad_number", "temp is not numeric"))
		return
	}
	cv, ok := convertTemp(v, unit)
	if !ok {
		*ds = append(*ds, badCell(row, "temp", raw, "unknown_temp_unit", "temp unit not convertible: "+unit))
		return
	}
	*dst = cv
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%g", t)
	case json.Number:
		return t.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func firstString(row map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := row[k]; ok {
			if s := asString(v); s != "" {
				return s
			}
		}
	}
	return ""
}

func unitOr(units map[string]string, keys ...string) string {
	for _, k := range keys {
		if u := units[k]; u != "" {
			return u
		}
	}
	return ""
}
