package engine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"tga-review/internal/domain"
)

// RawDoc is one submitted section. format is explicit so malformed input is a
// first-class outcome ("half-failed import") rather than a silent drop.
type RawDoc struct {
	LogicalID string `json:"logical_id"`
	Name      string `json:"name"`
	Layer     string `json:"layer"`
	Format    string `json:"format"` // json_series | json_furnace | json_gas | scalar
	Body      string `json:"body"`
}

type parsedStrata struct {
	rawPoints   []domain.RawPoint
	blankPoints []domain.RawPoint
	furnace     []domain.FurnaceSegment
	valves      []domain.ValveEvent
	initialMass *domain.RawValue
}

// parseDoc parses one record. On failure it returns an error describing the
// exact problem so the record can be stored as a failed input fragment.
func parseDoc(doc RawDoc, seqStart int) (parsedStrata, error) {
	var out parsedStrata
	switch domain.Layer(doc.Layer) {
	case domain.LayerRawPoints, domain.LayerBlank:
		pts, err := parseSeries(doc.Body, seqStart)
		if err != nil {
			return out, err
		}
		if len(pts) < 2 {
			return out, fmt.Errorf("%s: need at least 2 points", doc.Layer)
		}
		if domain.Layer(doc.Layer) == domain.LayerRawPoints {
			out.rawPoints = pts
		} else {
			out.blankPoints = pts
		}
	case domain.LayerFurnace:
		segs, err := parseFurnace(doc.Body)
		if err != nil {
			return out, err
		}
		out.furnace = segs
	case domain.LayerGas:
		valves, err := parseGas(doc.Body)
		if err != nil {
			return out, err
		}
		out.valves = valves
	case domain.LayerInitialMass:
		rv, err := parseScalar(doc.Body, domain.UnitMg)
		if err != nil {
			return out, err
		}
		out.initialMass = &rv
	default:
		return out, fmt.Errorf("unknown layer %q", doc.Layer)
	}
	return out, nil
}

type seriesRow struct {
	I     int    `json:"i"`
	T     string `json:"t"`
	TUnit string `json:"t_unit"`
	Temp  string `json:"temp"`
	TU    string `json:"temp_unit"`
	M     string `json:"m"`
	MUnit string `json:"m_unit"`
}

func parseSeries(body string, seqStart int) ([]domain.RawPoint, error) {
	var rows []seriesRow
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		return nil, fmt.Errorf("invalid JSON series: %w", err)
	}
	pts := make([]domain.RawPoint, 0, len(rows))
	for i, r := range rows {
		if r.T == "" || r.Temp == "" || r.M == "" {
			return nil, fmt.Errorf("row %d missing t/temp/m", i)
		}
		p := domain.RawPoint{
			Index: seqStart + i,
			T:     domain.ParseRawValue(r.T, orDefault(r.TUnit, domain.UnitSeconds), domain.UnitSeconds),
			Temp:  domain.ParseRawValue(r.Temp, orDefault(r.TU, domain.UnitCelsius), domain.UnitCelsius),
			M:     domain.ParseRawValue(r.M, orDefault(r.MUnit, domain.UnitMg), domain.UnitMg),
		}
		pts = append(pts, p)
	}
	return pts, nil
}

type furnaceRow struct {
	ID     string  `json:"id"`
	Kind   string  `json:"kind"`
	StartS float64 `json:"start_s"`
	EndS   float64 `json:"end_s"`
	StartC float64 `json:"start_c"`
	EndC   float64 `json:"end_c"`
}

func parseFurnace(body string) ([]domain.FurnaceSegment, error) {
	var rows []furnaceRow
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		return nil, fmt.Errorf("invalid JSON furnace: %w", err)
	}
	out := make([]domain.FurnaceSegment, 0, len(rows))
	for i, r := range rows {
		if r.Kind != "ramp" && r.Kind != "hold" {
			return nil, fmt.Errorf("furnace row %d: bad kind %q", i, r.Kind)
		}
		id := r.ID
		if id == "" {
			id = fmt.Sprintf("furnace-%d", i+1)
		}
		out = append(out, domain.FurnaceSegment{
			ID: id, Kind: r.Kind, StartS: r.StartS, EndS: r.EndS, StartC: r.StartC, EndC: r.EndC,
		})
	}
	return out, nil
}

type gasRow struct {
	ID      string  `json:"id"`
	AtS     float64 `json:"at_s"`
	FromGas string  `json:"from_gas"`
	ToGas   string  `json:"to_gas"`
}

func parseGas(body string) ([]domain.ValveEvent, error) {
	var rows []gasRow
	if err := json.Unmarshal([]byte(body), &rows); err != nil {
		return nil, fmt.Errorf("invalid JSON gas: %w", err)
	}
	out := make([]domain.ValveEvent, 0, len(rows))
	for i, r := range rows {
		if r.ToGas == "" {
			return nil, fmt.Errorf("gas row %d: to_gas empty", i)
		}
		id := r.ID
		if id == "" {
			id = fmt.Sprintf("valve-%d", i+1)
		}
		out = append(out, domain.ValveEvent{ID: id, AtS: r.AtS, FromGas: r.FromGas, ToGas: r.ToGas})
	}
	return out, nil
}

type scalarRow struct {
	Value string `json:"value"`
	Unit  string `json:"unit"`
}

func parseScalar(body string, wantUnit string) (domain.RawValue, error) {
	var r scalarRow
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return domain.RawValue{}, fmt.Errorf("invalid JSON scalar: %w", err)
	}
	if strings.TrimSpace(r.Value) == "" {
		return domain.RawValue{}, fmt.Errorf("scalar value empty")
	}
	if _, err := strconv.ParseFloat(strings.TrimSpace(r.Value), 64); err != nil {
		return domain.RawValue{Text: r.Value, Unit: r.Unit}, nil
	}
	return domain.ParseRawValue(r.Value, orDefault(r.Unit, wantUnit), wantUnit), nil
}

func orDefault(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}
