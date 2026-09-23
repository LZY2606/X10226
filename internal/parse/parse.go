// Package parse turns a copy-pasteable text bundle into preserved raw rows.
// It never discards input: structural failures are rejected-but-kept, and
// values that cannot be converted stay in place with diagnostics.
package parse

import (
	"fmt"
	"strconv"
	"strings"

	"tga-archive/internal/domain"
)

// Strata lists every accepted input layer, in review order.
var Strata = []string{"sample", "drift", "program", "gas", "blank", "mass0"}

func isStratum(s string) bool {
	for _, x := range Strata {
		if x == s {
			return true
		}
	}
	return false
}

// Bundle parses one import text. kind is initial|revision|partial and parent
// is the event seq it was based on (0 for the first import).
func Bundle(text, kind, parentEvent string, note string) domain.Bundle {
	b := domain.Bundle{
		Kind:        kind,
		ParentEvent: parentEvent,
		Sections:    map[string]string{},
		Note:        note,
	}

	type section struct {
		name   string
		header []col
		raw    strings.Builder
	}
	var cur *section
	sections := map[string]*section{}
	var order []string

	flush := func() {}
	_ = flush

	globalLine := 0
	for _, line := range strings.Split(text, "\n") {
		globalLine++
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			name := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if !isStratum(name) {
				b.Rows = append(b.Rows, domain.RawRow{
					Stratum:  name,
					LineNo:   globalLine,
					Raw:      line,
					Accepted: false,
					Diags:    []domain.Diag{{Code: "unknown_section", Message: "unknown stratum: " + name}},
				})
				cur = nil
				continue
			}
			if _, ok := sections[name]; ok {
				b.Rows = append(b.Rows, domain.RawRow{
					Stratum:  name,
					LineNo:   globalLine,
					Raw:      line,
					Accepted: false,
					Diags:    []domain.Diag{{Code: "duplicate_section", Message: "section appears twice in one bundle: " + name}},
				})
				cur = nil
				continue
			}
			cur = &section{name: name}
			sections[name] = cur
			order = append(order, name)
			b.Sections[name] = ""
			continue
		}
		if cur == nil {
			b.Rows = append(b.Rows, domain.RawRow{
				LineNo:   globalLine,
				Raw:      line,
				Accepted: false,
				Diags:    []domain.Diag{{Code: "outside_section", Message: "data row outside any [section]"}},
			})
			continue
		}
		cur.raw.WriteString(line + "\n")
		fields := splitCSV(line)
		if len(cur.header) == 0 {
			for _, f := range fields {
				cur.header = append(cur.header, parseCol(f))
			}
			continue
		}
		row := domain.RawRow{
			Stratum: cur.name,
			LineNo:  globalLine,
			Raw:     line,
			Fields:  map[string]domain.Value{},
		}
		if len(fields) != len(cur.header) {
			row.Accepted = false
			row.Diags = append(row.Diags, domain.Diag{
				Code: "arity",
				Message: fmt.Sprintf("expected %d columns, got %d", len(cur.header), len(fields)),
			})
			b.Rows = append(b.Rows, row)
			continue
		}
		row.Accepted = true
		for i, c := range cur.header {
			v := convertValue(fields[i], c.unit, c.name)
			row.Fields[c.name] = v
			if v.Bad != "" {
				row.Diags = append(row.Diags, domain.Diag{
					Code: "unconvertible_value", Field: c.name, Message: v.Bad,
				})
			}
		}
		b.Rows = append(b.Rows, row)
	}

	for _, name := range order {
		s := sections[name]
		b.Sections[name] = strings.TrimRight(s.raw.String(), "\n")
		st := domain.SectionStatus{Stratum: name, Present: true}
		if len(s.header) == 0 {
			st.Fatal = "empty section: no header row"
			b.Rows = append(b.Rows, domain.RawRow{Stratum: name, Accepted: false,
				Diags: []domain.Diag{{Code: "empty_section", Message: st.Fatal}}})
		}
		for _, r := range b.Rows {
			if r.Stratum != name {
				continue
			}
			if r.Accepted {
				st.Accepted++
			} else {
				st.Rejected++
			}
		}
		if st.Fatal != "" || st.Rejected > 0 {
			b.HalfFailed = true
		}
		b.Statuses = append(b.Statuses, st)
	}

	// Sections are reviewed in canonical order, not text order.
	out := b.Statuses[:0:0]
	for _, name := range Strata {
		for _, st := range b.Statuses {
			if st.Stratum == name {
				out = append(out, st)
			}
		}
	}
	b.Statuses = out
	return b
}

type col struct{ name, unit string }

func parseCol(h string) col {
	h = strings.TrimSpace(h)
	if i := strings.IndexByte(h, '['); i >= 0 && strings.HasSuffix(h, "]") {
		return col{name: strings.TrimSpace(h[:i]), unit: strings.TrimSpace(h[i+1 : len(h)-1])}
	}
	return col{name: h}
}

func splitCSV(line string) []string {
	var out []string
	for _, f := range strings.Split(line, ",") {
		out = append(out, strings.TrimSpace(f))
	}
	return out
}

// unit conversion tables applied in the IMPORT-adjacent typing step. The raw
// text is always retained; Num is the normalized value used by derived layers.
func convertValue(raw, unit, field string) domain.Value {
	v := domain.Value{Raw: raw, Unit: unit}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		v.Bad = "empty value"
		return v
	}
	if unit == "" {
		// gas names and other unitless columns are kept verbatim
		return v
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		v.Bad = fmt.Sprintf("not a number: %q", raw)
		return v
	}
	switch field {
	case "t":
		switch unit {
		case "s":
		case "min":
			n *= 60
			unit = "s"
		default:
			v.Bad = "unsupported time unit: " + unit
			return v
		}
	case "T", "Tprog", "Ts":
		switch unit {
		case "degC", "C":
		case "K":
			n -= 273.15
			unit = "degC"
		default:
			v.Bad = "unsupported temperature unit: " + unit
			return v
		}
	case "m", "dm", "mblank", "m0":
		switch unit {
		case "mg":
		case "g":
			n *= 1000
			unit = "mg"
		default:
			v.Bad = "unsupported mass unit: " + unit
			return v
		}
	default:
		v.Bad = "unknown column: " + field
		return v
	}
	v.Num = n
	v.Unit = unit
	return v
}
