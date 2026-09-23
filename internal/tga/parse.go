package tga

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseDiag is a non-fatal diagnostic attached to a raw layer. The offending
// value stays in place in the raw text; the derived layer only records why
// it could not be used.
type ParseDiag struct {
	Line int    `json:"line"`
	Raw  string `json:"raw"`
	Err  string `json:"err"`
}

// PointsData is the parsed form of sample_points, blank_curve and
// furnace_program. T and Y hold canonical values (s, mg or degC); if a unit
// is unknown the canonical values are NaN and Valid is false, while the raw
// text remains untouched in the RawInput.
type PointsData struct {
	UnitT string
	UnitY string
	T     []float64
	Y     []float64
	Valid bool
	Diags []ParseDiag
}

// DriftData is the parsed balance drift correction: drift(t) = A + B*t.
type DriftData struct {
	Method string
	A      float64 // mg
	B      float64 // mg/s
	Diags  []ParseDiag
}

// GasEvent is one valve switch.
type GasEvent struct {
	T     float64 // s, valve actuation time (not the sample reaction time)
	Valve string
	Gas   string
	Flow  float64 // sccm
}

// GasData is the parsed gas_events layer.
type GasData struct {
	Events []GasEvent
	Diags  []ParseDiag
}

// WeightData is the parsed initial_weight layer. Known is false when the
// value or its unit could not be converted; the raw text is preserved.
type WeightData struct {
	Value float64 // mg
	Unit  string
	Known bool
	Diags []ParseDiag
}

// LayerText is one section of a combined import document.
type LayerText struct {
	Layer string
	Text  string
}

// ParseCombined splits a combined import document into per-layer sections.
// Sections are introduced by lines of the form "=== layer: <name> ===".
// Unknown layer names are reported as failures, not silently dropped.
func ParseCombined(text string) ([]LayerText, []ImportFailure) {
	var sections []LayerText
	var failed []ImportFailure
	var cur *LayerText
	var body strings.Builder
	flush := func() {
		if cur == nil {
			return
		}
		cur.Text = strings.TrimSpace(body.String())
		sections = append(sections, *cur)
		body.Reset()
	}
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "===") && strings.HasSuffix(trim, "===") {
			flush()
			name := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trim, "==="), "==="))
			name = strings.TrimSpace(strings.TrimPrefix(name, "layer:"))
			cur = &LayerText{Layer: name}
			continue
		}
		if cur != nil {
			body.WriteString(line)
			body.WriteString("\n")
		}
	}
	flush()
	known := map[string]bool{}
	for _, l := range LayerOrder {
		known[l] = true
	}
	var out []LayerText
	for _, s := range sections {
		if !known[s.Layer] {
			failed = append(failed, ImportFailure{Layer: s.Layer, Error: "unknown layer name"})
			continue
		}
		out = append(out, s)
	}
	return out, failed
}

func timeFactor(unit string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "s", "sec", "second", "seconds":
		return 1, true
	case "min", "minute", "minutes":
		return 60, true
	case "h", "hr", "hour", "hours":
		return 3600, true
	}
	return 0, false
}

func massFactor(unit string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "mg":
		return 1, true
	case "g":
		return 1000, true
	case "ug", "µg":
		return 0.001, true
	case "kg":
		return 1e6, true
	}
	return 0, false
}

func tempOffset(unit string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "c", "degc", "°c":
		return 0, true
	case "k", "kelvin":
		return -273.15, true
	}
	return 0, false
}

