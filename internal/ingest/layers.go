package ingest

import (
	"strconv"
	"strings"

	"tga-review/internal/model"
)

// Parse dispatches a document to the layer-specific parser.
func Parse(kind model.SourceKind, text string) *ParsedLayer {
	p := &ParsedLayer{Kind: kind, Params: map[string]string{}}
	lines := splitLines(text)
	recs, nums := nonEmptyRecords(lines)
	switch kind {
	case model.KindSamples:
		parseSamples(p, recs, nums)
	case model.KindDrift:
		parseDrift(p, recs, nums)
	case model.KindFurnace:
		parseFurnace(p, recs, nums)
	case model.KindGas:
		parseGas(p, recs, nums)
	case model.KindBlank:
		parseBlank(p, recs, nums)
	case model.KindInitial:
		parseInitial(p, recs, nums)
	case model.KindParams:
		parseParams(p, recs, nums)
	default:
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Reason: "fatal: unknown layer kind " + string(kind)})
	}
	return p
}

func parseSamples(p *ParsedLayer, recs [][]string, nums []int) {
	if len(recs) == 0 || recs[0][0] != "units" {
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: 1, Field: "kind", Reason: "fatal: first record must be units,min,degC,mg"})
		return
	}
	if d := expectCols(recs[0], 4, nums[0], "units"); d != nil {
		p.Diagnostics = append(p.Diagnostics, *d)
		return
	}
	uTime, uTemp, uMass := recs[0][1], recs[0][2], recs[0][3]
	var ptSeq, gapSeq int
	for i := 1; i < len(recs); i++ {
		rec, line := recs[i], nums[i]
		if len(rec) == 0 {
			continue
		}
		switch rec[0] {
		case "point":
			if d := expectCols(rec, 4, line, "point"); d != nil {
				p.Diagnostics = append(p.Diagnostics, *d)
				return
			}
			ptSeq++
			sp := model.SamplePoint{Seq: ptSeq, UnitTime: uTime, UnitTemp: uTemp, UnitMass: uMass,
				TimeRaw: rec[1], TempRaw: rec[2], MassRaw: rec[3]}
			if v, ok := num(rec[1]); ok {
				sp.TimeS, sp.TimeP = ptr(v), true
			} else {
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "time", Value: rec[1], Reason: "unconvertible value kept at origin"})
			}
			if v, ok := num(rec[2]); ok {
				sp.TempS, sp.TempP = ptr(v), true
			} else {
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "temp", Value: rec[2], Reason: "unconvertible value kept at origin"})
			}
			if v, ok := num(rec[3]); ok {
				sp.MassS, sp.MassP = ptr(v), true
			} else {
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "mass", Value: rec[3], Reason: "unconvertible value kept at origin"})
			}
			p.Samples = append(p.Samples, sp)
		case "gap":
			if d := expectCols(rec, 3, line, "gap"); d != nil {
				p.Diagnostics = append(p.Diagnostics, *d)
				return
			}
			from, ok1 := num(rec[1])
			to, ok2 := num(rec[2])
			if !ok1 || !ok2 {
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "gap", Value: strings.Join(rec, ","), Reason: "fatal: gap bounds must be numeric"})
				return
			}
			gapSeq++
			p.Gaps = append(p.Gaps, model.Gap{Seq: gapSeq, FromS: from, ToS: to, Unit: uTime, Raw: strings.Join(rec, ",")})
		default:
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "kind", Value: rec[0], Reason: "fatal: record must start with point or gap"})
			return
		}
	}
	if len(p.Samples) < 3 {
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Field: "point", Reason: "fatal: need at least 3 sample points"})
	}
}

func parseDrift(p *ParsedLayer, recs [][]string, nums []int) {
	if len(recs) == 0 || recs[0][0] != "units" {
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: 1, Field: "kind", Reason: "fatal: first record must be units,min,mg"})
		return
	}
	if d := expectCols(recs[0], 3, nums[0], "units"); d != nil {
		p.Diagnostics = append(p.Diagnostics, *d)
		return
	}
	uTime, uDrift := recs[0][1], recs[0][2]
	for i := 1; i < len(recs); i++ {
		rec, line := recs[i], nums[i]
		if d := expectCols(rec, 3, line, "point"); d != nil {
			p.Diagnostics = append(p.Diagnostics, *d)
			return
		}
		if rec[0] != "point" {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "kind", Value: rec[0], Reason: "fatal: drift rows must start with point"})
			return
		}
		seq := i
		dp := model.DriftPoint{Seq: seq, UnitTime: uTime, UnitDrift: uDrift, TimeRaw: rec[1], DriftRaw: rec[2]}
		if v, ok := num(rec[1]); ok {
			dp.TimeS, dp.TimeP = ptr(v), true
		} else {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "time", Value: rec[1], Reason: "unconvertible value kept at origin"})
		}
		if v, ok := num(rec[2]); ok {
			dp.DriftS, dp.DriftP = ptr(v), true
		} else {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "drift", Value: rec[2], Reason: "unconvertible value kept at origin"})
		}
		p.Drift = append(p.Drift, dp)
	}
}

