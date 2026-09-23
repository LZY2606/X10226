// Package ingest parses the six stratified input layers from small,
// copy-pasteable CSV payloads. Original representations are always kept;
// normalization and unit conversion happen later in derived layers.
package ingest

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"tga-review/internal/domain"
)

// FileInput is one uploaded stratum.
type FileInput struct {
	Filename string
	Kind     string // optional; inferred from filename when empty
	Content  string
}

// FileResult reports what happened to one file in an import batch.
type FileResult struct {
	Kind       string
	Filename   string
	Source     *domain.Source
	Accepted   bool
	HardError  string
	Diagnostics []domain.Diagnostic
}

// BatchResult keeps both accepted and rejected strata of one import batch,
// including explicit format errors (import half-failure).
type BatchResult struct {
	BatchID string
	Files   []FileResult
}

// AcceptedSucceeded reports whether every stratum parsed without hard errors.
func (b *BatchResult) AcceptedSucceeded() bool {
	for _, f := range b.Files {
		if !f.Accepted {
			return false
		}
	}
	return true
}

// FailedKinds lists strata rejected in this batch.
func (b *BatchResult) FailedKinds() []string {
	var out []string
	for _, f := range b.Files {
		if !f.Accepted {
			out = append(out, f.Kind)
		}
	}
	return out
}

var kindByFile = map[string]string{
	"samples.csv":        domain.KindSamples,
	"drift.csv":          domain.KindDrift,
	"program.csv":        domain.KindProgram,
	"gas.csv":            domain.KindGasEvents,
	"blank.csv":          domain.KindBlank,
	"init_mass.csv":      domain.KindInitWeight,
	"initial_mass.csv":   domain.KindInitWeight,
}

var requiredHeaders = map[string][]string{
	domain.KindSamples:    {"time_s", "mass_mg"},
	domain.KindDrift:      {"time_s", "offset_mg"},
	domain.KindProgram:    {"from_s", "to_s"},
	domain.KindGasEvents:  {"at_s", "to_gas"},
	domain.KindBlank:      {"time_s", "mass_mg"},
	domain.KindInitWeight: {"mass_mg"},
}

// ParseBatch parses every file independently so one malformed stratum cannot
// poison the others; rejected files are preserved with their diagnostics.
func ParseBatch(projectID, batchID string, files []FileInput, versions map[string]int, parents map[string]string) BatchResult {
	res := BatchResult{BatchID: batchID}
	for _, f := range files {
		kind := f.Kind
		if kind == "" {
			kind = kindByFile[strings.ToLower(strings.TrimSpace(f.Filename))]
		}
		fr := parseFile(projectID, batchID, kind, f, versions[kind], parents[kind])
		res.Files = append(res.Files, fr)
	}
	return res
}

func parseFile(projectID, batchID, kind string, f FileInput, version int, parent string) FileResult {
	fr := FileResult{Kind: kind, Filename: f.Filename}
	if kind == "" {
		fr.Accepted = false
		fr.HardError = fmt.Sprintf("unknown input stratum for file %q", f.Filename)
		return fr
	}
	if version <= 0 {
		version = 1
	}
	hdr, rows, err := readCSV(f.Content)
	if err != nil {
		fr.Accepted = false
		fr.HardError = err.Error()
		fr.Source = &domain.Source{
			ID: ShortID("src", projectID, kind, version, f.Content, batchID, "rejected"),
			ProjectID: projectID, Kind: kind, Version: version, ParentID: parent,
			Filename: f.Filename, BatchID: batchID,
			Diagnostics: []domain.Diagnostic{{Code: "hard_error", Severity: domain.SevError, Message: err.Error()}},
		}
		return fr
	}
	units := headerUnits(hdr)
	var diags []domain.Diagnostic
	for _, req := range requiredHeaders[kind] {
		if !hasHeader(hdr, req) {
			diags = append(diags, domain.Diagnostic{
				Code: "missing_header", Severity: domain.SevError,
				Column: req, Message: fmt.Sprintf("required header %q missing", req),
			})
		}
	}
	if len(rows) == 0 {
		diags = append(diags, domain.Diagnostic{Code: "empty_body", Severity: domain.SevError, Message: "no data rows"})
	}
	validateValues(kind, hdr, rows, &diags)
	hard := false
	for _, d := range diags {
		if d.Severity == domain.SevError {
			hard = true
		}
	}
	idTail := f.Content
	if hard {
		idTail += "rejected"
	}
	src := &domain.Source{
		ID:          ShortID("src", projectID, kind, version, idTail, batchID),
		ProjectID:   projectID,
		Kind:        kind,
		Version:     version,
		ParentID:    parent,
		Filename:    f.Filename,
		Header:      hdr,
		Units:       units,
		Rows:        rows,
		BatchID:     batchID,
		Diagnostics: diags,
	}
	fr.Source = src
	fr.Diagnostics = diags
	fr.Accepted = !hard
	if hard {
		var msgs []string
		for _, d := range diags {
			if d.Severity == domain.SevError {
				msgs = append(msgs, d.Message)
			}
		}
		fr.HardError = strings.Join(msgs, "; ")
	}
	return fr
}

