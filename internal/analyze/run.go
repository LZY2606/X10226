package analyze

import (
	"fmt"
	"math"

	"tga-review/internal/model"
)

// Machine-readable finding codes. These are part of the stable archive format.
const (
	CodeAtmosphereDelay     = "ATMOSPHERE_DELAY_VALVE_NE_REACTION"
	CodeStagesMayOverlap    = "STAGES_MAY_OVERLAP_NOT_SUM_100"
	CodeGapNoInterpolation  = "GAP_NO_INTERPOLATION"
	CodeBalanceDrift        = "BALANCE_BASELINE_DRIFT_CORRECTED"
	CodeNonMonotonicNoise   = "NONMONOTONIC_MASS_UPTICK_NOISE"
	CodeOverlappingPeaks    = "OVERLAPPING_PEAKS_MULTIPLE_CANDIDATES"
	CodeRejectSmallLoss     = "REJECT_STAGE_LOSS_BELOW_THRESHOLD"
	CodeRejectSpansGap      = "REJECT_CANDIDATE_SPANS_GAP"
	CodeInitialUnknown      = "INITIAL_MASS_UNKNOWN_RESIDUE_UNKNOWN"
	CodeUnconvertibleOrigin = "UNCONVERTIBLE_VALUE_HELD_AT_ORIGIN"
	CodeHalfFailedLayer     = "LAYER_HALF_FAILED_PRIOR_ACTIVE_KEPT"
)

// Run executes tga-stage-v1 against the input snapshot.
func Run(in Inputs, params Params) RunResult {
	r := RunResult{
		Params:           params,
		ParamFingerprint: params.Fingerprint(),
		InputFingerprint: in.Fingerprint(),
		Gaps:             append([]model.Gap(nil), in.Gaps...),
	}

	// Initial mass (active declaration; revisions supersede upstream).
	if len(in.Initial) > 0 && in.Initial[len(in.Initial)-1].MassS != nil {
		m := *in.Initial[len(in.Initial)-1].MassS
		r.InitialMassMg = &m
		r.InitialBasis = in.Initial[len(in.Initial)-1].Basis
		r.InitialVersionID = in.Version[model.KindInitial]
		r.InitialStatus = "ok"
	} else {
		r.InitialStatus = "unknown"
		r.Findings = append(r.Findings, Finding{
			Code: CodeInitialUnknown, Severity: "warn",
			Subject: "initial_mass",
			Message: "no convertible initial mass; residue and TG% stay unknown",
		})
	}

	// Correction curves (already unit-normalized by the importer convention).
	var dts, dvs []float64
	for _, d := range in.Drift {
		if d.TimeS != nil && d.DriftS != nil {
			dts, dvs = append(dts, *d.TimeS), append(dvs, *d.DriftS)
		}
	}
	dts, dvs = sortedNodes(dts, dvs)
	driftUsed := len(dts) > 0
	if driftUsed {
		r.Findings = append(r.Findings, Finding{
			Code: CodeBalanceDrift, Severity: "info", Subject: "drift_correction",
			Message: "balance baseline drift interpolated and subtracted at sample times",
			Detail:  map[string]interface{}{"nodes": len(dts)},
		})
	}
	var bts, bvs []float64
	for _, b := range in.Blank {
		if b.TimeS != nil && b.MassS != nil {
			bts, bvs = append(bts, *b.TimeS), append(bvs, *b.MassS)
		}
	}
	bts, bvs = sortedNodes(bts, bvs)

	// Normalized points; unconvertible sample rows are skipped here but remain
	// upstream in raw storage with a diagnostic (never deleted for plotting).
	badRows := 0
	for _, sp := range in.Samples {
		if sp.TimeS == nil || sp.TempS == nil || sp.MassS == nil {
			badRows++
			continue
		}
		r.Points = append(r.Points, Point{
			Seq: sp.Seq, TimeMin: *sp.TimeS, TempC: *sp.TempC, RawMassMg: *sp.MassS,
		})
	}
	if badRows > 0 {
		r.Findings = append(r.Findings, Finding{
			Code: CodeUnconvertibleOrigin, Severity: "warn", Subject: "samples",
			Message: fmt.Sprintf("%d sample row(s) unconvertible; held at origin, excluded from derived curves", badRows),
			Detail:  map[string]interface{}{"rows": badRows},
		})
	}

	assignSegments(&r)
	applyCorrections(&r, dts, dvs, bts, bvs)
	computeTG(&r, params)
	computeDTG(&r)
	alignFurnace(&r, in)
	alignGas(&r, in, params)
	findCandidates(&r, params)
	computeResidue(&r)
	return r
}

