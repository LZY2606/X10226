package compute

import (
	"fmt"
	"math"

	"tga/internal/domain"
)

type peak struct {
	idx         int
	t           float64
	dtg         float64
	left, right int
	valley      bool // split valley when overlapping
}

// findPeaks detects significant DTG minima (mass-loss peaks) inside each
// contiguous segment. Two minima separated only by a shallow local maximum
// below PeakMinProm are returned as one overlapping peak group carrying a
// split valley, which later yields multiple non-unique stage candidates.
func findPeaks(pts []pt, p domain.Params, segs [][2]int) [][]peak {
	var groups [][]peak
	for _, seg := range segs {
		var minima []peak
		for i := seg[0] + 1; i < seg[1]; i++ {
			if !pts[i].Present {
				continue
			}
			if pts[i].DTG < pts[i-1].DTG && pts[i].DTG < pts[i+1].DTG &&
				pts[i].DTG < -p.PeakMinProm {
				minima = append(minima, peak{idx: i, t: pts[i].Time, dtg: pts[i].DTG})
			}
		}
		if len(minima) == 0 {
			continue
		}
		// merge adjacent minima with a shallow saddle => overlap group
		group := []peak{minima[0]}
		for k := 1; k < len(minima); k++ {
			saddle := maxDTG(pts, minima[k-1].idx, minima[k].idx)
			depth := math.Min(-minima[k-1].dtg, -minima[k].dtg)
			if -saddle < p.PeakMinProm || (saddle-minima[k-1].dtg) < depth*0.35 ||
				minima[k].t-minima[k-1].t < p.OverlapTolSec*3 {
				g := group[len(group)-1]
				g.valley = saddle > minima[k-1].dtg && saddle > minima[k].dtg
				group[len(group)-1] = g
				group = append(group, minima[k])
			} else {
				groups = append(groups, group)
				group = []peak{minima[k]}
			}
		}
		groups = append(groups, group)
	}
	// boundaries by crossing of the prominence threshold
	for gi := range groups {
		for k := range groups[gi] {
			pk := &groups[gi][k]
			pk.left = pk.idx
			for pk.left > 0 && pts[pk.left-1].Present && pts[pk.left].DTG < -pPeakMin(p) {
				pk.left--
			}
			pk.right = pk.idx
			for pk.right < len(pts)-1 && pts[pk.right+1].Present && pts[pk.right].DTG < -pPeakMin(p) {
				pk.right++
			}
		}
	}
	return groups
}

func pPeakMin(p domain.Params) float64 { return p.PeakMinProm }

func maxDTG(pts []pt, a, b int) float64 {
	m := math.Inf(-1)
	for k := a; k <= b; k++ {
		if pts[k].Present && pts[k].DTG > m {
			m = pts[k].DTG
		}
	}
	return m
}

func buildStages(in Inputs, pts []pt, groups [][]peak, segs [][2]int, res *domain.AnalysisResult) []domain.Stage {
	var stages []domain.Stage
	for _, g := range groups {
		if len(g) == 1 {
			stages = append(stages, singleStage(in, pts, g[0], "", nil, res))
			continue
		}
		// overlapping peaks: emit every legal boundary choice and keep them
		// all; nothing is forced to uniqueness.
		groupID := "grp_" + fmt.Sprintf("%x", int(g[0].t))
		var ids []string
		made := map[string]bool{}
		for k, pk := range g {
			left, right := pk.left, pk.right
			if k > 0 {
				// candidate A: split at the valley (deconvolution)
				vIdx := argMaxDTG(pts, g[k-1].idx, pk.idx)
				right = vIdx
			}
			if k < len(g)-1 {
				vIdx := argMaxDTG(pts, pk.idx, g[k+1].idx)
				left = vIdx
			}
			st := stageFromBounds(in, pts, pk, left, right, groupID, "valley", res)
			st.Overlaps = peerLabels(g, k)
			st.Notes = append(st.Notes, "boundary shared with overlapping peak; deconvolution not unique")
			if !made[st.ID] {
				stages = append(stages, st)
				made[st.ID] = true
				ids = append(ids, st.ID)
			}
			// candidate B: treat merged envelope as one stage
		}
		env := peak{idx: argMinDTG(pts, g[0].idx, g[len(g)-1].idx)}
		env.t = pts[env.idx].Time
		env.dtg = pts[env.idx].DTG
		env.left = g[0].left
		env.right = g[len(g)-1].right
		envStage := stageFromBounds(in, pts, env, env.left, env.right, groupID, "on-baseline", res)
		envStage.Notes = append(envStage.Notes, "merged-envelope candidate for overlapping peak group")
		envStage.Overlaps = ids
		stages = append(stages, envStage)
		res.Findings = append(res.Findings, domain.Finding{
			Code: "OVERLAPPING_PEAKS_NONUNIQUE", Severity: "warning", T: env.t,
			Message: fmt.Sprintf("overlapping DTG peaks near %.0fs: %d split candidates plus merged envelope all retained",
				env.t, len(g)),
			Data: map[string]any{"group": groupID, "split_count": len(g), "candidate_count": len(g) + 1}})
	}
	return stages
}