func readCSV(content string) ([]string, []domain.RawRow, error) {
	r := bufio.NewReader(strings.NewReader(content))
	var header []string
	var rows []domain.RawRow
	rowNo := 0
	for {
		line, err := r.ReadString('\n')
		trim := strings.TrimRight(line, "\r\n")
		if trim != "" {
			rowNo++
			cells := splitCSV(trim)
			if rowNo == 1 {
				header = cells
			} else {
				if len(cells) != len(header) {
					return nil, nil, fmt.Errorf("row %d: expected %d columns, got %d", rowNo, len(header), len(cells))
				}
				m := map[string]string{}
				for i, h := range header {
					m[h] = cells[i]
				}
				rows = append(rows, domain.RawRow{RowNo: rowNo - 1, Cells: m})
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
	}
	if header == nil {
		return nil, nil, fmt.Errorf("empty file: missing header row")
	}
	return header, rows, nil
}

func splitCSV(line string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case c == ',' && !inQuote:
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	out = append(out, strings.TrimSpace(cur.String()))
	return out
}

func hasHeader(hdr []string, name string) bool {
	for _, h := range hdr {
		if strings.EqualFold(h, name) {
			return true
		}
	}
	return false
}

// headerUnits extracts the unit/coordinate basis from the header suffix.
// Examples: time_s -> s, mass_mg -> mg, temp_C -> C, rate_Cpmin -> C/min.
func headerUnits(hdr []string) map[string]string {
	u := map[string]string{}
	for _, h := range hdr {
		parts := strings.SplitN(h, "_", 2)
		if len(parts) == 2 {
			u[h] = normalizeUnitName(parts[1])
		}
	}
	return u
}

func normalizeUnitName(s string) string {
	switch s {
	case "Cpmin":
		return "C/min"
	default:
		return s
	}
}

func validateValues(kind string, hdr []string, rows []domain.RawRow, diags *[]domain.Diagnostic) {
	numericCols := map[string]bool{}
	for _, h := range hdr {
		lh := strings.ToLower(h)
		if strings.HasPrefix(lh, "time") || strings.HasPrefix(lh, "mass") ||
			strings.HasPrefix(lh, "temp") || strings.HasPrefix(lh, "offset") ||
			strings.HasPrefix(lh, "blank") ||
			strings.HasPrefix(lh, "from") || strings.HasPrefix(lh, "to") || strings.HasPrefix(lh, "at") ||
			strings.HasPrefix(lh, "rate") {
			numericCols[h] = true
		}
	}
	for _, row := range rows {
		for col, raw := range row.Cells {
			if raw == "" {
				// Blank cell marks an explicit sampling gap coordinate later.
				continue
			}
			if numericCols[col] {
				if _, err := parseLooseFloat(raw); err != nil {
					*diags = append(*diags, domain.Diagnostic{
						Code: "unconvertible_value", Severity: domain.SevError,
						RowNo: row.RowNo, Column: col, Value: raw,
						Message: fmt.Sprintf("value %q in column %q cannot be converted; kept at original position", raw, col),
					})
				}
			}
		}
	}
}
