package ingest

import (
	"strings"

	"tga-review/internal/domain"
)

func unitAt(units []string, i int, fallback string) string {
	// units[0] is the section name; positions 1.. carry declared units
	if i < len(units) {
		return units[i]
	}
	return fallback
}

func parseSamples(kind domain.RawKind, units []string, named bool, header []string, rows [][]string) (Parsed, error) {
	mu, tu, tau := unitAt(units, 1, "mg"), unitAt(units, 2, "C"), unitAt(units, 3, "s")
	p := Parsed{Kind: kind}
	for ri, row := range rows {
		rt, okT := col(row, named, header, []string{"time", "t"}, 0)
		rf, okF := col(row, named, header, []string{"furnace_temp", "temp", "tf"}, 1)
		rm, okM := col(row, named, header, []string{"mass", "m"}, 2)
		if err := require(rt, okT, "time"); err != nil {
			return Parsed{}, err
		}
		if err := require(rf, okF, "furnace_temp"); err != nil {
			return Parsed{}, err
		}
		if err := require(rm, okM, "mass"); err != nil {
			return Parsed{}, err
		}
		rs, _ := col(row, named, header, []string{"sample_temp", "ts"}, 3)
		note, _ := col(row, named, header, []string{"note"}, 4)
		sp := domain.SamplePoint{
			Index: ri + 1,
			Time:  domain.ParseCell(rt, tau, domain.DimTime),
			FTemp: domain.ParseCell(rf, tu, domain.DimTemp),
			Mass:  domain.ParseCell(rm, mu, domain.DimMass),
			Note:  note,
		}
		if strings.TrimSpace(rs) != "" {
			sp.SampleT = domain.ParseCell(rs, tu, domain.DimTemp)
			cellDiag(&sp.SampleT, &p.Diags, kind, ri+1, "sample_temp")
		}
		cellDiag(&sp.Time, &p.Diags, kind, ri+1, "time")
		cellDiag(&sp.FTemp, &p.Diags, kind, ri+1, "furnace_temp")
		cellDiag(&sp.Mass, &p.Diags, kind, ri+1, "mass")
		p.Payload.Samples = append(p.Payload.Samples, sp)
	}
	return p, nil
}

func parseDrift(kind domain.RawKind, units []string, named bool, header []string, rows [][]string) (Parsed, error) {
	tu, mu := unitAt(units, 1, "s"), unitAt(units, 2, "mg")
	p := Parsed{Kind: kind}
	for ri, row := range rows {
		rt, okT := col(row, named, header, []string{"time", "t"}, 0)
		rd, okD := col(row, named, header, []string{"drift_mass", "drift", "d"}, 1)
		if err := require(rt, okT, "time"); err != nil {
			return Parsed{}, err
		}
		if err := require(rd, okD, "drift_mass"); err != nil {
			return Parsed{}, err
		}
		note, _ := col(row, named, header, []string{"note"}, 2)
		dp := domain.DriftPoint{
			Index: ri + 1,
			Time:  domain.ParseCell(rt, tu, domain.DimTime),
			Drift: domain.ParseCell(rd, mu, domain.DimMass),
			Note:  note,
		}
		cellDiag(&dp.Time, &p.Diags, kind, ri+1, "time")
		cellDiag(&dp.Drift, &p.Diags, kind, ri+1, "drift_mass")
		p.Payload.Drift = append(p.Payload.Drift, dp)
	}
	return p, nil
}

