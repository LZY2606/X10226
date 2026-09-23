package derive

import (
	"fmt"
	"math"
	"sort"

	"tga-review/internal/ingest"
	"tga-review/internal/domain"
)

// reactionWindow maps a valve switch (valve time) to the sample reaction
// window using the configured gas transport delay and temperature lag.
// valve switch time is explicitly NOT the sample reaction time.
type reactionWindow struct {
	GasEventIndex int      `json:"gas_event_index"`
	ValveT        float64  `json:"valve_t"`
	FromGas       string   `json:"from_gas"`
	ToGas         string   `json:"to_gas"`
	ReactionStart float64  `json:"reaction_start"`
	ReactionEnd   float64  `json:"reaction_end"`
}

func reactionWindows(gas []domain.GasEvent, p domain.Parameters) []reactionWindow {
	var out []reactionWindow
	shift := p.GasDelaySeconds + p.TempLagSeconds
	for _, g := range gas {
		start := g.AtT + p.GasDelaySeconds
		out = append(out, reactionWindow{
			GasEventIndex: g.Index, ValveT: g.AtT,
			FromGas: g.FromGas, ToGas: g.ToGas,
			ReactionStart: start,
			ReactionEnd:   g.AtT + shift + p.PeakWindowSeconds,
		})
	}
	return out
}

// findPeaks returns DTG local minima (mass-loss peaks) in mg/s.
func findPeaks(dtg []domain.Point, gaps []domain.Gap) []domain.Point {
	var peaks []domain.Point
	for i := 1; i < len(dtg)-1; i++ {
		if dtg[i].V < dtg[i-1].V && dtg[i].V < dtg[i+1].V && dtg[i].V < 0 {
			peaks = append(peaks, dtg[i])
		}
	}
	return peaks
}

// stageContext bundles derivation inputs for identity/naming.
type stageContext struct {
	projectID string
	params    domain.Parameters
}

// buildStages turns DTG peaks into stage candidates. Overlapping peaks yield
// ambiguous groups carrying every legal boundary alternative; peaks whose
// region crosses a sampling gap are not interpolated and stay unknown with
// all legal boundary candidates preserved.
func buildStages(corr []domain.Point, dtg []domain.Point, gaps []domain.Gap,
	windows []reactionWindow, p domain.Parameters, projectID string, initMass float64) []domain.Stage {
	peaks := findPeaks(dtg, gaps)
	// cluster peaks closer than overlap tolerance -> overlapping group
	groups := clusterPeaks(peaks, p.OverlapTolSeconds)
	var stages []domain.Stage
	for gi, group := range groups {
		stageID := ingest.ShortID("stage", projectID, p.Fingerprint,
			peakKey(group[0].T), len(group))
		win := nearestWindow(group[0].T, windows)
		base := domain.Stage{
			ID: stageID, WindowStart: group[0].T - p.PeakWindowSeconds/2,
			WindowEnd:  group[0].T + p.PeakWindowSeconds/2,
			PeakT:      group[0].T,
			UnitTime:   "s", UnitMass: "mg",
			State:      domain.CandProposed,
			SourceNodeID: ingest.ShortID("node", projectID, domain.NodeStages, p.Fingerprint),
		}
		if win != nil {
			idx := win.GasEventIndex
			vt := win.ValveT
			rt := win.ReactionStart
			base.LinkedGasEvent, base.ValveT, base.ReactionT = &idx, &vt, &rt
			base.WindowStart = math.Min(base.WindowStart, win.ReactionStart)
			base.WindowEnd = math.Max(base.WindowEnd, win.ReactionEnd)
		}
		crosses := crossesAnyGap(gaps, base.WindowStart, base.WindowEnd)
		if crosses {
			base.CrossesGap = true
			base.State = domain.CandUnknown
			base.UnknownReason = "stage region crosses a sampling gap; boundaries are not interpolated"
			base.Candidates = legalBoundaryCandidates(corr, gaps, base, initMass)
			stages = append(stages, base)
			continue
		}
		if len(group) > 1 {
			// overlapping peaks: mass loss cannot be uniquely attributed.
			base.Ambiguous = true
			base.State = domain.CandUnknown
			base.UnknownReason = fmt.Sprintf("%d overlapping DTG peaks within %.0fs; decomposition not unique",
				len(group), p.OverlapTolSeconds)
			base.Candidates = overlapCandidates(corr, group, windows, p, projectID, gi, initMass)
			stages = append(stages, base)
			continue
		}
		fillMass(&base, corr, initMass)
		stages = append(stages, base)
	}
	return stages
}

func peakKey(t float64) int64 { return int64(math.Round(t * 1000)) }

func clusterPeaks(peaks []domain.Point, tol float64) [][]domain.Point {
	sort.Slice(peaks, func(i, j int) bool { return peaks[i].T < peaks[j].T })
	var groups [][]domain.Point
	for _, pk := range peaks {
		if len(groups) == 0 || pk.T-groups[len(groups)-1][len(groups[len(groups)-1])-1].T > tol {
			groups = append(groups, []domain.Point{pk})
		} else {
			g := len(groups) - 1
			groups[g] = append(groups[g], pk)
		}
	}
	return groups
}

func nearestWindow(t float64, windows []reactionWindow) *reactionWindow {
	for i := range windows {
		if t >= windows[i].ReactionStart && t <= windows[i].ReactionEnd {
			return &windows[i]
		}
	}
	best := -1
	bestD := math.Inf(1)
	for i := range windows {
		d := math.Abs(windows[i].ReactionStart-t) + math.Abs(windows[i].ReactionEnd-t)
		if d < bestD {
			bestD, best = d, i
		}
	}
	if best >= 0 && bestD < 1e9 {
		w := windows[best]
		return &w
	}
	return nil
}

