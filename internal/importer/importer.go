// Package importer parses the six layered CSV sources into domain Fragments.
//
// Rules:
//   - the original token of every cell is retained verbatim (Cell.Raw);
//   - normalized numeric values are produced in canonical units only on the
//     derived layer (s, mg, degC, mL/min); values that cannot be converted stay
//     in place with a diagnostic and are never dropped "to make the chart";
//   - structural format errors (bad header, empty file) are reported wholesale;
//   - row-level failures produce a half-failed fragment: valid rows remain
//     usable, invalid rows stay recorded with diagnostics.
package importer

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"tga-lineage/internal/domain"
)

// Spec describes the expected columns of one fragment kind.
type Spec struct {
	Kind    domain.FragmentKind
	Format  string
	Aliases map[string][]string // canonical field -> accepted header names
	Required []string
}

var specs = map[domain.FragmentKind]Spec{
	domain.KindSample: {
		Kind: domain.KindSample, Format: "csv:sample_points/v1",
		Aliases: map[string][]string{
			"time": {"time_s", "time", "t", "炉时间_s"},
			"mass": {"raw_mass_mg", "raw_mass", "mass", "质量_mg"},
			"temp": {"sample_temp_c", "sample_temp", "temp", "温度_c"},
			"note": {"note", "备注"},
		},
		Required: []string{"time", "mass"},
	},
	domain.KindBlank: {
		Kind: domain.KindBlank, Format: "csv:blank_curve/v1",
		Aliases: map[string][]string{
			"time": {"time_s", "time", "t"},
			"mass": {"blank_mass_mg", "blank_mass", "mass"},
		},
		Required: []string{"time", "mass"},
	},
	domain.KindFurnace: {
		Kind: domain.KindFurnace, Format: "csv:furnace_program/v1",
		Aliases: map[string][]string{
			"time":     {"time_s", "time", "t"},
			"setpoint": {"setpoint_c", "setpoint", "furnace_temp_c", "炉温_c"},
		},
		Required: []string{"time", "setpoint"},
	},
	domain.KindGas: {
		Kind: domain.KindGas, Format: "csv:gas_valve_events/v1",
		Aliases: map[string][]string{
			"time":  {"time_s", "time", "t"},
			"valve": {"valve_state", "valve", "gas", "state", "气体"},
			"flow":  {"flow_ml_min", "flow", "流量"},
			"note":  {"note", "备注"},
		},
		Required: []string{"time", "valve"},
	},
	domain.KindTare: {
		Kind: domain.KindTare, Format: "csv:initial_mass/v1",
		Aliases: map[string][]string{
			"layer": {"layer", "kind", "分层"},
			"mass":  {"mass_mg", "mass", "质量_mg"},
			"basis": {"basis", "sample", "基准"},
			"note":  {"note", "备注"},
		},
		Required: []string{"layer", "mass"},
	},
}

// SpecFor exposes a kind's spec (used by the case-pack documentation).
func SpecFor(k domain.FragmentKind) Spec { return specs[k] }

type fmtError struct{ msg string }

func (e *fmtError) Error() string { return e.msg }

// IsFormatError reports whether an import failure is structural (no fragment
// is committed) versus a half-failure (fragment committed with diagnostics).
func IsFormatError(err error) bool { _, ok := err.(*fmtError); return ok }

