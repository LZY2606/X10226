package derive

import (
	"fmt"

	"tga-review/internal/domain"
)

// normSamples converts raw sample rows into canonical (s, mg/C) series,
// records explicit sampling gaps, keeps unconvertible values in place with
// diagnostics and flags non-monotonic mass noise.
func normSamples(src *domain.Source, gapMax float64) (*domain.SampleLayer, []domain.Diagnostic) {
	diags := append([]domain.Diagnostic(nil), src.Diagnostics...)
	tc := findCol(src, "time")
	mc := findCol(src, "mass")
	xc := findCol(src, "temp", "temperature")
	tu := src.Units[tc]
	mu := src.Units[mc]
	xu := ""
	if xc != "" {
		xu = src.Units[xc]
	}
	var masses, temps []domain.Point
	lastT := 0.0
	haveLast := false
	for _, row := range src.Rows {
		tRaw := row.Cells[tc]
		mRaw := ""
		if mc != "" {
			mRaw = row.Cells[mc]
		}
		t, tok := convert(tRaw, tu, timeScale)
		if !tok {
			diags = append(diags, domain.Diagnostic{Code: "unconvertible_time", Severity: domain.SevError,
				RowNo: row.RowNo, Column: tc, Value: tRaw,
				Message: "time coordinate kept at original position; point excluded"})
			continue
		}
		if haveLast && t-lastT > gapMax {
			masses = append(masses)
			temps = append(temps)
		}
		if mRaw == "" {
			// empty mass cell: explicit missing sample, no point produced
		} else if mv, ok := convert(mRaw, mu, massScale); ok {
			masses = append(masses, domain.Point{T: t, V: mv, UnitX: "s", UnitY: "mg", Reference: "normalized:" + src.ID})
		} else {
			diags = append(diags, domain.Diagnostic{Code: "unconvertible_mass", Severity: domain.SevWarning,
				RowNo: row.RowNo, Column: mc, Value: mRaw,
				Message: "mass value retained in raw layer; excluded from plot rather than dropped"})
		}
		if xc != "" {
			if xRaw := row.Cells[xc]; xRaw != "" {
				if xv, ok := tempConvert(xRaw, xu); ok {
					temps = append(temps, domain.Point{T: t, V: xv, UnitX: "s", UnitY: "C", Reference: "normalized:" + src.ID})
				} else {
					diags = append(diags, domain.Diagnostic{Code: "unconvertible_temp", Severity: domain.SevWarning,
						RowNo: row.RowNo, Column: xc, Value: xRaw,
						Message: "temperature retained in raw layer with unknown conversion"})
				}
			}
		}
		lastT = t
		haveLast = true
	}
	layer := &domain.SampleLayer{Mass: masses, Temp: temps, Diagnostics: diags}
	detectGaps(layer, gapMax)
	mono, noises := clampMonotone(layer.Mass)
	_ = mono
	layer.NonMono = noises
	if len(noises) > 0 {
		layer.Diagnostics = append(layer.Diagnostics, domain.Diagnostic{
			Code: "non_monotonic_noise", Severity: domain.SevInfo,
			Message: fmt.Sprintf("%d mass uptick(s) treated as noise, not as mass gain", len(noises))})
	}
	return layer, layer.Diagnostics
}

// detectGaps marks intervals between retained points wider than gapMax. Gaps
// are explicit first-class intervals and are never interpolated across.
func detectGaps(layer *domain.SampleLayer, gapMax float64) {
	for i := 1; i < len(layer.Mass); i++ {
		if d := layer.Mass[i].T - layer.Mass[i-1].T; d > gapMax {
			layer.Gaps = append(layer.Gaps, domain.Gap{
				From: layer.Mass[i-1].T, To: layer.Mass[i].T, Unit: "s",
				Reason: fmt.Sprintf("sampling gap %.0fs exceeds threshold %.0fs", d, gapMax)})
		}
	}
}

// normDrift parses the balance drift correction anchors.
func normDrift(src *domain.Source) []domain.DriftPoint {
	tc := findCol(src, "time")
	oc := findCol(src, "offset")
	var out []domain.DriftPoint
	for _, row := range src.Rows {
		t, ok1 := convert(row.Cells[tc], src.Units[tc], timeScale)
		o, ok2 := convert(row.Cells[oc], src.Units[oc], massScale)
		if !ok1 || !ok2 {
			continue
		}
		out = append(out, domain.DriftPoint{T: t, Offset: o, UnitX: "s", UnitY: "mg"})
	}
	return out
}

// normBlank parses the blank curve.
func normBlank(src *domain.Source) []domain.Point {
	tc := findCol(src, "time")
	mc := findCol(src, "mass")
	var out []domain.Point
	for _, row := range src.Rows {
		t, ok1 := convert(row.Cells[tc], src.Units[tc], timeScale)
		m, ok2 := convert(row.Cells[mc], src.Units[mc], massScale)
		if !ok1 || !ok2 {
			continue
		}
		out = append(out, domain.Point{T: t, V: m, UnitX: "s", UnitY: "mg"})
	}
	return out
}

// normProgram parses the furnace program.
func normProgram(src *domain.Source) []domain.ProgramStep {
	fc := findCol(src, "from")
	tc := findCol(src, "to")
	rc := findCol(src, "rate")
	var out []domain.ProgramStep
	for i, row := range src.Rows {
		from, ok1 := convert(row.Cells[fc], src.Units[fc], timeScale)
		to, ok2 := convert(row.Cells[tc], src.Units[tc], timeScale)
		if !ok1 || !ok2 {
			continue
		}
		step := domain.ProgramStep{Index: i + 1, FromT: from, ToT: to,
			UnitTemp: "C", UnitRate: "C/min"}
		if rc != "" {
			if r, ok := num(row.Cells[rc]); ok {
				step.Rate = r
			}
		}
		out = append(out, step)
	}
	return out
}

// normGas parses gas valve events in their original valve-time basis.
func normGas(src *domain.Source) []domain.GasEvent {
	ac := findCol(src, "at")
	fc := findCol(src, "from")
	tc := findCol(src, "to")
	var out []domain.GasEvent
	for i, row := range src.Rows {
		at, ok := convert(row.Cells[ac], src.Units[ac], timeScale)
		if !ok {
			continue
		}
		from := ""
		if fc != "" {
			from = row.Cells[fc]
		}
		out = append(out, domain.GasEvent{
			Index: i + 1, AtT: at, FromGas: from, ToGas: row.Cells[tc], Unit: "s"})
	}
	return out
}

// normInit parses the stratified initial sample mass.
func normInit(src *domain.Source) *domain.InitWeight {
	mc := findCol(src, "mass")
	row := src.Rows[0]
	m, _ := convert(row.Cells[mc], src.Units[mc], massScale)
	return &domain.InitWeight{
		Mass: m, Unit: "mg",
		OriginalRep: row.Cells[mc],
		Revised:     src.ParentID != "",
		ParentID:    src.ParentID,
	}
}
