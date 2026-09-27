package parse

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"tga/internal/domain"
)

func parseDriftDoc(out *domain.ParsedLayer, d *DriftDoc) {
	if d == nil {
		out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
			Code: "missing_drift", Message: "drift_correction document needs 'drift'"})
		return
	}
	spec := &domain.DriftSpec{Model: strings.TrimSpace(d.Model), RawText: d.Rate + " / " + d.Intercept}
	if spec.Model != "linear" && spec.Model != "none" {
		out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
			Code: "bad_model", Message: "drift model must be linear|none", Raw: d.Model})
		return
	}
	spec.Rate = parseValue(d.Rate, "rate", func(msg string) {
		out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
			Field: "rate", Code: "bad_value", Message: msg, Raw: d.Rate})
	})
	spec.Intercept = parseValue(d.Intercept, "mass", func(msg string) {
		out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
			Field: "intercept", Code: "bad_value", Message: msg, Raw: d.Intercept})
	})
	spec.AnchorAt = parseValue(d.AnchorAt, "time", func(msg string) {
		out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
			Field: "anchor_at", Code: "bad_value", Message: msg, Raw: d.AnchorAt})
	})
	out.Drift = spec
}

func parseFurnaceDoc(out *domain.ParsedLayer, rows []map[string]any) {
	if len(rows) == 0 {
		out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
			Code: "empty_program", Message: "furnace program is empty"})
		return
	}
	for i, r := range rows {
		seg := domain.FurnaceSegment{Index: i + 1}
		if s, ok := atStr(r, "from_time", "t0"); ok {
			seg.FromTime = parseValue(s, "time", nil)
		}
		if s, ok := atStr(r, "to_time", "t1"); ok {
			seg.ToTime = parseValue(s, "time", nil)
		}
		if s, ok := atStr(r, "from_temp", "temp0"); ok {
			seg.FromTemp = parseValue(s, "temp", nil)
		}
		if s, ok := atStr(r, "to_temp", "temp1"); ok {
			seg.ToTemp = parseValue(s, "temp", nil)
		}
		if k, ok := atStr(r, "kind", "type"); ok {
			seg.Kind = k
		}
		seg.Programmed = true
		if seg.Kind == "" {
			if seg.FromTemp.Value == seg.ToTemp.Value {
				seg.Kind = "hold"
			} else {
				seg.Kind = "ramp"
			}
		}
		out.Furnace = append(out.Furnace, seg)
	}
}

func parseGasDoc(out *domain.ParsedLayer, rows []map[string]any) {
	if len(rows) == 0 {
		out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
			Code: "empty_events", Message: "gas events list is empty"})
		return
	}
	for i, r := range rows {
		ev := domain.GasEvent{Index: i + 1}
		if s, ok := atStr(r, "at", "time", "t"); ok {
			ev.At = parseValue(s, "time", func(msg string) {
				out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
					Row: i + 1, Field: "at", Code: "bad_value", Message: msg, Raw: s})
			})
		}
		ev.FromGas, _ = atStr(r, "from_gas", "from")
		ev.ToGas, _ = atStr(r, "to_gas", "to")
		ev.Controller, _ = atStr(r, "controller", "clock")
		if s, ok := atStr(r, "flow"); ok {
			ev.Flow = parseValue(s, "flow", nil)
		}
		ev.RawText = fmt.Sprintf("%v -> %v @%v", ev.FromGas, ev.ToGas, ev.At.Raw)
		if ev.ToGas == "" {
			out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
				Row: i + 1, Field: "to_gas", Code: "missing_field", Message: "gas event needs to_gas"})
		}
		out.Gas = append(out.Gas, ev)
	}
}

func parseInitMassDoc(out *domain.ParsedLayer, m map[string]any) {
	im := &domain.InitialMass{}
	s, _ := atStr(m, "mass", "m", "initial_mass")
	im.Mass = parseValue(s, "mass", func(msg string) {
		out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
			Field: "mass", Code: "bad_value", Message: msg, Raw: s})
	})
	im.Basis, _ = atStr(m, "basis")
	if im.Basis == "" {
		im.Basis = "sample"
	}
	if n, ok := atStr(m, "recorded_at"); ok {
		im.RecordedAt = parseValue(n, "time", nil)
	}
	im.Note, _ = atStr(m, "note")
	out.InitMass = im
}

// ---------- CSV ----------

