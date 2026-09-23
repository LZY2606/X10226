package ingest

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gotga/internal/domain"
)

// LayerResult is one parsed layer of an import batch.
type LayerResult struct {
	Kind   domain.DocKind
	Label  string
	Source string
	Doc    *domain.RawDoc
	Err    error
}

// BatchResult distinguishes accepted layers from rejected ones so a partial
// import can commit accepted layers and record diagnostics for the rest.
type BatchResult struct {
	Accepted []LayerResult
	Rejected []LayerResult
}

// ParseBatch parses the sectioned text format:
//
//	## layer=<kind> label=... source=...
//	header col,col,col
//	records...
//
// A malformed section rejects only that section; other sections still parse.
func ParseBatch(projectID string, batchID string, text string) BatchResult {
	var res BatchResult
	sections := splitSections(text)
	if len(sections) == 0 {
		res.Rejected = append(res.Rejected, LayerResult{Err: fmt.Errorf("no ## layer sections found")})
		return res
	}
	for _, sec := range sections {
		lr := parseSection(projectID, batchID, sec)
		if lr.Err != nil {
			res.Rejected = append(res.Rejected, lr)
		} else {
			res.Accepted = append(res.Accepted, lr)
		}
	}
	return res
}

type section struct {
	header map[string]string
	lines  []string
	start  int
}

func splitSections(text string) []section {
	var out []section
	var cur *section
	for idx, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "##") {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &section{header: map[string]string{}, start: idx + 1}
			for _, kv := range fieldsIn(strings.TrimPrefix(trim, "##")) {
				if k, v, ok := strings.Cut(kv, "="); ok {
					cur.header[strings.TrimSpace(k)] = strings.TrimSpace(v)
				}
			}
			continue
		}
		if cur != nil && trim != "" {
			cur.lines = append(cur.lines, line)
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

func fieldsIn(s string) []string { return strings.Fields(s) }

func parseSection(projectID, batchID string, sec section) LayerResult {
	kind := domain.DocKind(sec.header["layer"])
	lr := LayerResult{Kind: kind, Label: sec.header["label"], Source: sec.header["source"]}
	switch kind {
	case domain.KindSamples, domain.KindBlank:
	case domain.KindDrift, domain.KindFurnace, domain.KindGas, domain.KindMass0:
	default:
		lr.Err = fmt.Errorf("section at line %d: unknown layer %q", sec.start, kind)
		return lr
	}
	if len(sec.lines) == 0 {
		lr.Err = fmt.Errorf("section %s at line %d: empty body", kind, sec.start)
		return lr
	}
	cols := strings.Split(sec.lines[0], ",")
	for i := range cols {
		cols[i] = strings.TrimSpace(cols[i])
	}
	if len(cols) < 2 {
		lr.Err = fmt.Errorf("section %s at line %d: need header row", kind, sec.start)
		return lr
	}
	records := make([]map[string]string, 0, len(sec.lines)-1)
	var diags []string
	for i := 1; i < len(sec.lines); i++ {
		row := sec.start + i
		line := strings.TrimSpace(sec.lines[i])
		if line == "" {
			continue
		}
		vals := strings.Split(line, ",")
		rec := map[string]string{}
		for ci, col := range cols {
			if ci < len(vals) {
				rec[col] = strings.TrimSpace(vals[ci])
			}
		}
		if len(vals) > len(cols) {
			diags = append(diags, fmt.Sprintf("row %d: %d extra columns ignored", row, len(vals)-len(cols)))
		}
		records = append(records, rec)
	}
	payload := map[string]any{
		"columns": cols,
		"records": records,
		"diags":   diags,
	}
	if kind == domain.KindSamples || kind == domain.KindBlank {
		p, pd := parsePoints(kind, cols, records, sec.start)
		payload["points"] = p
		diags = append(diags, pd...)
	}
	pb, _ := json.Marshal(payload)
	id := domain.HashID("doc", projectID, kind, batchID, sec.start)
	lr.Doc = &domain.RawDoc{
		ID:        id,
		ProjectID: projectID,
		Kind:      kind,
		Label:     lr.Label,
		Source:    lr.Source,
		RawText:   strings.Join(sec.lines, "\n"),
		Payload:   string(pb),
		Diags:     diags,
	}
	return lr
}

func parsePoints(kind domain.DocKind, cols []string, records []map[string]string, startLine int) ([]domain.SamplePoint, []string) {
	// Recognize common column names but keep raws either way.
	var tCol, mCol string
	for _, c := range cols {
		lc := strings.ToLower(c)
		if tCol == "" && (lc == "t" || lc == "time" || lc == "furnace_time" || strings.HasPrefix(lc, "time")) {
			tCol = c
		}
		if mCol == "" && (lc == "m" || lc == "mass" || strings.Contains(lc, "mass") || lc == "blank") {
			mCol = c
		}
	}
	var diags []string
	out := make([]domain.SamplePoint, 0, len(records))
	for i, rec := range records {
		row := startLine + 1 + i
		sp := domain.SamplePoint{Seq: i + 1, Row: row}
		if tCol != "" {
			sp.TRaw = rec[tCol]
			m := ParseMeasure(sp.TRaw, "time", tCol, row)
			if m.Valid {
				sp.T = &m.Value
				sp.TUnit = m.Unit
			} else {
				sp.Diags = append(sp.Diags, m.Diag)
				diags = append(diags, m.Diag)
			}
		} else {
			d := fmt.Sprintf("row %d: no time column in %s", row, kind)
			sp.Diags = append(sp.Diags, d)
			diags = append(diags, d)
		}
		if mCol != "" {
			sp.MRaw = rec[mCol]
			m := ParseMeasure(sp.MRaw, "mass", mCol, row)
			if m.Valid {
				sp.M = &m.Value
				sp.MUnit = m.Unit
			} else {
				sp.Diags = append(sp.Diags, m.Diag)
				diags = append(diags, m.Diag)
			}
		} else {
			d := fmt.Sprintf("row %d: no mass column in %s", row, kind)
			sp.Diags = append(sp.Diags, d)
			diags = append(diags, d)
		}
		out = append(out, sp)
	}
	return out, diags
}

// ParseScalar is a small helper used by tests and casepack generation.
func ParseScalar(s string) (float64, error) {
	m := ParseMeasure(s, "mass", "v", 0)
	if m.Valid {
		return m.Value, nil
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v, err
}
