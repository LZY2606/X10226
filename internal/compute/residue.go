package compute

import (
	"fmt"
	"math"

	"tga/internal/domain"
)

// atmosphereAt attributes a reaction time to an atmosphere, honoring the gas
// arrival delay: the valve switch is on the controller clock, but the new
// atmosphere only acts on the sample ValveDelaySec later.
func atmosphereAt(in Inputs, reactionT float64) (string, *domain.Ref) {
	current := ""
	var ref *domain.Ref
	for _, g := range in.Gas {
		if g.At.Unit == "" {
			continue
		}
		arrival := g.At.Value + in.Params.ValveDelaySec
		if reactionT >= arrival {
			current = g.ToGas
			r := domain.Ref{Kind: "gas_event", ID: domain.ShortID("gas", map[string]any{
				"index": g.Index, "at": g.At.Raw, "to": g.ToGas}),
				Label: g.RawText}
			ref = &r
		}
	}
	if current == "" && len(in.Gas) > 0 {
		current = in.Gas[0].FromGas
	}
	return current, ref
}

func linkValves(in Inputs, pts []pt, res *domain.AnalysisResult) []domain.ValveLink {
	var links []domain.ValveLink
	for _, g := range in.Gas {
		if g.At.Unit == "" {
			continue
		}
		from := g.At.Value + in.Params.ValveDelaySec
		to := from + 60
		if len(pts) > 0 {
			to = math.Min(to, pts[len(pts)-1].Time)
		}
		r := domain.Ref{Kind: "gas_event", ID: domain.ShortID("gas", map[string]any{
			"index": g.Index, "at": g.At.Raw, "to": g.ToGas}),
			Label: g.RawText}
		links = append(links, domain.ValveLink{
			GasEvent: r, ValveT: g.At.Value,
			ReactionFromT: from, ReactionToT: to,
			ToGas: g.ToGas, DelaySec: in.Params.ValveDelaySec})
		res.Findings = append(res.Findings, domain.Finding{
			Code: "ATMOSPHERE_DELAY", Severity: "info", T: g.At.Value,
			Message: fmt.Sprintf("valve switch at %.0fs (%s->%s) acts on sample only %.0fs later",
				g.At.Value, g.FromGas, g.ToGas, in.Params.ValveDelaySec),
			Refs: []domain.Ref{r},
			Data: map[string]any{
				"valve_t": g.At.Value, "reaction_from_t": round(from, 3),
				"delay_s": in.Params.ValveDelaySec, "to_gas": g.ToGas}})
	}
	return links
}

// buildResidues evaluates final residue in a tail window with a full,
// expandable calculation chain back to raw rows, initial mass and rules.
func buildResidues(in Inputs, pts []pt, segs [][2]int, res *domain.AnalysisResult) []domain.Residue {
	if len(segs) == 0 {
		return nil
	}
	last := segs[len(segs)-1]
	tail := in.Params.ResidueTailSec
	tEnd := pts[last[1]].Time
	tFrom := tEnd - tail
	var chosen []int
	for i := last[0]; i <= last[1]; i++ {
		if pts[i].Present && pts[i].Time >= tFrom {
			chosen = append(chosen, i)
		}
	}
	if len(chosen) < 3 {
		// widen within the final segment rather than crossing a gap
		chosen = chosen[:0]
		for i := last[0]; i <= last[1]; i++ {
			chosen = append(chosen, i)
		}
		if len(chosen) > 8 {
			chosen = chosen[len(chosen)-8:]
		}
	}
	var sum, sumSq float64
	for _, i := range chosen {
		sum += pts[i].MassCorr
	}
	mean := sum / float64(len(chosen))
	for _, i := range chosen {
		sumSq += (pts[i].MassCorr - mean) * (pts[i].MassCorr - mean)
	}
	sd := math.Sqrt(sumSq / float64(len(chosen)))
	m0 := in.Init.Mass.Value
	pct := mean / m0 * 100
	sigmaPct := sd / m0 * 100
	trunc := pts[last[1]].GapBefore || tEnd-tFrom < tail*0.8

	chain := []domain.ChainNode{
		{Step: "raw_samples", Ref: &domain.Ref{Kind: "source",
			ID:    domain.ShortID("rawhead", map[string]any{"first": in.Raw[0].Row}),
			Label: "original sample points (raw representation retained)"}},
		{Step: "drift_correction",
			Value: domain.Value{Raw: in.Params.DriftModel, Unit: "model"},
			Note:  "balance baseline drift removal rule"},
		{Step: "tail_window", Quantity: round(tail, 3), Unit: "s",
			Note: fmt.Sprintf("rows %d..%d selected in final contiguous segment (gaps never crossed)",
				pts[chosen[0]].Row, pts[chosen[len(chosen)-1]].Row)},
		{Step: "mean_corrected_mass", Quantity: round(mean, 6), Unit: "mg",
			Note: fmt.Sprintf("arithmetic mean over %d present samples", len(chosen))},
		{Step: "tail_scatter_sd", Quantity: round(sd, 6), Unit: "mg"},
		{Step: "initial_mass",
			Ref: &domain.Ref{Kind: "source", ID: domain.ShortID("inimass", map[string]any{"m0": in.Init.Mass.Raw}),
				Label: "layered sample initial weight"},
			Value: in.Init.Mass},
		{Step: "residue_percent", Quantity: round(pct, 4), Unit: "%",
			Note: "mean corrected tail mass / initial mass * 100"},
		{Step: "algo_version", Value: domain.Value{Raw: domain.AlgoVersion, Unit: "version"}},
		{Step: "params_fingerprint",
			Value: domain.Value{Raw: in.Params.Fingerprint, Unit: "fingerprint"}},
	}
	id := domain.ShortID("residue", map[string]any{
		"input": analysisInputKey(in), "from": round(tFrom, 3), "to": round(tEnd, 3)})
	r := domain.Residue{
		ID: id, WindowFromT: round(tFrom, 3), WindowToT: round(tEnd, 3),
		MeanMassMg: round(mean, 6), Percent: round(pct, 4),
		SigmaPct: round(sigmaPct, 4), Truncated: trunc, Chain: chain}
	if trunc {
		res.Findings = append(res.Findings, domain.Finding{Code: "RESIDUE_TRUNCATED",
			Severity: "warning", T: tEnd, Refs: []domain.Ref{{Kind: "residue", ID: id}},
			Message: "final residue window truncated by run end or a sampling gap"})
	}
	return []domain.Residue{r}
}

func round(v float64, n int) float64 {
	p := math.Pow(10, float64(n))
	return math.Round(v*p) / p
}
