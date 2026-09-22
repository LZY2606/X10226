package compute

import (
	"fmt"

	"tgareview/internal/domain"
)

// buildStages produces decomposition candidates. Candidates may overlap and
// their loss percentages are never normalized to sum to 100%. Every rejected
// candidate is retained with an explicit machine-readable reason.
func buildStages(peaks []domain.Peak, atms []domain.AtmosphereInterval,
	corr []domain.CorrPoint, weights []domain.WeightRow, gas []domain.GasEvent,
	p domain.Params, prov domain.Provenance, diags []string,
) ([]domain.StageCandidate, []string) {
	w := activeWeight(weights)
	var stages []domain.StageCandidate

	windowFor := func(pk domain.Peak) (float64, float64) {
		i := indexAtTime(corr, pk.TimeS)
		l, r := i, i
		for l > 0 && slopeAt(corr, l) < 0 {
			l--
		}
		for r < len(corr)-1 && slopeAt(corr, r+1) < 0 {
			r++
		}
		// Do not extend a boundary across a sampling gap.
		for l > 0 && spansGap(corr[l-1].TimeS, corr[l].TimeS, p) {
			l++
		}
		for r < len(corr)-1 && spansGap(corr[r].TimeS, corr[r+1].TimeS, p) {
			r--
		}
		if l >= r {
			return corr[i].TimeS, corr[i].TimeS
		}
		return corr[l].TimeS, corr[r].TimeS
	}

	covered := func(t float64) bool {
		for _, s := range stages {
			if t >= s.StartS && t <= s.EndS {
				return true
			}
		}
		return false
	}

	for _, pk := range peaks {
		start, end := windowFor(pk)
		ids := stageID("peak", start, end, pk.ID)
		st := domain.StageCandidate{
			ID: ids, StartS: start, EndS: end, PeakID: pk.ID, Origin: "peak",
			Status: domain.StatusCurrent,
			Prov: withSources(prov,
				domain.Source{Kind: "derived", ID: pk.ID, Layer: "dtg_peak"},
				domain.Source{Kind: "derived", ID: ids, Detail: codeStagesOverlap},
				domain.Source{Kind: "derived", ID: ids, Detail: codeNoSum100}),
		}
		fillStage(&st, corr, w, atms, p)
		if pk.Rejected != nil {
			st.Rejected = &domain.Rejection{
				Code:    "peak_below_prominence",
				Message: "stage source peak " + pk.ID + " rejected: " + pk.Rejected.Message,
			}
		}
		stages = append(stages, st)
	}

	// Gas-switch candidates: a valve change can drive a reaction even without
	// a DTG peak. Reaction time = valve time + delay; flagged ambiguous when
	// the window overlaps another boundary.
	for _, e := range gas {
		rt := e.ValveAtS.Value + p.AtmosphereDelayS
		if covered(rt) {
			continue
		}
		start, end := bracket(corr, rt, p)
		if end <= start {
			continue
		}
		id := domain.ShortID("stage-gas", f64(e.ValveAtS.Value))
		st := domain.StageCandidate{
			ID: id, StartS: start, EndS: end, Origin: "gas",
			Status: domain.StatusCurrent,
			Prov: withSources(prov,
				domain.Source{Kind: "row", ID: e.ID, Layer: "gas_valve"},
				domain.Source{Kind: "derived", ID: id, Detail: codeValveNotReaction}),
		}
		fillStage(&st, corr, w, atms, p)
		st.Ambiguous = gasAt(atms, rt, p.AtmosphereDelayS)
		if len(st.Ambiguous) > 1 {
			st.Rejected = &domain.Rejection{
				Code:    codeAmbiguousGas,
				Message: fmt.Sprintf("reaction at t=%.0fs may belong to %v", rt, st.Ambiguous),
			}
			diags = append(diags, st.Rejected.Message)
		}
		stages = append(stages, st)
	}

	// Overlap report (first-class diagnostic): stages may legitimately overlap.
	for i := 0; i < len(stages); i++ {
		for j := i + 1; j < len(stages); j++ {
			if stages[i].StartS < stages[j].EndS && stages[j].StartS < stages[i].EndS {
				diags = append(diags, fmt.Sprintf(
					"stages %s and %s overlap; losses are reported independently",
					stages[i].ID, stages[j].ID))
			}
		}
	}
	return stages, diags
}

func fillStage(st *domain.StageCandidate, corr []domain.CorrPoint,
	w *domain.WeightRow, atms []domain.AtmosphereInterval, p domain.Params) {
	ms, _ := massAt(corr, st.StartS)
	me, _ := massAt(corr, st.EndS)
	st.MassStart = ms
	st.MassEnd = me
	st.LossMg = ms - me
	st.MeanTempC = (tempAt(corr, st.StartS) + tempAt(corr, st.EndS)) / 2
	g := gasAt(atms, (st.StartS+st.EndS)/2, p.AtmosphereDelayS)
	st.Gas = g[0]
	if len(g) > 1 {
		st.Ambiguous = g
	}
	if w != nil {
		pct := 100 * (ms - me) / w.Mg.Value
		st.LossPct = domain.Q{Value: pct, Unit: domain.UnitPct, Basis: "active_initial_mass:" + w.ID}
	} else {
		st.LossPct = domain.Q{Bad: true, Unit: domain.UnitPct, Diag: codeWeightUnknown}
	}
}

func stageID(origin string, start, end float64, suffix string) string {
	return domain.ShortID("stage-"+origin, f64(start), f64(end), suffix)
}

func indexAtTime(corr []domain.CorrPoint, t float64) int {
	best, bd := 0, -1.0
	for i, c := range corr {
		d := c.TimeS - t
		if d < 0 {
			d = -d
		}
		if bd < 0 || d < bd {
			best, bd = i, d
		}
	}
	return best
}

func slopeAt(corr []domain.CorrPoint, i int) float64 {
	if i <= 0 || i >= len(corr) {
		return 0
	}
	dt := corr[i].TimeS - corr[i-1].TimeS
	if dt == 0 {
		return 0
	}
	return (corr[i].CorrectedMg - corr[i-1].CorrectedMg) / dt
}

func spansGap(a, b float64, p domain.Params) bool {
	d := b - a
	if d < 0 {
		d = -d
	}
	return d > p.GapThresholdS
}

// bracket returns a mass-loss neighborhood around a reaction time without
// crossing any sampling gap.
func bracket(corr []domain.CorrPoint, t float64, p domain.Params) (float64, float64) {
	i := indexAtTime(corr, t)
	l, r := i, i
	for l > 0 && !spansGap(corr[l-1].TimeS, corr[l].TimeS, p) && slopeAt(corr, l) < 0 {
		l--
	}
	for r < len(corr)-1 && !spansGap(corr[r].TimeS, corr[r+1].TimeS, p) && slopeAt(corr, r+1) < 0 {
		r++
	}
	if r <= l {
		return 0, 0
	}
	return corr[l].TimeS, corr[r].TimeS
}

// RefillStage recomputes mass/temperature/gas/loss fields of a stage after a
// manual boundary adjustment. It exists so the engine does not duplicate the
// physical formulas.
func RefillStage(st *domain.StageCandidate, view *domain.DerivedView,
	weights []domain.WeightRow, p domain.Params) {
	fillStage(st, view.Corr, activeWeight(weights), view.Atmosphere, p)
	st.Adjusted = true
}

// ParamsFingerprint is exported for tests/service.
func ParamsFingerprint(p domain.Params) string { return p.Fingerprint() }