func peerLabels(g []peak, k int) []string {
	var out []string
	for i := range g {
		if i != k {
			out = append(out, fmt.Sprintf("peak@%.0fs", g[i].t))
		}
	}
	return out
}

func singleStage(in Inputs, pts []pt, pk peak, groupID string, _ []string, res *domain.AnalysisResult) domain.Stage {
	return stageFromBounds(in, pts, pk, pk.left, pk.right, groupID, "on-baseline", res)
}

func stageFromBounds(in Inputs, pts []pt, pk peak, left, right int, groupID, basis string, res *domain.AnalysisResult) domain.Stage {
	tg0 := tgAt(pts, left)
	tg1 := tgAt(pts, right)
	loss := tg0 - tg1
	if loss < 0 {
		loss = 0
	}
	m0 := in.Init.Mass.Value
	trunc := pts[right].GapBefore || right >= len(pts)-1 || !pts[right].Present
	st := domain.Stage{
		GroupID: groupID,
		OnsetT:  pts[left].Time, PeakT: pts[pk.idx].Time, OffsetT: pts[right].Time,
		OnsetTemp: pts[left].SampleT, PeakTemp: pts[pk.idx].SampleT, OffsetTemp: pts[right].SampleT,
		MassLossPct: round(loss, 4),
		MassLossMg:  domain.Value{Raw: fmt.Sprintf("%.5f mg", loss/100*m0), Value: round(loss/100*m0, 6), Unit: "mg"},
		SigmaPct:    stageSigma(pts, left, right),
		Status:      "candidate", Truncated: trunc, Basis: basis,
	}
	if atmo, ref := atmosphereAt(in, pts[pk.idx].Time); atmo != "" {
		st.Atmosphere = atmo
		st.GasEvent = ref
	}
	if trunc {
		st.Uncertain = true
		st.Notes = append(st.Notes, "offset truncated by gap or run end")
	}
	st.ID = domain.ShortID("stage", map[string]any{
		"analysis_input": analysisInputKey(in),
		"onset":          round(st.OnsetT, 3), "offset": round(st.OffsetT, 3),
		"peak": round(st.PeakT, 3), "basis": basis,
	})
	return st
}

func analysisInputKey(in Inputs) string {
	ids := []string{in.Params.Fingerprint}
	for _, r := range in.Raw {
		ids = append(ids, fmt.Sprintf("%d:%s", r.Row, r.Mass.Raw))
	}
	if in.Init != nil {
		ids = append(ids, "m0="+in.Init.Mass.Raw)
	}
	return joinStrings(ids)
}

func joinStrings(s []string) string {
	out := ""
	for _, v := range s {
		out += v + "|"
	}
	return out
}

func stageSigma(pts []pt, left, right int) float64 {
	// half-width uncertainty proxy from DTG spread around the minimum
	return round(math.Max(0, pts[right].Time-pts[left].Time)*0.02, 3)
}

func tgAt(pts []pt, i int) float64 {
	if i < 0 || i >= len(pts) {
		return 0
	}
	return pts[i].TG
}

func argMaxDTG(pts []pt, a, b int) int {
	mi, mv := a, math.Inf(-1)
	for k := a; k <= b; k++ {
		if pts[k].Present && pts[k].DTG > mv {
			mi, mv = k, pts[k].DTG
		}
	}
	return mi
}

func argMinDTG(pts []pt, a, b int) int {
	mi, mv := a, math.Inf(1)
	for k := a; k <= b; k++ {
		if pts[k].Present && pts[k].DTG < mv {
			mi, mv = k, pts[k].DTG
		}
	}
	return mi
}
