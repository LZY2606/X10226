package ingest

import (
	"bufio"
	"strconv"
	"strings"

	"tga-review/internal/domain"
)

// ParseText parses the copy-paste line DSL. Layer headers look like:
//
//	## sample_points
//
// Blank lines and lines starting with '#' that are not headers are ignored
// except that they are preserved by the caller if needed. Each layer section
// is parsed independently so a half-failure stays local to that layer.
//
// Inside a section, a line beginning with '@' sets section options, e.g.
//
//	@basis time_unit=s temp_unit=degC mass_unit=mg
//
// Other lines are comma- or whitespace-separated key=value pairs, or for
// series sections bare positional fields.
func ParseText(data []byte) (*Result, error) {
	type section struct {
		name  string
		lines []string
	}
	var sections []section
	var cur *section
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "## ") || strings.HasPrefix(trim, "##\t") {
			name := strings.TrimSpace(strings.TrimPrefix(trim, "##"))
			sections = append(sections, section{name: name})
			cur = &sections[len(sections)-1]
			continue
		}
		if cur == nil {
			continue
		}
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		cur.lines = append(cur.lines, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	byName := map[string][]string{}
	fragByName := map[string]string{}
	known := map[string]bool{}
	for _, l := range domain.Layers {
		known[l] = true
	}
	for _, s := range sections {
		byName[s.name] = s.lines
		fragByName[s.name] = strings.Join(s.lines, "\n")
	}

	res := &Result{}
	for _, name := range domain.Layers {
		lines, ok := byName[name]
		lr := LayerResult{Layer: name, Fragment: fragByName[name]}
		if !ok {
			lr.Status = StatusAbsent
			res.Layers = append(res.Layers, lr)
			continue
		}
		payload, diags, err := parseTextLayer(name, lines)
		lr.Diagnostics = append(lr.Diagnostics, diags...)
		if err != nil {
			lr.Status = StatusFailed
			lr.Diagnostics = append(lr.Diagnostics, diag(domain.SevError, "layer_text", err.Error(), name, 0, ""))
		} else {
			lr.Status = StatusParsed
			lr.Payload = payload
		}
		res.Layers = append(res.Layers, lr)
	}
	return res, nil
}

// fields splits one DSL line on commas and whitespace, keeping quoted tokens.
func fields(line string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			inQuote = !inQuote
		case (c == ' ' || c == '\t' || c == ',') && !inQuote:
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// kv parses key=value tokens; positional bare tokens are returned as values
// keyed by their position.
func kv(line string) (map[string]string, []string) {
	opts := map[string]string{}
	var positional []string
	for _, f := range fields(line) {
		if eq := strings.IndexByte(f, '='); eq > 0 {
			opts[strings.TrimSpace(f[:eq])] = strings.TrimSpace(f[eq+1:])
		} else {
			positional = append(positional, f)
		}
	}
	return opts, positional
}

// sectionOpts reads '@' option lines and returns remaining data lines.
func sectionOpts(lines []string) (map[string]string, []string) {
	opts := map[string]string{}
	var data []string
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		if strings.HasPrefix(trim, "@") {
			m, _ := kv(strings.TrimPrefix(trim, "@"))
			for k, v := range m {
				opts[k] = v
			}
			continue
		}
		data = append(data, l)
	}
	return opts, data
}

func optStr(opts map[string]string, key, def string) string {
	if v, ok := opts[key]; ok {
		return v
	}
	return def
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return n
	}
	return def
}