func parseProgram(kind domain.RawKind, units []string, named bool, header []string, rows [][]string) (Parsed, error) {
	tu, tempu := unitAt(units, 1, "s"), unitAt(units, 2, "C")
	p := Parsed{Kind: kind}
	for ri, row := range rows {
		rs, okS := col(row, named, header, []string{"start", "start_time"}, 0)
		re, okE := col(row, named, header, []string{"end", "end_time"}, 1)
		rts, okTS := col(row, named, header, []string{"temp_start", "tstart"}, 2)
		rte, okTE := col(row, named, header, []string{"temp_end", "tend"}, 3)
		mode, _ := col(row, named, header, []string{"mode"}, 4)
		for nm, v := range map[string]string{"start_time": rs, "end_time": re, "temp_start": rts, "temp_end": rte} {
			if err := require(v, true, nm); err != nil {
				return Parsed{}, err
			}
		}
		if !okS || !okE || !okTS || !okTE {
			return Parsed{}, &ParseError{Msg: "program requires start,end,temp_start,temp_end"}
		}
		if mode == "" {
			mode = "ramp"
		}
		if mode != "ramp" && mode != "hold" && mode != "cool" {
			diag(&p.Diags, kind, ri+1, "warning", "unknown program mode "+mode+" (kept)")
		}
		st := domain.ProgramStep{
			Index:  ri + 1,
			Start:  domain.ParseCell(rs, tu, domain.DimTime),
			End:    domain.ParseCell(re, tu, domain.DimTime),
			TStart: domain.ParseCell(rts, tempu, domain.DimTemp),
			TEnd:   domain.ParseCell(rte, tempu, domain.DimTemp),
			Mode:   mode,
		}
		for _, c := range []*domain.Cell{&st.Start, &st.End, &st.TStart, &st.TEnd} {
			cellDiag(c, &p.Diags, kind, ri+1, "program cell")
		}
		p.Payload.Program = append(p.Payload.Program, st)
	}
	return p, nil
}

func parseGas(kind domain.RawKind, units []string, named bool, header []string, rows [][]string) (Parsed, error) {
	tu := unitAt(units, 1, "s")
	p := Parsed{Kind: kind}
	for ri, row := range rows {
		rt, okT := col(row, named, header, []string{"time", "t"}, 0)
		rg, okG := col(row, named, header, []string{"gas", "atmosphere"}, 1)
		if err := require(rt, okT, "time"); err != nil {
			return Parsed{}, err
		}
		if err := require(rg, okG, "gas"); err != nil {
			return Parsed{}, err
		}
		note, _ := col(row, named, header, []string{"note"}, 2)
		ge := domain.GasEvent{
			Index: ri + 1,
			Time:  domain.ParseCell(rt, tu, domain.DimTime),
			Gas:   domain.ParseCell(rg, "", domain.DimGas),
			Note:  note,
		}
		cellDiag(&ge.Time, &p.Diags, kind, ri+1, "time")
		p.Payload.Gas = append(p.Payload.Gas, ge)
	}
	return p, nil
}

func parseBlank(kind domain.RawKind, units []string, named bool, header []string, rows [][]string) (Parsed, error) {
	tu, mu := unitAt(units, 1, "s"), unitAt(units, 2, "mg")
	p := Parsed{Kind: kind}
	for ri, row := range rows {
		rt, okT := col(row, named, header, []string{"time", "t"}, 0)
		rm, okM := col(row, named, header, []string{"mass", "m"}, 1)
		if err := require(rt, okT, "time"); err != nil {
			return Parsed{}, err
		}
		if err := require(rm, okM, "mass"); err != nil {
			return Parsed{}, err
		}
		bp := domain.BlankPoint{
			Index: ri + 1,
			Time:  domain.ParseCell(rt, tu, domain.DimTime),
			Mass:  domain.ParseCell(rm, mu, domain.DimMass),
		}
		cellDiag(&bp.Time, &p.Diags, kind, ri+1, "time")
		cellDiag(&bp.Mass, &p.Diags, kind, ri+1, "mass")
		p.Payload.Blank = append(p.Payload.Blank, bp)
	}
	return p, nil
}

func parseInit(kind domain.RawKind, units []string, named bool, header []string, rows [][]string) (Parsed, error) {
	mu := unitAt(units, 1, "mg")
	p := Parsed{Kind: kind}
	if len(rows) != 1 {
		return Parsed{}, &ParseError{Msg: "initmass section must contain exactly one row"}
	}
	row := rows[0]
	rm, okM := col(row, named, header, []string{"mass", "initial_mass", "m"}, 0)
	if err := require(rm, okM, "mass"); err != nil {
		return Parsed{}, err
	}
	note, _ := col(row, named, header, []string{"note"}, 1)
	im := domain.InitMass{Mass: domain.ParseCell(rm, mu, domain.DimMass), Note: note}
	cellDiag(&im.Mass, &p.Diags, kind, 1, "mass")
	p.Payload.InitMass = &im
	return p, nil
}
