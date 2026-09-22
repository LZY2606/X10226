package importer

import (
	"fmt"
	"strings"

	"tga-review/internal/domain"
	"tga-review/internal/ident"
)

func buildSection(res *Result, project, source, layer string, lines []string) error {
	if len(lines) < 1 {
		return fmt.Errorf("empty section %s", layer)
	}
	bid := ident.BatchID(project, source, []byte(layer+joinLines(lines)))
	cols, err := parseHeader(splitTokens(lines[0]))
	if err != nil {
		return fmt.Errorf("%s: %v", layer, err)
	}
	m := colMap(cols)
	for i, l := range lines[1:] {
		ord := i + 1
		if err := buildRow(res, layer, bid, ord, m, cols, splitTokens(l)); err != nil {
			return err
		}
	}
	return nil
}

func joinLines(lines []string) string {
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	return sb.String()
}

func needCols(m map[string]int, names ...string) error {
	var missing []string
	for _, n := range names {
		if _, ok := m[n]; !ok {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing column(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

func buildRow(res *Result, layer, bid string, ord int, m map[string]int, cols []headerCol, fields []string) error {
	if len(fields) > len(cols) {
		return fmt.Errorf("%s row %d: %d fields but header has %d columns", layer, ord, len(fields), len(cols))
	}
	switch layer {
	case domain.LayerSamples:
		if err := needCols(m, "t", "mass", "temp"); err != nil {
			return err
		}
		res.Data.Samples = append(res.Data.Samples, domain.TimePoint{
			ID:      ident.SampleID(bid, ord),
			Ordinal: ord,
			T:       get(fields, m, "t", unitOf(cols, m, "t")),
			Mass:    get(fields, m, "mass", unitOf(cols, m, "mass")),
			Temp:    get(fields, m, "temp", unitOf(cols, m, "temp")),
		})
	case domain.LayerDrift:
		if err := needCols(m, "t", "offset"); err != nil {
			return err
		}
		kind := getStr(fields, m, "kind")
		if kind == "" {
			kind = "point"
		}
		res.Data.Drift = append(res.Data.Drift, domain.DriftCorrection{
			ID: ident.DriftID(bid, ord), Ordinal: ord,
			T: get(fields, m, "t", unitOf(cols, m, "t")),
			Offset: get(fields, m, "offset", unitOf(cols, m, "offset")),
			Kind: kind,
		})
	case domain.LayerProgram:
		if err := needCols(m, "t_start", "t_end", "temp_start", "temp_end"); err != nil {
			return err
		}
		rate := get(fields, m, "rate", domain.UnitCelsius+"/min")
		if rate.Raw == "" {
			rate = domain.Quantity{Unit: domain.UnitCelsius + "/min", Canonical: domain.UnitCelsius + "/min"}
		}
		res.Data.Program = append(res.Data.Program, domain.ProgramSegment{
			ID: ident.ProgramID(bid, ord), Ordinal: ord,
			TStart:    get(fields, m, "t_start", unitOf(cols, m, "t_start")),
			TEnd:      get(fields, m, "t_end", unitOf(cols, m, "t_end")),
			TempStart: get(fields, m, "temp_start", unitOf(cols, m, "temp_start")),
			TempEnd:   get(fields, m, "temp_end", unitOf(cols, m, "temp_end")),
			Rate:      rate,
			Label:     getStr(fields, m, "label"),
		})
	case domain.LayerGasEvents:
		if err := needCols(m, "t", "from_gas", "to_gas"); err != nil {
			return err
		}
		flow := get(fields, m, "flow", domain.UnitSCCM)
		if flow.Raw == "" {
			flow = domain.Quantity{Unit: domain.UnitSCCM, Canonical: domain.UnitSCCM}
		}
		res.Data.Gas = append(res.Data.Gas, domain.GasEvent{
			ID: ident.GasID(bid, ord), Ordinal: ord,
			T:       get(fields, m, "t", unitOf(cols, m, "t")),
			FromGas: getStr(fields, m, "from_gas"),
			ToGas:   getStr(fields, m, "to_gas"),
			Flow:    flow,
		})
	case domain.LayerBlank:
		if err := needCols(m, "t", "mass"); err != nil {
			return err
		}
		res.Data.Blank = append(res.Data.Blank, domain.BlankPoint{
			ID: ident.BlankID(bid, ord), Ordinal: ord,
			T:    get(fields, m, "t", unitOf(cols, m, "t")),
			Mass: get(fields, m, "mass", unitOf(cols, m, "mass")),
		})
	case domain.LayerInitial:
		if err := needCols(m, "mass"); err != nil {
			return err
		}
		res.Data.Initial = append(res.Data.Initial, domain.InitialMass{
			ID: ident.InitialID(bid, ord), Ordinal: ord,
			Mass:   get(fields, m, "mass", unitOf(cols, m, "mass")),
			Reason: getStr(fields, m, "reason"),
		})
	}
	return nil
}

func unitOf(cols []headerCol, m map[string]int, name string) string {
	idx, ok := m[name]
	if !ok {
		return ""
	}
	return cols[idx].unit
}
