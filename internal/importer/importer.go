// Package importer parses the six layered TGA inputs. The raw text is always
// preserved verbatim by the caller; parsing only produces a normalized view.
// A layer that fails to parse yields a fatal error for that layer only, so an
// import can half-succeed and keep the good layers (import.partial_failure).
package importer

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"

	"tga/internal/model"
	"tga/internal/units"
)

// Point is one canonical raw sample: time s, furnace temperature C, mass mg.
type Point struct {
	TS    float64 `json:"t_s"`
	TempC float64 `json:"temp_C"`
	Mass  float64 `json:"mass_mg"`
}

type Points struct {
	Points      []Point            `json:"points"`
	Diagnostics []model.Diagnostic `json:"diagnostics,omitempty"`
}

type DriftPoint struct {
	TS    float64 `json:"t_s"`
	Drift float64 `json:"drift_mg"`
}

type Drift struct {
	Points      []DriftPoint       `json:"points"`
	Diagnostics []model.Diagnostic `json:"diagnostics,omitempty"`
}

// FurnaceSeg: from StartS, ramp at RateCPerMin toward TargetC.
type FurnaceSeg struct {
	StartS      float64 `json:"start_s"`
	RateCPerMin float64 `json:"rate_C_per_min"`
	TargetC     float64 `json:"target_C"`
}

type Furnace struct {
	Segments    []FurnaceSeg       `json:"segments"`
	Diagnostics []model.Diagnostic `json:"diagnostics,omitempty"`
}

type GasEvent struct {
	TS    float64 `json:"t_s"`
	Valve string  `json:"valve"`
	Gas   string  `json:"gas"`
	Flow  float64 `json:"flow_mL_min"`
}

type GasEvents struct {
	Events      []GasEvent         `json:"events"`
	Diagnostics []model.Diagnostic `json:"diagnostics,omitempty"`
}

type BlankPoint struct {
	TS    float64 `json:"t_s"`
	Blank float64 `json:"blank_mg"`
}

type Blank struct {
	Points      []BlankPoint       `json:"points"`
	Diagnostics []model.Diagnostic `json:"diagnostics,omitempty"`
}

// MassLayer is one layer of the stratified initial mass (as_received, dry...).
type MassLayer struct {
	Layer  string  `json:"layer"`
	MassMg float64 `json:"mass_mg"`
}

type InitialMass struct {
	Layers      []MassLayer        `json:"layers"`
	Diagnostics []model.Diagnostic `json:"diagnostics,omitempty"`
}

func readCSV(raw string) ([]string, [][]string, error) {
	r := csv.NewReader(strings.NewReader(raw))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("csv parse: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("empty input")
	}
	header := make([]string, len(rows[0]))
	for i, h := range rows[0] {
		header[i] = strings.ToLower(strings.TrimSpace(h))
	}
	return header, rows[1:], nil
}

// splitCol splits a header like "time_s" into ("time","s").
func splitCol(h string) (name, unit string) {
	if i := strings.LastIndex(h, "_"); i > 0 {
		return h[:i], h[i+1:]
	}
	return h, ""
}

func colIndex(header []string, want string) int {
	for i, h := range header {
		n, _ := splitCol(h)
		if n == want {
			return i
		}
	}
	return -1
}

// ParseRawPoints parses the raw sampling layer.
func ParseRawPoints(raw string) (*Points, error) {
	header, rows, err := readCSV(raw)
	if err != nil {
		return nil, err
	}
	ti, te, mi := colIndex(header, "time"), colIndex(header, "temp"), colIndex(header, "mass")
	if ti < 0 || te < 0 || mi < 0 {
		return nil, fmt.Errorf("raw_points requires time/temp/mass columns, got %v", header)
	}
	_, tu := splitCol(header[ti])
	_, eu := splitCol(header[te])
	_, mu := splitCol(header[mi])
	out := &Points{}
	for i, row := range rows {
		loc := fmt.Sprintf("row %d", i+2)
		if len(row) <= mi || len(row) <= te || len(row) <= ti {
			out.Diagnostics = append(out.Diagnostics, model.Diagnostic{Code: "short_row", Message: "not enough columns", Location: loc})
			continue
		}
		tv, u, err := units.ParseFloat(row[ti])
		if err != nil {
			out.Diagnostics = append(out.Diagnostics, model.Diagnostic{Code: "bad_value", Message: err.Error(), Location: loc})
			continue
		}
		if u == "" {
			u = tu
		}
		t, err := units.ToSeconds(tv, u)
		if err != nil {
			out.Diagnostics = append(out.Diagnostics, model.Diagnostic{Code: "unconvertible", Message: err.Error(), Location: loc})
			continue
		}
		ev, u, err := units.ParseFloat(row[te])
		if err != nil {
			out.Diagnostics = append(out.Diagnostics, model.Diagnostic{Code: "bad_value", Message: err.Error(), Location: loc})
			continue
		}
		if u == "" {
			u = eu
		}
		temp, err := units.ToCelsius(ev, u)
		if err != nil {
			out.Diagnostics = append(out.Diagnostics, model.Diagnostic{Code: "unconvertible", Message: err.Error(), Location: loc})
			continue
		}
		mv, u, err := units.ParseFloat(row[mi])
		if err != nil {
			out.Diagnostics = append(out.Diagnostics, model.Diagnostic{Code: "bad_value", Message: err.Error(), Location: loc})
			continue
		}
		if u == "" {
			u = mu
		}
		mass, err := units.ToMilligram(mv, u)
		if err != nil {
			out.Diagnostics = append(out.Diagnostics, model.Diagnostic{Code: "unconvertible", Message: err.Error(), Location: loc})
			continue
		}
		out.Points = append(out.Points, Point{TS: t, TempC: temp, Mass: mass})
	}
	if len(out.Points) == 0 {
		return out, fmt.Errorf("no usable raw points")
	}
	return out, nil
}

