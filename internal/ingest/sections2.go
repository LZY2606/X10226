package ingest

import (
	"fmt"
	"strings"

	"tgareview/internal/domain"
)

// [program] rows: ramp <to°C> <rate °C/min> ; hold <seconds> ; start <°C>
func parseProgram(lines []string, start int, sr *SectionResult) {
	d := dataLines(lines)
	if len(d) == 0 {
		sr.Fatal = true
		sr.Diagnostics = append(sr.Diagnostics, diag("error", "[program]", "E_EMPTY", "empty program"))
		return
	}
	var segs []domain.ProgramSegment
	for _, row := range d {
		cells := cleanCells(splitRow(row.s))
		loc := fmt.Sprintf("line %d", start+row.n)
		if len(cells) < 2 {
			sr.Fatal = true
			sr.Diagnostics = append(sr.Diagnostics, diag("error", loc, "E_COLUMNS",
				"program row needs at least kind + parameter"))
			return
		}
		seg := domain.ProgramSegment{Line: start + row.n, Kind: strings.ToLower(cells[0])}
		switch seg.Kind {
		case "start":
			seg.StartTempRaw = cells[1]
			if !domain.NormalizeTemp(cells[1], "°C").Valid {
				sr.Diagnostics = append(sr.Diagnostics, diag("error", loc, "E_UNIT", "bad start temp (raw retained)"))
			}
		case "ramp":
			if len(cells) < 3 {
				sr.Fatal = true
				sr.Diagnostics = append(sr.Diagnostics, diag("error", loc, "E_COLUMNS",
					"ramp needs target °C and rate °C/min"))
				return
			}
			seg.ToTempRaw = cells[1]
			seg.RateRaw = cells[2]
			if !domain.NormalizeTemp(cells[1], "°C").Valid {
				sr.Diagnostics = append(sr.Diagnostics, diag("error", loc, "E_UNIT", "bad ramp target (raw retained)"))
			}
		case "hold":
			seg.HoldRaw = cells[1]
			if !domain.NormalizeTime(cells[1], "s").Valid {
				sr.Diagnostics = append(sr.Diagnostics, diag("error", loc, "E_UNIT", "bad hold time (raw retained)"))
			}
		default:
			sr.Fatal = true
			sr.Diagnostics = append(sr.Diagnostics, diag("error", loc, "E_KIND",
				"program step must be start, ramp or hold, got: "+seg.Kind))
			return
		}
		segs = append(segs, seg)
	}
	sr.Source.Program = segs
}

// [gas] rows: <time> <s|min>, <from gas>, <to gas>
func parseGas(lines []string, start int, sr *SectionResult) {
	d := dataLines(lines)
	if len(d) == 0 {
		sr.Fatal = true
		sr.Diagnostics = append(sr.Diagnostics, diag("error", "[gas]", "E_EMPTY", "empty gas section"))
		return
	}
	var events []domain.GasEvent
	for _, row := range d {
		cells := cleanCells(splitRow(row.s))
		loc := fmt.Sprintf("line %d", start+row.n)
		if len(cells) < 3 {
			sr.Fatal = true
			sr.Diagnostics = append(sr.Diagnostics, diag("error", loc, "E_COLUMNS",
				"gas row needs time, from gas, to gas"))
			return
		}
		// time may be "<value> <unit>" in one cell or value then unit cell.
		timeRaw, unit := splitTimeUnit(cells[0])
		if !domain.NormalizeTime(timeRaw, unit).Valid {
			sr.Diagnostics = append(sr.Diagnostics, diag("error", loc, "E_UNIT",
				"gas time unconvertible (raw retained)"))
		}
		events = append(events, domain.GasEvent{
			Line: start + row.n, TimeRaw: timeRaw + " " + unit,
			FromGas: cells[1], ToGas: cells[2],
		})
	}
	sr.Source.GasEvents = events
}

func splitTimeUnit(cell string) (string, string) {
	parts := strings.Fields(cell)
	if len(parts) == 1 {
		return parts[0], "s"
	}
	return parts[0], parts[1]
}

// [initial_mass] rows: mass, unit, method[, reason]
func parseInitial(lines []string, start int, sr *SectionResult) {
	d := dataLines(lines)
	if len(d) == 0 {
		sr.Fatal = true
		sr.Diagnostics = append(sr.Diagnostics, diag("error", "[initial_mass]", "E_EMPTY",
			"empty initial_mass section"))
		return
	}
	// Accept header + value row, or direct value row.
	var row struct{ n int; s string }
	row = d[0]
	if strings.Contains(strings.ToLower(row.s), "mass") && len(d) >= 2 {
		row = d[1]
	}
	cells := cleanCells(splitRow(row.s))
	loc := fmt.Sprintf("line %d", start+row.n)
	if len(cells) < 3 {
		sr.Fatal = true
		sr.Diagnostics = append(sr.Diagnostics, diag("error", loc, "E_COLUMNS",
			"initial_mass needs mass, unit, method"))
		return
	}
	im := &domain.InitialMass{Line: start + row.n, MassRaw: cells[0], Unit: cells[1], Method: cells[2]}
	if len(cells) > 3 {
		im.Reason = strings.Join(cells[3:], ", ")
	}
	if !domain.NormalizeMass(im.MassRaw, im.Unit).Valid {
		sr.Diagnostics = append(sr.Diagnostics, diag("error", loc, "E_UNIT",
			"initial mass unconvertible (raw retained)"))
	}
	sr.Source.InitialMass = im
}
