package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

type SourceSet struct {
	MassPoints   []RawMassPoint
	Drift        *BalanceDrift
	Furnace      []FurnaceSegment
	GasEvents    []GasEvent
	Blank        []BlankPoint
	InitialMass  *InitialMass
	SourceIDs    map[string]string
	Diagnostics  []string
}

type ErrSource struct {
	Kind string
	Message string
}

func ParseSources(sources []Source) (SourceSet, []ErrSource, error) {
	set := SourceSet{SourceIDs: map[string]string{}}
	var errs []ErrSource
	latest := map[string]Source{}
	for _, src := range sources {
		if src.State == "rejected" {
			continue
		}
		if old, ok := latest[src.Kind]; ok {
			if src.CreatedAt >= old.CreatedAt {
				latest[src.Kind] = src
			}
		} else {
			latest[src.Kind] = src
		}
	}
	for kind, src := range latest {
		set.SourceIDs[kind] = src.ID
		switch kind {
		case KindMassPoints:
			var rows []RawMassPoint
			if err := json.Unmarshal(src.RawJSON, &rows); err != nil {
				var wrapped struct{ Points []RawMassPoint `json:"points"` }
				if err2 := json.Unmarshal(src.RawJSON, &wrapped); err2 != nil {
					errs = append(errs, ErrSource{Kind: kind, Message: err.Error()})
					continue
				}
				rows = wrapped.Points
			}
			set.MassPoints = rows
		case KindBalanceDrift:
			var d BalanceDrift
			if err := json.Unmarshal(src.RawJSON, &d); err != nil {
				errs = append(errs, ErrSource{Kind: kind, Message: err.Error()})
				continue
			}
			set.Drift = &d
		case KindFurnace:
			var rows []FurnaceSegment
			if err := json.Unmarshal(src.RawJSON, &rows); err != nil {
				var wrapped struct{ Segments []FurnaceSegment `json:"segments"` }
				if err2 := json.Unmarshal(src.RawJSON, &wrapped); err2 != nil {
					errs = append(errs, ErrSource{Kind: kind, Message: err.Error()})
					continue
				}
				rows = wrapped.Segments
			}
			set.Furnace = rows
		case KindGasEvents:
			var rows []GasEvent
			if err := json.Unmarshal(src.RawJSON, &rows); err != nil {
				var wrapped struct{ Events []GasEvent `json:"events"` }
				if err2 := json.Unmarshal(src.RawJSON, &wrapped); err2 != nil {
					errs = append(errs, ErrSource{Kind: kind, Message: err.Error()})
					continue
				}
				rows = wrapped.Events
			}
			set.GasEvents = rows
		case KindBlankCurve:
			var rows []BlankPoint
			if err := json.Unmarshal(src.RawJSON, &rows); err != nil {
				var wrapped struct{ Points []BlankPoint `json:"points"` }
				if err2 := json.Unmarshal(src.RawJSON, &wrapped); err2 != nil {
					errs = append(errs, ErrSource{Kind: kind, Message: err.Error()})
					continue
				}
				rows = wrapped.Points
			}
			set.Blank = rows
		case KindInitialMass:
			var im InitialMass
			if err := json.Unmarshal(src.RawJSON, &im); err != nil {
				errs = append(errs, ErrSource{Kind: kind, Message: err.Error()})
				continue
			}
			set.InitialMass = &im
		}
	}
	sort.Slice(set.MassPoints, func(i, j int) bool { return set.MassPoints[i].Seq < set.MassPoints[j].Seq })
	sort.Slice(set.Blank, func(i, j int) bool { return set.Blank[i].Seq < set.Blank[j].Seq })
	sort.Slice(set.Furnace, func(i, j int) bool { return set.Furnace[i].Index < set.Furnace[j].Index })
	sort.Slice(set.GasEvents, func(i, j int) bool { return gasOrder(set.GasEvents[i]) < gasOrder(set.GasEvents[j]) })
	if set.InitialMass == nil {
		errs = append(errs, ErrSource{Kind: KindInitialMass, Message: "accepted initial mass layer is required"})
	}
	return set, errs, nil
}

func gasOrder(e GasEvent) float64 {
	if e.ValveAt.Value != nil { return *e.ValveAt.Value }
	return math.MaxFloat64
}

func ParseQuantity(raw json.RawMessage, expectedUnit string) Quantity {
	q := Quantity{Original: append(json.RawMessage(nil), raw...)}
	var asMap map[string]any
	if err := json.Unmarshal(raw, &asMap); err == nil {
		if rv, ok := asMap["raw"]; ok {
			q.Raw = fmt.Sprint(rv)
		}
		if u, ok := asMap["unit"].(string); ok { q.Unit = u }
		if b, ok := asMap["coordinate_basis"].(string); ok { q.CoordinateBasis = b }
		if v, ok := asMap["value"]; ok {
			f, err := anyToFloat(v)
			q.Value = &f
			if err != nil { q.Value = nil; q.Unconvertible = true; q.Diagnostics = append(q.Diagnostics, err.Error()) }
		} else if r, ok := asMap["raw"]; ok {
			f, err := parseNumber(fmt.Sprint(r))
			if err == nil { q.Value = &f } else { q.Unconvertible = true; q.Diagnostics = append(q.Diagnostics, err.Error()) }
		}
	} else {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			q.Raw = s
			f, err := parseNumber(s)
			if err != nil { q.Unconvertible = true; q.Diagnostics = append(q.Diagnostics, err.Error()) } else { q.Value = &f }
		} else {
			var f float64
			if err := json.Unmarshal(raw, &f); err == nil {
				q.Raw = strconv.FormatFloat(f, 'f', -1, 64); q.Value = &f
			} else { q.Raw = string(raw); q.Unconvertible = true; q.Diagnostics = append(q.Diagnostics, "unparseable numeric value")
			}
		}
	}
	if q.Value != nil {
		converted, unit, err := convertUnit(*q.Value, q.Unit, expectedUnit)
		if err != nil {
			q.Unconvertible = true; q.Diagnostics = append(q.Diagnostics, err.Error())
		} else {
			q.Value = &converted; q.Unit = unit
		}
	}
	return q
}

func anyToFloat(v any) (float64, error) {
	switch x := v.(type) {
	case float64: return x, nil
	case string: return parseNumber(x)
	case json.Number: return x.Float64()
	default: return 0, fmt.Errorf("numeric value has unsupported representation %T", v)
	}
}

func parseNumber(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "NA") || strings.EqualFold(s, "null") { return 0, fmt.Errorf("empty or unavailable value remains at original position") }
	return strconv.ParseFloat(s, 64)
}

func convertUnit(v float64, unit, expected string) (float64, string, error) {
	unit = strings.TrimSpace(unit)
	expected = strings.TrimSpace(expected)
	if unit == "" { unit = expected }
	if unit == expected { return v, unit, nil }
	switch expected {
	case "mg":
		if unit == "g" { return v * 1000, "mg", nil }
	case "min":
		if unit == "s" { return v / 60, "min", nil }
	case "°C":
		if unit == "K" { return v - 273.15, "°C", nil }
	case "mg/min":
		if unit == "mg/s" { return v * 60, "mg/min", nil }
	}
	return v, unit, fmt.Errorf("cannot convert %s to %s; original retained", unit, expected)
}

func StableHash(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(h[:])[:16]
}

func Fingerprint(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])[:24]
}