func parsePairs(raw, aName, bName string, conv func(float64, string) (float64, error)) ([][2]float64, []model.Diagnostic, error) {
	header, rows, err := readCSV(raw)
	if err != nil {
		return nil, nil, err
	}
	ai, bi := colIndex(header, aName), colIndex(header, bName)
	if ai < 0 || bi < 0 {
		return nil, nil, fmt.Errorf("requires %s/%s columns, got %v", aName, bName, header)
	}
	_, au := splitCol(header[ai])
	_, bu := splitCol(header[bi])
	var pairs [][2]float64
	var diags []model.Diagnostic
	for i, row := range rows {
		loc := fmt.Sprintf("row %d", i+2)
		if len(row) <= ai || len(row) <= bi {
			diags = append(diags, model.Diagnostic{Code: "short_row", Message: "not enough columns", Location: loc})
			continue
		}
		av, u, err := units.ParseFloat(row[ai])
		if err != nil {
			diags = append(diags, model.Diagnostic{Code: "bad_value", Message: err.Error(), Location: loc})
			continue
		}
		if u == "" {
			u = au
		}
		a, err := conv(av, u)
		if err != nil {
			diags = append(diags, model.Diagnostic{Code: "unconvertible", Message: err.Error(), Location: loc})
			continue
		}
		bv, u, err := units.ParseFloat(row[bi])
		if err != nil {
			diags = append(diags, model.Diagnostic{Code: "bad_value", Message: err.Error(), Location: loc})
			continue
		}
		if u == "" {
			u = bu
		}
		b, err := conv(bv, u)
		if err != nil {
			diags = append(diags, model.Diagnostic{Code: "unconvertible", Message: err.Error(), Location: loc})
			continue
		}
		pairs = append(pairs, [2]float64{a, b})
	}
	return pairs, diags, nil
}

// ParseDrift parses the balance drift correction layer (time, drift mg).
func ParseDrift(raw string) (*Drift, error) {
	pairs, diags, err := parsePairs(raw, "time", "drift", func(v float64, u string) (float64, error) {
		if s, err := units.ToSeconds(v, u); err == nil {
			return s, nil
		}
		return units.ToMilligram(v, u)
	})
	if err != nil {
		return nil, err
	}
	d := &Drift{Diagnostics: diags}
	for _, p := range pairs {
		d.Points = append(d.Points, DriftPoint{TS: p[0], Drift: p[1]})
	}
	return d, nil
}

// ParseBlank parses the blank curve layer (time, apparent mass mg).
func ParseBlank(raw string) (*Blank, error) {
	pairs, diags, err := parsePairs(raw, "time", "blank", func(v float64, u string) (float64, error) {
		if s, err := units.ToSeconds(v, u); err == nil {
			return s, nil
		}
		return units.ToMilligram(v, u)
	})
	if err != nil {
		return nil, err
	}
	b := &Blank{Diagnostics: diags}
	for _, p := range pairs {
		b.Points = append(b.Points, BlankPoint{TS: p[0], Blank: p[1]})
	}
	return b, nil
}

// ParseFurnace parses the furnace program (start, ramp rate, target).
func ParseFurnace(raw string) (*Furnace, error) {
	header, rows, err := readCSV(raw)
	if err != nil {
		return nil, err
	}
	si, ri, ti := colIndex(header, "start"), colIndex(header, "rate"), colIndex(header, "target")
	if si < 0 || ri < 0 || ti < 0 {
		return nil, fmt.Errorf("furnace_program requires start/rate/target columns, got %v", header)
	}
	f := &Furnace{}
	for i, row := range rows {
		loc := fmt.Sprintf("row %d", i+2)
		if len(row) <= si || len(row) <= ri || len(row) <= ti {
			f.Diagnostics = append(f.Diagnostics, model.Diagnostic{Code: "short_row", Message: "not enough columns", Location: loc})
			continue
		}
		sv, u, _ := units.ParseFloat(row[si])
		if u == "" {
			_, u = splitCol(header[si])
		}
		start, err := units.ToSeconds(sv, u)
		if err != nil {
			f.Diagnostics = append(f.Diagnostics, model.Diagnostic{Code: "unconvertible", Message: err.Error(), Location: loc})
			continue
		}
		rate, err := strconv.ParseFloat(strings.TrimSpace(strings.Fields(row[ri])[0]), 64)
		if err != nil {
			f.Diagnostics = append(f.Diagnostics, model.Diagnostic{Code: "bad_value", Message: err.Error(), Location: loc})
			continue
		}
		tv, u, _ := units.ParseFloat(row[ti])
		if u == "" {
			_, u = splitCol(header[ti])
		}
		target, err := units.ToCelsius(tv, u)
		if err != nil {
			f.Diagnostics = append(f.Diagnostics, model.Diagnostic{Code: "unconvertible", Message: err.Error(), Location: loc})
			continue
		}
		f.Segments = append(f.Segments, FurnaceSeg{StartS: start, RateCPerMin: rate, TargetC: target})
	}
	if len(f.Segments) == 0 {
		return f, fmt.Errorf("no usable furnace segments")
	}
	return f, nil
}