// parseUnitsLine parses "# units: t=s, mass=mg" style headers.
func parseUnitsLine(line string) map[string]string {
	out := map[string]string{}
	line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
	if !strings.HasPrefix(line, "units:") {
		return out
	}
	for _, kv := range strings.Split(strings.TrimPrefix(line, "units:"), ",") {
		parts := strings.SplitN(strings.TrimSpace(kv), "=", 2)
		if len(parts) == 2 {
			out[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return out
}

func isHeaderLine(line string) bool {
	for _, f := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if f == "" {
			continue
		}
		if _, err := strconv.ParseFloat(f, 64); err != nil {
			return true
		}
	}
	return false
}

func splitFields(line string) []string {
	if strings.Contains(line, ",") {
		parts := strings.Split(line, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	return strings.Fields(line)
}

// ParsePoints parses a two-column points layer (sample_points, blank_curve,
// furnace_program). yKind is "mass" or "temp" and selects the unit table.
// Bad rows are kept as diagnostics; a layer with zero valid rows is fatal.
func ParsePoints(layer, text, yKind string) (*PointsData, error) {
	pd := &PointsData{Valid: true}
	var rows [][2]float64
	units := map[string]string{}
	for ln, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "#") {
			for k, v := range parseUnitsLine(trim) {
				units[k] = v
			}
			continue
		}
		if isHeaderLine(trim) && len(rows) == 0 {
			continue
		}
		fields := splitFields(trim)
		if len(fields) != 2 {
			pd.Diags = append(pd.Diags, ParseDiag{Line: ln + 1, Raw: trim, Err: "expected 2 columns"})
			continue
		}
		t, err1 := strconv.ParseFloat(fields[0], 64)
		y, err2 := strconv.ParseFloat(fields[1], 64)
		if err1 != nil || err2 != nil {
			pd.Diags = append(pd.Diags, ParseDiag{Line: ln + 1, Raw: trim, Err: "non-numeric value"})
			continue
		}
		rows = append(rows, [2]float64{t, y})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("layer %s: no valid data rows", layer)
	}
	pd.UnitT = orDefault(units["t"], "s")
	yKey := "mass"
	if yKind == "temp" {
		yKey = "temp"
	}
	pd.UnitY = orDefault(units[yKey], orDefault(units["y"], map[string]string{"mass": "mg", "temp": "C"}[yKind]))
	tf, tok := timeFactor(pd.UnitT)
	if !tok {
		pd.Valid = false
		pd.Diags = append(pd.Diags, ParseDiag{Line: 0, Raw: pd.UnitT, Err: "unknown time unit; values kept raw"})
	}
	var yconv func(float64) float64
	if yKind == "temp" {
		off, ok := tempOffset(pd.UnitY)
		if !ok {
			pd.Valid = false
			pd.Diags = append(pd.Diags, ParseDiag{Line: 0, Raw: pd.UnitY, Err: "unknown temperature unit; values kept raw"})
		}
		yconv = func(v float64) float64 { return v + off }
	} else {
		fac, ok := massFactor(pd.UnitY)
		if !ok {
			pd.Valid = false
			pd.Diags = append(pd.Diags, ParseDiag{Line: 0, Raw: pd.UnitY, Err: "unknown mass unit; values kept raw"})
		}
		yconv = func(v float64) float64 { return v * fac }
	}
	for _, r := range rows {
		if !pd.Valid {
			pd.T = append(pd.T, math.NaN())
			pd.Y = append(pd.Y, math.NaN())
			continue
		}
		pd.T = append(pd.T, r[0]*tf)
		pd.Y = append(pd.Y, yconv(r[1]))
	}
	return pd, nil
}

func orDefault(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}

// ParseDrift parses the drift_correction layer (key=value lines).
func ParseDrift(text string) (*DriftData, error) {
	dd := &DriftData{Method: "linear"}
	seen := false
	for ln, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		kv := strings.SplitN(trim, "=", 2)
		if len(kv) != 2 {
			dd.Diags = append(dd.Diags, ParseDiag{Line: ln + 1, Raw: trim, Err: "expected key=value"})
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		seen = true
		switch key {
		case "method":
			dd.Method = val
		case "a_mg":
			v, unit := splitValueUnit(val)
			fac, ok := massFactor(orDefault(unit, "mg"))
			if !ok {
				dd.Diags = append(dd.Diags, ParseDiag{Line: ln + 1, Raw: trim, Err: "unknown mass unit"})
				continue
			}
			dd.A = v * fac
		case "b_mg_per_s":
			v, err := strconv.ParseFloat(val, 64)
			if err != nil {
				dd.Diags = append(dd.Diags, ParseDiag{Line: ln + 1, Raw: trim, Err: "non-numeric value"})
				continue
			}
			dd.B = v
		default:
			dd.Diags = append(dd.Diags, ParseDiag{Line: ln + 1, Raw: trim, Err: "unknown key"})
		}
	}
	if !seen {
		return nil, fmt.Errorf("layer drift_correction: no key=value lines")
	}
	return dd, nil
}

func splitValueUnit(v string) (float64, string) {
	fields := strings.Fields(v)
	if len(fields) == 0 {
		return 0, ""
	}
	f, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return math.NaN(), ""
	}
	if len(fields) > 1 {
		return f, fields[1]
	}
	return f, ""
}

// ParseGas parses the gas_events layer (t,valve,gas,flow rows).
func ParseGas(text string) (*GasData, error) {
	gd := &GasData{}
	units := map[string]string{}
	for ln, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "#") {
			for k, v := range parseUnitsLine(trim) {
				units[k] = v
			}
			continue
		}
		if isHeaderLine(trim) && len(gd.Events) == 0 {
			continue
		}
		fields := splitFields(trim)
		if len(fields) != 4 {
			gd.Diags = append(gd.Diags, ParseDiag{Line: ln + 1, Raw: trim, Err: "expected 4 columns t,valve,gas,flow"})
			continue
		}
		t, err1 := strconv.ParseFloat(fields[0], 64)
		flow, err2 := strconv.ParseFloat(fields[3], 64)
		if err1 != nil || err2 != nil {
			gd.Diags = append(gd.Diags, ParseDiag{Line: ln + 1, Raw: trim, Err: "non-numeric t or flow"})
			continue
		}
		gd.Events = append(gd.Events, GasEvent{T: t, Valve: fields[1], Gas: strings.ToLower(fields[2]), Flow: flow})
	}
	if len(gd.Events) == 0 {
		return nil, fmt.Errorf("layer gas_events: no valid events")
	}
	tf, ok := timeFactor(orDefault(units["t"], "s"))
	if !ok {
		return nil, fmt.Errorf("layer gas_events: unknown time unit %q", units["t"])
	}
	for i := range gd.Events {
		gd.Events[i].T *= tf
	}
	return gd, nil
}

