// Package compute is the pure, deterministic TGA derivation pipeline.
// It receives the layered raw inputs plus a parameter revision and returns
// candidates, rejections, unknowns and full provenance. It never mutates
// inputs and never interpolates across sampling gaps.
package compute

import (
	"fmt"
	"math"
	"sort"

	"tgareview/internal/domain"
)

// Input bundles the active layered inputs at one replay position.
type Input struct {
	ProjectID string
	AtSeq     int64
	Params    domain.Params
	Raw       []domain.RawPoint
	Drifts    []domain.DriftRow
	Blanks    []domain.BlankPoint
	Program   []domain.ProgramStep
	Gas       []domain.GasEvent
	Weight    []domain.WeightRow
	Notes     []domain.NotePayload
}

const (
	codeValveNotReaction = "valve_time_ne_reaction_time"
	codeStagesOverlap    = "stages_may_overlap"
	codeNoSum100         = "stage_loss_not_forced_to_100pct"
	codeGapNoInterp      = "gap_not_spanned_by_interpolation"
	codeWeightUnknown    = "initial_weight_unknown"
	codeAmbiguousGas     = "ambiguous_gas_assignment"
	codeMergedPeaks      = "overlapping_peaks_kept_separate"
)

// Run executes the pipeline and returns the materialized derived view.
func Run(in Input) *domain.DerivedView {
	p := in.Params
	view := &domain.DerivedView{
		ProjectID: in.ProjectID, AtSeq: in.AtSeq, Params: p,
		Status: domain.StatusCurrent,
	}
	noteCodes := noteCodes(in.Notes)
	baseProv := domain.Provenance{
		AlgorithmVersion:  p.AlgorithmVersion,
		ParamsFingerprint: p.Fingerprint(),
		NoteCodes:         noteCodes,
	}

	pts, diags := normalizePoints(in.Raw)
	view.Diagnostics = append(view.Diagnostics, diags...)

	driftRate := activeDrift(in.Drifts, p)
	blankAt := blankInterpolator(in.Blanks, p)

	// Corrected layer: corrected = raw - drift - blank (no gap interpolation).
	corr := make([]domain.CorrPoint, 0, len(pts))
	for _, pt := range pts {
		driftMg := driftRate * pt.TimeS.Value / 60.0
		blankMg, blankKnown := blankAt(pt.TimeS.Value)
		flag := ""
		if isNonMonotonic(pts, corr, pt, p) {
			flag = "noise"
			view.Diagnostics = append(view.Diagnostics,
				fmt.Sprintf("non-monotonic sample at t=%.0fs within noise tolerance", pt.TimeS.Value))
		}
		c := pt.MassMg.Value - driftMg
		if blankKnown {
			c -= blankMg * p.BlankScale
		}
		corr = append(corr, domain.CorrPoint{
			TimeS: pt.TimeS.Value, TempC: pt.TempC.Value,
			RawMassMg: pt.MassMg.Value, DriftMg: driftMg,
			BlankMg: blankMg, CorrectedMg: c, Flag: flag,
		})
	}
	view.Corr = corr

	// Gaps are first-class; DTG derivatives never span them.
	gaps := findGaps(corr, p, baseProv)
	view.Gaps = gaps
	inGap := gapSet(gaps)
	view.DTG = dtg(corr, inGap)

	view.Peaks = findPeaks(view.DTG, corr, p, baseProv)
	view.Atmosphere = atmosphereIntervals(in.Gas, p, baseProv)
	view.Stages, view.Diagnostics = buildStages(view.Peaks, view.Atmosphere, corr,
		in.Weight, in.Gas, p, baseProv, view.Diagnostics)
	view.Residues = residues(corr, in.Weight, gaps, in.Program, p, baseProv)
	view.InitialWeightKnown = activeWeight(in.Weight) != nil
	view.Prov = baseProv
	return view
}

func normalizePoints(raw []domain.RawPoint) ([]domain.RawPoint, []string) {
	var pts []domain.RawPoint
	var diags []string
	for _, rp := range raw {
		if rp.TimeS.Bad || rp.MassMg.Bad || rp.TempC.Bad {
			diags = append(diags, fmt.Sprintf("sample row %s kept but excluded from derivation: %s",
				rp.ID, firstDiag(rp)))
			continue
		}
		pts = append(pts, rp)
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].TimeS.Value < pts[j].TimeS.Value })
	return pts, diags
}

func firstDiag(rp domain.RawPoint) string {
	for _, q := range []domain.Q{rp.TimeS, rp.TempC, rp.MassMg} {
		if q.Bad {
			return q.Diag
		}
	}
	return "unconvertible value"
}

// isNonMonotonic flags a corrected mass increase within the noise tolerance.
// Such points remain in the series (never deleted); stages must not claim a
// positive loss across them without a note.
func isNonMonotonic(all []domain.RawPoint, corr []domain.CorrPoint, cur domain.RawPoint, p domain.Params) bool {
	if len(corr) == 0 {
		return false
	}
	prev := all[len(corr)-1]
	d := cur.MassMg.Value - prev.MassMg.Value
	return d > 0 && d <= p.NoiseTolMg
}

