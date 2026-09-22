// Package ingest parses the human-copyable, sectioned text format used to
// import raw TGA inputs. Raw text is preserved verbatim; only the derived
// layer normalizes units. Structural failures reject a whole fragment; per-row
// failures are attached as diagnostics while the rows that parse survive.
package ingest

import (
	"fmt"
	"strings"

	"tga-review/internal/domain"
)

// Fragment is one section of an import submission.
type Fragment struct {
	Header string // raw header line, e.g. "[samples mg C s]"
	Kind   domain.RawKind
	Text   string
	Lines  []string // non-comment content lines including header-stripped rows
}

// ParseError rejects the whole fragment structurally (half-failure).
type ParseError struct{ Msg string }

func (e *ParseError) Error() string { return e.Msg }

// headerMap maps header tokens to layer kinds.
var headerMap = map[string]domain.RawKind{
	"samples":     domain.KindSamples,
	"drift":       domain.KindDrift,
	"program":     domain.KindProgram,
	"gas":         domain.KindGas,
	"blank":       domain.KindBlank,
	"initmass":    domain.KindInitMass,
	"initialmass": domain.KindInitMass,
}

// Split divides a submission into fragments. Unknown sections become rejected
// fragments rather than being silently ignored.
func Split(text string) []Fragment {
	var frags []*Fragment
	var cur *Fragment
	var curLines []string
	flush := func() {
		if cur != nil {
			cur.Lines = curLines
			frags = append(frags, cur)
		}
		cur, curLines = nil, nil
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, "//") {
			if cur != nil && trim != "" {
				// comments inside a fragment are dropped from its data lines
			}
			continue
		}
		if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
			flush()
			cur = &Fragment{Header: trim, Text: line + "\n"}
			curLines = nil
			continue
		}
		if cur == nil {
			// content outside any section: synthesize a rejected fragment
			cur = &Fragment{Header: "[?]", Kind: "?", Text: ""}
		}
		cur.Text += line + "\n"
		curLines = append(curLines, line)
	}
	flush()
	out := make([]Fragment, 0, len(frags))
	for _, f := range frags {
		out = append(out, *f)
	}
	return out
}

func headerKind(header string) (domain.RawKind, error) {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(header), "["), "]"))
	fields := strings.Fields(inner)
	if len(fields) == 0 {
		return "", &ParseError{Msg: "empty section header"}
	}
	k, ok := headerMap[strings.ToLower(fields[0])]
	if !ok {
		return "", &ParseError{Msg: fmt.Sprintf("unknown section %q", fields[0])}
	}
	return k, nil
}

// headerUnits returns unit tokens declared after the section name.
func headerUnits(header string) []string {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(header), "["), "]"))
	return strings.Fields(inner)
}

func splitRow(line string) []string {
	if strings.Contains(line, ";") {
		line = strings.ReplaceAll(line, ";", ",")
	}
	return strings.Split(line, ",")
}

func clean(in []string) []string {
	out := make([]string, len(in))
	for i := range in {
		out[i] = strings.TrimSpace(in[i])
	}
	return out
}
