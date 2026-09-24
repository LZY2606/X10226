package pipeline

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"tga/internal/domain"
)

// parseError marks a whole layer as rejected; row-level problems are diagnostics.
type parseError struct{ msg string }

func (e *parseError) Error() string { return e.msg }

const tsLayout = "2006-01-02T15:04:05"

// ImportRaw parses the six import layers out of a tagged, line-oriented text
// format. Unparseable rows stay as diagnostics and, when the raw value cannot
// be normalised, a zero/valid=false Q keeps the original text in place.
//
// Section tags:
//
//	## samples            t_s,T_C,m_mg
//	## drift              t_s,offset_mg
//	## program            Tstart_C,Tend_C,rate_Cmin,dur_s
//	## gas                t_s,from->to
//	## blank              t_s,m_mg
//	## initial            v,m_mg,note
func ImportRaw(text string) (*domain.ImportedLayers, error) {
	out := &domain.ImportedLayers{}
	section := ""
	var samplesSeen int
	gotAny := false
	for lineno, rawline := range strings.Split(text, "\n") {
		lineno++
		line := strings.TrimSpace(rawline)
		if line == "" || strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "##") {
			continue
		}
		if strings.HasPrefix(line, "##") {
			switch strings.TrimSpace(strings.TrimPrefix(line, "##")) {
			case "samples", "raw":
				section = domain.KindSamples
			case "drift":
				section = domain.KindDrift
			case "program":
				section = domain.KindProgram
			case "gas":
				section = domain.KindGas
			case "blank":
				section = domain.KindBlank
			case "initial":
				section = domain.KindInitialM
			default:
				out.Diags = append(out.Diags, domain.Diagnostics{Line: lineno, Code: "unknown_section", Message: "unknown section tag: " + line, Raw: rawline})
				section = ""
			}
			continue
		}
		gotAny = true
		switch section {
		case domain.KindSamples:
			fields := strings.Split(line, ",")
			if len(fields) < 3 {
				out.Diags = append(out.Diags, bad(lineno, section, "samples_need_3_fields", line))
				continue
			}
			pt := domain.SamplePoint{Index: samplesSeen, Line: lineno, Kept: true}
			pt.Time = parseTimeQ(strings.TrimSpace(fields[0]), lineno, "time")
			pt.Temp = parseTempQ(strings.TrimSpace(fields[1]), lineno)
			pt.Mass = parseMassQ(strings.TrimSpace(fields[2]), lineno)
			if !pt.Time.Valid || !pt.Temp.Valid || !pt.Mass.Valid {
				pt.Kept = false
				pt.Diag = "row kept but not normalised"
				out.Diags = append(out.Diags, domain.Diagnostics{Line: lineno, Layer: section, Code: "sample_row_not_normalised", Message: "at least one field failed conversion", Raw: rawline})
			}
			out.Samples = append(out.Samples, pt)
			samplesSeen++
		case domain.KindDrift:
			fields := strings.Split(line, ",")
			if len(fields) < 2 {
				out.Diags = append(out.Diags, bad(lineno, section, "drift_need_2_fields", line))
				continue
			}
			out.Drift = append(out.Drift, domain.DriftPoint{
				Line:   lineno,
				Time:   parseTimeQ(strings.TrimSpace(fields[0]), lineno, "time"),
				Offset: parseMassQ(strings.TrimSpace(fields[1]), lineno),
			})
		case domain.KindProgram:
			fields := strings.Split(line, ",")
			if len(fields) < 4 {
				out.Diags = append(out.Diags, bad(lineno, section, "program_need_4_fields", line))
				continue
			}
			out.Program = append(out.Program, domain.ProgramSegment{
				Line:      lineno,
				StartTemp: parseTempQ(strings.TrimSpace(fields[0]), lineno),
				EndTemp:   parseTempQ(strings.TrimSpace(fields[1]), lineno),
				Rate:      parseRateQ(strings.TrimSpace(fields[2]), lineno),
				Duration:  parseDurationQ(strings.TrimSpace(fields[3]), lineno),
			})
		case domain.KindGas:
			fields := strings.SplitN(line, ",", 2)
			if len(fields) < 2 || !strings.Contains(fields[1], "->") {
				out.Diags = append(out.Diags, bad(lineno, section, "gas_need_t_and_arrow", line))
				continue
			}
			arrow := strings.SplitN(fields[1], "->", 2)
			out.Gas = append(out.Gas, domain.GasEvent{
				Line:    lineno,
				Time:    parseTimeQ(strings.TrimSpace(fields[0]), lineno, "time"),
				FromGas: strings.TrimSpace(arrow[0]),
				ToGas:   strings.TrimSpace(arrow[1]),
				Raw:     rawline,
			})
		case domain.KindBlank:
			fields := strings.Split(line, ",")
			if len(fields) < 2 {
				out.Diags = append(out.Diags, bad(lineno, section, "blank_need_2_fields", line))
				continue
			}
			out.Blank = append(out.Blank, domain.BlankPoint{
				Line: lineno,
				Time: parseTimeQ(strings.TrimSpace(fields[0]), lineno, "time"),
				Mass: parseMassQ(strings.TrimSpace(fields[1]), lineno),
			})
		case domain.KindInitialM:
			fields := strings.SplitN(line, ",", 3)
			if len(fields) < 2 {
				out.Diags = append(out.Diags, bad(lineno, section, "initial_need_v_and_mass", line))
				continue
			}
			v, err := strconv.Atoi(strings.TrimSpace(fields[0]))
			note := ""
			if len(fields) == 3 {
				note = strings.TrimSpace(fields[2])
			}
			mq := parseMassQ(strings.TrimSpace(fields[1]), lineno)
			if err != nil {
				mq.Valid = false
				mq.Diag = "initial mass version not an integer"
				out.Diags = append(out.Diags, domain.Diagnostics{Line: lineno, Layer: section, Code: "initial_version_bad", Message: err.Error(), Raw: rawline})
			}
			out.InitialM = append(out.InitialM, domain.InitialMassLayer{Line: lineno, Version: v, Mass: mq, Note: note})
		default:
			out.Diags = append(out.Diags, domain.Diagnostics{Line: lineno, Code: "data_outside_section", Message: "line outside any ## section", Raw: rawline})
		}
	}
	if !gotAny {
		return nil, &parseError{msg: "import contained no data rows"}
	}
	if len(out.Samples) == 0 {
		return nil, &parseError{msg: "import failed entirely: no sample rows (hard failure, nothing committed)"}
	}
	// Mark the highest-numbered initial mass the active one.
	best := -1
	for i, im := range out.InitialM {
		if im.Mass.Valid && (best == -1 || im.Version > out.InitialM[best].Version) {
			best = i
		}
	}
	for i := range out.InitialM {
		out.InitialM[i].Active = i == best
	}
	return out, nil
}