// Parse reads a CSV document of the given kind.
func Parse(kind domain.FragmentKind, filename string, r io.Reader) (*domain.Fragment, error) {
	spec, ok := specs[kind]
	if !ok {
		return nil, &fmtError{msg: fmt.Sprintf("unknown fragment kind %q", kind)}
	}
	cr := csv.NewReader(bufio.NewReader(r))
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err == io.EOF {
		return nil, &fmtError{msg: "empty file: header row missing"}
	}
	if err != nil {
		return nil, &fmtError{msg: "cannot read header: " + cleanCSVErr(err)}
	}
	colOf := map[string]int{}
	for i, h := range header {
		h = strings.ToLower(strings.TrimSpace(h))
		for canon, aliases := range spec.Aliases {
			for _, a := range aliases {
				if h == strings.ToLower(a) {
					colOf[canon] = i
				}
			}
		}
	}
	for _, req := range spec.Required {
		if _, ok := colOf[req]; !ok {
			return nil, &fmtError{msg: fmt.Sprintf("missing required column %q in header %v", req, header)}
		}
	}

	frag := &domain.Fragment{Kind: kind, Version: 0, Filename: filename, Format: spec.Format, Imported: true, Accepted: true}
	rowIdx := 0
	seenTime := map[float64]int{}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			frag.Imported = false
			frag.Rows = append(frag.Rows, domain.Row{
				Index: rowIdx + 1,
				Diagnostics: []domain.Diagnostic{{Code: "csv_row", Severity: domain.SevError,
					Row: rowIdx + 1, Message: "malformed CSV row: " + cleanCSVErr(err)}},
			})
			rowIdx++
			continue
		}
		rowIdx++
		row := domain.Row{Index: rowIdx, Original: map[string]domain.Cell{}}
		get := func(field string) (string, bool) {
			if c, ok := colOf[field]; ok && c < len(rec) {
				return strings.TrimSpace(rec[c]), true
			}
			return "", false
		}
		for canon := range spec.Aliases {
			raw, present := get(canon)
			if !present {
				continue
			}
			cell := domain.Cell{Raw: raw}
			switch canon {
			case "time":
				parseNumCell(&cell, raw, "s")
				if cell.ParseOK {
					if cell.Value < 0 || math.IsNaN(cell.Value) {
						row.Diagnostics = append(row.Diagnostics, domain.Diagnostic{Code: "bad_time", Severity: domain.SevError, Row: rowIdx, Field: canon, Message: "time must be >= 0, original kept"})
						cell.ParseOK = false
					} else if prev, dup := seenTime[cell.Value]; dup {
						row.Diagnostics = append(row.Diagnostics, domain.Diagnostic{Code: "dup_time", Severity: domain.SevWarn, Row: rowIdx, Field: canon, Message: fmt.Sprintf("duplicate time also seen at row %d", prev)})
					} else {
						seenTime[cell.Value] = rowIdx
					}
				}
			case "mass", "setpoint", "flow":
				unit := map[string]string{"mass": "mg", "setpoint": "degC", "flow": "mL/min"}[canon]
				parseNumCell(&cell, raw, unit)
			case "temp":
				parseNumCell(&cell, raw, "degC")
			case "valve":
				v := strings.ToUpper(raw)
				switch v {
				case "N2", "O2", "AIR", "AR", "HE":
					cell.Value = 0
					cell.ParseOK = true
					cell.Unit = "gas"
					cell.Raw = raw
				default:
					row.Diagnostics = append(row.Diagnostics, domain.Diagnostic{Code: "bad_valve", Severity: domain.SevError, Row: rowIdx, Field: canon, Message: fmt.Sprintf("unknown valve state %q (allowed N2/O2/AIR/AR/HE)", raw)})
				}
			case "layer":
				l := strings.ToLower(raw)
				switch l {
				case "tare", "initial", "revision", "crucible":
					cell.ParseOK = true
					cell.Unit = "layer"
				default:
					row.Diagnostics = append(row.Diagnostics, domain.Diagnostic{Code: "bad_layer", Severity: domain.SevError, Row: rowIdx, Field: canon, Message: fmt.Sprintf("unknown mass layer %q (allowed tare/initial/revision/crucible)", raw)})
				}
			case "note", "basis":
				cell.ParseOK = true
				cell.Unit = "text"
			}
			if !cell.ParseOK && canon != "note" && canon != "basis" {
				cell.Unit = ""
			}
			row.Original[canon] = cell
			if !cell.ParseOK && canon != "note" && canon != "basis" {
				frag.Imported = false
				already := false
				for _, d := range row.Diagnostics {
					if d.Field == canon {
						already = true
					}
				}
				if !already {
					row.Diagnostics = append(row.Diagnostics, domain.Diagnostic{Code: "unconvertible", Severity: domain.SevError, Row: rowIdx, Field: canon, Message: fmt.Sprintf("value %q cannot be converted; kept in place", raw)})
				}
			}
		}
		for _, req := range spec.Required {
			if c, ok := row.Original[req]; !ok || (!c.ParseOK) {
				found := false
				for _, d := range row.Diagnostics {
					if d.Field == req {
						found = true
					}
				}
				if !found {
					row.Diagnostics = append(row.Diagnostics, domain.Diagnostic{Code: "missing_required", Severity: domain.SevError, Row: rowIdx, Field: req, Message: "required field absent or unusable"})
				}
			}
		}
		frag.Rows = append(frag.Rows, row)
	}
	if len(frag.Rows) == 0 {
		return nil, &fmtError{msg: "file contains a header but no data rows"}
	}
	return frag, nil
}

