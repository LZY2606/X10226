// Package importpkg parses the small, copy-pasteable domain text formats.
//
// Each fragment starts with a "#!<kind>" header. Physical quantities keep
// their raw token and unit/coordinate basis; a token that cannot be converted
// to a number stays at its original position with an error diagnostic rather
// than being dropped. Structural problems (missing/unknown header, wrong layer,
// malformed rows) are reported so a batch can partially commit.
package importpkg

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"tgareview/internal/domain"
)

// ParseResult is the outcome of parsing one fragment.
type ParseResult struct {
	Kind        string
	Body        any
	Diagnostics []domain.Diagnostic
	Fatal       bool // fragment must be rejected
}

func fatalf(file string, line int, code, format string, args ...any) ParseResult {
	return ParseResult{
		Fatal:       true,
		Diagnostics: []domain.Diagnostic{{Level: "error", Code: code, File: file, Line: line, Message: fmt.Sprintf(format, args...)}},
	}
}

func (r *ParseResult) err(line int, code, format string, args ...any) {
	r.Diagnostics = append(r.Diagnostics, domain.Diagnostic{Level: "error", Code: code, File: "", Line: line, Message: fmt.Sprintf(format, args...)})
}

func (r *ParseResult) warn(line int, code, format string, args ...any) {
	r.Diagnostics = append(r.Diagnostics, domain.Diagnostic{Level: "warning", Code: code, File: "", Line: line, Message: fmt.Sprintf(format, args...)})
}

// Parse dispatches one text fragment to the layer parser.
func Parse(name, text string) ParseResult {
	header, headerLine, rest := splitHeader(text)
	if header == "" {
		return fatalf(name, headerLine, "E_NO_HEADER", "缺少 '#!<layer>' 分层标识头，无法判定来源层")
	}
	if !knownKind(header) {
		return fatalf(name, headerLine, "E_UNKNOWN_LAYER", "未知来源层标识 %q", header)
	}
	r := ParseResult{Kind: header}
	switch header {
	case domain.KindPoints:
		parsePoints(&r, name, rest)
	case domain.KindDrift:
		parseDrift(&r, name, rest)
	case domain.KindFurnace:
		parseFurnace(&r, name, rest)
	case domain.KindGas:
		parseGas(&r, name, rest)
	case domain.KindBlank:
		parseBlank(&r, name, rest)
	case domain.KindInitialMass:
		parseInitialMass(&r, name, rest)
	case domain.KindAlgoParams:
		parseAlgoParams(&r, name, rest)
	}
	for i := range r.Diagnostics {
		r.Diagnostics[i].File = name
	}
	return r
}

func knownKind(k string) bool {
	for _, x := range domain.SourceKinds {
		if x == k {
			return true
		}
	}
	return false
}

func splitHeader(text string) (string, int, []line) {
	ls := splitLines(text)
	for i, ln := range ls {
		t := strings.TrimSpace(ln.text)
		if t == "" || strings.HasPrefix(t, "#") {
			if strings.HasPrefix(t, "#!") {
				return strings.TrimSpace(strings.TrimPrefix(t, "#!")), i + 1, ls
			}
			continue
		}
		return "", i + 1, ls
	}
	return "", len(ls), ls
}

type line struct {
	no   int
	text string
}

func splitLines(text string) []line {
	raw := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]line, 0, len(raw))
	for i, t := range raw {
		out = append(out, line{no: i + 1, text: t})
	}
	return out
}

// colSpec describes a "# a[u]@coord; b[u]" header.
type colSpec struct {
	name       string
	unit       string
	coordinate string
}

func parseColumnHeader(comment string) []colSpec {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(comment), "#"))
	var cols []colSpec
	for _, part := range strings.Split(t, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		cs := colSpec{name: part}
		if at := strings.Index(part, "@"); at >= 0 {
			cs.coordinate = strings.TrimSpace(part[at+1:])
			part = part[:at]
		}
		if lb := strings.Index(part, "["); lb >= 0 && strings.HasSuffix(strings.TrimSpace(part), "]") {
			cs.name = strings.TrimSpace(part[:lb])
			cs.unit = part[lb+1 : len(part)-1]
		}
		cols = append(cols, cs)
	}
	return cols
}

// qty builds a domain.Qty, preserving raw tokens that fail to parse.
func qty(raw, unit, coord string) domain.Qty {
	raw = strings.TrimSpace(raw)
	q := domain.Qty{Raw: raw, Unit: unit, Coordinate: coord}
	if raw == "" {
		return q
	}
	if v, err := strconv.ParseFloat(raw, 64); err == nil {
		q.Value = v
		q.Valid = true
	}
	return q
}

// keyed parses "name[u]@coord=value", returning name, unit, coord, raw value.
func keyed(s string) (name, unit, coord, val string, ok bool) {
	eq := strings.Index(s, "=")
	if eq < 0 {
		return "", "", "", "", false
	}
	left := strings.TrimSpace(s[:eq])
	val = strings.TrimSpace(s[eq+1:])
	name = left
	if at := strings.Index(left, "@"); at >= 0 {
		coord = strings.TrimSpace(left[at+1:])
		left = left[:at]
	}
	if lb := strings.Index(left, "["); lb >= 0 && strings.HasSuffix(left, "]") {
		name = strings.TrimSpace(left[:lb])
		unit = left[lb+1 : len(left)-1]
	}
	return name, unit, coord, val, true
}

func dataLines(ls []line) []line {
	out := ls[:0]
	for _, ln := range ls {
		t := strings.TrimSpace(ln.text)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, ln)
	}
	return out
}

func headerComment(ls []line) (string, bool) {
	for _, ln := range ls {
		t := strings.TrimSpace(ln.text)
		if strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "#!") {
			return t, true
		}
	}
	return "", false
}

func splitRow(s string) []string {
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func bodyJSON(r *ParseResult, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		r.Fatal = true
		r.Diagnostics = append(r.Diagnostics, domain.Diagnostic{Level: "error", Code: "E_INTERNAL", Message: err.Error()})
		return
	}
	r.Body = b
}