// ParseWeight parses the initial_weight layer. Unconvertible values are not
// fatal: the raw text stays in place and Known is false so downstream
// normalized quantities become explicitly unknown.
func ParseWeight(text string) (*WeightData, error) {
	wd := &WeightData{}
	seen := false
	for ln, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		seen = true
		val := trim
		if kv := strings.SplitN(trim, "=", 2); len(kv) == 2 {
			val = strings.TrimSpace(kv[1])
		}
		v, unit := splitValueUnit(val)
		if math.IsNaN(v) {
			wd.Diags = append(wd.Diags, ParseDiag{Line: ln + 1, Raw: trim, Err: "unparseable weight value; kept raw"})
			continue
		}
		fac, ok := massFactor(unit)
		if !ok {
			wd.Diags = append(wd.Diags, ParseDiag{Line: ln + 1, Raw: trim, Err: "missing or unknown mass unit; kept raw"})
			continue
		}
		wd.Value = v * fac
		wd.Unit = unit
		wd.Known = true
	}
	if !seen {
		return nil, fmt.Errorf("layer initial_weight: empty")
	}
	return wd, nil
}

// ValidateLayer parses a layer text and returns a fatal error if the layer
// cannot be accepted at all. Non-fatal problems are diagnostics only.
func ValidateLayer(layer, text string) error {
	var err error
	switch layer {
	case LayerSamplePoints:
		_, err = ParsePoints(layer, text, "mass")
	case LayerBlankCurve:
		_, err = ParsePoints(layer, text, "mass")
	case LayerFurnaceProgram:
		_, err = ParsePoints(layer, text, "temp")
	case LayerDriftCorrection:
		_, err = ParseDrift(text)
	case LayerGasEvents:
		_, err = ParseGas(text)
	case LayerInitialWeight:
		_, err = ParseWeight(text)
	default:
		err = fmt.Errorf("unknown layer %q", layer)
	}
	return err
}
