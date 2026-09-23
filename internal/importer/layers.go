package importer

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"tga-review/internal/domain"
)

// ValidateLayers parses every present layer independently. Each present
// layer yields one LayerResult; errors never abort sibling layers.
func ValidateLayers(b *Bundle) []LayerResult {
	var out []LayerResult
	out = append(out, parseSamples(b.Samples))
	out = append(out, parseDrift(b.Drift))
	out = append(out, parseFurnace(b.Furnace))
	out = append(out, parseGas(b.Gas))
	out = append(out, parseBlank(b.Blank))
	out = append(out, parseInitial(b.Initial))
	out = append(out, parseParams(b.Params))
	return out
}

func result(layer string, present bool, payload any, diags []domain.Diagnostic) LayerResult {
	accepted := present
	sev := domain.StatusOK
	_ = sev
	for _, d := range diags {
		if d.Severity == domain.SevError {
			accepted = false
		}
	}
	raw, _ := json.Marshal(payload)
	return LayerResult{
		Layer: layer, Present: present, Accepted: accepted,
		Domain: payload, Diagnostics: diags,
		Source: domain.Source{Layer: layer, Raw: raw,
			Status: map[bool]string{true: "accepted", false: "rejected"}[accepted],
			Diagnostics: diags},
	}
}

func diag(code, sev, layer, msg string, args ...any) domain.Diagnostic {
	return domain.Diagnostic{Code: code, Severity: sev, Layer: layer, Message: fmt.Sprintf(msg, args...)}
}

func parseSamples(l *SamplesLayer) LayerResult {
	if l == nil {
		return LayerResult{Layer: domain.LayerSamples}
	}
	points := make([]domain.RawPoint, 0, len(l.Points))
	var diags []domain.Diagnostic
	if len(l.Points) == 0 {
		diags = append(diags, diag("SAMPLES_EMPTY", domain.SevError, domain.LayerSamples, "no sample points"))
	}
	var lastT float64
	haveLast := false
	for i, row := range l.Points {
		p := domain.RawPoint{Seq: i, TimeRaw: row.Time, TempRaw: row.Temp, MassRaw: row.Mass}
		p.Time = parseQuantity(row.Time, row.TimeUnit, domain.UnitSeconds, toSeconds, toMG, toCelsius)
		p.Temp = parseQuantity(row.Temp, row.TempUnit, domain.UnitCelsius, toSeconds, toMG, toCelsius)
		p.Mass = parseQuantity(row.Mass, row.MassUnit, domain.UnitMG, toSeconds, toMG, toCelsius)
		for cell, n := range map[string]domain.Number{"time": p.Time, "temp": p.Temp, "mass": p.Mass} {
			if n.Status != domain.StatusOK {
				diags = append(diags, diag("BAD_CELL:"+cell, sevFor(n.Status), domain.LayerSamples,
					"row %d %s: %s", i, cell, n.DiagMsg))
			}
		}
		if p.Time.Status == domain.StatusOK {
			if haveLast {
				if p.Time.Value < lastT {
					diags = append(diags, diag("TIME_NOT_MONOTONIC", domain.SevError, domain.LayerSamples,
						"row %d time %.3f precedes previous %.3f", i, p.Time.Value, lastT))
				} else if p.Time.Value-lastT > domain.GapThresholdSec {
					p.GapAfter = false // marked relative to previous point below
				}
			}
			lastT, haveLast = p.Time.Value, true
		}
		points = append(points, p)
	}
	// mark gaps between consecutive valid times
	for i := 1; i < len(points); i++ {
		if points[i].Time.Status == domain.StatusOK && points[i-1].Time.Status == domain.StatusOK &&
			points[i].Time.Value-points[i-1].Time.Value > domain.GapThresholdSec {
			points[i-1].GapAfter = true
			diags = append(diags, diag("SAMPLING_GAP", domain.SevWarning, domain.LayerSamples,
				"gap of %.1fs between rows %d and %d; no interpolation crosses it",
				points[i].Time.Value-points[i-1].Time.Value, i-1, i))
		}
	}
	// non-monotonic mass beyond noise floor is flagged (not deleted)
	for i := 1; i < len(points); i++ {
		if points[i].Mass.Status == domain.StatusOK && points[i-1].Mass.Status == domain.StatusOK &&
			points[i].Mass.Value > points[i-1].Mass.Value {
			points[i].NonMono = true
			diags = append(diags, diag("NON_MONOTONIC_MASS", domain.SevInfo, domain.LayerSamples,
				"row %d mass rises from %.4f to %.4f mg; retained as noise observation",
				i, points[i-1].Mass.Value, points[i].Mass.Value))
		}
	}
	return result(domain.LayerSamples, true, points, diags)
}