// assignSegments labels each point with a contiguous segment id; a declared
// gap breaks segments so no derivative or candidate ever crosses it.
func assignSegments(r *RunResult) {
	seg := 0
	for i := range r.Points {
		if i > 0 {
			t0, t1 := r.Points[i-1].TimeMin, r.Points[i].TimeMin
			for _, g := range r.Gaps {
				if t0 >= g.FromS-1e-9 && t1 <= g.ToS+1e-9 {
					seg++
					break
				}
			}
		}
		r.Points[i].Segment = seg
	}
	if len(r.Gaps) > 0 {
		r.Findings = append(r.Findings, Finding{
			Code: CodeGapNoInterpolation, Severity: "info", Subject: "gaps",
			Message: fmt.Sprintf("%d declared gap(s); DTG and stage candidates never interpolate across", len(r.Gaps)),
			Detail:  map[string]interface{}{"gaps": len(r.Gaps)},
		})
	}
}

func applyCorrections(r *RunResult, dts, dvs, bts, bvs []float64) {
	for i := range r.Points {
		p := &r.Points[i]
		if len(dts) > 0 {
			p.DriftMg = interp(dts, dvs, p.TimeMin)
		}
		if len(bts) > 0 {
			p.BlankMg = interp(bts, bvs, p.TimeMin)
		}
		p.CorrectedMg = p.RawMassMg - p.DriftMg - p.BlankMg
	}
}

func computeTG(r *RunResult, params Params) {
	if r.InitialMassMg == nil || *r.InitialMassMg == 0 {
		return
	}
	m0 := *r.InitialMassMg
	for i := range r.Points {
		p := &r.Points[i]
		p.TGPct = 100 * p.CorrectedMg / m0
		if i > 0 {
			prev := r.Points[i-1]
			if p.Segment == prev.Segment && p.CorrectedMg > prev.CorrectedMg {
				delta := 100 * (p.CorrectedMg - prev.CorrectedMg) / m0
				if math.Abs(delta) > params.NonMonotonicTolPct {
					p.NonMonotonic = true
					p.NonMonotonicDeltaPct = delta
				}
			}
		}
		r.Corrections = append(r.Corrections, CorrectionStep{
			Seq: p.Seq, TimeMin: p.TimeMin, RawMg: p.RawMassMg,
			DriftMg: p.DriftMg, BlankMg: p.BlankMg, CorrMg: p.CorrectedMg,
			InitialMg: m0, TGPct: p.TGPct,
		})
	}
	noisy := 0
	for _, p := range r.Points {
		if p.NonMonotonic {
			noisy++
		}
	}
	if noisy > 0 {
		r.Findings = append(r.Findings, Finding{
			Code: CodeNonMonotonicNoise, Severity: "info", Subject: "tg_curve",
			Message: fmt.Sprintf("%d non-monotonic uptick(s) beyond tolerance; treated as noise, not mass gain", noisy),
			Detail:  map[string]interface{}{"points": noisy, "tolerance_pct": params.NonMonotonicTolPct},
		})
	}
}

// computeDTG uses a central difference within each contiguous segment only.
func computeDTG(r *RunResult) {
	for i := range r.Points {
		if i == 0 || i == len(r.Points)-1 {
			continue
		}
		a, b := r.Points[i-1], r.Points[i+1]
		if a.Segment != r.Points[i].Segment || b.Segment != r.Points[i].Segment {
			continue
		}
		dt := b.TimeMin - a.TimeMin
		if dt <= 0 || r.InitialMassMg == nil || *r.InitialMassMg == 0 {
			continue
		}
		r.Points[i].DTGPctMin = 100 * (b.CorrectedMg - a.CorrectedMg) / dt / *r.InitialMassMg
	}
}
