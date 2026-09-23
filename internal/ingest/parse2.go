package ingest

import (
	"fmt"
	"strconv"
	"strings"

	"tgalineage/internal/model"
)

func parseSection2(kind string, body []string, sr *SectionResult) {
	switch kind {
	case model.KindFurnace:
		p := parseFurnace(body, sr)
		sr.Kind, sr.Parsed, sr.Valid = kind, p, !hasError(sr.Diagnostics)
	case model.KindGas:
		g := parseGas(body, sr)
		sr.Kind, sr.Parsed, sr.Valid = kind, g, !hasError(sr.Diagnostics)
	case model.KindMass:
		m := parseMass(body, sr)
		sr.Kind, sr.Parsed, sr.Valid = kind, m, !hasError(sr.Diagnostics)
	case model.KindParams:
		p := parseParams(body, sr)
		sr.Kind, sr.Parsed, sr.Valid = kind, p, !hasError(sr.Diagnostics)
	}
}

// parseFurnace expects:
//
//	initial 30C
//	ramp 10C/min to 900C
//	isothermal 120s
func parseFurnace(body []string, sr *SectionResult) model.FurnaceProgram {
	var fp model.FurnaceProgram
	for idx, line := range body {
		ln := idx + 1
		fields := splitCells(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "initial":
			if len(fields) < 2 {
				sr.Diagnostics = append(sr.Diagnostics, errAt(ln, model.KindFurnace,
					"row-shape", "initial needs a temperature"))
				continue
			}
			r, d := asRepr(fields[1], UnitC, UnitC)
			if d.Message != "" {
				d.Line = ln
				sr.Diagnostics = append(sr.Diagnostics, d)
			} else {
				fp.Initial = r
			}
		case "ramp":
			seg := model.FurnaceSegment{ID: fmt.Sprintf("fs_%06d", len(fp.Segments)+1),
				Line: ln, Kind: "ramp"}
			rate, target, ok := rampPair(fields[1:])
			if !ok {
				sr.Diagnostics = append(sr.Diagnostics, errAt(ln, model.KindFurnace,
					"row-shape", "ramp needs '<rate> to <target>'"))
			}
			if rate != "" {
				r, d := asRepr(rate, UnitCPM, UnitCPM)
				if d.Message != "" {
					d.Line = ln
					sr.Diagnostics = append(sr.Diagnostics, d)
				} else {
					seg.Rate = r
				}
			}
			if target != "" {
				r, d := asRepr(target, UnitC, UnitC)
				if d.Message != "" {
					d.Line = ln
					sr.Diagnostics = append(sr.Diagnostics, d)
				} else {
					seg.Target = r
				}
			}
			fp.Segments = append(fp.Segments, seg)
		case "isothermal":
			if len(fields) < 2 {
				sr.Diagnostics = append(sr.Diagnostics, errAt(ln, model.KindFurnace,
					"row-shape", "isothermal needs a duration"))
				continue
			}
			r, d := asRepr(fields[1], UnitS, UnitS)
			seg := model.FurnaceSegment{ID: fmt.Sprintf("fs_%06d", len(fp.Segments)+1),
				Line: ln, Kind: "isothermal", Duration: r}
			if d.Message != "" {
				d.Line = ln
				sr.Diagnostics = append(sr.Diagnostics, d)
			}
			fp.Segments = append(fp.Segments, seg)
		default:
			sr.Diagnostics = append(sr.Diagnostics, model.Diagnostic{
				Level: "warning", Code: "unknown-furnace-keyword", Line: ln,
				Section: model.KindFurnace,
				Message: fmt.Sprintf("ignoring unknown furnace keyword %q", fields[0]),
			})
		}
	}
	if !fp.Initial.Parsable {
		sr.Diagnostics = append(sr.Diagnostics, errAt(0, model.KindFurnace,
			"missing-initial", "furnace program needs an initial temperature"))
	}
	return fp
}

func rampPair(fields []string) (rate, target string, ok bool) {
	for i, f := range fields {
		if strings.EqualFold(f, "to") && i > 0 && i+1 < len(fields) {
			return fields[i-1], fields[i+1], true
		}
	}
	return "", "", false
}