func sevFor(status string) string {
	if status == domain.StatusUnknown {
		return domain.SevWarning
	}
	return domain.SevError
}

func parseDrift(l *DriftLayer) LayerResult {
	if l == nil {
		return LayerResult{Layer: domain.LayerDrift}
	}
	var diags []domain.Diagnostic
	model := strings.TrimSpace(strings.ToLower(l.Model))
	switch model {
	case "blank_subtract", "linear", "none":
	default:
		diags = append(diags, diag("DRIFT_BAD_MODEL", domain.SevError, domain.LayerDrift,
			"unknown drift model %q", l.Model))
	}
	if model == "linear" && l.RateMGMin == 0 {
		diags = append(diags, diag("DRIFT_RATE_ZERO", domain.SevWarning, domain.LayerDrift,
			"linear drift model has zero rate; correction will be identity"))
	}
	d := domain.DriftSpec{Model: model, RateMgMin: l.RateMGMin, BlankID: l.BlankRef}
	return result(domain.LayerDrift, true, d, diags)
}

func parseFurnace(l *FurnaceLayer) LayerResult {
	if l == nil {
		return LayerResult{Layer: domain.LayerFurnace}
	}
	var diags []domain.Diagnostic
	segs := make([]domain.FurnaceSegment, 0, len(l.Segments))
	for i, row := range l.Segments {
		startN := parseQuantity(row.Start, "", domain.UnitSeconds, toSeconds, toMG, toCelsius)
		endN := parseQuantity(row.End, "", domain.UnitSeconds, toSeconds, toMG, toCelsius)
		fromN := parseQuantity(row.From, "", domain.UnitCelsius, toSeconds, toMG, toCelsius)
		toN := parseQuantity(row.To, "", domain.UnitCelsius, toSeconds, toMG, toCelsius)
		for _, n := range []domain.Number{startN, endN, fromN, toN} {
			if n.Status != domain.StatusOK {
				diags = append(diags, diag("FURNACE_BAD_CELL", domain.SevError, domain.LayerFurnace,
					"segment %d: %s", i, n.DiagMsg))
			}
		}
		kind := strings.ToLower(strings.TrimSpace(row.Kind))
		if kind != "ramp" && kind != "hold" {
			diags = append(diags, diag("FURNACE_BAD_KIND", domain.SevError, domain.LayerFurnace,
				"segment %d unknown kind %q", i, row.Kind))
		}
		var rate float64
		if startN.Status == domain.StatusOK && endN.Status == domain.StatusOK && endN.Value > startN.Value {
			rate = (toN.Value - fromN.Value) / ((endN.Value - startN.Value) / 60.0)
		}
		segs = append(segs, domain.FurnaceSegment{
			Seq: i, Kind: kind, StartSec: startN.Value, EndSec: endN.Value,
			FromC: fromN.Value, ToC: toN.Value, RateCMin: rate,
			ID: fmt.Sprintf("furn-seg-%d", i),
		})
	}
	sort.SliceStable(segs, func(i, j int) bool { return segs[i].StartSec < segs[j].StartSec })
	return result(domain.LayerFurnace, true, segs, diags)
}