// ParseGasEvents parses the gas valve event layer.
func ParseGasEvents(raw string) (*GasEvents, error) {
	header, rows, err := readCSV(raw)
	if err != nil {
		return nil, err
	}
	ti, vi, gi, fi := colIndex(header, "time"), colIndex(header, "valve"), colIndex(header, "gas"), colIndex(header, "flow")
	if ti < 0 || vi < 0 || gi < 0 {
		return nil, fmt.Errorf("gas_events requires time/valve/gas columns, got %v", header)
	}
	_, tu := splitCol(header[ti])
	g := &GasEvents{}
	for i, row := range rows {
		loc := fmt.Sprintf("row %d", i+2)
		if len(row) <= gi {
			g.Diagnostics = append(g.Diagnostics, model.Diagnostic{Code: "short_row", Message: "not enough columns", Location: loc})
			continue
		}
		tv, u, err := units.ParseFloat(row[ti])
		if err != nil {
			g.Diagnostics = append(g.Diagnostics, model.Diagnostic{Code: "bad_value", Message: err.Error(), Location: loc})
			continue
		}
		if u == "" {
			u = tu
		}
		t, err := units.ToSeconds(tv, u)
		if err != nil {
			g.Diagnostics = append(g.Diagnostics, model.Diagnostic{Code: "unconvertible", Message: err.Error(), Location: loc})
			continue
		}
		flow := 0.0
		if fi >= 0 && len(row) > fi && strings.TrimSpace(row[fi]) != "" {
			fv, u, err := units.ParseFloat(row[fi])
			if err != nil {
				g.Diagnostics = append(g.Diagnostics, model.Diagnostic{Code: "bad_value", Message: err.Error(), Location: loc})
				continue
			}
			if u == "" {
				_, u = splitCol(header[fi])
			}
			flow, err = units.ToFlow(fv, u)
			if err != nil {
				g.Diagnostics = append(g.Diagnostics, model.Diagnostic{Code: "unconvertible", Message: err.Error(), Location: loc})
				continue
			}
		}
		g.Events = append(g.Events, GasEvent{TS: t, Valve: strings.TrimSpace(row[vi]), Gas: strings.TrimSpace(row[gi]), Flow: flow})
	}
	if len(g.Events) == 0 {
		return g, fmt.Errorf("no usable gas events")
	}
	return g, nil
}

// ParseInitialMass parses the stratified initial mass layer.
// Format: CSV with header layer,value,unit.
func ParseInitialMass(raw string) (*InitialMass, error) {
	header, rows, err := readCSV(raw)
	if err != nil {
		return nil, err
	}
	li, vi, ui := colIndex(header, "layer"), colIndex(header, "value"), colIndex(header, "unit")
	if li < 0 || vi < 0 {
		return nil, fmt.Errorf("initial_mass requires layer/value columns, got %v", header)
	}
	im := &InitialMass{}
	for i, row := range rows {
		loc := fmt.Sprintf("row %d", i+2)
		if len(row) <= vi {
			im.Diagnostics = append(im.Diagnostics, model.Diagnostic{Code: "short_row", Message: "not enough columns", Location: loc})
			continue
		}
		mv, u, err := units.ParseFloat(row[vi])
		if err != nil {
			im.Diagnostics = append(im.Diagnostics, model.Diagnostic{Code: "bad_value", Message: err.Error(), Location: loc})
			continue
		}
		if u == "" && ui >= 0 && len(row) > ui {
			u = strings.TrimSpace(row[ui])
		}
		mass, err := units.ToMilligram(mv, u)
		if err != nil {
			im.Diagnostics = append(im.Diagnostics, model.Diagnostic{Code: "unconvertible", Message: err.Error(), Location: loc})
			continue
		}
		im.Layers = append(im.Layers, MassLayer{Layer: strings.TrimSpace(row[li]), MassMg: mass})
	}
	if len(im.Layers) == 0 {
		return im, fmt.Errorf("no usable initial mass layers")
	}
	return im, nil
}
