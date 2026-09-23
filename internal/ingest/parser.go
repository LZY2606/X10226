package ingest

import (
	"fmt"
	"strings"

	"tgaline/internal/domain"
)

// ParseResult is a document candidate plus parse-level diagnostics. IDs and
// versions are assigned later by the service layer.
type ParseResult struct {
	Kind       domain.Kind
	Name       string
	MediaType  string
	Raw        string
	Units      map[string]string
	Samples    []domain.SamplePoint
	Curve      []domain.CurvePoint
	Gas        []domain.GasEvent
	Masses     []domain.InitialMass
	Diagnostics []domain.Diagnostic
	Fatal      error // structural format error: nothing in this file is accepted
}

// Parse parses one submitted file. The format may be CSV or JSON.
func Parse(name, mediaType string, data []byte) ParseResult {
	res := ParseResult{Name: name, MediaType: mediaType, Raw: string(data), Units: map[string]string{}}
	trimmed := strings.TrimLeftFunc(string(data), func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' })
	if strings.HasPrefix(trimmed, "{") {
		parseJSON(&res, data)
		return res
	}
	if strings.Contains(strings.ToLower(name), ".json") && !strings.HasPrefix(trimmed, "#") && !looksCSV(trimmed) {
		res.Fatal = fmt.Errorf("%s: expected JSON object", name)
		return res
	}
	parseCSV(&res, data)
	return res
}

func looksCSV(s string) bool {
	return strings.Contains(s, ",")
}

func sev(d diag) (domain.Severity, string) {
	return domain.SeverityError, d.code
}

func toDomainDiags(ds []diag) []domain.Diagnostic {
	out := make([]domain.Diagnostic, 0, len(ds))
	for _, d := range ds {
		s, code := sev(d)
		out = append(out, domain.Diagnostic{
			Scope: "row", Row: d.row, Field: d.field, Code: code,
			Message: d.msg, Raw: d.raw, Severity: s,
		})
	}
	return out
}
