// Package analysis implements the TGA domain computations: normalization of
// raw layered inputs, balance-drift and blank correction, DTG, atmosphere
// delay / temperature lag modelling, stage candidate detection and residue
// calculation chains. All functions are pure and deterministic.
package analysis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"tga-gsb/internal/domain"
)

// AlgoVersion is stamped on every derived value.
const AlgoVersion = "tga-stage-v1"

// Params are the analysis parameters; their fingerprint is stored on every
// derived result so a parameter change only expires dependent results.
type Params struct {
	AtmosphereDelaySec   float64 `json:"atmosphere_delay_sec"`
	TemperatureLagSec    float64 `json:"temperature_lag_sec"`
	GapThresholdSec      float64 `json:"gap_threshold_sec"`
	PeakThresholdMgMin   float64 `json:"peak_threshold_mg_per_min"`
	NoiseSpikeMg         float64 `json:"noise_spike_mg"`
	SmoothingWindow      int     `json:"smoothing_window"`
}

func DefaultParams() Params {
	return Params{
		AtmosphereDelaySec: 45,
		TemperatureLagSec:  20,
		GapThresholdSec:    30,
		PeakThresholdMgMin: 0.05,
		NoiseSpikeMg:       0.05,
		SmoothingWindow:    5,
	}
}

// Fingerprint returns a stable fingerprint of the parameters.
func Fingerprint(p Params) string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// StableID builds a content-addressed id with the given prefix.
func StableID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}

// ---------------------------------------------------------------------------
// Raw parsing. Original representation is preserved by the caller; here we
// normalize what we can and emit diagnostics for the rest.
// ---------------------------------------------------------------------------

// RawPoint is one parsed sample row; OK=false rows stay in place with a
// diagnostic and are excluded from normalized curves but never dropped.
type RawPoint struct {
	Line                int
	TRaw, MassRaw, TempRaw string
	T, Mass, Temp       float64 // s, mg, C
	OK                  bool
}

func parseQty(raw, dimension string) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}
	// split numeric prefix and optional unit suffix
	i := 0
	for i < len(s) && (strings.ContainsRune("0123456789+-.eE", rune(s[i]))) {
		i++
	}
	num, unit := strings.TrimSpace(s[:i]), strings.TrimSpace(s[i:])
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, fmt.Errorf("not a number: %q", raw)
	}
	if unit == "" {
		return v, nil // header-declared units apply
	}
	switch dimension {
	case "time":
		switch unit {
		case "s":
			return v, nil
		case "min":
			return v * 60, nil
		case "h":
			return v * 3600, nil
		}
	case "mass":
		switch unit {
		case "mg":
			return v, nil
		case "g":
			return v * 1000, nil
		case "ug":
			return v / 1000, nil
		}
	case "temp":
		switch unit {
		case "C":
			return v, nil
		case "K":
			return v - 273.15, nil
		}
	case "rate":
		switch unit {
		case "mg/min":
			return v, nil
		case "mg/s":
			return v * 60, nil
		}
	}
	return 0, fmt.Errorf("unconvertible unit %q for %s", unit, dimension)
}

// parseLayer splits meta lines (# key: value) and data rows.
func parseLayer(raw string) (map[string]string, [][]string, []int) {
	meta := map[string]string{}
	var rows [][]string
	var lines []int
	for n, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			body := strings.TrimPrefix(line, "#")
			if k, v, ok := strings.Cut(body, ":"); ok {
				meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
			continue
		}
		fields := strings.Split(line, ",")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		rows = append(rows, fields)
		lines = append(lines, n+1)
	}
	return meta, rows, lines
}

// ParseSamplePoints parses the raw sample-points layer.
func ParseSamplePoints(raw string) ([]RawPoint, []domain.Diagnostic, error) {
	meta, rows, lines := parseLayer(raw)
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("sample_points: no data rows")
	}
	var pts []RawPoint
	var diags []domain.Diagnostic
	for i, f := range rows {
		if len(f) != 3 {
			return nil, nil, fmt.Errorf("sample_points line %d: want 3 fields (time,mass,temp), got %d", lines[i], len(f))
		}
		p := RawPoint{Line: lines[i], TRaw: f[0], MassRaw: f[1], TempRaw: f[2], OK: true}
		var err error
		if p.T, err = parseQty(f[0], "time"); err != nil {
			p.OK = false
			diags = append(diags, domain.Diagnostic{Layer: domain.KindSamplePoints, Where: fmt.Sprintf("line %d", p.Line), Raw: f[0], Reason: err.Error()})
		}
		if p.Mass, err = parseQty(f[1], "mass"); err != nil {
			p.OK = false
			diags = append(diags, domain.Diagnostic{Layer: domain.KindSamplePoints, Where: fmt.Sprintf("line %d", p.Line), Raw: f[1], Reason: err.Error()})
		}
		if p.Temp, err = parseQty(f[2], "temp"); err != nil {
			p.OK = false
			diags = append(diags, domain.Diagnostic{Layer: domain.KindSamplePoints, Where: fmt.Sprintf("line %d", p.Line), Raw: f[2], Reason: err.Error()})
		}
		_ = meta
		pts = append(pts, p)
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].T < pts[j].T })
	return pts, diags, nil
}

