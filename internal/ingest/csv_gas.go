package ingest

import (
	"strings"

	"tgaline/internal/domain"
)

func parseGasRows(res *ParseResult, header []string, records [][]string, units map[string]string) {
	ti := colIndex(header, "time", "t", "valve_time")
	ai := colIndex(header, "atmosphere", "gas", "name")
	ci := colIndex(header, "action", "event", "state")
	if ti < 0 || ai < 0 {
		res.Fatal = errf("csv gas: header must contain time and atmosphere columns, got %v", header)
		return
	}
	var diags []diag
	for r := range records {
		rowNo := r + 2
		ge := domain.GasEvent{Row: rowNo}
		ge.TimeRaw = cell(records, r, ti)
		ge.Atmosphere = strings.TrimSpace(cell(records, r, ai))
		if ci >= 0 {
			ge.Action = strings.ToLower(strings.TrimSpace(cell(records, r, ci)))
		}
		if ge.Action == "" {
			ge.Action = "switch"
		}
		if v, ok := parseNumeric(ge.TimeRaw); ok {
			if cv, ok2 := convertTime(v, units["time"]); ok2 {
				ge.TimeS = cv
			} else {
				diags = append(diags, badCell(rowNo, "time", ge.TimeRaw, "unknown_time_unit", "time unit not convertible"))
			}
		} else {
			diags = append(diags, badCell(rowNo, "time", ge.TimeRaw, "bad_number", "time is not numeric"))
		}
		if ge.Atmosphere == "" {
			diags = append(diags, badCell(rowNo, "atmosphere", ge.Atmosphere, "missing_atmosphere", "atmosphere is empty"))
		}
		if ge.Action != "on" && ge.Action != "off" && ge.Action != "switch" {
			diags = append(diags, badCell(rowNo, "action", ge.Action, "unknown_action", "action must be on|off|switch"))
		}
		res.Gas = append(res.Gas, ge)
	}
	res.Diagnostics = append(res.Diagnostics, toDomainDiags(diags)...)
}

func parseMassRows(res *ParseResult, header []string, records [][]string, units map[string]string) {
	mi := colIndex(header, "mass", "m")
	bi := colIndex(header, "basis", "stratum", "type")
	ti := colIndex(header, "time", "t")
	if mi < 0 {
		res.Fatal = errf("csv initial_mass: header must contain a mass column, got %v", header)
		return
	}
	mu := units["mass"]
	var diags []diag
	for r := range records {
		rowNo := r + 2
		im := domain.InitialMass{Row: rowNo}
		im.MassRaw = cell(records, r, mi)
		if bi >= 0 {
			im.Basis = strings.ToLower(strings.TrimSpace(cell(records, r, bi)))
		}
		if im.Basis == "" {
			im.Basis = "net"
		}
		if ti >= 0 {
			im.TimeRaw = cell(records, r, ti)
			if v, ok := parseNumeric(im.TimeRaw); ok {
				if cv, ok2 := convertTime(v, units["time"]); ok2 {
					im.TimeS = cv
				} else {
					diags = append(diags, badCell(rowNo, "time", im.TimeRaw, "unknown_time_unit", "time unit not convertible"))
				}
			}
		}
		if v, ok := parseNumeric(im.MassRaw); ok {
			if cv, ok2 := convertMass(v, mu); ok2 {
				im.MassMg = cv
			} else {
				diags = append(diags, badCell(rowNo, "mass", im.MassRaw, "unknown_mass_unit", "mass unit not convertible: "+mu))
			}
		} else {
			diags = append(diags, badCell(rowNo, "mass", im.MassRaw, "bad_number", "mass is not numeric"))
		}
		if im.Basis != "gross" && im.Basis != "net" && im.Basis != "tare" && im.Basis != "pan" {
			diags = append(diags, badCell(rowNo, "basis", im.Basis, "unknown_basis", "basis must be gross|net|tare|pan"))
		}
		res.Masses = append(res.Masses, im)
	}
	res.Diagnostics = append(res.Diagnostics, toDomainDiags(diags)...)
}
