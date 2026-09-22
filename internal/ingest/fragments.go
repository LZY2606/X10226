package ingest

import (
	"fmt"
	"strings"

	"tga-review/internal/domain"
)

// Parsed is one structurally accepted fragment with typed rows and diags.
type Parsed struct {
	Kind    domain.RawKind
	Payload domain.Payload
	Diags   []domain.RowDiag
}

// ParseFragment parses one section. Structural problems (unknown section, bad
// header, missing required columns) return ParseError so the whole fragment is
// rejected; individual bad values are kept with diagnostics.
func ParseFragment(f Fragment) (Parsed, error) {
	kind, err := headerKind(f.Header)
	if err != nil {
		return Parsed{}, err
	}
	units := headerUnits(f.Header)
	if len(f.Lines) == 0 {
		return Parsed{}, &ParseError{Msg: fmt.Sprintf("section %s has no rows", kind)}
	}
	rows := make([][]string, 0, len(f.Lines))
	for _, ln := range f.Lines {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		rows = append(rows, clean(splitRow(ln)))
	}
	if len(rows) == 0 {
		return Parsed{}, &ParseError{Msg: fmt.Sprintf("section %s has no rows", kind)}
	}
	headerRow, dataRows, named := splitHeader(rows)
	_ = headerRow
	switch kind {
	case domain.KindSamples:
		return parseSamples(kind, units, named, headerRow, dataRows)
	case domain.KindDrift:
		return parseDrift(kind, units, named, headerRow, dataRows)
	case domain.KindProgram:
		return parseProgram(kind, units, named, headerRow, dataRows)
	case domain.KindGas:
		return parseGas(kind, units, named, headerRow, dataRows)
	case domain.KindBlank:
		return parseBlank(kind, units, named, headerRow, dataRows)
	case domain.KindInitMass:
		return parseInit(kind, units, named, headerRow, dataRows)
	}
	return Parsed{}, &ParseError{Msg: "unhandled section"}
}

func splitHeader(rows [][]string) ([]string, [][]string, bool) {
	first := rows[0]
	named := false
	for _, c := range first {
		l := strings.ToLower(c)
		if l == "time" || l == "t" || l == "mass" || l == "m" || l == "temp" ||
			l == "gas" || l == "start" || l == "end" || strings.HasPrefix(l, "start_") ||
			strings.HasPrefix(l, "end_") || l == "mode" || l == "furnace_temp" ||
			l == "sample_temp" || l == "drift_mass" || l == "temp_start" ||
			l == "temp_end" || l == "initial_mass" {
			named = true
		}
	}
	if named {
		return first, rows[1:], true
	}
	return nil, rows, false
}

func col(row []string, named bool, header []string, names []string, pos int) (string, bool) {
	if named {
		for i, h := range header {
			hl := strings.ToLower(strings.TrimSpace(h))
			for _, n := range names {
				if hl == n && i < len(row) {
					return row[i], true
				}
			}
		}
		return "", false
	}
	if pos < len(row) {
		return row[pos], true
	}
	return "", false
}

func diag(ds *[]domain.RowDiag, kind domain.RawKind, row int, level, msg string) {
	*ds = append(*ds, domain.RowDiag{Layer: string(kind), Row: row, Level: level, Msg: msg})
}

func require(v string, ok bool, name string) error {
	if !ok || strings.TrimSpace(v) == "" {
		return &ParseError{Msg: "missing required column " + name}
	}
	return nil
}

func cellDiag(c *domain.Cell, ds *[]domain.RowDiag, kind domain.RawKind, row int, field string) {
	if c.Diagnostic != "" {
		diag(ds, kind, row, "error", field+": "+c.Diagnostic)
	}
}
