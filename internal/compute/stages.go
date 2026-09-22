package compute

import (
	"fmt"
	"math"

	"tgasys/internal/domain"
)

type dtgPeak struct {
	id      string
	t       float64
	prom    float64
	leftT   float64
	rightT  float64
	overlap bool
}

// buildStages finds DTG minima (loss peaks), splits overlaps, and keeps all
// legal candidates when a boundary cannot be uniquely decided.
func buildStages(dtg []domain.DTGPoint, tg []domain.TGPoint, atm []domain.AtmSegment, furnace []domain.FurnacePoint, gaps []domain.Gap, p *domain.Parameters, resultID string) ([]domain.StageCandidate, []domain.Finding) {
	var findings []domain.Finding
	peaks := findPeaks(dtg, p, gaps)
	if len(peaks) == 0 {
		findings = append(findings, domain.Finding{Code: "stage.no_peak", Severity: "warning", Message: "no DTG peak exceeded prominence"})
		return nil, findings
	}

	// Mark overlapping peaks (valleys between them never return near zero).
	for i := 0; i < len(peaks); i++ {
		for j := i + 1; j < len(peaks); j++ {
			if peaksOverlap(peaks[i], peaks[j], dtg, p) {
				peaks[i].overlap = true
				peaks[j].overlap = true
			}
		}
	}
	var overlapCount int
	for _, pk := range peaks {
		if pk.overlap {
			overlapCount++
		}
	}
	if overlapCount > 0 {
		findings = append(findings, domain.Finding{
			Code: "peak.overlap", Severity: "warning",
			Message: fmt.Sprintf("%d peaks overlap; intermediate valley does not return to baseline; all legal splits retained", overlapCount),
			Data:    map[string]any{"peaks": overlapCount, "merge_gap_s": p.MergeGapS},
		})
	}

	stages := make([]domain.StageCandidate, 0, len(peaks))
	for i, pk := range peaks {
		startT := pk.leftT
		endT := pk.rightT
		loss := lossBetween(tg, startT, endT)
		gas, reactionT := atmAt(atm, pk.t, p.AtmDelayS)
		cand := domain.StageCandidate{
			ID:        fmt.Sprintf("%s-stage-%d", resultID, i+1),
			Index:     i + 1,
			StartT:    startT,
			EndT:      endT,
			PeakT:     pk.t,
			ReactionT: reactionT,
			AtmGas:    gas,
			LossPct:   loss.pct,
			LossPctErr: loss.err,
			Basis:     "percent_of_declared_initial_mass; stages may overlap; not normalized to 100%",
		}
		if pk.overlap {
			cand.Ambiguous = true
			// Two legal boundaries: valley minimum vs merge-gap midpoint.
			alt := cand
			alt.ID = cand.ID + "-alt"
			mid := midpointBoundary(peaks, i, dtg)
			if !math.IsNaN(mid) {
				altL := lossBetween(tg, startT, mid)
				alt.StartT = mid // alt split shifts boundary (recorded separately)
				alt.LossPct = altL.pct
				alt.LossPctErr = altL.err
				cand.AlternativeIDs = append(cand.AlternativeIDs, alt.ID)
				alt.AlternativeIDs = append(alt.AlternativeIDs, cand.ID)
				stages = append(stages, cand, alt)
				continue
			}
		}
		if math.Abs(loss.err) > 0 {
			cand.LossPctErr = loss.err
		}
		stages = append(stages, cand)
	}

	// Explicit non-100% guarantee finding.
	total := 0.0
	merged := map[string]bool{}
	for _, s := range stages {
		if !merged[s.ID] {
			total += s.LossPct
		}
	}
	final := tgAt(tg, lastFiniteT(tg))
	findings = append(findings, domain.Finding{
		Code: "stage.sum_not_forced", Severity: "info",
		Message: fmt.Sprintf("sum of stage losses %.2f%% plus final residue %.2f%% is not forced to equal 100%% (overlap/noise/unknown allowed)", total, final),
		Data: map[string]any{
			"stage_sum_pct":      total,
			"final_residue_pct":  final,
			"normalized_to_100":  false,
		},
	})
	return stages, findings
}

func findPeaks(dtg []domain.DTGPoint, p *domain.Parameters, gaps []domain.Gap) []dtgPeak {
	var vals []float64
	for _, d := range dtg {
		if !math.IsNaN(d.Value) {
			vals = append(vals, d.Value)
		}
	}
	if len(vals) < 5 {
		return nil
	}
	minV, maxV := vals[0], vals[0]
	for _, v := range vals {
		minV = math.Min(minV, v)
		maxV = math.Max(maxV, v)
	}
	span := math.Max(math.Abs(maxV-minV), 1e-9)
	promAbs := p.PeakPromPct * span / 100 * 12 // prominence scaled from percent param
	_ = promAbs
	var peaks []dtgPeak
	for i := 2; i < len(dtg)-2; i++ {
		if math.IsNaN(dtg[i].Value) {
			continue
		}
		v := dtg[i].Value
		if v >= dtg[i-1].Value || v > dtg[i+1].Value {
			continue
		}
		// prominence: climb both sides until a higher neighbor window
		l := climbLeft(dtg, i)
		r := climbRight(dtg, i)
		prom := math.Min(l, r) - v
		if prom < p.PeakPromPct {
			continue
		}
		leftT := zeroCrossingLeft(dtg, i)
		rightT := zeroCrossingRight(dtg, i)
		if spansGap(leftT, rightT, gaps) {
			// keep peak but mark endpoints at gap edges (no crossing)
		}
		peaks = append(peaks, dtgPeak{
			id: fmt.Sprintf("peak-%d", len(peaks)+1), t: dtg[i].T,
			prom: prom, leftT: leftT, rightT: rightT,
		})
	}
	// de-duplicate peaks closer than merge window: keep stronger
	var out []dtgPeak
	for _, pk := range peaks {
		repl := false
		for k := range out {
			if math.Abs(out[k].t-pk.t) < p.MergeGapS {
				if pk.prom > out[k].prom {
					out[k] = pk
				}
				repl = true
				break
			}
		}
		if !repl {
			out = append(out, pk)
		}
	}
	return out
}

