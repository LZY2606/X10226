package ingest

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"tgaline/internal/domain"
)

// CSV layout (small, copy-pasteable):
//
//	# kind=samples
//	# units: time=s, mass=mg, temp=C
//	time,mass,temp
//	0,10.02,30.1
//
// kinds: samples, drift, furnace, gas, blank, initial_mass.
func parseCSV(res *ParseResult, data []byte) {
	kind := domain.Kind("")
	units := map[string]string{}
	// strip comment lines for the CSV reader but remember them.
	var body []string
	lines := strings.Split(string(data), "\n")
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, "//") {
			applyMeta(trim, &kind, units)
			continue
		}
		if trim != "" {
			body = append(body, ln)
		}
	}
	if kind == "" {
		res.Fatal = fmt.Errorf("csv: missing '# kind=...' header declaration")
		return
	}
	if len(body) < 1 {
		res.Fatal = fmt.Errorf("csv: no header row after meta comments")
		return
	}
	res.Kind = kind
	res.Units = units
	r := csv.NewReader(strings.NewReader(strings.Join(body, "\n")))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		res.Fatal = fmt.Errorf("csv: malformed csv: %w", err)
		return
	}
	header := rows[0]
	for i := range header {
		header[i] = strings.TrimSpace(strings.ToLower(header[i]))
	}
	switch kind {
	case domain.KindSamples:
		parseSampleRows(res, header, rows[1:], units)
	case domain.KindDrift, domain.KindFurnace, domain.KindBlank:
		parseCurveRows(res, kind, header, rows[1:], units)
	case domain.KindGas:
		parseGasRows(res, header, rows[1:], units)
	case domain.KindInitialMass:
		parseMassRows(res, header, rows[1:], units)
	default:
		res.Fatal = fmt.Errorf("csv: unknown kind %q", kind)
	}
}

func applyMeta(line string, kind *domain.Kind, units map[string]string) {
	s := strings.TrimLeft(line, "#/ \t")
	s = strings.TrimSpace(s)
	if strings.HasPrefix(strings.ToLower(s), "kind=") {
		*kind = domain.Kind(strings.TrimSpace(s[len("kind="):]))
		return
	}
	if strings.HasPrefix(strings.ToLower(s), "units:") {
		s = s[len("units:"):]
		for _, pair := range strings.Split(s, ",") {
			kv := strings.SplitN(pair, "=", 2)
			if len(kv) == 2 {
				units[strings.TrimSpace(strings.ToLower(kv[0]))] = strings.TrimSpace(kv[1])
			}
		}
	}
}

func colIndex(header []string, names ...string) int {
	for _, n := range names {
		for i, h := range header {
			if h == n {
				return i
			}
		}
	}
	return -1
}

func cell(records [][]string, row, idx int) string {
	if idx < 0 || row >= len(records) || idx >= len(records[row]) {
		return ""
	}
	return strings.TrimSpace(records[row][idx])
}

var _ = io.EOF
var _ = strconv.Atoi