// Required headers per layer. CSV keeps the exact raw cell text; normalization
// happens in the derived layer only.
var csvHeaders = map[domain.Layer][]string{
	domain.LayerRaw:      {"time", "temp", "mass"},
	domain.LayerBlank:    {"time", "mass"},
	domain.LayerGas:      {"at", "from_gas", "to_gas"},
	domain.LayerFurnace:  {"from_time", "to_time", "from_temp", "to_temp", "kind"},
	domain.LayerDrift:    {"model", "rate", "intercept", "anchor_at"},
	domain.LayerInitMass: {"mass", "basis"},
}

func parseCSV(text string, declared domain.Layer) (*domain.ParsedLayer, []domain.Diag) {
	layer := declared
	if !layer.Valid() {
		return nil, []domain.Diag{{Severity: "error", Code: "missing_layer",
			Message: "CSV import requires a declared layer"}}
	}
	r := csv.NewReader(strings.NewReader(text))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	out := &domain.ParsedLayer{Layer: layer}
	header, err := r.Read()
	if err == io.EOF || err != nil {
		return nil, []domain.Diag{{Severity: "error", Layer: string(layer),
			Code: "bad_csv", Message: "cannot read CSV header: " + errString(err)}}
	}
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.TrimSpace(strings.ToLower(h))] = i
	}
	for _, need := range csvHeaders[layer] {
		if _, ok := idx[need]; !ok {
			out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(layer),
				Code: "missing_column", Field: need,
				Message: "CSV missing required column '" + need + "'"})
		}
	}
	if out.HasError() {
		return out, out.Diags
	}
	cell := func(rec []string, name string) string {
		if i, ok := idx[name]; ok && i < len(rec) {
			return rec[i]
		}
		return ""
	}
	row := 1
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(layer),
				Row: row, Code: "bad_csv", Message: err.Error()})
			break
		}
		if isBlankRecord(rec) {
			row++
			continue
		}
		switch layer {
		case domain.LayerRaw:
			m := map[string]any{"time": cell(rec, "time"), "temp": cell(rec, "temp"), "mass": cell(rec, "mass")}
			sub := &domain.ParsedLayer{}
			parsePointRows(sub, []map[string]any{m}, []string{"time", "temp", "mass"})
			p := sub.Raw[0]
			p.Row = row
			out.Raw = append(out.Raw, p)
			for _, d := range sub.Diags {
				d.Row = row
				out.Diags = append(out.Diags, d)
			}
		case domain.LayerBlank:
			m := map[string]any{"time": cell(rec, "time"), "mass": cell(rec, "mass")}
			if t := cell(rec, "temp"); t != "" {
				m["temp"] = t
			}
			sub := &domain.ParsedLayer{}
			parseBlankRows(sub, []map[string]any{m})
			bp := sub.Blank[0]
			bp.Row = row
			out.Blank = append(out.Blank, bp)
		case domain.LayerGas:
			m := map[string]any{"at": cell(rec, "at"), "from_gas": cell(rec, "from_gas"), "to_gas": cell(rec, "to_gas")}
			if f := cell(rec, "flow"); f != "" {
				m["flow"] = f
			}
			if c := cell(rec, "controller"); c != "" {
				m["controller"] = c
			}
			sub := &domain.ParsedLayer{}
			parseGasDoc(sub, []map[string]any{m})
			out.Gas = append(out.Gas, sub.Gas...)
		case domain.LayerFurnace:
			m := map[string]any{
				"from_time": cell(rec, "from_time"), "to_time": cell(rec, "to_time"),
				"from_temp": cell(rec, "from_temp"), "to_temp": cell(rec, "to_temp"),
				"kind": cell(rec, "kind"),
			}
			sub := &domain.ParsedLayer{}
			parseFurnaceDoc(sub, []map[string]any{m})
			out.Furnace = append(out.Furnace, sub.Furnace...)
		case domain.LayerDrift:
			parseDriftDoc(out, &DriftDoc{Model: cell(rec, "model"), Rate: cell(rec, "rate"),
				Intercept: cell(rec, "intercept"), AnchorAt: cell(rec, "anchor_at")})
		case domain.LayerInitMass:
			parseInitMassDoc(out, map[string]any{"mass": cell(rec, "mass"), "basis": cell(rec, "basis")})
		}
		row++
	}
	if layer == domain.LayerDrift || layer == domain.LayerInitMass {
		// single-row layers parsed above per record; last record wins but warn if >1
	}
	return out, out.Diags
}

func isBlankRecord(rec []string) bool {
	for _, c := range rec {
		if strings.TrimSpace(c) != "" {
			return false
		}
	}
	return true
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// AsFloat is a small helper used by the case engine.
func AsFloat(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f, err == nil
}
