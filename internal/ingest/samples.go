package ingest

import (
	"encoding/csv"
	"strings"

	"tga-archive/internal/domain"
)

// ParseSamples parses TSV/CSV sample tables. Columns are detected by header:
//
//	time_s/time, temp_C/temp, mass_mg/mass
//
// Empty fields inside the span are declared sampling gaps and must never be
// interpolated. Unconvertible cells stay on the point with a diagnostic.
func ParseSamples(text string) ([]domain.RawSample, []domain.Gap, []domain.Diagnostic, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, nil, []domain.Diagnostic{{Severity: domain.SevError, Code: "empty_section", Layer: domain.LayerSamples, Message: "raw_samples section is empty"}}, false
	}
	lines := strings.Split(text, "\n")
	sep := detectSep(lines[0])
	r := csv.NewReader(strings.NewReader(text))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.Comma = sep
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		return nil, nil, []domain.Diagnostic{{Severity: domain.SevError, Code: "unreadable_table", Layer: domain.LayerSamples, Fragment: firstLine(text), Message: "sample table must have a header row and data rows"}}, false
	}
	ti, pi, mi := -1, -1, -1
	for i, h := range rows[0] {
		switch normalizeHeader(h) {
		case "time", "times", "t":
			ti = i
		case "temp", "temperature", "t_c":
			pi = i
		case "mass", "m", "weight":
			mi = i
		}
	}
	var diags []domain.Diagnostic
	if ti < 0 || pi < 0 || mi < 0 {
		diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "missing_columns", Layer: domain.LayerSamples, Fragment: strings.Join(rows[0], string(sep)),
			Message: "header must identify time, temp and mass columns"})
		return nil, nil, diags, false
	}

	var samples []domain.RawSample
	var gaps []domain.Gap
	for ri, row := range rows[1:] {
		get := func(idx int) string {
			if idx >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[idx])
		}
		tTok, pTok, mTok := get(ti), get(pi), get(mi)
		if tTok == "" {
			diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "row_without_time", Layer: domain.LayerSamples, Fragment: strings.Join(row, string(sep)), Message: "sample row missing time coordinate"})
			continue
		}
		tm := domain.ParseMeasure(appendUnit(tTok, rows[0][ti]), domain.UnitSecond)
		pm := domain.ParseMeasure(appendUnit(pTok, rows[0][pi]), domain.UnitKelvin)
		mm := domain.ParseMeasure(appendUnit(mTok, rows[0][mi]), domain.UnitMg)
		if !tm.Valid {
			diags = append(diags, pointDiag(tTok, ri, "time", "unconvertible_time"))
		}
		if !pm.Valid {
			diags = append(diags, pointDiag(pTok, ri, "temp", "unconvertible_temp"))
		}
		if !mm.Valid {
			diags = append(diags, pointDiag(mTok, ri, "mass", "unconvertible_mass"))
		}
		// Declared gap: a row whose time exists but mass/temp are empty or
		// an explicit GAP marker spans the interval to the next valid time.
		if (mTok == "" || strings.EqualFold(mTok, "gap")) && tm.Valid {
			nextTime := nextValidTime(rows, mi, ti, ri+1, rows[0][ti], sep)
			if nextTime.Valid {
				gaps = append(gaps, domain.Gap{From: tm, To: nextTime, Note: "declared sampling gap; no interpolation"})
				continue
			}
			gaps = append(gaps, domain.Gap{From: tm, To: tm, Note: "open-ended sampling gap; no interpolation"})
			continue
		}
		var flags []string
		if pm.Valid && mm.Valid && len(samples) > 0 {
			prev := samples[len(samples)-1]
			if mm.Value > prev.Mass.Value {
				flags = append(flags, "mass_increase")
			}
		}
		samples = append(samples, domain.RawSample{Index: len(samples), Time: tm, Temp: pm, Mass: mm, Flags: flags})
	}
	if len(samples) == 0 {
		diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "no_valid_points", Layer: domain.LayerSamples, Message: "no usable sampling points"})
		return nil, gaps, diags, false
	}
	return samples, gaps, diags, true
}

func detectSep(line string) rune {
	switch {
	case strings.Contains(line, "\t"):
		return '\t'
	case strings.Count(line, ",") >= 2:
		return ','
	case strings.Count(line, ";") >= 2:
		return ';'
	default:
		return ' '
	}
}

func normalizeHeader(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	// strip a trailing unit suffix like _s, _C, (mg), [mg/s]
	for _, cut := range []string{"(", "["} {
		if i := strings.Index(h, cut); i >= 0 {
			h = h[:i]
		}
	}
	h = strings.Trim(h, "_ ")
	if i := strings.LastIndex(h, "_"); i >= 0 {
		cand := h[i+1:]
		if cand == "s" || cand == "c" || cand == "k" || cand == "mg" || cand == "g" {
			h = h[:i]
		}
	}
	return h
}

func appendUnit(tok, header string) string {
	if tok == "" {
		return tok
	}
	// token already carries a unit ("12.3 mg")
	if strings.ContainsAny(tok, " \t") {
		return tok
	}
	u := headerUnit(header)
	if u == "" {
		return tok
	}
	return tok + " " + u
}

func headerUnit(header string) string {
	h := strings.ToLower(header)
	if i := strings.LastIndex(h, "("); i >= 0 {
		u := strings.TrimSuffix(strings.TrimPrefix(h[i+1:], ""), ")")
		return strings.TrimSpace(u)
	}
	if i := strings.LastIndex(h, "["); i >= 0 {
		u := strings.TrimSuffix(h[i+1:], "]")
		return strings.TrimSpace(u)
	}
	if i := strings.LastIndex(h, "_"); i >= 0 {
		switch strings.TrimSpace(h[i+1:]) {
		case "c":
			return "C"
		case "k":
			return "K"
		case "s":
			return "s"
		case "mg":
			return "mg"
		case "g":
			return "g"
		}
		return strings.TrimSpace(h[i+1:])
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func pointDiag(tok string, row int, field, code string) domain.Diagnostic {
	return domain.Diagnostic{Severity: domain.SevError, Code: code, Layer: domain.LayerSamples, Fragment: tok,
		Message: "row " + itoa(row+1) + " " + field + " kept at source; value not converted"}
}

func nextValidTime(rows [][]string, mi, ti, start int, timeHeader string, sep rune) domain.Measure {
	for r := start; r < len(rows); r++ {
		row := rows[r]
		get := func(idx int) string {
			if idx >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[idx])
		}
		if mi >= len(row) || get(mi) == "" || strings.EqualFold(get(mi), "gap") {
			continue
		}
		return domain.ParseMeasure(appendUnit(get(ti), timeHeader), domain.UnitSecond)
	}
	return domain.Measure{Valid: false}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}
