// Package tgalang parses the small, copy-pastable layered TGA import format.
// Every source layer is parsed independently; bad rows are preserved with
// diagnostics instead of aborting the whole import.
package tgalang

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"tgaline/internal/domain"
)

var headerToKind = map[string]domain.LayerKind{
	"samples:raw":     domain.LayerSample,
	"drift:balance":   domain.LayerDrift,
	"furnace:program": domain.LayerFurnace,
	"gas:valves":      domain.LayerGas,
	"blank:curve":     domain.LayerBlank,
	"initial:mass":    domain.LayerInitial,
}

type Options struct {
	// ExplicitTruncation treats a final unterminated physical line as an
	// import half-failure: the valid prefix commits, the partial tail row is
	// rejected with a truncation diagnostic.
	ExplicitTruncation bool
}

type SectionResult struct {
	Kind      domain.LayerKind
	Header    string
	Rows      []domain.LayerRow
	Diags     []domain.ImportDiagnostic
	Truncated bool
}

// physicalLines splits text into lines, reporting whether each line ended
// with a newline. A trailing non-empty line without '\n' reports terminated=false.
func physicalLines(text string) (lines []string, terminated []bool) {
	rest := text
	for {
		i := strings.IndexByte(rest, '\n')
		if i < 0 {
			if rest != "" {
				lines = append(lines, strings.TrimSuffix(rest, "\r"))
				terminated = append(terminated, false)
			}
			return
		}
		line := rest[:i]
		lines = append(lines, strings.TrimSuffix(line, "\r"))
		terminated = append(terminated, true)
		rest = rest[i+1:]
	}
}

// Parse splits an import text into layer sections with per-row outcomes.
func Parse(text string, opts Options) ([]SectionResult, []domain.ImportDiagnostic) {
	lines, terms := physicalLines(text)

	var sections []SectionResult
	var globalDiags []domain.ImportDiagnostic
	var cur *SectionResult

	flush := func() {
		if cur != nil {
			sections = append(sections, *cur)
			cur = nil
		}
	}
	addGlobal := func(lineNo int, level, code, msg, raw string) {
		globalDiags = append(globalDiags, domain.ImportDiagnostic{
			LineNo: lineNo, Level: level, Code: code, Message: msg, Raw: raw,
		})
	}

	for idx, raw := range lines {
		lineNo := idx + 1
		trim := strings.TrimSpace(raw)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.HasPrefix(trim, "@") {
			if cur == nil {
				addGlobal(lineNo, "warning", "directive",
					"directive retained but not interpreted: "+trim, raw)
			} else {
				cur.Diags = append(cur.Diags, domain.ImportDiagnostic{
					LineNo: lineNo, Level: "warning", Code: "directive_in_section",
					Message: "directive inside section ignored: " + trim, Raw: raw,
				})
			}
			continue
		}
		if k, ok := headerToKind[trim]; ok {
			flush()
			cur = &SectionResult{Kind: k, Header: trim}
			continue
		}
		if strings.HasSuffix(trim, ":") {
			flush()
			cur = &SectionResult{Kind: "", Header: trim}
			cur.Diags = append(cur.Diags, domain.ImportDiagnostic{
				LineNo: lineNo, Level: "warning", Code: "unknown_section",
				Message: "unknown section header (rows retained verbatim, not normalized): " + trim, Raw: raw,
			})
			continue
		}

		if cur == nil {
			addGlobal(lineNo, "error", "outside_section",
				"data row outside any section: "+trim, raw)
			continue
		}
		if cur.Kind == "" {
			cur.Rows = append(cur.Rows, domain.LayerRow{
				LineNo: lineNo, Raw: raw, Status: domain.RowRejected,
				Diagnostic: "row in unknown section retained verbatim",
			})
			continue
		}
		row := parseRow(cur.Kind, lineNo, raw)
		cur.Rows = append(cur.Rows, row)
		if row.Status == domain.RowRejected {
			cur.Diags = append(cur.Diags, domain.ImportDiagnostic{
				LineNo: lineNo, Level: "error", Code: "row_rejected",
				Message: row.Diagnostic, Raw: raw,
			})
		}
	}

	if opts.ExplicitTruncation && len(terms) > 0 && !terms[len(terms)-1] {
		// Last physical line was non-empty and unterminated. If it belonged to
		// a known section, mark that final row as a half-failure tail.
		target := cur
		if target != nil && target.Kind != "" && len(target.Rows) > 0 {
			last := &target.Rows[len(target.Rows)-1]
			last.Status = domain.RowRejected
			last.Diagnostic = "truncated row: stream ended before line terminator (import half-failure)"
			target.Truncated = true
			target.Diags = append(target.Diags, domain.ImportDiagnostic{
				LineNo: last.LineNo, Level: "error", Code: "truncated_row",
				Message: "stream ended mid-row; valid prefix committed, tail aborted", Raw: last.Raw,
			})
		} else {
			addGlobal(len(lines), "error", "truncated_stream",
				"stream ended without line terminator (import half-failure)", lines[len(lines)-1])
		}
	}
	flush()
	return sections, globalDiags
}

