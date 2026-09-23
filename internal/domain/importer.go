package domain

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Bundle is the copyable, domain-specific import representation for the six
// stratified raw layers. Every section is optional for revisions; a section
// that is present but malformed causes that layer to fail (half failure),
// while the other layers still commit.
type Bundle struct {
	AttemptID string                `json:"attempt_id,omitempty"`
	Reason    string                `json:"reason,omitempty"`
	Sample    []json.RawMessage     `json:"sample,omitempty"`
	Blank     []json.RawMessage     `json:"blank,omitempty"`
	Drift     json.RawMessage       `json:"drift,omitempty"`
	Furnace   []json.RawMessage     `json:"furnace,omitempty"`
	Gas       []json.RawMessage     `json:"gas,omitempty"`
	Initial   json.RawMessage       `json:"initial,omitempty"`
}

type numField struct {
	V    any    `json:"v"`
	Unit string `json:"unit"`
	Raw  string `json:"raw,omitempty"`
}

func parseNumField(raw json.RawMessage, canonical string) (Quantity, error) {
	if len(raw) == 0 {
		return Quantity{Diagnostic: "missing field"}, fmt.Errorf("missing field")
	}
	var nf numField
	if err := json.Unmarshal(raw, &nf); err != nil {
		return Quantity{}, fmt.Errorf("invalid numeric object: %w", err)
	}
	rawText := nf.Raw
	if rawText == "" {
		rawText = string(raw)
	}
	q := ParseQuantity(rawText, nf.V, nf.Unit, canonical)
	if q.Diagnostic != "" {
		return q, fmt.Errorf("%s", q.Diagnostic)
	}
	return q, nil
}

type ptRow struct {
	T      json.RawMessage `json:"t"`
	TUnit  string          `json:"t_unit"`
	Mass   json.RawMessage `json:"mass"`
	Temp   json.RawMessage `json:"temp"`
	Note   string          `json:"note"`
}

func parsePoints(rows []json.RawMessage, wantBlank bool) ([]SamplePoint, []Diagnostic) {
	var pts []SamplePoint
	var diags []Diagnostic
	for i, raw := range rows {
		var row ptRow
		if err := json.Unmarshal(raw, &row); err != nil {
			diags = append(diags, Diagnostic{Code: "format", Message: fmt.Sprintf("row %d: invalid JSON object: %v", i, err), Ref: fmt.Sprintf("row:%d", i)})
			continue
		}
		tq, err := parseNumField(row.T, "s")
		if err != nil {
			diags = append(diags, Diagnostic{Code: "format", Message: fmt.Sprintf("row %d: bad time: %v", i, err), Ref: fmt.Sprintf("row:%d", i)})
			continue
		}
		mq, err := parseNumField(row.Mass, "mg")
		if err != nil {
			diags = append(diags, Diagnostic{Code: "format", Message: fmt.Sprintf("row %d: bad mass: %v", i, err), Ref: fmt.Sprintf("row:%d", i)})
			// unconvertible mass stays at the original position with a diagnostic
			mq = ParseQuantity(string(row.Mass), extractV(row.Mass), extractUnit(row.Mass), "mg")
		}
		tq2, _ := parseNumField(row.Temp, "degc")
		layer := "sample"
		if wantBlank {
			layer = "blank"
		}
		if mq.Diagnostic != "" {
			mq.Diagnostic = "mass kept with original representation: " + mq.Diagnostic
			diags = append(diags, Diagnostic{Code: "unconvertible", Layer: layer, Message: fmt.Sprintf("row %d: %s", i, mq.Diagnostic), Ref: fmt.Sprintf("row:%d", i)})
		}
		if tq2.Diagnostic != "" {
			tq2.Diagnostic = "temperature kept with original representation: " + tq2.Diagnostic
			diags = append(diags, Diagnostic{Code: "unconvertible", Layer: layer, Message: fmt.Sprintf("row %d: %s", i, tq2.Diagnostic), Ref: fmt.Sprintf("row:%d", i)})
		}
		pts = append(pts, SamplePoint{Idx: i, T: tq.Value, Mass: mq, Temp: tq2, Note: row.Note})
	}
	sort.SliceStable(pts, func(a, b int) bool { return pts[a].T < pts[b].T })
	return pts, diags
}

func extractV(raw json.RawMessage) any {
	var nf numField
	if err := json.Unmarshal(raw, &nf); err != nil {
		return string(raw)
	}
	return nf.V
}

func extractUnit(raw json.RawMessage) string {
	var nf numField
	_ = json.Unmarshal(raw, &nf)
	return nf.Unit
}

type furRow struct {
	T0     json.RawMessage `json:"t0"`
	T1     json.RawMessage `json:"t1"`
	Target json.RawMessage `json:"target"`
	Rate   json.RawMessage `json:"rate"`
	Mode   string          `json:"mode"`
}

type gasRow struct {
	T      json.RawMessage `json:"t"`
	Gas    string          `json:"gas"`
	Action string          `json:"action"`
	Flow   json.RawMessage `json:"flow"`
	Note   string          `json:"note"`
}

type initialRow struct {
	Mass  json.RawMessage `json:"mass"`
	AtT   json.RawMessage `json:"at_t"`
	TUnit string          `json:"t_unit"`
	Note  string          `json:"note"`
}

type driftRow struct {
	B0          json.RawMessage `json:"b0"`
	B1          json.RawMessage `json:"b1"`
	UseBlank    *bool           `json:"use_blank"`
	BlankSpline bool            `json:"blank_spline"`
	Note        string          `json:"note"`
}

