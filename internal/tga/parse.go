package tga

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseResult carries the converted items plus per-line diagnostics for
// values that could not be converted (kept in place, never dropped).
type ParseResult[T any] struct {
	Items []T
	Diags []Diagnostic
}

func parseLines(raw string, expect int, conv func([]string) (int, error)) []Diagnostic {
	var diags []Diagnostic
	fields := 0
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ",")
		for j := range parts {
			parts[j] = strings.TrimSpace(parts[j])
		}
		n, err := conv(parts)
		if err != nil {
			diags = append(diags, Diagnostic{Line: i + 1, Raw: line, Reason: err.Error()})
			continue
		}
		fields += n
	}
	_ = expect
	_ = fields
	return diags
}

func f(s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("not a number: %q", s)
	}
	return v, nil
}

// ParseSamplePoints parses "t_s,temp_c,mass_mg" rows.
func ParseSamplePoints(raw string) ParseResult[SamplePoint] {
	var out []SamplePoint
	diags := parseLines(raw, 3, func(p []string) (int, error) {
		if len(p) != 3 {
			return 0, fmt.Errorf("want 3 columns t_s,temp_c,mass_mg, got %d", len(p))
		}
		t, err := f(p[0])
		if err != nil {
			return 0, err
		}
		tmp, err := f(p[1])
		if err != nil {
			return 0, err
		}
		m, err := f(p[2])
		if err != nil {
			return 0, err
		}
		out = append(out, SamplePoint{T: t, TempC: tmp, MassMg: m})
		return 1, nil
	})
	return ParseResult[SamplePoint]{Items: out, Diags: diags}
}

// ParseDrift parses "t_s,drift_mg" rows.
func ParseDrift(raw string) ParseResult[DriftPoint] {
	var out []DriftPoint
	diags := parseLines(raw, 2, func(p []string) (int, error) {
		if len(p) != 2 {
			return 0, fmt.Errorf("want 2 columns t_s,drift_mg, got %d", len(p))
		}
		t, err := f(p[0])
		if err != nil {
			return 0, err
		}
		d, err := f(p[1])
		if err != nil {
			return 0, err
		}
		out = append(out, DriftPoint{T: t, DriftMg: d})
		return 1, nil
	})
	return ParseResult[DriftPoint]{Items: out, Diags: diags}
}

// ParseFurnace parses "t_s,setpoint_c,rate_c_per_min" rows.
func ParseFurnace(raw string) ParseResult[FurnacePoint] {
	var out []FurnacePoint
	diags := parseLines(raw, 3, func(p []string) (int, error) {
		if len(p) != 3 {
			return 0, fmt.Errorf("want 3 columns t_s,setpoint_c,rate_c_per_min, got %d", len(p))
		}
		t, err := f(p[0])
		if err != nil {
			return 0, err
		}
		sp, err := f(p[1])
		if err != nil {
			return 0, err
		}
		r, err := f(p[2])
		if err != nil {
			return 0, err
		}
		out = append(out, FurnacePoint{T: t, SetpointC: sp, RateCPerMin: r})
		return 1, nil
	})
	return ParseResult[FurnacePoint]{Items: out, Diags: diags}
}

// ParseGasEvents parses "t_s,valve,gas,flow_ml_min" rows.
func ParseGasEvents(raw string) ParseResult[GasEvent] {
	var out []GasEvent
	diags := parseLines(raw, 4, func(p []string) (int, error) {
		if len(p) != 4 {
			return 0, fmt.Errorf("want 4 columns t_s,valve,gas,flow_ml_min, got %d", len(p))
		}
		t, err := f(p[0])
		if err != nil {
			return 0, err
		}
		fl, err := f(p[3])
		if err != nil {
			return 0, err
		}
		if p[1] == "" || p[2] == "" {
			return 0, fmt.Errorf("valve and gas must be non-empty")
		}
		out = append(out, GasEvent{T: t, Valve: p[1], Gas: p[2], FlowMlMin: fl})
		return 1, nil
	})
	return ParseResult[GasEvent]{Items: out, Diags: diags}
}

// ParseBlank parses "temp_c,blank_mg" rows.
func ParseBlank(raw string) ParseResult[BlankPoint] {
	var out []BlankPoint
	diags := parseLines(raw, 2, func(p []string) (int, error) {
		if len(p) != 2 {
			return 0, fmt.Errorf("want 2 columns temp_c,blank_mg, got %d", len(p))
		}
		tmp, err := f(p[0])
		if err != nil {
			return 0, err
		}
		b, err := f(p[1])
		if err != nil {
			return 0, err
		}
		out = append(out, BlankPoint{TempC: tmp, BlankMg: b})
		return 1, nil
	})
	return ParseResult[BlankPoint]{Items: out, Diags: diags}
}

// ParseInitialMass parses "mass_mg,unit[,note]".
func ParseInitialMass(raw string) (InitialMass, []Diagnostic) {
	var im InitialMass
	var diags []Diagnostic
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ",", 3)
		if len(parts) < 2 {
			diags = append(diags, Diagnostic{Line: i + 1, Raw: line, Reason: "want mass_mg,unit[,note]"})
			continue
		}
		m, err := f(strings.TrimSpace(parts[0]))
		if err != nil {
			diags = append(diags, Diagnostic{Line: i + 1, Raw: line, Reason: err.Error()})
			continue
		}
		im.MassMg = m
		im.Unit = strings.TrimSpace(parts[1])
		if len(parts) == 3 {
			im.Note = strings.TrimSpace(parts[2])
		}
		return im, diags
	}
	diags = append(diags, Diagnostic{Line: 0, Raw: "", Reason: "no initial mass record found"})
	return im, diags
}
