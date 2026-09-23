package algo

import (
	"fmt"
	"sort"
)

// buildStages turns detected peaks into stage candidates.
//
// Rules:
//   - alternative groups yield multiple legal readings; exactly one is marked
//     preferred, others carry rejection rationale but are first-class;
//   - bounds are peak shoulders; stages may overlap (and the overlap graph is
//     reported); losses never normalized to sum to 100%;
//   - crossing a sampling gap makes the loss unknown over the missing span:
//     the candidate stays, carries unknown_mass and crosses_gap=true;
//   - atmosphere association uses reaction-from (valve+delay), not valve time.
func buildStages(pts []Point, gaps []Gap, peaks []Peak, atmo []AtmosphereWindow,
	m0 float64, massRef string) []StageCandidate {
	var out []StageCandidate
	idx := map[string]int{}
	for i, p := range pts {
		idx[p.ID] = i
	}
	// group alternatives by AltGroup
	groups := map[string][]Peak{}
	var single []Peak
	for _, pk := range peaks {
		if pk.AltGroup != "" {
			groups[pk.AltGroup] = append(groups[pk.AltGroup], pk)
		} else {
			single = append(single, pk)
		}
	}
	n := 0
	newStage := func(pk Peak, preferred bool, rejectReason string) StageCandidate {
		n++
		st := StageCandidate{
			ID:           fmt.Sprintf("stg_%03d", n),
			AltGroup:     pk.AltGroup,
			PeakID:       pk.ID,
			LeftID:       pk.LeftID,
			RightID:      pk.RightID,
			LeftS:        pk.LeftS,
			RightS:       pk.RightS,
			LeftC:        pts[idx[pk.LeftID]].TempC,
			RightC:       pts[idx[pk.RightID]].TempC,
			Preferred:    preferred,
			Basis:        "DTG shoulder bounds on smoothed corrected series; peaks may overlap and are not renormalized",
			RejectReason: rejectReason,
			Rejected:     rejectReason != "",
			Atmosphere:   atmosphereAt(atmo, pk.CenterS),
		}
		loss, unknown, crosses := integrateLoss(pts, gaps, idx[pk.LeftID], idx[pk.RightID], m0)
		st.LossPct = loss
		st.UnknownMassMg = unknown
		st.CrossesGap = crosses
		st.Chain = stageChain(pts[idx[pk.LeftID]], pts[idx[pk.RightID]], m0, loss,
			unknown, crosses, pk, massRef)
		return st
	}
	for _, pk := range single {
		out = append(out, newStage(pk, true, ""))
	}
	groupNames := make([]string, 0, len(groups))
	for g := range groups {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)
	for _, g := range groupNames {
		pks := groups[g]
		// The envelope peak is the first emitted for the group; prefer it,
		// offer the sub-peak decomposition as the legal alternative.
		envelope := pks[0]
		out = append(out, newStage(envelope, true, ""))
		for _, pk := range pks[1:] {
			out = append(out, newStage(pk, false,
				"not preferred: sub-peak split is a legal alternative only when reviewers accept shoulder separation"))
		}
	}
	// overlap graph (only among preferred candidates)
	sort.SliceStable(out, func(i, j int) bool { return out[i].LeftS < out[j].LeftS })
	for i := range out {
		if !out[i].Preferred {
			continue
		}
		for j := range out {
			if i == j || !out[j].Preferred {
				continue
			}
			if out[j].LeftS < out[i].RightS && out[j].RightS > out[i].LeftS {
				out[i].OverlapsWith = append(out[i].OverlapsWith, out[j].ID)
			}
		}
	}
	return out
}

func atmosphereAt(atmo []AtmosphereWindow, tS float64) string {
	cur := ""
	for _, w := range atmo {
		if tS >= w.ReactionFrom {
			cur = w.ToGas
		}
	}
	if cur == "" && len(atmo) > 0 {
		return atmo[0].FromGas
	}
	return cur
}

