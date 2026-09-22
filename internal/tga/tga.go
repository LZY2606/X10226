// Package tga implements the thermogravimetric domain computations:
// unit-aware normalization, balance drift correction, blank subtraction,
// atmosphere delay/thermal-lag modelling, DTG, stage candidates and residue.
//
// Domain rules encoded here:
//   - valve switch time != sample reaction time (atmosphere delay + thermal lag)
//   - stages may overlap; stage mass losses are NOT forced to sum to 100%
//   - sampling gaps are never interpolated across
//   - ambiguous decompositions keep ALL legal candidates
//   - raw representations are preserved; normalization happens in derived layer
package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// AlgoVersion identifies the computation algorithm version stamped on
// every derived value.
const AlgoVersion = "tga/1.2.0"

// Diagnostic describes a value that could not be normalized. The raw value
// stays in place in the source; the diagnostic explains why.
type Diagnostic struct {
	Where   string `json:"where"`
	Message string `json:"message"`
}

// Series is a normalized TGA trace: time (min), temperature (°C), mass (mg).
type Series struct {
	Time  []float64    `json:"time_min"`
	Temp  []float64    `json:"temp_C"`
	Mass  []float64    `json:"mass_mg"`
	Diags []Diagnostic `json:"diagnostics,omitempty"`
}

// Fingerprint returns a stable short hash of any JSON-able value.
func Fingerprint(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// ---- unit handling -------------------------------------------------------

var unitScale = map[string]map[string]float64{
	"time": {"s": 1.0 / 60.0, "sec": 1.0 / 60.0, "min": 1, "h": 60},
	"temp": {"C": 1, "K": math.NaN()}, // K handled specially
	"mass": {"mg": 1, "g": 1000, "ug": 0.001},
}

// Convert converts value expressed in unit to the canonical unit of
// dimension (min, °C, mg). ok=false when the unit is unknown.
func Convert(dimension, unit string, value float64) (float64, bool) {
	if dimension == "temp" && unit == "K" {
		return value - 273.15, true
	}
	m, ok := unitScale[dimension]
	if !ok {
		return 0, false
	}
	f, ok := m[unit]
	if !ok || math.IsNaN(f) {
		return 0, false
	}
	return value * f, true
}

// splitHeaderToken splits "mass_mg" into ("mass","mg").
func splitHeaderToken(tok string) (name, unit string) {
	tok = strings.TrimSpace(tok)
	if i := strings.LastIndex(tok, "_"); i > 0 {
		return tok[:i], tok[i+1:]
	}
	return tok, ""
}

// ParseCSV parses raw CSV text with a unit-annotated header such as
// "time_s,temp_C,mass_mg". It returns normalized columns keyed by base
// name, converting to canonical units. Rows that fail conversion are
// reported as diagnostics and skipped (never silently dropped: the raw
// text remains in the source version).
func ParseCSV(raw, sourceLabel string, dims map[string]string) (map[string][]float64, []string, []Diagnostic, error) {
	lines := []string{}
	for _, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimSpace(ln)
		if ln != "" && !strings.HasPrefix(ln, "#") {
			lines = append(lines, ln)
		}
	}
	if len(lines) < 2 {
		return nil, nil, nil, fmt.Errorf("%s: need header plus at least one data row, got %d lines", sourceLabel, len(lines))
	}
	header := strings.Split(lines[0], ",")
	names := make([]string, len(header))
	units := make([]string, len(header))
	for i, tok := range header {
		names[i], units[i] = splitHeaderToken(tok)
	}
	cols := map[string][]float64{}
	var diags []Diagnostic
	var rawRows []string
	for li, ln := range lines[1:] {
		fields := strings.Split(ln, ",")
		if len(fields) != len(header) {
			diags = append(diags, Diagnostic{
				Where:   fmt.Sprintf("%s:%d", sourceLabel, li+2),
				Message: fmt.Sprintf("column count %d != header %d; row left in raw source only", len(fields), len(header)),
			})
			continue
		}
		vals := make([]float64, len(fields))
		okRow := true
		for i, f := range fields {
			f = strings.TrimSpace(f)
			v, err := strconv.ParseFloat(f, 64)
			if err != nil {
				diags = append(diags, Diagnostic{
					Where:   fmt.Sprintf("%s:%d", sourceLabel, li+2),
					Message: fmt.Sprintf("value %q is not numeric; kept raw, excluded from normalized series", f),
				})
				okRow = false
				break
			}
			dim, want := dims[names[i]]
			if want {
				cv, ok := Convert(dim, units[i], v)
				if !ok {
					diags = append(diags, Diagnostic{
						Where:   fmt.Sprintf("%s:%d", sourceLabel, li+2),
						Message: fmt.Sprintf("unit %q for %s has no conversion to canonical %s; value left unconverted", units[i], names[i], dim),
					})
					okRow = false
					break
				}
				v = cv
			}
			vals[i] = v
		}
		if !okRow {
			continue
		}
		rawRows = append(rawRows, ln)
		for i, name := range names {
			cols[name] = append(cols[name], vals[i])
		}
	}
	return cols, rawRows, diags, nil
}
