package domain

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseStreamCSV parses one layered stream from header+rows CSV. Unknown
// columns are retained in the record's Raw map. Rows with structural problems
// are returned as rejected records so an import can partially succeed.
func ParseStreamCSV(kind Kind, r io.Reader) (records []ImportedRecord, fatal error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("missing header: %w", err)
	}
	cols := make([]string, len(header))
	for i, h := range header {
		cols[i] = strings.TrimSpace(h)
	}
	seq := 0
	for {
		row, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			seq++
			records = append(records, ImportedRecord{
				Key:         strconv.Itoa(seq),
				Seq:         seq,
				Accepted:    false,
				Raw:         map[string]string{"_row": strings.Join(row, ",")},
				Diagnostics: []Diagnostic{{Code: "csv_row_error", Message: err.Error()}},
			})
			break
		}
		seq++
		raw := map[string]string{}
		for i, c := range cols {
			if i < len(row) {
				raw[c] = row[i]
			}
		}
		rec := ImportedRecord{Key: strconv.Itoa(seq), Seq: seq, Raw: raw}
		typed, diags := buildTyped(kind, seq, raw)
		rec.Typed = typed
		rec.Diagnostics = diags
		rec.Accepted = len(diags) == 0
		records = append(records, rec)
	}
	return records, nil
}

func val(raw map[string]string, field, unit string, dim Dimension) RawValue {
	token, ok := raw[field]
	if !ok {
		return RawValue{Dim: dim, Diagnostic: "missing_field:" + field}
	}
	if dim == DimTemperature {
		return ToTempCanonical(token, unit)
	}
	return ToCanonical(token, unit, dim)
}

func diagsOf(prefix string, vs ...RawValue) []Diagnostic {
	var out []Diagnostic
	for _, v := range vs {
		if !v.Normalized {
			out = append(out, Diagnostic{Code: "unconvertible_value", Field: prefix, Message: v.Diagnostic, Raw: v.Raw})
		}
	}
	return out
}

func buildTyped(kind Kind, seq int, raw map[string]string) (any, []Diagnostic) {
	switch kind {
	case KindPoints:
		p := SamplePoint{
			Index:    seq,
			Elapsed:  val(raw, "elapsed_s", rawUnit(raw, "elapsed_unit", UnitS), DimTime),
			FurnaceT: val(raw, "furnace_t", rawUnit(raw, "furnace_t_unit", UnitC), DimTemperature),
			SampleT:  val(raw, "sample_t", rawUnit(raw, "sample_t_unit", UnitC), DimTemperature),
			Mass:     val(raw, "mass", rawUnit(raw, "mass_unit", UnitMg), DimMass),
			RawLine:  raw["_row"],
		}
		return p, diagsOf("points", p.Elapsed, p.FurnaceT, p.SampleT, p.Mass)
	case KindDrift:
		d := DriftModel{
			Index:       seq,
			Slope:       ToCanonical(raw["slope_mg_s"], UnitMg+"/s", DimNone), // slope kept numeric, mg/s
			Intercept:   val(raw, "intercept", UnitMg, DimMass),
			EffectiveAt: val(raw, "effective_at_s", UnitS, DimTime),
			Basis:       raw["basis"],
		}
		slope, err := strconv.ParseFloat(strings.TrimSpace(raw["slope_mg_s"]), 64)
		d.Slope = RawValue{Raw: raw["slope_mg_s"], Unit: "mg/s", Dim: DimNone, Numeric: slope, Normalized: err == nil}
		if err != nil {
			d.Slope.Diagnostic = "not_a_number"
		}
		return d, diagsOf("drift", d.Slope, d.Intercept, d.EffectiveAt)
	case KindFurnace:
		f := FurnaceSegment{
			Index:    seq,
			Start:    val(raw, "start_s", UnitS, DimTime),
			End:      val(raw, "end_s", UnitS, DimTime),
			StartT:   val(raw, "start_t", rawUnit(raw, "start_t_unit", UnitC), DimTemperature),
			EndT:     val(raw, "end_t", rawUnit(raw, "end_t_unit", UnitC), DimTemperature),
			RampRate: val(raw, "ramp_rate_c_min", "C/min", DimNone),
		}
		ramp, err := strconv.ParseFloat(strings.TrimSpace(raw["ramp_rate_c_min"]), 64)
		f.RampRate = RawValue{Raw: raw["ramp_rate_c_min"], Unit: "C/min", Dim: DimNone, Numeric: ramp, Normalized: err == nil}
		if err != nil {
			f.RampRate.Diagnostic = "not_a_number"
		}
		return f, diagsOf("furnace", f.Start, f.End, f.StartT, f.EndT, f.RampRate)
	case KindGas:
		g := GasEvent{
			Index:   seq,
			At:      val(raw, "at_s", UnitS, DimTime),
			FromGas: raw["from_gas"],
			ToGas:   raw["to_gas"],
			Flow:    val(raw, "flow", UnitMLMin, DimGasFlow),
			Note:    raw["note"],
		}
		return g, diagsOf("gas", g.At, g.Flow)
	case KindBlank:
		b := BlankPoint{
			Index:   seq,
			Elapsed: val(raw, "elapsed_s", rawUnit(raw, "elapsed_unit", UnitS), DimTime),
			Mass:    val(raw, "mass", rawUnit(raw, "mass_unit", UnitMg), DimMass),
		}
		return b, diagsOf("blank", b.Elapsed, b.Mass)
	case KindM0:
		m := M0Estimate{
			Index:   seq,
			Analyst: raw["analyst"],
			Mass:    val(raw, "mass", rawUnit(raw, "mass_unit", UnitMg), DimMass),
			At:      val(raw, "at_s", UnitS, DimTime),
			Basis:   raw["basis"],
		}
		var extra []Diagnostic
		if strings.TrimSpace(m.Analyst) == "" {
			extra = append(extra, Diagnostic{Code: "missing_analyst", Field: "m0"})
		}
		return m, append(diagsOf("m0", m.Mass, m.At), extra...)
	default:
		return nil, []Diagnostic{{Code: "unknown_kind", Message: string(kind)}}
	}
}

func rawUnit(raw map[string]string, key, fallback string) string {
	if u := strings.TrimSpace(raw[key]); u != "" {
		return u
	}
	return fallback
}
