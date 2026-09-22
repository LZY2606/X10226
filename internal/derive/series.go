package derive

import (
	"math"

	"tga/internal/domain"
)

func buildSeries(p domain.Params, in *Inputs) (domain.NormalizedSeries, []domain.RuleEvent) {
	var s domain.NormalizedSeries
	var rules []domain.RuleEvent
	n := len(in.Samples)
	s.Times = make([]float64, n)
	s.MassMG = make([]float64, n)
	s.MassPct = make([]float64, n)
	s.TempC = make([]float64, n)
	s.DTG = make([]float64, n)
	s.InterpMask = make([]bool, n)
	valid := make([]bool, n)

	// calibration curves
	dt, doff := calibrationDrift(in.Drift)
	bt, bm := calibrationBlank(in.Blank)
	correctedByDrift := false

	for i := range in.Samples {
		sp := in.Samples[i]
		tv, td := normalizeTime(sp.Time.Raw, sp.Time.Unit)
		mv, md := normalizeMass(sp.Mass.Raw, sp.Mass.Unit)
		ev, ed := normalizeTemp(sp.Temp.Raw, sp.Temp.Unit)
		if td != "" {
			s.Diagnostics = append(s.Diagnostics, domain.Diagnostic{Path: "samples[" + itoa(i) + "].time", Code: "BAD_UNIT", Message: td, KeptRaw: true})
		}
		if md != "" {
			s.Diagnostics = append(s.Diagnostics, domain.Diagnostic{Path: "samples[" + itoa(i) + "].mass", Code: "BAD_UNIT", Message: md, KeptRaw: true})
		}
		if ed != "" {
			s.Diagnostics = append(s.Diagnostics, domain.Diagnostic{Path: "samples[" + itoa(i) + "].temp", Code: "BAD_UNIT", Message: ed, KeptRaw: true})
		}
		if tv == nil || mv == nil || ev == nil {
			s.Times[i], s.MassMG[i], s.MassPct[i], s.TempC[i], s.DTG[i] = nan(), nan(), nan(), nan(), nan()
			s.Unknown = append(s.Unknown, i)
			continue
		}
		t, mass, temp := *tv, *mv, *ev
		if len(dt) > 0 {
			if off, ok := pieceLinear(dt, doff, t); ok {
				mass -= off
				correctedByDrift = true
			}
		}
		if len(bt) > 0 {
			if b, ok := pieceLinear(bt, bm, t); ok {
				mass -= b
			}
		}
		s.Times[i], s.MassMG[i], s.TempC[i] = t, mass, temp
		valid[i] = true
	}
	if len(s.Unknown) > 0 {
		rules = append(rules, domain.RuleEvent{
			Code: domain.RuleUnconvertibleKept, Severity: "warn",
			Message:  "unconvertible sample values kept in place with diagnostics; excluded from numeric plots",
			Evidence: map[string]any{"count": len(s.Unknown), "indices": s.Unknown},
		})
	}
	if correctedByDrift {
		rules = append(rules, domain.RuleEvent{
			Code: domain.RuleDriftCorrection, Severity: "info",
			Message:  "balance baseline drift subtracted pointwise from raw mass",
			Evidence: map[string]any{"drift_points": len(in.Drift)}, SourceIDs: sourceID(in, domain.KindDrift),
		})
	}

	// Detect sampling gaps: consecutive valid points farther apart than the
	// median cadence * gap factor / threshold. Interpolation never spans them.
	gaps := detectGaps(s.Times, valid, p.GapThreshold)
	s.Gaps = gaps
	for _, g := range gaps {
		s.DTG[g.StartIndex] = nan()
		s.DTG[g.EndIndex] = nan()
	}
	if len(gaps) > 0 {
		rules = append(rules, gapRule(gaps))
	}

	// Non-monotonic noise: small upward mass steps below threshold are flagged
	// but never silently removed.
	var noiseIdx []int
	for i := 1; i < n; i++ {
		if !valid[i] || !valid[i-1] || isGapBetween(gaps, i-1, i) {
			continue
		}
		if d := s.MassMG[i] - s.MassMG[i-1]; d > 0 && d <= p.NoiseThreshold {
			noiseIdx = append(noiseIdx, i)
		}
	}
	if len(noiseIdx) > 0 {
		rules = append(rules, domain.RuleEvent{
			Code: domain.RuleNoiseNonMonotonic, Severity: "warn",
			Message:  "non-monotonic positive mass steps at/below noise threshold flagged; values retained",
			Evidence: map[string]any{"indices": noiseIdx, "threshold_mg": p.NoiseThreshold},
		})
	}

	// Initial mass pinning and TG percentage.
	initial := resolveInitialMass(in, s.MassMG, valid)
	rules = append(rules, domain.RuleEvent{
		Code: domain.RuleInitialMassPinned, Severity: "info",
		Message: "TG percentage normalized against pinned initial mass version",
		Evidence: map[string]any{
			"initial_mg": round(initial, 6),
			"method":     initialMethod(in),
			"version":    in.Versions[domain.KindInitialMass],
		},
		SourceIDs: sourceID(in, domain.KindInitialMass),
	})
	for i := range s.MassMG {
		if !valid[i] {
			continue
		}
		if initial > 0 {
			s.MassPct[i] = s.MassMG[i] / initial * 100
		} else {
			s.MassPct[i] = nan()
		}
	}

	// DTG (mg/min) via central difference, NaN across gaps/unknown neighbors,
	// smoothed only inside contiguous runs (never bridging gaps).
	s.DTG = computeDTG(s.Times, s.MassMG, valid, gaps, p.WindowSize)
	return s, rules
}

func calibrationDrift(d []domain.DriftPoint) ([]float64, []float64) {
	var ts, vs []float64
	for i := range d {
		t, _ := normalizeTime(d[i].Time.Raw, d[i].Time.Unit)
		o, _ := normalizeMass(d[i].Offset.Raw, d[i].Offset.Unit)
		if t != nil && o != nil {
			ts = append(ts, *t)
			vs = append(vs, *o)
		}
	}
	return ts, vs
}

func calibrationBlank(b []domain.BlankPoint) ([]float64, []float64) {
	var ts, vs []float64
	for i := range b {
		t, _ := normalizeTime(b[i].Time.Raw, b[i].Time.Unit)
		m, _ := normalizeMass(b[i].Mass.Raw, b[i].Mass.Unit)
		if t != nil && m != nil {
			ts = append(ts, *t)
			vs = append(vs, *m)
		}
	}
	return ts, vs
}

func resolveInitialMass(in *Inputs, mass []float64, valid []bool) float64 {
	if in.InitialMass != nil && in.InitialMass.Value.Normalized != nil {
		return *in.InitialMass.Value.Normalized
	}
	for i, v := range valid {
		if v {
			return mass[i]
		}
	}
	return 0
}

func initialMethod(in *Inputs) string {
	if in.InitialMass != nil {
		return in.InitialMass.Method
	}
	return "first_valid_sample"
}

func sourceID(in *Inputs, k domain.SourceKind) []string {
	if _, ok := in.Versions[k]; !ok {
		return nil
	}
	return []string{string(k) + ":v" + itoa(in.Versions[k])}
}

func nan() float64 { return math.NaN() }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