func parseRow(kind domain.LayerKind, lineNo int, raw string) domain.LayerRow {
	row := domain.LayerRow{LineNo: lineNo, Raw: raw, Status: domain.RowAccepted}
	comment := ""
	if i := strings.Index(raw, "#"); i >= 0 {
		comment = strings.TrimSpace(raw[i+1:])
		raw = raw[:i]
	}
	toks := strings.Fields(raw)
	putExtra := func(k, v string) {
		if row.Extra == nil {
			row.Extra = map[string]string{}
		}
		row.Extra[k] = v
	}
	reject := func(format string, args ...any) domain.LayerRow {
		row.Status = domain.RowRejected
		row.Diagnostic = fmt.Sprintf(format, args...)
		row.Fields = toks
		return row
	}

	switch kind {
	case domain.LayerSample:
		if len(toks) < 6 {
			return reject("sample row needs: <time> <unit> <T> <unit> <mass> <unit> (got %d fields)", len(toks))
		}
		tq, err1 := qField(toks[0], toks[1], domain.DimTime)
		Tq, err2 := qField(toks[2], toks[3], domain.DimTemperature)
		mq, err3 := qField(toks[4], toks[5], domain.DimMass)
		row.TimeQ, row.TQ, row.MassQ = &tq, &Tq, &mq
		if len(toks) > 6 {
			putExtra("label", strings.Join(toks[6:], " "))
		}
		if comment != "" {
			putExtra("comment", comment)
		}
		if errs := joinErrs(err1, err2, err3); errs != nil {
			return reject("sample row: %v", errs)
		}
		row.Fields = toks
		return row

	case domain.LayerDrift:
		if len(toks) < 4 {
			return reject("drift row needs: <time> <unit> <drift_rate> <unit> (got %d fields)", len(toks))
		}
		tq, err1 := qField(toks[0], toks[1], domain.DimTime)
		rq, err2 := qField(toks[2], toks[3], domain.DimRate)
		row.TimeQ, row.MassQ = &tq, &rq
		if comment != "" {
			putExtra("comment", comment)
		}
		if errs := joinErrs(err1, err2); errs != nil {
			return reject("drift row: %v", errs)
		}
		row.Fields = toks
		return row

	case domain.LayerFurnace:
		if len(toks) < 4 {
			return reject("furnace row needs: <time> <unit> <T> <unit> (got %d fields)", len(toks))
		}
		tq, err1 := qField(toks[0], toks[1], domain.DimTime)
		Tq, err2 := qField(toks[2], toks[3], domain.DimTemperature)
		row.TimeQ, row.TQ = &tq, &Tq
		if len(toks) > 4 {
			putExtra("segment", strings.Join(toks[4:], " "))
		}
		if errs := joinErrs(err1, err2); errs != nil {
			return reject("furnace row: %v", errs)
		}
		row.Fields = toks
		return row

	case domain.LayerGas:
		if len(toks) < 6 {
			return reject("gas row needs: <time> <unit> <gas> <on|off> <flow> <unit> (got %d)", len(toks))
		}
		tq, err1 := qField(toks[0], toks[1], domain.DimTime)
		gas := toks[2]
		state := strings.ToLower(toks[3])
		if state != "on" && state != "off" {
			return reject("gas row: state must be on|off, got %q", toks[3])
		}
		fq, err6 := qField(toks[4], toks[5], domain.DimFlow)
		row.TimeQ = &tq
		putExtra("gas", gas)
		putExtra("state", state)
		row.MassQ = &fq
		if comment != "" {
			putExtra("comment", comment)
		}
		if errs := joinErrs(err1, err6); errs != nil {
			return reject("gas row: %v", errs)
		}
		row.Fields = toks
		return row

	case domain.LayerBlank:
		if len(toks) < 4 {
			return reject("blank row needs: <time> <unit> <mass> <unit> (got %d fields)", len(toks))
		}
		tq, err1 := qField(toks[0], toks[1], domain.DimTime)
		mq, err2 := qField(toks[2], toks[3], domain.DimMass)
		row.TimeQ, row.MassQ = &tq, &mq
		if errs := joinErrs(err1, err2); errs != nil {
			return reject("blank row: %v", errs)
		}
		row.Fields = toks
		return row

	case domain.LayerInitial:
		if len(toks) < 4 {
			return reject("initial row needs: <mass> <unit> source <id> [<time> <unit>] (got %d)", len(toks))
		}
		mq, err1 := qField(toks[0], toks[1], domain.DimMass)
		if !strings.EqualFold(toks[2], "source") {
			return reject("initial row: expected 'source', got %q", toks[2])
		}
		src := toks[3]
		row.MassQ = &mq
		putExtra("source", src)
		if len(toks) >= 6 {
			tq, errt := qField(toks[4], toks[5], domain.DimTime)
			row.TimeQ = &tq
			if errt != nil {
				return reject("initial row: %v", errt)
			}
		}
		if comment != "" {
			putExtra("comment", comment)
		}
		if err1 != nil {
			return reject("initial row: %v", err1)
		}
		row.Fields = toks
		return row
	}
	return reject("no parser for section %q", kind)
}

func qField(valTok, unitTok string, dim domain.Dimension) (domain.Q, error) {
	v, err := strconv.ParseFloat(valTok, 64)
	if err != nil {
		return domain.Q{Raw: 0, RawUnit: unitTok, Dim: dim},
			fmt.Errorf("bad number %q", valTok)
	}
	return domain.NewQ(v, unitTok, dim)
}

func joinErrs(errs ...error) error {
	var parts []string
	for _, e := range errs {
		if e != nil {
			parts = append(parts, e.Error())
		}
	}
	if parts == nil {
		return nil
	}
	return errors.New(strings.Join(parts, "; "))
}
