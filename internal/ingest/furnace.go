package ingest

import (
	"strings"

	"tga-archive/internal/domain"
)

// ParseFurnace accepts hold/ramp lines:
//
//	ramp 30 C -> 600 C over 0 min -> 30 min
//	hold 600 C 30 min -> 45 min
//
// and also "start: 30 C at 0 min" as the origin line.
func ParseFurnace(text string) ([]domain.FurnaceSegment, []domain.Diagnostic, bool) {
	var segs []domain.FurnaceSegment
	var diags []domain.Diagnostic
	var curTemp, curTime domain.Measure
	curTemp = domain.Measure{Valid: false}
	curTime = domain.Measure{Valid: false}
	seenStart := false

	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "start") {
			if _, rest, ok := strings.Cut(line, ":"); ok {
				tempTok, timeTok, found := splitAt(rest, "at")
				if !found {
					diags = append(diags, badFurnaceLine(line, "start needs '<temp> at <time>'"))
					continue
				}
				curTemp = domain.ParseMeasure(tempTok, domain.UnitKelvin)
				curTime = domain.ParseMeasure(timeTok, domain.UnitSecond)
				if !curTemp.Valid {
					diags = append(diags, badFurnaceLine(line, "start temperature: "+curTemp.Note))
				}
				if !curTime.Valid {
					diags = append(diags, badFurnaceLine(line, "start time: "+curTime.Note))
				}
				if curTemp.Valid && curTime.Valid {
					seenStart = true
				}
			}
			continue
		}
		if !seenStart {
			diags = append(diags, badFurnaceLine(line, "furnace program must declare 'start:' first"))
			continue
		}
		var kind, body string
		switch {
		case strings.HasPrefix(lower, "ramp"):
			kind, body = "ramp", strings.TrimSpace(line[4:])
		case strings.HasPrefix(lower, "hold"):
			kind, body = "hold", strings.TrimSpace(line[4:])
		default:
			diags = append(diags, badFurnaceLine(line, "expected ramp/hold/start"))
			continue
		}
		left, right, found := splitAt(body, "over")
		if !found {
			diags = append(diags, badFurnaceLine(line, "missing 'over' interval"))
			continue
		}
		var endTemp domain.Measure
		if kind == "ramp" {
			fromTok, toTok, f2 := splitAt(left, "->")
			if !f2 {
				diags = append(diags, badFurnaceLine(line, "ramp needs '<temp> -> <temp>'"))
				continue
			}
			startTemp := domain.ParseMeasure(fromTok, domain.UnitKelvin)
			endTemp = domain.ParseMeasure(toTok, domain.UnitKelvin)
			if !startTemp.Valid || !endTemp.Valid {
				diags = append(diags, badFurnaceLine(line, "ramp temperatures unconvertible"))
				continue
			}
			if approx(startTemp.Value, curTemp.Value) {
				startTemp = curTemp
			} else if !approx(startTemp.Value, curTemp.Value) {
				diags = append(diags, domain.Diagnostic{Severity: domain.SevWarning, Code: "furnace_jump", Layer: domain.LayerFurnace, Fragment: line, Message: "ramp start does not match previous end"})
			}
			curTemp = endTemp
		} else {
			toks := strings.Fields(left)
			if len(toks) < 2 {
				diags = append(diags, badFurnaceLine(line, "hold needs '<temp> <start>'"))
				continue
			}
			endTemp = domain.ParseMeasure(strings.Join(toks[:2], " "), domain.UnitKelvin)
			if !endTemp.Valid || !approx(endTemp.Value, curTemp.Value) {
				diags = append(diags, domain.Diagnostic{Severity: domain.SevWarning, Code: "furnace_jump", Layer: domain.LayerFurnace, Fragment: line, Message: "hold temperature differs from previous segment"})
			}
			curTemp = endTemp
		}
		fromTok, toTok, f3 := splitAt(right, "->")
		if !f3 {
			diags = append(diags, badFurnaceLine(line, "interval needs '<start> -> <end>'"))
			continue
		}
		startTime := domain.ParseMeasure(fromTok, domain.UnitSecond)
		endTime := domain.ParseMeasure(toTok, domain.UnitSecond)
		if !startTime.Valid || !endTime.Valid {
			diags = append(diags, badFurnaceLine(line, "segment times unconvertible"))
			continue
		}
		if approx(startTime.Value, curTime.Value) {
			startTime = curTime
		}
		seg := domain.FurnaceSegment{Kind: kind, StartTemp: tempAt(kind, curTemp, endTemp), EndTemp: endTemp, StartTime: startTime, EndTime: endTime, Raw: line}
		if kind == "ramp" && endTime.Value > startTime.Value {
			seg.Rate = domain.Measure{Value: (endTemp.Value - seg.StartTemp.Value) / (endTime.Value - startTime.Value), Unit: domain.UnitKPerS, Valid: true}
		}
		segs = append(segs, seg)
		curTemp = endTemp
		curTime = endTime
	}
	if len(segs) == 0 {
		diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "no_segments", Layer: domain.LayerFurnace, Message: "no furnace segments parsed"})
		return nil, diags, false
	}
	return segs, diags, true
}

func tempAt(kind string, cur, end domain.Measure) domain.Measure {
	if kind == "hold" {
		return end
	}
	return cur
}

func splitAt(s, sep string) (string, string, bool) {
	i := strings.Index(strings.ToLower(s), sep)
	if i < 0 {
		return s, "", false
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(sep):]), true
}

func approx(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 0.01
}

func badFurnaceLine(line, msg string) domain.Diagnostic {
	return domain.Diagnostic{Severity: domain.SevError, Code: "bad_segment", Layer: domain.LayerFurnace, Fragment: line, Message: msg}
}
