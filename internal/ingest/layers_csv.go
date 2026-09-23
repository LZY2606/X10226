package ingest

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"tga-review/internal/domain"
)

func parseCSV(layer domain.SourceLayer, text string) Result {
	if strings.TrimSpace(text) == "" {
		return fatal(layer, "empty payload")
	}
	header, records, err := readCSV(text)
	if err != nil {
		return fatal(layer, err.Error())
	}
	switch layer {
	case domain.LayerSamples:
		return parseSamplesRows(layer, header, records)
	case domain.LayerDrift:
		return parseDriftRows(layer, header, records)
	case domain.LayerFurnace:
		return parseFurnaceRows(layer, header, records)
	case domain.LayerValves:
		return parseValvesRows(layer, header, records)
	case domain.LayerBlank:
		return parseBlankRows(layer, header, records)
	case domain.LayerInitMass:
		return parseInitMassRows(layer, header, records)
	}
	return fatal(layer, "unknown layer")
}

// ---- samples ----

func parseSamplesRows(layer domain.SourceLayer, header []string, records [][]string) Result {
	ti := colIndex(header, "t_s", "time_s", "time", "t")
	mi := colIndex(header, "mass_mg", "mass", "m")
	ci := colIndex(header, "temp_c", "sample_temp_c", "temp_c", "temp", "t_c")
	if ti < 0 || mi < 0 {
		return fatal(layer, "samples csv requires columns: t_s, mass_mg (optional temp_c)")
	}
	pts := make([]domain.SamplePoint, 0, len(records))
	var diags []domain.Diag
	salvaged := false
	for ri, rec := range records {
		row := ri + 2
		if allEmpty(rec) {
			continue
		}
		p := domain.SamplePoint{Row: row}
		tQ, d := parseQ(cell(rec, ti), domain.UnitSecond, row, "t_s")
		if !tQ.Valid {
			diags = append(diags, d)
			salvaged = true
			p.Diag = appendDiag(p.Diag, d)
			pts = append(pts, p)
			continue
		}
		p.T = tQ.Value
		mQ, d := parseQ(cell(rec, mi), domain.UnitMilligram, row, "mass_mg")
		p.MassRaw = cell(rec, mi)
		if !mQ.Valid {
			diags = append(diags, d)
			salvaged = true
		}
		p.Mass = mQ
		if ci >= 0 {
			p.TempRaw = cell(rec, ci)
			if cell(rec, ci) != "" {
				cQ, d := parseQ(cell(rec, ci), domain.UnitCelsius, row, "temp_c")
				if !cQ.Valid {
					diags = append(diags, d)
				}
				p.Temp = cQ
			}
		}
		pts = append(pts, p)
	}
	if len(pts) == 0 {
		return fatal(layer, "no sample rows")
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].T < pts[j].T })
	return Result{Layer: layer, Payload: map[string]any{"points": pts}, Diag: diags, Salvaged: salvaged}
}