// integrateLoss computes (corr(left)-corr(right))/m0*100 from observed pieces
// only. If the interval crosses gaps, the mass difference across each missing
// span is returned as unknown (no interpolation); loss covers observed pieces.
func integrateLoss(pts []Point, gaps []Gap, li, ri int, m0 float64) (float64, float64, bool) {
	if li > ri {
		li, ri = ri, li
	}
	gapBetween := map[[2]string]bool{}
	for _, g := range gaps {
		gapBetween[[2]string{g.BeforeID, g.AfterID}] = true
	}
	crosses := false
	observed := 0.0
	unknown := 0.0
	pieceStart := li
	for k := li; k < ri; k++ {
		if gapBetween[[2]string{pts[k].ID, pts[k+1].ID}] {
			observed += pts[pieceStart].CorrMg - pts[k].CorrMg
			unknown += pts[k].CorrMg - pts[k+1].CorrMg
			pieceStart = k + 1
			crosses = true
		}
	}
	observed += pts[pieceStart].CorrMg - pts[ri].CorrMg
	if m0 <= 0 {
		return 0, round2(unknown), crosses
	}
	return round2(observed / m0 * 100), round2(unknown), crosses
}

func stageChain(l, r Point, m0, loss, unknown float64, crosses bool, pk Peak, massRef string) []ChainNode {
	chain := []ChainNode{
		{Step: "initial mass m0", Value: strconvF(m0), Unit: "mg", Source: massRef},
		{Step: "corrected mass at left shoulder", Value: strconvF(l.CorrMg), Unit: "mg",
			Source: l.ID, RefIDs: []string{l.ID}, Note: "raw - drift(t)"},
		{Step: "corrected mass at right shoulder", Value: strconvF(r.CorrMg), Unit: "mg",
			Source: r.ID, RefIDs: []string{l.ID, r.ID}},
		{Step: "observed mass loss", Value: strconvF(l.CorrMg - r.CorrMg), Unit: "mg",
			Source: "integral", RefIDs: []string{l.ID, r.ID}},
	}
	if crosses {
		chain = append(chain, ChainNode{
			Step: "gap crossing", Value: strconvF(unknown), Unit: "mg unknown",
			Source: "gap", Note: "no interpolation across sampling gap; loss covers observed pieces only",
		})
	}
	chain = append(chain,
		ChainNode{Step: "stage loss percent", Value: strconvF(loss), Unit: "% of m0",
			Source: pk.ID, Note: "stages may overlap; not forced to sum to 100%"},
		ChainNode{Step: "bound basis", Value: "DTG shoulders (smoothed, gaps excluded)", Source: pk.ID},
	)
	return chain
}

// buildResidue computes final residue candidates. Normally one value at the
// last observed point; if a gap precedes the end or the tail itself is
// missing, the residue is unknown and alternatives are retained.
func buildResidue(pts []Point, gaps []Gap, m0 float64, massRef string) []ResidueCandidate {
	if len(pts) == 0 || m0 <= 0 {
		return nil
	}
	last := pts[len(pts)-1]
	crosses := false
	for _, g := range gaps {
		if g.AfterID == last.ID || g.BeforeID == last.ID {
			crosses = true
		}
	}
	val := round2(last.CorrMg / m0 * 100)
	rc := ResidueCandidate{
		ID:         "res_final",
		ValuePct:   val,
		AtS:        last.TS,
		AtC:        last.TempC,
		PointID:    last.ID,
		CrossesGap: false,
		Unknown:    false,
		Note:       "corrected mass at last observed point / initial mass",
		Chain: []ChainNode{
			{Step: "last observed corrected mass", Value: strconvF(last.CorrMg), Unit: "mg",
				Source: last.ID, RefIDs: []string{last.ID}},
			{Step: "initial mass", Value: strconvF(m0), Unit: "mg", Source: massRef},
			{Step: "residue", Value: strconvF(val), Unit: "%",
				Note: "residue is a direct quotient; stage losses are not assumed to close to 100%"},
		},
	}
	out := []ResidueCandidate{rc}
	if crosses {
		out[0].Unknown = true
		out[0].CrossesGap = true
		out[0].Note = "tail abuts a sampling gap; residue not unique; both legal readings retained"
		out = append(out, ResidueCandidate{
			ID: "res_final_low", ValuePct: round2(pts[len(pts)-2].CorrMg / m0 * 100),
			AtS: pts[len(pts)-2].TS, AtC: pts[len(pts)-2].TempC,
			PointID: pts[len(pts)-2].ID, Unknown: true, CrossesGap: true,
			Note: "lower-bound reading: last point before the gap",
		})
	}
	return out
}
