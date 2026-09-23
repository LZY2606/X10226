package ingest

import (
	"fmt"
	"strconv"
	"strings"

	"tgareview/internal/domain"
)

func dataLines(lines []string) []struct {
	n int
	s string
} {
	var out []struct {
		n int
		s string
	}
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		out = append(out, struct {
			n int
			s string
		}{i + 1, t})
	}
	return out
}

func firstDataLine(lines []string) (int, string) {
	d := dataLines(lines)
	if len(d) == 0 {
		return 0, ""
	}
	return d[0].n, d[0].s
}

// parseSamples parses a header row "time,temperature,mass" with unit row
// "s,°C,mg" then data rows. Blank uses the same shape.
func parseSamples(lines []string, start int, sr *SectionResult, blank bool) {
	d := dataLines(lines)
	if len(d) < 2 {
		sr.Fatal = true
		sr.Diagnostics = append(sr.Diagnostics, diag("error",
			fmt.Sprintf("section %s", sr.Kind), "E_HEADER",
			"need header + unit rows and at least one data row"))
		return
	}
	hdr := cleanCells(splitRow(d[0].s))
	if len(hdr) < 3 {
		sr.Fatal = true
		sr.Diagnostics = append(sr.Diagnostics, diag("error",
			fmt.Sprintf("line %d", start+d[0].n), "E_HEADER",
			"header must have time,temperature,mass columns"))
		return
	}
	if len(d) < 3 {
		sr.Fatal = true
		sr.Diagnostics = append(sr.Diagnostics, diag("error",
			fmt.Sprintf("section %s", sr.Kind), "E_NODATA", "no data rows"))
		return
	}
	units := cleanCells(splitRow(d[1].s))
	if len(units) < 3 {
		sr.Fatal = true
		sr.Diagnostics = append(sr.Diagnostics, diag("error",
			fmt.Sprintf("line %d", start+d[1].n), "E_UNITS",
			"unit row must declare time/temperature/mass units"))
		return
	}
	var pts []domain.RawSample
	for _, row := range d[2:] {
		cells := cleanCells(splitRow(row.s))
		loc := fmt.Sprintf("line %d", start+row.n)
		if len(cells) < 3 {
			sr.Fatal = true
			sr.Diagnostics = append(sr.Diagnostics, diag("error", loc, "E_COLUMNS",
				"expected 3 columns time,temperature,mass"))
			return
		}
		// Validate conversion but KEEP raw text; a bad cell is a row diagnostic.
		timeQ := domain.NormalizeTime(cells[0], units[0])
		tempQ := domain.NormalizeTemp(cells[1], units[1])
		massQ := domain.NormalizeMass(cells[2], units[2])
		for _, q := range []domain.Q{timeQ, tempQ, massQ} {
			if !q.Valid {
				sr.Diagnostics = append(sr.Diagnostics, diag("error", loc, "E_UNIT", q.Note+
					" (raw value retained)"))
			}
		}
		pts = append(pts, domain.RawSample{
			Line: start + row.n, TimeRaw: cells[0], TempRaw: cells[1], MassRaw: cells[2],
			TimeUnit: units[0], TempUnit: units[1], MassUnit: units[2],
		})
	}
	// Unordered / non-monotonic time is a structural import error.
	for i := 1; i < len(pts); i++ {
		a, _ := strconv.ParseFloat(pts[i-1].TimeRaw, 64)
		b, _ := strconv.ParseFloat(pts[i].TimeRaw, 64)
		if b <= a {
			sr.Diagnostics = append(sr.Diagnostics, diag("warning",
				fmt.Sprintf("line %d", pts[i].Line), "W_TIME_ORDER",
				"sample times must be strictly increasing"))
		}
	}
	if blank {
		sr.Source.Blank = pts
	} else {
		sr.Source.Samples = pts
	}
}

func parseDrift(lines []string, start int, sr *SectionResult) {
	d := dataLines(lines)
	if len(d) == 0 {
		sr.Fatal = true
		sr.Diagnostics = append(sr.Diagnostics, diag("error", "[drift]", "E_HEADER", "empty drift section"))
		return
	}
	hdr := strings.ToLower(d[0].s)
	cells := cleanCells(splitRow(hdr))
	dc := &domain.DriftCorrection{Line: start + d[0].n, Basis: "furnace time (s)"}
	switch {
	case strings.HasPrefix(hdr, "model"):
		if len(d) < 2 {
			sr.Fatal = true
			sr.Diagnostics = append(sr.Diagnostics, diag("error", "[drift]", "E_VALUE", "model row missing value"))
			return
		}
		vals := cleanCells(splitRow(d[1].s))
		if len(vals) < 2 {
			sr.Fatal = true
			sr.Diagnostics = append(sr.Diagnostics, diag("error", fmt.Sprintf("line %d", start+d[1].n),
				"E_VALUE", "expected model,rate,unit"))
			return
		}
		dc.Model = strings.ToLower(vals[0])
		dc.RateRaw = vals[1]
		unit := "mg/min"
		if len(vals) > 2 {
			unit = vals[2]
		}
		dc.RateUnit = unit
		rq := domain.NormalizeRate(dc.RateRaw, unit)
		if !rq.Valid {
			sr.Diagnostics = append(sr.Diagnostics, diag("error",
				fmt.Sprintf("line %d", start+d[1].n), "E_UNIT", rq.Note+" (raw retained)"))
		}
		if dc.Model != "linear" && dc.Model != "points" {
			sr.Fatal = true
			sr.Diagnostics = append(sr.Diagnostics, diag("error",
				fmt.Sprintf("line %d", start+d[1].n), "E_MODEL",
				"drift model must be linear or points"))
			return
		}
	case len(cells) == 2:
		dc.Model = "points"
		for _, row := range d {
			v := cleanCells(splitRow(row.s))
			if len(v) < 2 {
				sr.Fatal = true
				sr.Diagnostics = append(sr.Diagnostics, diag("error",
					fmt.Sprintf("line %d", start+row.n), "E_COLUMNS", "expected time,mass"))
				return
			}
			tq := domain.NormalizeTime(v[0], "s")
			mq := domain.NormalizeMass(v[1], "mg")
			if !tq.Valid || !mq.Valid {
				sr.Diagnostics = append(sr.Diagnostics, diag("error",
					fmt.Sprintf("line %d", start+row.n), "E_UNIT",
					"drift point unconvertible (raw retained)"))
			}
			dc.Points = append(dc.Points, domain.DriftPoint{
				Line: start + row.n, TimeRaw: v[0], MassRaw: v[1],
			})
		}
	default:
		sr.Fatal = true
		sr.Diagnostics = append(sr.Diagnostics, diag("error", fmt.Sprintf("line %d", start+d[0].n),
			"E_HEADER", "drift section must be 'model' row or time,mass points"))
		return
	}
	sr.Source.Drift = dc
}