func allEmpty(rec []string) bool {
	for _, c := range rec {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func appendDiag(s string, d domain.Diag) string {
	if s == "" {
		return fmt.Sprintf("%s: %s", d.Code, d.Message)
	}
	return s + "; " + fmt.Sprintf("%s: %s", d.Code, d.Message)
}

// ---- drift ----

func parseDriftRows(layer domain.SourceLayer, header []string, records [][]string) Result {
	ti := colIndex(header, "t_s", "time_s", "time")
	mi := colIndex(header, "mass_mg", "mass", "m")
	if ti < 0 || mi < 0 {
		return fatal(layer, "drift csv requires columns: t_s, mass_mg")
	}
	pts := make([]domain.DriftPoint, 0, len(records))
	var diags []domain.Diag
	var sx, sy, sxx, sxy float64
	n := 0
	for ri, rec := range records {
		row := ri + 2
		if allEmpty(rec) {
			continue
		}
		tQ, d := parseQ(cell(rec, ti), domain.UnitSecond, row, "t_s")
		mQ, d2 := parseQ(cell(rec, mi), domain.UnitMilligram, row, "mass_mg")
		if !tQ.Valid {
			diags = append(diags, d)
			continue
		}
		if !mQ.Valid {
			diags = append(diags, d2)
		} else {
			sx += tQ.Value
			sy += mQ.Value
			sxx += tQ.Value * tQ.Value
			sxy += tQ.Value * mQ.Value
			n++
		}
		pts = append(pts, domain.DriftPoint{TS: tQ.Value, MassRaw: cell(rec, mi), Mass: mQ})
	}
	if n < 2 {
		return Result{Layer: layer, Fatal: true, Diag: append(diags, domain.Diag{Code: "E_DRIFT", Severity: "error", Message: "drift calibration needs at least 2 valid points"})}
	}
	denom := float64(n)*sxx - sx*sx
	if denom == 0 {
		return Result{Layer: layer, Fatal: true, Diag: []domain.Diag{{Code: "E_DRIFT", Severity: "error", Message: "degenerate drift times"}}}
	}
	slope := (float64(n)*sxy - sx*sy) / denom
	intercept := (sy - slope*sx) / float64(n)
	for i := range pts {
		pts[i].Intercept = intercept
		pts[i].Slope = slope
	}
	return Result{Layer: layer, Payload: map[string]any{
		"points":        pts,
		"intercept_mg":  intercept,
		"slope_mg_per_s": slope,
	}, Diag: diags}
}

// ---- furnace ----

func parseFurnaceRows(layer domain.SourceLayer, header []string, records [][]string) Result {
	fi := colIndex(header, "from_c", "start_c")
	ti := colIndex(header, "to_c", "end_c")
	ri := colIndex(header, "rate_k_per_min", "rate", "rate_kmin")
	di := colIndex(header, "duration_s", "duration")
	gi := colIndex(header, "gas", "atmosphere")
	ki := colIndex(header, "kind", "type")
	if ti < 0 || (ri < 0 && di < 0) {
		return fatal(layer, "furnace csv requires columns: to_c and (rate_k_per_min or duration_s); optional from_c, gas, kind")
	}
	segs := make([]domain.FurnaceSegment, 0, len(records))
	var diags []domain.Diag
	var cursor float64
	var prevTo float64
	haveFrom := false
	for ri2, rec := range records {
		row := ri2 + 2
		if allEmpty(rec) {
			continue
		}
		seg := domain.FurnaceSegment{StartS: cursor}
		if fi >= 0 && cell(rec, fi) != "" {
			q, d := parseQ(cell(rec, fi), domain.UnitCelsius, row, "from_c")
			if !q.Valid {
				diags = append(diags, d)
			}
			seg.FromC = q.Value
			haveFrom = true
		} else if len(segs) > 0 {
			seg.FromC = prevTo
		}
		q, d := parseQ(cell(rec, ti), domain.UnitCelsius, row, "to_c")
		if !q.Valid {
			return Result{Layer: layer, Fatal: true, Diag: append(diags, d)}
		}
		seg.ToC = q.Value
		if !haveFrom {
			seg.FromC = seg.ToC
		}
		if ri >= 0 && cell(rec, ri) != "" {
			rq, d := parseQ(cell(rec, ri), domain.UnitPerMinute, row, "rate_k_per_min")
			if !rq.Valid {
				diags = append(diags, d)
			} else {
				seg.RateKmin = rq.Value
				delta := seg.ToC - seg.FromC
				if rq.Value != 0 {
					seg.DurationS = abs64(delta) / abs64(rq.Value) * 60
				}
			}
		}
		if di >= 0 && cell(rec, di) != "" {
			dq, d := parseQ(cell(rec, di), domain.UnitSecond, row, "duration_s")
			if dq.Valid {
				seg.DurationS = dq.Value
				if seg.RateKmin == 0 && seg.DurationS > 0 {
					seg.RateKmin = (seg.ToC - seg.FromC) / seg.DurationS * 60
				}
			} else {
				diags = append(diags, d)
			}
		}
		if seg.DurationS <= 0 {
			return Result{Layer: layer, Fatal: true, Diag: append(diags, domain.Diag{Code: "E_FURNACE", Row: row, Severity: "error", Message: "segment must have positive duration"})}
		}
		if gi >= 0 {
			seg.Gas = cell(rec, gi)
		}
		if ki >= 0 && cell(rec, ki) != "" {
			seg.Kind = strings.ToLower(cell(rec, ki))
		} else {
			if seg.FromC == seg.ToC {
				seg.Kind = "hold"
			} else {
				seg.Kind = "ramp"
			}
		}
		segs = append(segs, seg)
		cursor += seg.DurationS
		prevTo = seg.ToC
		haveFrom = false
	}
	if len(segs) == 0 {
		return fatal(layer, "no furnace rows")
	}
	return Result{Layer: layer, Payload: map[string]any{"segments": segs, "total_s": cursor}, Diag: diags}
}

func abs64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

var _ = strconv.Atoi

// ---- valves ----

func parseValvesRows(layer domain.SourceLayer, header []string, records [][]string) Result {
	ti := colIndex(header, "t_s", "time_s", "time")
	gi := colIndex(header, "gas", "valve", "atmosphere")
	if ti < 0 || gi < 0 {
		return fatal(layer, "valves csv requires columns: t_s, gas")
	}
	evs := make([]domain.ValveEvent, 0, len(records))
	var diags []domain.Diag
	for ri, rec := range records {
		row := ri + 2
		if allEmpty(rec) {
			continue
		}
		tQ, d := parseQ(cell(rec, ti), domain.UnitSecond, row, "t_s")
		gas := cell(rec, gi)
		if !tQ.Valid {
			diags = append(diags, d)
			continue
		}
		if gas == "" {
			diags = append(diags, domain.Diag{Code: "E_VALVE", Row: row, Field: "gas", Severity: "error", Message: "valve event missing gas"})
			continue
		}
		evs = append(evs, domain.ValveEvent{TS: tQ.Value, Gas: gas, Original: strings.Join(rec, ",")})
	}
	if len(evs) == 0 {
		return Result{Layer: layer, Fatal: true, Diag: append(diags, domain.Diag{Code: "E_VALVE", Severity: "error", Message: "no valid valve events"})}
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].TS < evs[j].TS })
	return Result{Layer: layer, Payload: map[string]any{"events": evs}, Diag: diags}
}