func bad(line int, layer, code, raw string) domain.Diagnostics {
	return domain.Diagnostics{Line: line, Layer: layer, Code: code, Message: "malformed row", Raw: raw}
}

// parseTimeQ accepts an elapsed seconds/minutes number or an ISO wall-clock
// timestamp. Wall clock cannot be placed on the elapsed furnace axis without a
// reference, so it is kept with basis wall_clock and flagged.
func parseTimeQ(s string, line int, field string) domain.Q {
	q := domain.Q{Raw: s, Line: line}
	if v, unit, ok := numberUnit(s); ok {
		switch unit {
		case "", domain.UnitSecond:
			q.Value, q.Unit, q.Basis, q.Valid = v, domain.UnitSecond, domain.BasisFurnaceTime, true
		case domain.UnitMinute:
			q.Value, q.Unit, q.Basis, q.Valid = v*60, domain.UnitSecond, domain.BasisFurnaceTime, true
		default:
			q.Diag = "unsupported time unit: " + unit
		}
		return q
	}
	if _, err := time.Parse(tsLayout, s); err == nil {
		q.Unit, q.Basis = domain.BasisWallClock, domain.BasisWallClock
		q.Diag = "wall clock timestamp needs a reference epoch for furnace axis"
		return q
	}
	q.Diag = fmt.Sprintf("%s not parseable", field)
	return q
}

func parseTempQ(s string, line int) domain.Q {
	q := domain.Q{Raw: s, Line: line}
	v, unit, ok := numberUnit(s)
	if !ok {
		q.Diag = "temperature not parseable"
		return q
	}
	switch unit {
	case "", "C":
		q.Value, q.Unit, q.Basis, q.Valid = v, domain.UnitCelsius, domain.BasisTemperatureMeas, true
	case "K":
		q.Value, q.Unit, q.Basis, q.Valid = v-273.15, domain.UnitCelsius, domain.BasisTemperatureMeas, true
	default:
		q.Diag = "unsupported temperature unit: " + unit
	}
	return q
}

func parseMassQ(s string, line int) domain.Q {
	q := domain.Q{Raw: s, Line: line}
	v, unit, ok := numberUnit(s)
	if !ok {
		q.Diag = "mass not parseable"
		return q
	}
	switch unit {
	case "", "mg":
		q.Value, q.Unit, q.Valid = v, domain.UnitMilligram, true
	case domain.UnitGram, "g":
		q.Value, q.Unit, q.Valid = v*1000, domain.UnitMilligram, true
	default:
		q.Diag = "unsupported mass unit: " + unit
	}
	return q
}

func parseRateQ(s string, line int) domain.Q {
	q := domain.Q{Raw: s, Line: line}
	v, unit, ok := numberUnit(s)
	if !ok {
		q.Diag = "rate not parseable"
		return q
	}
	switch unit {
	case "", "C/min", "degC/min":
		q.Value, q.Unit, q.Valid = v, "degC/min", true
	default:
		q.Diag = "unsupported rate unit: " + unit
	}
	return q
}

func parseDurationQ(s string, line int) domain.Q {
	q := domain.Q{Raw: s, Line: line}
	v, unit, ok := numberUnit(s)
	if !ok {
		q.Diag = "duration not parseable"
		return q
	}
	switch unit {
	case "", domain.UnitSecond:
		q.Value, q.Unit, q.Valid = v, domain.UnitSecond, true
	case domain.UnitMinute:
		q.Value, q.Unit, q.Valid = v*60, domain.UnitSecond, true
	default:
		q.Diag = "unsupported duration unit: " + unit
	}
	return q
}

// numberUnit splits "12.3mg" / "12.3" / "0.4 g" into number and unit.
func numberUnit(s string) (float64, string, bool) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && (isDigitByte(s[i]) || s[i] == '.' || s[i] == '-' || s[i] == '+' || s[i] == 'e' || s[i] == 'E') {
		i++
	}
	num := s[:i]
	unit := strings.TrimSpace(s[i:])
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, "", false
	}
	return v, unit, true
}

func isDigitByte(b byte) bool { return b >= '0' && b <= '9' }