// parseGas expects: <valve_time> N2->air delay=<seconds>s
// e.g. 1200s N2 -> air delay 45s ; tolerant forms documented in the case pack.
func parseGas(body []string, sr *SectionResult) []model.GasEvent {
	var out []model.GasEvent
	for idx, line := range body {
		ln := idx + 1
		ge := model.GasEvent{ID: fmt.Sprintf("ge_%06d", idx+1), Line: ln}
		tok := splitCells(line)
		if len(tok) < 3 {
			sr.Diagnostics = append(sr.Diagnostics, errAt(ln, model.KindGas, "row-shape",
				"gas event needs '<time> <from> -> <to> [delay=<ds>]'"))
			out = append(out, ge)
			continue
		}
		if r, d := asRepr(tok[0], UnitS, UnitS); d.Message != "" {
			d.Line = ln
			sr.Diagnostics = append(sr.Diagnostics, d)
		} else {
			ge.ValveTime = r
		}
		arrow := -1
		for i, t := range tok {
			if t == "->" || t == "→" {
				arrow = i
				break
			}
		}
		if arrow < 1 || arrow+1 >= len(tok) {
			sr.Diagnostics = append(sr.Diagnostics, errAt(ln, model.KindGas, "missing-arrow",
				"gas event needs 'from -> to'"))
		} else {
			ge.FromGas, ge.ToGas = tok[arrow-1], tok[arrow+1]
		}
		var delayCell string
		for i, t := range tok {
			if strings.HasPrefix(t, "delay=") && i+1 < len(tok) {
				delayCell = strings.TrimPrefix(t, "delay=") + tok[i+1]
				break
			}
			if strings.HasPrefix(t, "delay") && i+1 < len(tok) {
				delayCell = tok[i+1]
				break
			}
		}
		if delayCell == "" {
			ge.DelayS = model.Repr{Value: 0, Unit: UnitS, Parsable: true, Raw: "0"}
		} else if r, d := asRepr(delayCell, UnitS, UnitS); d.Message != "" {
			d.Line = ln
			sr.Diagnostics = append(sr.Diagnostics, d)
		} else {
			ge.DelayS = r
		}
		out = append(out, ge)
	}
	if len(out) == 0 {
		sr.Diagnostics = append(sr.Diagnostics, errAt(0, model.KindGas,
			"empty-gas", "gas section contains no events"))
	}
	return out
}

// parseMass expects: value=<x>mg source=<s> note=<text>
func parseMass(body []string, sr *SectionResult) model.InitialMass {
	var im model.InitialMass
	if len(body) == 0 {
		sr.Diagnostics = append(sr.Diagnostics, errAt(0, model.KindMass,
			"empty-mass", "mass section is empty"))
		return im
	}
	if len(body) > 1 {
		sr.Diagnostics = append(sr.Diagnostics, model.Diagnostic{
			Level: "error", Code: "multiple-mass-rows", Section: model.KindMass,
			Message: "initial mass section must have exactly one row",
		})
	}
	tok := splitCells(body[0])
	rest := strings.Join(tok, " ")
	for i, t := range tok {
		switch {
		case strings.HasPrefix(t, "value=") && i+1 < len(tok):
			cell := strings.TrimPrefix(t, "value=") + tok[i+1]
			r, d := asRepr(cell, UnitMg, UnitMg)
			if d.Message != "" {
				d.Line = 1
				sr.Diagnostics = append(sr.Diagnostics, d)
			}
			im.Value = r
		case strings.HasPrefix(t, "source="):
			im.Source = strings.TrimPrefix(t, "source=")
		case strings.HasPrefix(t, "note="):
			im.Note = strings.TrimPrefix(t, "note=")
		}
	}
	if !im.Value.Parsable {
		sr.Diagnostics = append(sr.Diagnostics, errAt(1, model.KindMass,
			"missing-value", "initial mass needs value=<x>mg"))
	}
	_ = rest
	return im
}

// parseParams accepts key=value pairs, one per line.
func parseParams(body []string, sr *SectionResult) model.Params {
	p := DefaultParams()
	for idx, line := range body {
		ln := idx + 1
		tok := splitCells(line)
		if len(tok) == 0 {
			continue
		}
		kv := strings.SplitN(tok[0], "=", 2)
		if len(kv) != 2 {
			sr.Diagnostics = append(sr.Diagnostics, model.Diagnostic{
				Level: "error", Code: "bad-param", Line: ln, Section: model.KindParams,
				Message: fmt.Sprintf("expected key=value, got %q", tok[0]),
			})
			continue
		}
		key, raw := kv[0], kv[1]
		if key == "drift_source" {
			if raw != "anchors" && raw != "blank" {
				sr.Diagnostics = append(sr.Diagnostics, model.Diagnostic{
					Level: "error", Code: "bad-param", Line: ln, Section: model.KindParams,
					Message: fmt.Sprintf("drift_source must be anchors|blank, got %q", raw),
				})
			} else {
				p.DriftSource = raw
			}
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			sr.Diagnostics = append(sr.Diagnostics, model.Diagnostic{
				Level: "error", Code: "bad-param", Line: ln, Section: model.KindParams,
				Message: fmt.Sprintf("parameter %s not numeric: %q", key, raw),
			})
			continue
		}
		switch key {
		case "smoothing_window":
			p.SmoothingWindow = int(v)
		case "prominence":
			p.Prominence = v
		case "min_separation_s":
			p.MinSeparationS = v
		case "gap_factor":
			p.GapFactor = v
		case "noise_z_sigma":
			p.NoiseZSigma = v
		case "drift_source":
			sr.Diagnostics = append(sr.Diagnostics, model.Diagnostic{
				Level: "warning", Code: "param-type", Line: ln, Section: model.KindParams,
				Message: "drift_source is string-valued; ignored in numeric parser",
			})
		default:
			sr.Diagnostics = append(sr.Diagnostics, model.Diagnostic{
				Level: "warning", Code: "unknown-param", Line: ln, Section: model.KindParams,
				Message: fmt.Sprintf("unknown parameter %q retained but unused", key),
			})
		}
	}
	return p
}

// DefaultParams returns the default algorithm parameter set.
func DefaultParams() model.Params {
	return model.Params{
		SmoothingWindow: 5,
		Prominence:      0.02,
		MinSeparationS:  30,
		GapFactor:       5,
		DriftSource:     "anchors",
		NoiseZSigma:     4,
	}
}