func parseGas(l *GasLayer) LayerResult {
	if l == nil {
		return LayerResult{Layer: domain.LayerGas}
	}
	var diags []domain.Diagnostic
	evs := make([]domain.GasEvent, 0, len(l.Events))
	for i, row := range l.Events {
		t := parseQuantity(row.Time, "", domain.UnitSeconds, toSeconds, toMG, toCelsius)
		if t.Status != domain.StatusOK {
			diags = append(diags, diag("GAS_BAD_TIME", domain.SevError, domain.LayerGas,
				"event %d: %s", i, t.DiagMsg))
		}
		if strings.TrimSpace(row.Gas) == "" {
			diags = append(diags, diag("GAS_NO_NAME", domain.SevError, domain.LayerGas, "event %d has no gas", i))
		}
		evs = append(evs, domain.GasEvent{
			Seq: i, TimeSec: t.Value, Gas: strings.TrimSpace(row.Gas),
			FlowSCCM: row.FlowSCCM, Note: row.Note, ID: fmt.Sprintf("gas-%d", i),
		})
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].TimeSec < evs[j].TimeSec })
	return result(domain.LayerGas, true, evs, diags)
}

func parseBlank(l *BlankLayer) LayerResult {
	if l == nil {
		return LayerResult{Layer: domain.LayerBlank}
	}
	var diags []domain.Diagnostic
	pts := make([]domain.BlankPoint, 0, len(l.Points))
	for i, row := range l.Points {
		t := parseQuantity(row.Time, "", domain.UnitSeconds, toSeconds, toMG, toCelsius)
		m := parseQuantity(row.Mass, "", domain.UnitMG, toSeconds, toMG, toCelsius)
		if t.Status != domain.StatusOK || m.Status != domain.StatusOK {
			diags = append(diags, diag("BLANK_BAD_CELL", domain.SevError, domain.LayerBlank,
				"point %d: time=%q mass=%q", i, row.Time, row.Mass))
		}
		pts = append(pts, domain.BlankPoint{Seq: i, TimeSec: t.Value, MassMg: m.Value, ID: fmt.Sprintf("blank-%d", i)})
	}
	return result(domain.LayerBlank, true, pts, diags)
}

func parseInitial(l *InitialLayer) LayerResult {
	if l == nil {
		return LayerResult{Layer: domain.LayerInitial}
	}
	var diags []domain.Diagnostic
	recs := make([]domain.InitialMass, 0, len(l.Records))
	for i, row := range l.Records {
		m := parseQuantity(row.Mass, "", domain.UnitMG, toSeconds, toMG, toCelsius)
		at := parseQuantity(row.At, "", domain.UnitSeconds, toSeconds, toMG, toCelsius)
		if m.Status != domain.StatusOK {
			diags = append(diags, diag("INITIAL_BAD_MASS", domain.SevError, domain.LayerInitial,
				"record %d: %s", i, m.DiagMsg))
		}
		basis := strings.TrimSpace(row.Basis)
		if basis == "" {
			diags = append(diags, diag("INITIAL_NO_BASIS", domain.SevWarning, domain.LayerInitial,
				"record %d lacks basis; defaulting to instrument", i))
			basis = "instrument"
		}
		recs = append(recs, domain.InitialMass{
			MassMg: m.Value, Basis: basis, AtSec: at.Value, Note: row.Note,
			ID: fmt.Sprintf("initial-%s-%d", basis, i),
		})
	}
	return result(domain.LayerInitial, true, recs, diags)
}

func parseParams(l *ParamsLayer) LayerResult {
	if l == nil {
		return LayerResult{Layer: domain.LayerParams}
	}
	var diags []domain.Diagnostic
	if l.AtmosDelaySec < 0 || l.TempLagSec < 0 {
		diags = append(diags, diag("PARAMS_NEGATIVE_DELAY", domain.SevError, domain.LayerParams,
			"delays must be non-negative"))
	}
	p := domain.Params{
		AtmosDelaySec: l.AtmosDelaySec, TempLagSec: l.TempLagSec,
		OverlapMergeC: l.OverlapMergeC, NoiseFloorMG: l.NoiseFloorMG, PeakPromMGMin: l.PeakPromMGMin,
	}
	if p.OverlapMergeC == 0 {
		p.OverlapMergeC = 40
	}
	if p.NoiseFloorMG == 0 {
		p.NoiseFloorMG = 0.02
	}
	if p.PeakPromMGMin == 0 {
		p.PeakPromMGMin = 0.05
	}
	return result(domain.LayerParams, true, p, diags)
}