func fillMass(st *domain.Stage, corr []domain.Point, initMass float64) {
	if sm, ok := valueAt(corr, st.WindowStart); ok {
		st.StartMass = &sm
	}
	if em, ok := valueAt(corr, st.WindowEnd); ok {
		st.EndMass = &em
	}
	if st.StartMass != nil && st.EndMass != nil {
		loss := *st.StartMass - *st.EndMass
		st.MassLoss = &loss
		if initMass > 0 {
			pct := loss / initMass * 100
			st.MassLossPct = &pct
		}
	}
}

// valueAt interpolates ONLY between adjacent retained points; callers must
// ensure t is not inside a gap.
func valueAt(corr []domain.Point, t float64) (float64, bool) {
	if len(corr) == 0 {
		return 0, false
	}
	if t < corr[0].T || t > corr[len(corr)-1].T {
		return 0, false
	}
	return interp(corr, t)
}

// legalBoundaryCandidates keeps both legal endpoint choices (last point
// before / first point after the gap) instead of interpolating.
func legalBoundaryCandidates(corr []domain.Point, gaps []domain.Gap, base domain.Stage, initMass float64) []domain.Stage {
	var out []domain.Stage
	for _, g := range gaps {
		if base.WindowStart < g.To && base.WindowEnd > g.From {
			before := lastPointBefore(corr, g.From)
			after := firstPointAfter(corr, g.To)
			for _, bound := range [][2]float64{{before, after}} {
				for endIdx := 0; endIdx < 2; endIdx++ {
					cand := base
					cand.ID = base.ID + fmt.Sprintf(":gap%d", len(out))
					cand.CrossesGap = true
					cand.WindowStart, cand.WindowEnd = bound[0], bound[1]
					if endIdx == 0 {
						cand.WindowEnd = base.WindowEnd
					}
					if sm, ok := valueAt(corr, cand.WindowStart); ok {
						cand.StartMass = &sm
					}
					if em, ok := valueAt(corr, cand.WindowEnd); ok && !crossesAnyGap(gaps, cand.WindowStart, cand.WindowEnd) {
						cand.EndMass = &em
					}
					if cand.StartMass != nil && cand.EndMass != nil && initMass > 0 {
						loss := *cand.StartMass - *cand.EndMass
						cand.MassLoss = &loss
						pct := loss / initMass * 100
						cand.MassLossPct = &pct
					}
					out = append(out, cand)
				}
			}
		}
	}
	return out
}

func lastPointBefore(corr []domain.Point, t float64) float64 {
	out := corr[0].T
	for _, p := range corr {
		if p.T <= t {
			out = p.T
		}
	}
	return out
}

func firstPointAfter(corr []domain.Point, t float64) float64 {
	for _, p := range corr {
		if p.T >= t {
			return p.T
		}
	}
	return corr[len(corr)-1].T
}

// overlapCandidates enumerates legal per-peak attributions. Boundaries
// between overlapping peaks are shared/overlapping so mass losses are not
// forced to partition 100%.
func overlapCandidates(corr []domain.Point, group []domain.Point, windows []reactionWindow,
	p domain.Parameters, projectID string, gi int, initMass float64) []domain.Stage {
	var out []domain.Stage
	for k, pk := range group {
		c := domain.Stage{
			ID:          ingest.ShortID("stage", projectID, p.Fingerprint, peakKey(pk.T), "alt", k),
			PeakT:       pk.T,
			WindowStart: pk.T - p.PeakWindowSeconds/2,
			WindowEnd:   pk.T + p.PeakWindowSeconds/2,
			UnitTime:    "s", UnitMass: "mg", Ambiguous: true, State: domain.CandProposed,
		}
		if w := nearestWindow(pk.T, windows); w != nil {
			idx := w.GasEventIndex
			vt := w.ValveT
			rt := w.ReactionStart
			c.LinkedGasEvent, c.ValveT, c.ReactionT = &idx, &vt, &rt
		}
		fillMass(&c, corr, initMass)
		out = append(out, c)
	}
	return out
}

// buildResidue computes final residue percent with an explicit chain:
// initial mass -> drift/blank corrected final mass -> percentage.
func buildResidue(corr []domain.Point, init *domain.InitWeight, p domain.Parameters,
	projectID string) domain.Residue {
	nodeID := ingest.ShortID("node", projectID, domain.NodeResidue, p.Fingerprint)
	r := domain.Residue{ID: ingest.ShortID("res", projectID, p.Fingerprint),
		Unit: "mg", State: domain.CandProposed, SourceNodeID: nodeID}
	r.InitialMass = init.Mass
	chain := []domain.CalcStep{{
		Label: "initial_mass_stratified", Value: init.Mass, Unit: "mg",
		NodeID: ingest.ShortID("node", projectID, domain.NodeNormInit, p.Fingerprint),
		Detail: "original representation: " + init.OriginalRep,
	}}
	if len(corr) == 0 {
		r.State = domain.CandUnknown
		r.RejectReason = "no corrected samples available"
		r.Chain = chain
		return r
	}
	final := corr[len(corr)-1].V
	r.FinalMass = final
	chain = append(chain, domain.CalcStep{
		Label: "final_corrected_mass", Value: final, Unit: "mg",
		NodeID: ingest.ShortID("node", projectID, domain.NodeCorrected, p.Fingerprint),
		Op: "raw_final_mass - drift_offset - blank_mass",
	})
	chain = append(chain, domain.CalcStep{
		Label: "residue_pct", Value: final / init.Mass * 100, Unit: "%",
		NodeID: nodeID, Op: "final_corrected_mass / initial_mass * 100",
	})
	r.ValuePct = final / init.Mass * 100
	r.Chain = chain
	return r
}
