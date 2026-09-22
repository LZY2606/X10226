// Package ingest parses the small, copyable CSV formats used for TGA imports.
// Parsing is deliberately strict about structure but never throws away raw
// representation: a token like "N/A" stays in the row and yields a diagnostic
// (half-failed layer) instead of making the whole import vanish.
package ingest

import (
	"fmt"
	"strconv"
	"strings"

	"tga-review/internal/model"
)

// Diagnostic describes one import problem while keeping the layer visible.
type Diagnostic struct {
	Line   int    `json:"line"`
	Field  string `json:"field"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("line %d field %s: %s (raw=%q)", d.Line, d.Field, d.Reason, d.Value)
}

// ParsedLayer is the result of parsing one layer document.
type ParsedLayer struct {
	Kind        model.SourceKind
	Diagnostics []Diagnostic
	Samples     []model.SamplePoint
	Gaps        []model.Gap
	Drift       []model.DriftPoint
	Furnace     []model.FurnaceSeg
	Gas         []model.GasEvent
	Blank       []model.BlankPoint
	Initial     []model.InitialMass
	Params      map[string]string
}

// OK reports whether the layer parsed structurally (may still have diagnostics
// on individual values; fatal diagnostics make it a half-failed layer).
func (p *ParsedLayer) OK() bool {
	for _, d := range p.Diagnostics {
		if d.Reason == "fatal" {
			return false
		}
	}
	return true
}

func splitLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}

func num(token string) (float64, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(token, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func ptr(v float64) *float64 { return &v }

// nonEmptyRecords strips comments/blank lines, returning line-numbered records.
func nonEmptyRecords(lines []string) ([][]string, []int) {
	var recs [][]string
	var nums []int
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		fields := strings.Split(ln, ",")
		for j := range fields {
			fields[j] = strings.TrimSpace(fields[j])
		}
		recs = append(recs, fields)
		nums = append(nums, i+1)
	}
	return recs, nums
}

func expectCols(rec []string, n int, line int, field string) *Diagnostic {
	if len(rec) < n {
		return &Diagnostic{Line: line, Field: field, Value: strings.Join(rec, ","), Reason: "fatal: expected " + strconv.Itoa(n) + " columns"}
	}
	return nil
}
