package importer

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"tga-review/internal/domain"
)

// Parsed is the layered result of parsing one import text.
type Parsed struct {
	Kind     domain.Kind
	Samples  []domain.SamplePoint
	Drift    []domain.DriftPoint
	Furnace  []domain.FurnaceSegment
	Valves   []domain.ValveEvent
	Blank    []domain.BlankPoint
	M0       []domain.M0Layer
	Rows     []domain.ImportRow
	Status   string // complete | partial
	Diag     string
}

// Parse dispatches on kind and preserves every original row.
func Parse(kind domain.Kind, text string) (*Parsed, error) {
	if !kind.Valid() {
		return nil, fmt.Errorf("unknown import kind %q", kind)
	}
	p := &Parsed{Kind: kind, Status: "complete"}
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	header := map[string]int{}
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		trim := strings.TrimSpace(raw)
		if trim == "" {
			continue
		}
		fields := splitCSVLine(trim)
		if lineNo == 1 && looksHeader(fields) {
			for i, f := range fields {
				header[strings.ToLower(strings.TrimSpace(f))] = i
			}
			continue
		}
		diag := ""
		switch kind {
		case domain.KindSamples:
			diag = p.parseSample(fields, header, lineNo)
		case domain.KindDrift:
			diag = p.parseDrift(fields, header, lineNo)
		case domain.KindFurnace:
			diag = p.parseFurnace(fields, header, lineNo)
		case domain.KindValves:
			diag = p.parseValves(fields, header, lineNo)
		case domain.KindBlank:
			diag = p.parseBlank(fields, header, lineNo)
		case domain.KindM0:
			diag = p.parseM0(fields, header, lineNo)
		}
		status := domain.RowOK
		if diag != "" {
			status = domain.RowInvalid
			p.Status = "partial"
			p.Diag = "one or more rows rejected; kept in place with diagnostics"
		}
		p.Rows = append(p.Rows, domain.ImportRow{
			LineNo: lineNo, RawLine: trim, Status: status, Diag: diag,
		})
	}
	if err := sc.Err(); err != nil && err != io.EOF {
		return nil, err
	}
	return p, nil
}

func field(fields []string, header map[string]int, idx int, name string) string {
	if i, ok := header[name]; ok && i < len(fields) {
		return fields[i]
	}
	if idx < len(fields) {
		return fields[idx]
	}
	return ""
}

func looksHeader(f []string) bool {
	j := strings.ToLower(strings.Join(f, ","))
	return strings.Contains(j, "time") || strings.Contains(j, "mass") || strings.Contains(j, "gas")
}

// splitCSVLine is a small RFC-4180-ish splitter supporting quoted fields.
func splitCSVLine(line string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			if inQuote && i+1 < len(line) && line[i+1] == '"' {
				cur.WriteByte('"')
				i++
			} else {
				inQuote = !inQuote
			}
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