// ---- blank ----

func parseBlankRows(layer domain.SourceLayer, header []string, records [][]string) Result {
	ti := colIndex(header, "t_s", "time_s", "time")
	mi := colIndex(header, "mass_mg", "mass", "m")
	if ti < 0 || mi < 0 {
		return fatal(layer, "blank csv requires columns: t_s, mass_mg")
	}
	pts := make([]domain.BlankPoint, 0, len(records))
	var diags []domain.Diag
	for ri, rec := range records {
		row := ri + 2
		if allEmpty(rec) {
			continue
		}
		tQ, d := parseQ(cell(rec, ti), domain.UnitSecond, row, "t_s")
		mQ, d2 := parseQ(cell(rec, mi), domain.UnitMilligram, row, "mass_mg")
		if !tQ.Valid {
			diags = append(diags, d)
			continue
		}
		if !mQ.Valid {
			diags = append(diags, d2)
		}
		pts = append(pts, domain.BlankPoint{T: tQ.Value, MassRaw: cell(rec, mi), Mass: mQ})
	}
	if len(pts) < 2 {
		return Result{Layer: layer, Fatal: true, Diag: append(diags, domain.Diag{Code: "E_BLANK", Severity: "error", Message: "blank curve needs at least 2 points"})}
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].T < pts[j].T })
	return Result{Layer: layer, Payload: map[string]any{"points": pts}, Diag: diags}
}

// ---- initial mass ----

func parseInitMassRows(layer domain.SourceLayer, header []string, records [][]string) Result {
	mi := colIndex(header, "mass_mg", "mass", "m")
	ni := colIndex(header, "note", "comment")
	if mi < 0 {
		return fatal(layer, "initial_mass csv requires column: mass_mg (optional note)")
	}
	for _, rec := range records {
		if allEmpty(rec) {
			continue
		}
		mQ, d := parseQ(cell(rec, mi), domain.UnitMilligram, 2, "mass_mg")
		im := domain.InitialMass{MassRaw: cell(rec, mi), Mass: mQ}
		if ni >= 0 {
			im.Note = cell(rec, ni)
		}
		if !mQ.Valid {
			return Result{Layer: layer, Fatal: true, Diag: []domain.Diag{d}}
		}
		return Result{Layer: layer, Payload: map[string]any{"initial_mass": im}}
	}
	return fatal(layer, "initial_mass csv is empty")
}