func cleanCSVErr(err error) string {
	s := err.Error()
	if i := strings.Index(s, " - "); i >= 0 {
		s = s[i+3:]
	}
	return s
}

// parseNumCell extracts a number with an optional trailing/embedded unit and
// normalizes to the target canonical unit. On failure ParseOK stays false and
// the raw representation is preserved.
func parseNumCell(c *domain.Cell, raw, canonical string) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return
	}
	unit, num := splitUnit(s)
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return
	}
	fromUnit := unit
	if fromUnit == "" {
		fromUnit = canonical // bare number assumed already in canonical unit
	}
	conv, ok := convert(v, fromUnit, canonical)
	if !ok {
		return
	}
	c.Value = conv
	c.ParseOK = true
	c.Unit = canonical
}

func splitUnit(s string) (unit, num string) {
	// Forms: "12.3 mg", "12.3mg", "mg 12.3" (rare).
	fields := strings.Fields(s)
	if len(fields) >= 2 {
		if _, err := strconv.ParseFloat(fields[0], 64); err == nil {
			return normalizeUnit(strings.Join(fields[1:], " ")), fields[0]
		}
	}
	// glued: split at first non-sign/dot/digit
	i := 0
	for i < len(s) {
		ch := s[i]
		if (ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '+' || ch == 'e' || ch == 'E' {
			i++
			continue
		}
		break
	}
	return normalizeUnit(strings.TrimSpace(s[i:])), strings.TrimSpace(s[:i])
}

func normalizeUnit(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	u = strings.ReplaceAll(u, "℃", "c")
	u = strings.ReplaceAll(u, "°c", "c")
	return u
}

var unitConversions = map[[2]string]func(float64) float64{
	{"s", "s"}:        func(v float64) float64 { return v },
	{"min", "s"}:      func(v float64) float64 { return v * 60 },
	{"sec", "s"}:      func(v float64) float64 { return v },
	{"mg", "mg"}:      func(v float64) float64 { return v },
	{"g", "mg"}:       func(v float64) float64 { return v * 1000 },
	{"ug", "mg"}:      func(v float64) float64 { return v / 1000 },
	{"µg", "mg"}:      func(v float64) float64 { return v / 1000 },
	{"c", "degC"}:     func(v float64) float64 { return v },
	{"k", "degC"}:     func(v float64) float64 { return v - 273.15 },
	{"ml/min", "mL/min"}: func(v float64) float64 { return v },
	{"mlmin", "mL/min"}:  func(v float64) float64 { return v },
	{"sccm", "mL/min"}:   func(v float64) float64 { return v },
}

func convert(v float64, from, to string) (float64, bool) {
	if f, ok := unitConversions[[2]string{from, to}]; ok {
		return f(v), true
	}
	return 0, false
}