func activeDrift(drifts []domain.DriftRow, p domain.Params) float64 {
	var best *domain.DriftRow
	for i := range drifts {
		if drifts[i].Active {
			d := drifts[i]
			best = &d
		}
	}
	if best != nil && !best.RateMgPerMin.Bad {
		return best.RateMgPerMin.Value
	}
	return p.DriftRateMgPerMin
}

// blankInterpolator interpolates the empty-pan run by time. Values outside the
// blank range are unknown (zero, false) rather than extrapolated.
func blankInterpolator(blanks []domain.BlankPoint, p domain.Params) func(float64) (float64, bool) {
	var bp [][2]float64
	for _, b := range blanks {
		if !b.TimeS.Bad && !b.MassMg.Bad {
			bp = append(bp, [2]float64{b.TimeS.Value, b.MassMg.Value})
		}
	}
	sort.Slice(bp, func(i, j int) bool { return bp[i][0] < bp[j][0] })
	return func(t float64) (float64, bool) {
		if len(bp) == 0 || t < bp[0][0] || t > bp[len(bp)-1][0] {
			return 0, false
		}
		for i := 1; i < len(bp); i++ {
			if t <= bp[i][0] {
				x0, x1 := bp[i-1][0], bp[i][0]
				if x1 == x0 {
					return bp[i-1][1], true
				}
				f := (t - x0) / (x1 - x0)
				return bp[i-1][1] + f*(bp[i][1]-bp[i-1][1]), true
			}
		}
		return bp[len(bp)-1][1], true
	}
}

func findGaps(corr []domain.CorrPoint, p domain.Params, prov domain.Provenance) []domain.Gap {
	var gaps []domain.Gap
	for i := 1; i < len(corr); i++ {
		d := corr[i].TimeS - corr[i-1].TimeS
		if d > p.GapThresholdS {
			id := domain.ShortID("gap", f64(corr[i-1].TimeS), f64(corr[i].TimeS))
			gaps = append(gaps, domain.Gap{
				ID: id, StartS: corr[i-1].TimeS, EndS: corr[i].TimeS,
				Status: domain.StatusCurrent, Prov: withSources(prov,
					domain.Source{Kind: "derived", ID: id, Detail: codeGapNoInterp}),
			})
		}
	}
	return gaps
}

func gapSet(gaps []domain.Gap) map[[2]float64]bool {
	m := map[[2]float64]bool{}
	for _, g := range gaps {
		m[[2]float64{g.StartS, g.EndS}] = true
	}
	return m
}

// dtg uses a centered 3-point slope. Intervals that cross a gap are emitted
// with spanning_gap=true and never feed peak detection.
func dtg(corr []domain.CorrPoint, gaps map[[2]float64]bool) []domain.DTGPoint {
	out := make([]domain.DTGPoint, 0, len(corr))
	for i := 0; i < len(corr); i++ {
		if i == 0 || i == len(corr)-1 {
			out = append(out, domain.DTGPoint{TimeS: corr[i].TimeS, DMgPerS: 0})
			continue
		}
		a, b := corr[i-1], corr[i+1]
		span := gaps[[2]float64{a.TimeS, b.TimeS}] ||
			gaps[[2]float64{a.TimeS, corr[i].TimeS}] ||
			gaps[[2]float64{corr[i].TimeS, b.TimeS}]
		d := (b.CorrectedMg - a.CorrectedMg) / (b.TimeS - a.TimeS)
		out = append(out, domain.DTGPoint{
			TimeS: corr[i].TimeS, DMgPerS: d, Spanning: span,
		})
	}
	return out
}

func f64(v float64) string { return fmt.Sprintf("%g", v) }

func withSources(prov domain.Provenance, src ...domain.Source) domain.Provenance {
	prov.DirectSources = append(append([]domain.Source{}, prov.DirectSources...), src...)
	return prov
}

func noteCodes(notes []domain.NotePayload) []string {
	codes := []string{codeValveNotReaction, codeStagesOverlap, codeNoSum100, codeGapNoInterp}
	for _, n := range notes {
		codes = append(codes, n.Code)
	}
	return codes
}

func activeWeight(ws []domain.WeightRow) *domain.WeightRow {
	var best *domain.WeightRow
	for i := range ws {
		if ws[i].Active {
			w := ws[i]
			best = &w
		}
	}
	return best
}

func massAt(corr []domain.CorrPoint, t float64) (float64, bool) {
	if len(corr) == 0 || t < corr[0].TimeS || t > corr[len(corr)-1].TimeS {
		return 0, false
	}
	for i := 1; i < len(corr); i++ {
		if t <= corr[i].TimeS {
			return corr[i-1].CorrectedMg +
				(corr[i].CorrectedMg-corr[i-1].CorrectedMg)*
					(t-corr[i-1].TimeS)/(corr[i].TimeS-corr[i-1].TimeS), true
		}
	}
	return corr[len(corr)-1].CorrectedMg, true
}

func tempAt(corr []domain.CorrPoint, t float64) float64 {
	for i := 1; i < len(corr); i++ {
		if t <= corr[i].TimeS {
			f := 0.0
			if corr[i].TimeS != corr[i-1].TimeS {
				f = (t - corr[i-1].TimeS) / (corr[i].TimeS - corr[i-1].TimeS)
			}
			return corr[i-1].TempC + f*(corr[i].TempC-corr[i-1].TempC)
		}
	}
	if len(corr) > 0 {
		return corr[len(corr)-1].TempC
	}
	return math.NaN()
}
