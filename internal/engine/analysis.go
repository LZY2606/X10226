package engine

import (
	"fmt"
	"math"
	"sort"

	"tga-review/internal/domain"
	"tga-review/internal/ident"
)

// atmosphere converts valve timestamps to sample reaction timestamps. The
// reaction interval is shifted by gas_delay (transport) and temp_lag (thermal
// response); both are kept visible and the valve time is never reused as the
// reaction time.
func (e *Engine) atmosphere(in Inputs, add findingAdder) []domain.AtmosphereEpisode {
	var events []domain.GasEvent
	for _, g := range in.Gas {
		if t, ok, _ := usableQuantity(g.T); ok {
			g.T.ConvertedValue = t
			events = append(events, g)
		}
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].T.ConvertedValue < events[j].T.ConvertedValue })
	eps := make([]domain.AtmosphereEpisode, 0, len(events))
	shift := in.Params.GasDelayS + in.Params.TempLagS
	for i, g := range events {
		valve := g.T.ConvertedValue
		reaction := valve + shift
		end := math.Inf(1)
		if i+1 < len(events) {
			end = events[i+1].T.ConvertedValue + shift
		}
		eps = append(eps, domain.AtmosphereEpisode{
			ID:         "atm-" + ident.Short(fmt.Sprintf("%s|atm|%d", in.RunID, i)),
			GasEventID: g.ID,
			Gas:        g.ToGas,
			ValveT:     valve,
			ReactionT:  reaction,
			GasDelay:   in.Params.GasDelayS,
			TempLag:    in.Params.TempLagS,
			EndT:       end,
		})
		add("VALVE_NEQ_REACTION", "info",
			fmt.Sprintf("valve switch at %.0fs reaches sample reaction window at %.0fs (+%ds gas, +%ds lag)",
				valve, reaction, int(in.Params.GasDelayS), int(in.Params.TempLagS)),
			g.ID, g.ID)
	}
	return eps
}

// stages proposes one stage per peak, plus combined legal candidates when peaks
// overlap. Overlapping stages are kept with overlaps_with; percentages are
// computed independently and never normalised to a 100% total.
func (e *Engine) stages(peaks []domain.Peak, dtg []domain.DTGPoint, eps []domain.AtmosphereEpisode, in Inputs, add findingAdder) []domain.StageCandidate {
	if len(peaks) == 0 {
		add("NO_PEAK_STAGE", "warning", "no confident DTG minimum: no unique stage candidates", "")
		return nil
	}
	// local onset/end for each peak: walk outward while DTG < 0 (losing mass)
	bounds := func(pk domain.Peak) (float64, float64) {
		idx := -1
		for i, d := range dtg {
			if d.T == pk.T && d.Confidence == "OK" {
				idx = i
				break
			}
		}
		if idx < 0 {
			return pk.T, pk.T
		}
		on, end := idx, idx
		for on > 0 && dtg[on-1].Confidence == "OK" && !math.IsNaN(dtg[on-1].DTG) && dtg[on-1].DTG < 0 {
			on--
		}
		for end+1 < len(dtg) && dtg[end+1].Confidence == "OK" && !math.IsNaN(dtg[end+1].DTG) && dtg[end+1].DTG < 0 {
			end++
		}
		return dtg[on].T, dtg[end].T
	}
	massAt := func(t float64) (float64, float64, bool) {
		for i := range dtg {
			if math.Abs(dtg[i].T-t) < 1e-6 && dtg[i].Confidence == "OK" {
				return dtg[i].Mass, dtg[i].Temp, true
			}
		}
		return 0, 0, false
	}
	gasAt := func(t float64) string {
		for _, ep := range eps {
			if t >= ep.ReactionT && (math.IsInf(ep.EndT, 1) || t < ep.EndT) {
				return ep.Gas
			}
		}
		return ""
	}
	initial := currentInitialMg(in.Initial)

	makeStage := func(i int, pk domain.Peak, on, end float64, idSuffix string) domain.StageCandidate {
		mOn, tempOn, _ := massAt(on)
		mEnd, tempEnd, _ := massAt(end)
		loss := mOn - mEnd
		pct := math.NaN()
		if initial > 0 {
			pct = loss / initial * 100
		}
		chain := []domain.CalcStep{
			{Label: "corrected mass at onset", Value: mOn, Unit: domain.UnitMilligram, Detail: fmt.Sprintf("t=%.1fs T=%.1fdegC", on, tempOn)},
			{Label: "corrected mass at end", Value: mEnd, Unit: domain.UnitMilligram, Detail: fmt.Sprintf("t=%.1fs T=%.1fdegC", end, tempEnd)},
			{Label: "stage mass loss", Value: loss, Unit: domain.UnitMilligram, Detail: "onset - end (independent, not forced into 100%)"},
		}
		if initial > 0 {
			chain = append(chain, domain.CalcStep{Label: "current initial mass", Value: initial, Unit: domain.UnitMilligram, Detail: "latest initial-mass revision"})
			chain = append(chain, domain.CalcStep{Label: "stage loss fraction", Value: pct, Unit: domain.UnitPercent, Detail: "100 * loss / initial_mass"})
		}
		return domain.StageCandidate{
			ID:          "stg-" + ident.Short(fmt.Sprintf("%s|stg|%d|%s", in.RunID, i, idSuffix)),
			PeakIDs:     []string{pk.ID},
			TOnset:      on,
			TEnd:        end,
			TempOnset:   tempOn,
			TempEnd:     tempEnd,
			MassOnset:   mOn,
			MassEnd:     mEnd,
			MassLossMg:  loss,
			MassLossPct: pct,
			Gas:         gasAt(pk.T),
			Attribution: pk.Attribution,
			Status:      "PROPOSED",
			Chain:       chain,
		}
	}

	var stages []domain.StageCandidate
	type bnd struct{ on, end float64 }
	bs := make([]bnd, len(peaks))
	for i, pk := range peaks {
		on, end := bounds(pk)
		bs[i] = bnd{on, end}
		st := makeStage(i, pk, on, end, "single")
		if pk.Attribution == "AMBIGUOUS" {
			st.RejectReason = ""
			add("AMBIGUOUS_STAGE", "info",
				fmt.Sprintf("stage near %.0f degC not uniquely attributable: alternative legal candidates retained", pk.Temp), pk.ID)
		}
		stages = append(stages, st)
	}
	// overlapping alternatives: each peak also gets a widened window ending at
	// the next/previous peak, expressing that attribution between close minima
	// is not unique.
	for i := range peaks {
		for j := range peaks {
			if i == j {
				continue
			}
			if math.Abs(peaks[i].T-peaks[j].T) < in.Params.OverlapWindowS*2 {
				on := math.Min(bs[i].on, bs[j].on)
				end := math.Max(bs[i].end, bs[j].end)
				alt := makeStage(i, peaks[i], on, end, fmt.Sprintf("alt%d", j))
				alt.Attribution = "AMBIGUOUS"
				alt.OverlapsWith = []string{stages[i].ID}
				alt.PeakIDs = []string{peaks[i].ID, peaks[j].ID}
				stages = append(stages, alt)
			}
		}
	}
	// fill overlaps_with for singles that truly overlap in time
	for i := range stages {
		for j := range stages {
			if i != j && stages[i].ID != stages[j].ID && stages[i].TOnset < stages[j].TEnd && stages[j].TOnset < stages[i].TEnd &&
				len(stages[i].OverlapsWith) == 0 && stages[j].Attribution == "AMBIGUOUS" {
				stages[i].OverlapsWith = append(stages[i].OverlapsWith, stages[j].ID)
			}
		}
	}
	sort.SliceStable(stages, func(a, b int) bool {
		if stages[a].TOnset != stages[b].TOnset {
			return stages[a].TOnset < stages[b].TOnset
		}
		return stages[a].ID < stages[b].ID
	})
	return stages
}