func parseFurnace(p *ParsedLayer, recs [][]string, nums []int) {
	if len(recs) == 0 || recs[0][0] != "units" {
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: 1, Field: "kind", Reason: "fatal: first record must be units,min,degC"})
		return
	}
	if d := expectCols(recs[0], 3, nums[0], "units"); d != nil {
		p.Diagnostics = append(p.Diagnostics, *d)
		return
	}
	uTime, uTemp := recs[0][1], recs[0][2]
	for i := 1; i < len(recs); i++ {
		rec, line := recs[i], nums[i]
		if d := expectCols(rec, 4, line, "seg"); d != nil {
			p.Diagnostics = append(p.Diagnostics, *d)
			return
		}
		if rec[0] != "seg" {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "kind", Value: rec[0], Reason: "fatal: furnace rows must start with seg"})
			return
		}
		kind := rec[1]
		if kind != "start" && kind != "ramp" && kind != "hold" {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "seg_kind", Value: kind, Reason: "fatal: segment kind must be start|ramp|hold"})
			return
		}
		seg := model.FurnaceSeg{Seq: i, Kind: kind, UnitTime: uTime, UnitTemp: uTemp, TimeRaw: rec[2], TempRaw: rec[3]}
		if v, ok := num(rec[2]); ok {
			seg.TimeS, seg.TimeP = ptr(v), true
		}
		if v, ok := num(rec[3]); ok {
			seg.TempS, seg.TempP = ptr(v), true
		}
		p.Furnace = append(p.Furnace, seg)
	}
}

func parseGas(p *ParsedLayer, recs [][]string, nums []int) {
	if len(recs) == 0 || recs[0][0] != "units" {
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: 1, Field: "kind", Reason: "fatal: first record must be units,min"})
		return
	}
	if d := expectCols(recs[0], 2, nums[0], "units"); d != nil {
		p.Diagnostics = append(p.Diagnostics, *d)
		return
	}
	uTime := recs[0][1]
	for i := 1; i < len(recs); i++ {
		rec, line := recs[i], nums[i]
		if d := expectCols(rec, 4, line, "event"); d != nil {
			p.Diagnostics = append(p.Diagnostics, *d)
			return
		}
		if rec[0] != "event" {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "kind", Value: rec[0], Reason: "fatal: gas rows must start with event"})
			return
		}
		ev := model.GasEvent{Seq: i, UnitTime: uTime, TimeRaw: rec[1], GasRaw: rec[2], ActionRaw: rec[3],
			Gas: rec[2], Action: rec[3]}
		if v, ok := num(rec[1]); ok {
			ev.TimeS, ev.TimeP = ptr(v), true
		}
		p.Gas = append(p.Gas, ev)
	}
}

func parseBlank(p *ParsedLayer, recs [][]string, nums []int) {
	if len(recs) == 0 || recs[0][0] != "units" {
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: 1, Field: "kind", Reason: "fatal: first record must be units,min,mg"})
		return
	}
	if d := expectCols(recs[0], 3, nums[0], "units"); d != nil {
		p.Diagnostics = append(p.Diagnostics, *d)
		return
	}
	uTime, uMass := recs[0][1], recs[0][2]
	for i := 1; i < len(recs); i++ {
		rec, line := recs[i], nums[i]
		if d := expectCols(rec, 3, line, "point"); d != nil {
			p.Diagnostics = append(p.Diagnostics, *d)
			return
		}
		if rec[0] != "point" {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "kind", Value: rec[0], Reason: "fatal: blank rows must start with point"})
			return
		}
		bp := model.BlankPoint{Seq: i, UnitTime: uTime, UnitMass: uMass, TimeRaw: rec[1], MassRaw: rec[2]}
		if v, ok := num(rec[1]); ok {
			bp.TimeS, bp.TimeP = ptr(v), true
		}
		if v, ok := num(rec[2]); ok {
			bp.MassS, bp.MassP = ptr(v), true
		}
		p.Blank = append(p.Blank, bp)
	}
}

func parseInitial(p *ParsedLayer, recs [][]string, nums []int) {
	if len(recs) == 0 || recs[0][0] != "units" {
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: 1, Field: "kind", Reason: "fatal: first record must be units,mg"})
		return
	}
	if d := expectCols(recs[0], 2, nums[0], "units"); d != nil {
		p.Diagnostics = append(p.Diagnostics, *d)
		return
	}
	unit := recs[0][1]
	for i := 1; i < len(recs); i++ {
		rec, line := recs[i], nums[i]
		if d := expectCols(rec, 3, line, "mass"); d != nil {
			p.Diagnostics = append(p.Diagnostics, *d)
			return
		}
		if rec[0] != "mass" {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "kind", Value: rec[0], Reason: "fatal: initial rows must start with mass"})
			return
		}
		im := model.InitialMass{Seq: i, Unit: unit, Basis: rec[2], MassRaw: rec[1]}
		if v, ok := num(rec[1]); ok {
			im.MassS, im.MassP = ptr(v), true
		} else {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: line, Field: "mass", Value: rec[1], Reason: "unconvertible value kept at origin"})
		}
		p.Initial = append(p.Initial, im)
	}
	if len(p.Initial) == 0 {
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Field: "mass", Reason: "fatal: no initial mass declaration"})
	}
}

func parseParams(p *ParsedLayer, recs [][]string, nums []int) {
	for i, rec := range recs {
		if d := expectCols(rec, 2, nums[i], "param"); d != nil {
			p.Diagnostics = append(p.Diagnostics, *d)
			return
		}
		key := strings.TrimSpace(rec[0])
		if key == "" {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: nums[i], Field: "key", Reason: "fatal: empty parameter key"})
			return
		}
		if _, err := strconv.ParseFloat(strings.TrimSpace(rec[1]), 64); err != nil {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Line: nums[i], Field: key, Value: rec[1], Reason: "fatal: parameter value must be numeric"})
			return
		}
		p.Params[key] = rec[1]
	}
	if len(p.Params) == 0 {
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Field: "params", Reason: "fatal: empty parameter set"})
	}
}