// ParseLayer parses one layer section. A present-but-malformed section returns
// an error (explicit format error); the raw text is returned alongside for the
// half-failure record.
func ParseLayer(kind LayerKind, raw json.RawMessage) (LayerVersion, error) {
	lv := LayerVersion{Kind: kind, RawText: raw}
	switch kind {
	case LayerSample, LayerBlank:
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			return lv, fmt.Errorf("%s: expected an array of point objects: %w", kind, err)
		}
		pts, diags := parsePoints(rows, kind == LayerBlank)
		if len(pts) == 0 && len(rows) > 0 {
			return lv, fmt.Errorf("%s: every point failed to parse", kind)
		}
		if kind == LayerSample {
			lv.Points = pts
		} else {
			lv.Blank = pts
		}
		lv.Diagnostics = diags
	case LayerFurnace:
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			return lv, fmt.Errorf("furnace: expected an array: %w", err)
		}
		var diags []Diagnostic
		for i, r := range rows {
			var fr furRow
			if err := json.Unmarshal(r, &fr); err != nil {
				diags = append(diags, Diagnostic{Code: "format", Layer: "furnace", Message: fmt.Sprintf("seg %d: %v", i, err)})
				continue
			}
			t0, e0 := parseNumField(fr.T0, "s")
			t1, e1 := parseNumField(fr.T1, "s")
			target, e2 := parseNumField(fr.Target, "degc")
			rate := Quantity{}
			if len(fr.Rate) > 0 {
				var e3 error
				rate, e3 = parseNumField(fr.Rate, "k/min")
				if e3 != nil {
					diags = append(diags, Diagnostic{Code: "unconvertible", Layer: "furnace", Message: fmt.Sprintf("seg %d: rate %v", i, e3)})
				}
			}
			if e0 != nil || e1 != nil || e2 != nil {
				diags = append(diags, Diagnostic{Code: "format", Layer: "furnace", Message: fmt.Sprintf("seg %d: t0/t1/target invalid", i)})
				continue
			}
			mode := fr.Mode
			if mode == "" {
				mode = "ramp"
			}
			lv.Furnace = append(lv.Furnace, FurnaceSeg{Idx: i, T0: t0.Value, T1: t1.Value, Target: target, Rate: rate, Mode: mode})
		}
		lv.Diagnostics = diags
	case LayerGas:
		var rows []json.RawMessage
		if err := json.Unmarshal(raw, &rows); err != nil {
			return lv, fmt.Errorf("gas: expected an array: %w", err)
		}
		var diags []Diagnostic
		for i, r := range rows {
			var gr gasRow
			if err := json.Unmarshal(r, &gr); err != nil {
				diags = append(diags, Diagnostic{Code: "format", Layer: "gas", Message: fmt.Sprintf("event %d: %v", i, err)})
				continue
			}
			tq, err := parseNumField(gr.T, "s")
			if err != nil || gr.Gas == "" || gr.Action == "" {
				diags = append(diags, Diagnostic{Code: "format", Layer: "gas", Message: fmt.Sprintf("event %d: t/gas/action invalid", i)})
				continue
			}
			flow := Quantity{}
			if len(gr.Flow) > 0 {
				fq, ferr := parseNumField(gr.Flow, "ml/min")
				if ferr != nil {
					diags = append(diags, Diagnostic{Code: "unconvertible", Layer: "gas", Message: fmt.Sprintf("event %d: flow %v", i, ferr)})
				}
				flow = fq
			}
			lv.Gas = append(lv.Gas, GasEvent{Idx: i, T: tq.Value, Gas: gr.Gas, Action: gr.Action, Flow: flow, Note: gr.Note})
		}
		sort.SliceStable(lv.Gas, func(a, b int) bool { return lv.Gas[a].T < lv.Gas[b].T })
		lv.Diagnostics = diags
	case LayerInitial:
		var ir initialRow
		if err := json.Unmarshal(raw, &ir); err != nil {
			return lv, fmt.Errorf("initial: invalid object: %w", err)
		}
		mass, err := parseNumField(ir.Mass, "mg")
		if err != nil {
			return lv, fmt.Errorf("initial mass: %w", err)
		}
		atT := 0.0
		if len(ir.AtT) > 0 {
			at, err := parseNumField(ir.AtT, "s")
			if err != nil {
				return lv, fmt.Errorf("initial at_t: %w", err)
			}
			atT = at.Value
		}
		tunit := ir.TUnit
		if tunit == "" {
			tunit = "s"
		}
		lv.Initial = &InitialMass{Mass: mass, AtT: atT, TUnit: tunit, Note: ir.Note}
	case LayerDrift:
		var dr driftRow
		if err := json.Unmarshal(raw, &dr); err != nil {
			return lv, fmt.Errorf("drift: invalid object: %w", err)
		}
		b0, e0 := parseNumField(dr.B0, "mg")
		b1, e1 := parseNumField(dr.B1, "mg/min")
		if e0 != nil || e1 != nil {
			return lv, fmt.Errorf("drift: b0/b1 invalid (%v/%v)", e0, e1)
		}
		dm := &DriftModel{B0: b0, B1: b1, BlankSpline: dr.BlankSpline, Note: dr.Note}
		if dr.UseBlank != nil {
			dm.UseBlank = *dr.UseBlank
		}
		lv.Drift = dm
	}
	return lv, nil
}