// residue computes final residue against the latest initial mass with a full
// expandable chain. If the final usable point borders a gap or the initial mass
// is unusable, the residue is Unknown rather than silently guessed.
func (e *Engine) residue(pts []*point, in Inputs, add findingAdder) domain.ResidueResult {
	res := domain.ResidueResult{
		ID:     "res-" + ident.Short(in.RunID+"|residue"),
		Status: "CURRENT",
	}
	imID, imMg := currentInitial(in.Initial)
	res.InitialMassID = imID
	res.InitialMassMg = imMg

	var last *point
	for _, p := range pts {
		if p.usable {
			last = p
		}
	}
	if last == nil {
		res.Unknown = true
		res.UnknownReason = "no usable sample point"
		add("RESIDUE_UNKNOWN", "warning", res.UnknownReason, res.ID)
		return res
	}
	res.FinalMassMg = last.corrMass
	res.FinalT = last.t
	res.FinalTemp = last.temp
	if last.gapBefore {
		res.Unknown = true
		res.UnknownReason = "final usable point follows a sampling gap; residue not interpolated"
		add("RESIDUE_UNKNOWN", "warning", res.UnknownReason, res.ID)
	}
	if imID == "" || imMg <= 0 {
		res.Unknown = true
		if res.UnknownReason != "" {
			res.UnknownReason += "; "
		}
		res.UnknownReason += "no usable initial mass revision"
		add("RESIDUE_UNKNOWN", "warning", "residue cannot be normalised without initial mass", res.ID)
	}
	res.Chain = []domain.CalcStep{
		{Label: "initial mass (latest revision)", Value: imMg, Unit: domain.UnitMilligram, Detail: imID},
		{Label: "blank contribution at end", Value: last.blankMass, Unit: domain.UnitMilligram, Detail: "blank curve subtraction"},
		{Label: "balance drift offset at end", Value: last.driftOff, Unit: domain.UnitMilligram, Detail: "drift correction"},
		{Label: "raw reading at end", Value: last.rawMass, Unit: domain.UnitMilligram, Detail: fmt.Sprintf("t=%.1fs T=%.1fdegC", last.t, last.temp)},
		{Label: "corrected final mass", Value: last.corrMass, Unit: domain.UnitMilligram, Detail: "raw - blank - drift"},
	}
	if !res.Unknown {
		res.ResiduePct = last.corrMass / imMg * 100
		res.Chain = append(res.Chain, domain.CalcStep{
			Label: "residue fraction", Value: res.ResiduePct, Unit: domain.UnitPercent,
			Detail: "100 * corrected_final / initial",
		})
	}
	return res
}

func currentInitial(ims []domain.InitialMass) (string, float64) {
	// latest by ordinal; revisions append rather than overwrite
	idx := -1
	for i := range ims {
		if ims[i].Mass.Convertible && ims[i].Mass.ConvertedValue > 0 {
			if idx == -1 || ims[i].Ordinal > ims[idx].Ordinal {
				idx = i
			}
		}
	}
	if idx == -1 {
		return "", 0
	}
	return ims[idx].ID, ims[idx].Mass.ConvertedValue
}

func currentInitialMg(ims []domain.InitialMass) float64 {
	_, v := currentInitial(ims)
	return v
}
