package ingest

import (
	"strings"

	"tga-archive/internal/domain"
)

// ParseBlank parses a "time mass" table with a header (same conventions as
// samples but only two columns).
func ParseBlank(text string) ([]domain.BlankPoint, []domain.Diagnostic, bool) {
	lines := nonEmptyLines(text)
	var diags []domain.Diagnostic
	if len(lines) < 2 {
		return nil, []domain.Diagnostic{{Severity: domain.SevError, Code: "empty_blank", Layer: domain.LayerBlank, Message: "blank curve needs header and at least one point"}}, false
	}
	var points []domain.BlankPoint
	for ri, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "bad_blank_row", Layer: domain.LayerBlank, Fragment: line, Message: "row " + itoa(ri+1) + ": expected '<time> <unit> <mass> <unit>'"})
			continue
		}
		tm := domain.ParseMeasure(fields[0]+" "+fields[1], domain.UnitSecond)
		mm := domain.ParseMeasure(fields[2]+" "+fields[3], domain.UnitMg)
		if !tm.Valid || !mm.Valid {
			diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "unconvertible_blank", Layer: domain.LayerBlank, Fragment: line, Message: "blank point kept raw; not converted"})
			continue
		}
		points = append(points, domain.BlankPoint{Time: tm, Mass: mm})
	}
	if len(points) == 0 {
		return nil, append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "no_blank_points", Layer: domain.LayerBlank, Message: "no usable blank points"}), false
	}
	return points, diags, true
}

func nonEmptyLines(text string) []string {
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}
