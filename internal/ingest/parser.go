// Package ingest parses the small, copy-pastable TGA text format.
//
// Each section is parsed independently so that a malformed section causes a
// partial failure (that fragment is rejected with diagnostics) while valid
// sections are still committed. Raw text and per-cell diagnostics are retained.
package ingest

import (
	"bufio"
	"fmt"
	"strings"

	"tgareview/internal/domain"
)

type SectionResult struct {
	Kind       string
	Origin     string
	Source     *domain.SourceData
	Fatal      bool
	Diagnostics []domain.Diagnostic
	RawSnippet string
}

// header -> kind
var sectionMap = map[string]string{
	"[samples]":      domain.KindSamples,
	"[drift]":        domain.KindDrift,
	"[program]":      domain.KindProgram,
	"[gas]":          domain.KindGas,
	"[blank]":        domain.KindBlank,
	"[initial_mass]": domain.KindInitialMass,
}

// Parse splits a submission into sections and parses each independently.
func Parse(text string) []SectionResult {
	type rawSec struct {
		header string
		origin string
		lines  []string
		start  int
	}
	var raws []rawSec
	var cur *rawSec
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		trim := strings.TrimSpace(line)
		if kind, ok := sectionMap[strings.ToLower(trim)]; ok {
			if cur != nil {
				raws = append(raws, *cur)
			}
			cur = &rawSec{header: kind, origin: trim, start: lineNo}
			continue
		}
		if cur != nil {
			cur.lines = append(cur.lines, line)
		}
	}
	if cur != nil {
		raws = append(raws, *cur)
	}

	var out []SectionResult
	for _, r := range raws {
		sr := parseSection(r.header, r.start, r.lines)
		sr.Origin = r.origin
		sr.RawSnippet = strings.Join(r.lines, "\n")
		out = append(out, sr)
	}
	return out
}

func diag(sev, loc, code, msg string) domain.Diagnostic {
	return domain.Diagnostic{Severity: sev, Location: loc, Code: code, Message: msg}
}

func parseSection(kind string, start int, lines []string) SectionResult {
	sr := SectionResult{Kind: kind, Source: &domain.SourceData{}}
	switch kind {
	case domain.KindSamples:
		parseSamples(lines, start, &sr, false)
	case domain.KindBlank:
		parseSamples(lines, start, &sr, true)
	case domain.KindDrift:
		parseDrift(lines, start, &sr)
	case domain.KindProgram:
		parseProgram(lines, start, &sr)
	case domain.KindGas:
		parseGas(lines, start, &sr)
	case domain.KindInitialMass:
		parseInitial(lines, start, &sr)
	}
	return sr
}

// splitRow splits a comma or tab separated row.
func splitRow(line string) []string {
	if strings.Contains(line, "\t") {
		return strings.Split(line, "\t")
	}
	return strings.Split(line, ",")
}

func cleanCells(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = strings.TrimSpace(c)
	}
	return out
}