func climbLeft(d []domain.DTGPoint, i int) float64 {
	base := d[i].Value
	for j := i - 1; j >= 0; j-- {
		if math.IsNaN(d[j].Value) {
			return base
		}
		if d[j].Value > base {
			return d[j].Value
		}
	}
	return base
}

func climbRight(d []domain.DTGPoint, i int) float64 {
	base := d[i].Value
	for j := i + 1; j < len(d); j++ {
		if math.IsNaN(d[j].Value) {
			return base
		}
		if d[j].Value > base {
			return d[j].Value
		}
	}
	return base
}

func zeroCrossingLeft(d []domain.DTGPoint, i int) float64 {
	for j := i - 1; j > 0; j-- {
		if math.IsNaN(d[j].Value) {
			return d[j+1].T
		}
		if d[j].Value >= 0 {
			return d[j].T
		}
	}
	return d[0].T
}

func zeroCrossingRight(d []domain.DTGPoint, i int) float64 {
	for j := i + 1; j < len(d)-1; j++ {
		if math.IsNaN(d[j].Value) {
			return d[j-1].T
		}
		if d[j].Value >= 0 {
			return d[j].T
		}
	}
	return d[len(d)-1].T
}

func peaksOverlap(a, b dtgPeak, d []domain.DTGPoint, p *domain.Parameters) bool {
	if math.Abs(a.t-b.t) > 600 {
		return false
	}
	lo, hi := int(0), len(d)-1
	for k, q := range d {
		if !math.IsNaN(q.Value) && q.T <= math.Max(a.t, b.t) && q.T >= math.Min(a.t, b.t) {
			_ = k
		}
	}
	_ = lo
	_ = hi
	// find valley between the peaks
	iA, iB := nearestIndex(d, a.t), nearestIndex(d, b.t)
	if iA > iB {
		iA, iB = iB, iA
	}
	valley := math.Inf(-1)
	for k := iA; k <= iB; k++ {
		if math.IsNaN(d[k].Value) {
			continue
		}
		valley = math.Max(valley, d[k].Value)
	}
	// overlap if valley stays strongly negative (never approaches zero loss)
	return valley < -p.PeakPromPct*0.6
}

func nearestIndex(d []domain.DTGPoint, t float64) int {
	best, bd := 0, math.Abs(d[0].T-t)
	for i := range d {
		if dd := math.Abs(d[i].T - t); dd < bd {
			best, bd = i, dd
		}
	}
	return best
}

func midpointBoundary(peaks []dtgPeak, i int, d []domain.DTGPoint) float64 {
	if i+1 >= len(peaks) || !peaks[i].overlap {
		return math.NaN()
	}
	a, b := nearestIndex(d, peaks[i].t), nearestIndex(d, peaks[i+1].t)
	if a > b {
		a, b = b, a
	}
	idx, best := a, math.Inf(-1)
	for k := a; k <= b; k++ {
		if math.IsNaN(d[k].Value) {
			continue
		}
		if d[k].Value > best {
			best, idx = d[k].Value, k
		}
	}
	return d[idx].T
}

type lossEstimate struct{ pct, err float64 }

func lossBetween(tg []domain.TGPoint, a, b float64) lossEstimate {
	ma := tgAt(tg, a)
	mb := tgAt(tg, b)
	if math.IsNaN(ma) || math.IsNaN(mb) {
		return lossEstimate{pct: math.NaN(), err: math.NaN()}
	}
	return lossEstimate{pct: ma - mb, err: 0}
}

func tgAt(tg []domain.TGPoint, t float64) float64 {
	if len(tg) == 0 {
		return math.NaN()
	}
	for i := range tg {
		if math.Abs(tg[i].T-t) < 1e-9 {
			return tg[i].Pct
		}
		if i > 0 && tg[i-1].T <= t && t <= tg[i].T {
			// No interpolation across gap edges: return nearest endpoint value.
			if tg[i-1].Sparse || tg[i].Sparse {
				if t-tg[i-1].T < tg[i].T-t {
					return tg[i-1].Pct
				}
				return tg[i].Pct
			}
			f := (t - tg[i-1].T) / (tg[i].T - tg[i-1].T)
			return tg[i-1].Pct + f*(tg[i].Pct-tg[i-1].Pct)
		}
	}
	if t <= tg[0].T {
		return tg[0].Pct
	}
	return tg[len(tg)-1].Pct
}

func lastFiniteT(tg []domain.TGPoint) float64 {
	for i := len(tg) - 1; i >= 0; i-- {
		if !tg[i].InGap {
			return tg[i].T
		}
	}
	if len(tg) > 0 {
		return tg[len(tg)-1].T
	}
	return 0
}

func atmAt(atm []domain.AtmSegment, peakT, delay float64) (string, float64) {
	for _, s := range atm {
		if peakT >= s.StartT && (math.IsInf(s.EndT, 1) || peakT < s.EndT) {
			// Effective valve switch explaining gas present at the sample.
			return s.Gas, s.ValveT
		}
	}
	return "", peakT
}