// Drift is a linear balance-drift model: offset(t) = Intercept + Rate*t.
type Drift struct {
	RateMgPerMin float64
	InterceptMg  float64
}

func (d Drift) Offset(tSec float64) float64 { return d.InterceptMg + d.RateMgPerMin*tSec/60 }

func ParseDrift(raw string) (Drift, error) {
	meta, _, _ := parseLayer(raw)
	rate, err := parseQty(meta["rate"], "rate")
	if err != nil {
		return Drift{}, fmt.Errorf("drift_correction: rate: %v", err)
	}
	intercept := 0.0
	if v, ok := meta["intercept"]; ok {
		intercept, err = parseQty(v, "mass")
		if err != nil {
			return Drift{}, fmt.Errorf("drift_correction: intercept: %v", err)
		}
	}
	return Drift{RateMgPerMin: rate, InterceptMg: intercept}, nil
}

// Furnace program: piecewise-linear setpoint segments.
type FurnacePoint struct{ T, Setpoint float64 }

func ParseFurnace(raw string) ([]FurnacePoint, error) {
	_, rows, lines := parseLayer(raw)
	if len(rows) == 0 {
		return nil, fmt.Errorf("furnace_program: no data rows")
	}
	var out []FurnacePoint
	for i, f := range rows {
		if len(f) != 2 {
			return nil, fmt.Errorf("furnace_program line %d: want 2 fields (time,setpoint)", lines[i])
		}
		t, err := parseQty(f[0], "time")
		if err != nil {
			return nil, fmt.Errorf("furnace_program line %d: %v", lines[i], err)
		}
		sp, err := parseQty(f[1], "temp")
		if err != nil {
			return nil, fmt.Errorf("furnace_program line %d: %v", lines[i], err)
		}
		out = append(out, FurnacePoint{T: t, Setpoint: sp})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out, nil
}

// SetpointAt interpolates the furnace setpoint; never extrapolates wildly:
// outside the program it holds the edge setpoint.
func SetpointAt(prog []FurnacePoint, t float64) float64 {
	if len(prog) == 0 {
		return math.NaN()
	}
	if t <= prog[0].T {
		return prog[0].Setpoint
	}
	if t >= prog[len(prog)-1].T {
		return prog[len(prog)-1].Setpoint
	}
	for i := 1; i < len(prog); i++ {
		if t <= prog[i].T {
			f := (t - prog[i-1].T) / (prog[i].T - prog[i-1].T)
			return prog[i-1].Setpoint + f*(prog[i].Setpoint-prog[i-1].Setpoint)
		}
	}
	return prog[len(prog)-1].Setpoint
}

// GasEvent is one valve switch.
type GasEvent struct {
	T     float64
	Valve string
	Gas   string
}

func ParseGasEvents(raw string) ([]GasEvent, error) {
	_, rows, lines := parseLayer(raw)
	if len(rows) == 0 {
		return nil, fmt.Errorf("gas_events: no data rows")
	}
	var out []GasEvent
	for i, f := range rows {
		if len(f) != 3 {
			return nil, fmt.Errorf("gas_events line %d: want 3 fields (time,valve,gas)", lines[i])
		}
		t, err := parseQty(f[0], "time")
		if err != nil {
			return nil, fmt.Errorf("gas_events line %d: %v", lines[i], err)
		}
		out = append(out, GasEvent{T: t, Valve: f[1], Gas: f[2]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out, nil
}

// BlankPoint is one empty-pan apparent-mass sample.
type BlankPoint struct{ T, Mass float64 }

func ParseBlank(raw string) ([]BlankPoint, error) {
	_, rows, lines := parseLayer(raw)
	if len(rows) == 0 {
		return nil, fmt.Errorf("blank_curve: no data rows")
	}
	var out []BlankPoint
	for i, f := range rows {
		if len(f) != 2 {
			return nil, fmt.Errorf("blank_curve line %d: want 2 fields (time,mass)", lines[i])
		}
		t, err := parseQty(f[0], "time")
		if err != nil {
			return nil, fmt.Errorf("blank_curve line %d: %v", lines[i], err)
		}
		m, err := parseQty(f[1], "mass")
		if err != nil {
			return nil, fmt.Errorf("blank_curve line %d: %v", lines[i], err)
		}
		out = append(out, BlankPoint{T: t, Mass: m})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out, nil
}

func ParseInitialMass(raw string) (float64, error) {
	meta, _, _ := parseLayer(raw)
	v, ok := meta["mass"]
	if !ok {
		return 0, fmt.Errorf("initial_mass: missing 'mass' entry")
	}
	m, err := parseQty(v, "mass")
	if err != nil {
		return 0, fmt.Errorf("initial_mass: %v", err)
	}
	if m <= 0 {
		return 0, fmt.Errorf("initial_mass: mass must be positive, got %v mg", m)
	}
	return m, nil
}
