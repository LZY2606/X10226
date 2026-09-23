package derive

import (
	"sort"

	"tgaline/internal/domain"
)

type peak struct {
	idx   int
	t     float64
	depth float64 // |negative DTG|
}

// detectPeaks finds DTG minima (mass loss maxima) inside each gap segment.
func detectPeaks(pts []domain.Point, prominence float64) []peak {
	var pks []peak
	for _, seg := range segments(pts) {
		if len(seg) < 3 {
			continue
		}
		base := indexOf(pts, seg[0].T)
		for i := 1; i < len(seg)-1; i++ {
			v := seg[i].DTGMgPerMin
			if v < seg[i-1].DTGMgPerMin && v < seg[i+1].DTGMgPerMin && -v >= prominence {
				pks = append(pks, peak{idx: base + i, t: pts[base+i].T, depth: -v})
			}
		}
	}
	return pks
}

// boundary returns the onset/offset time where DTG returns close to zero,
// moving outward from a peak, stopping at segment edges and at gaps.
func boundary(pts []domain.Point, peakIdx, dir int, thr float64) (float64, bool) {
	i := peakIdx
	for {
		ni := i + dir
		if ni < 0 || ni >= len(pts) {
			return pts[i].T, false
		}
		if dir == 1 && pts[ni].GapBefore {
			return pts[i].T, true
		}
		if -pts[ni].DTGMgPerMin <= thr {
			return pts[ni].T, false
		}
		i = ni
	}
}

type span struct{ on, off float64 }

func buildStages(projectID string, in Inputs, g *Graph, dtg, gasArt *domain.Artifact) *domain.Artifact {
	pts := asPoints(dtg)
	status := dtg.Status
	diags := append([]domain.Diagnostic(nil), dtg.Diagnostics...)
	var stages []domain.StageCandidate
	group := 0
	pks := detectPeaks(pts, in.Params.MinProminenceMgPerMin)
	sort.Slice(pks, func(i, j int) bool { return pks[i].t < pks[j].t })
	bandsRaw := gasArt.Data["bands"].([]any)
	bands := bandsFromAny(bandsRaw)
	used := map[int]bool{}
	i := 0
	for i < len(pks) {
		// cluster peaks closer than 4x mean spacing => overlapping group
		j := i + 1
		for j < len(pks) {
			density := avgSpacing(pts, pks[j-1].idx, pks[j].idx)
			if pks[j].t-pks[j-1].t <= 4*density {
				j++
				continue
			}
			break
		}
		cluster := pks[i:j]
		group++
		var candidateSpans []span
		merged := false
		if len(cluster) > 1 {
			merged = true
			// legal reading A: one envelope stage (unresolved overlaps)
			on := onsetOf(pts, cluster[0].idx, in.Params.MinProminenceMgPerMin)
			off := offsetOf(pts, cluster[len(cluster)-1].idx, in.Params.MinProminenceMgPerMin)
			candidateSpans = append(candidateSpans, span{on, off})
			// legal reading B: split at the valley between peaks
			valley := valleyIdx(pts, cluster[0].idx, cluster[len(cluster)-1].idx)
			candidateSpans = append(candidateSpans, splitSpan(pts, cluster, valley, in.Params.MinProminenceMgPerMin)...)
		} else {
			on := onsetOf(pts, cluster[0].idx, in.Params.MinProminenceMgPerMin)
			off := offsetOf(pts, cluster[0].idx, in.Params.MinProminenceMgPerMin)
			candidateSpans = []span{{on, off}}
		}
		var altIDs []string
		spans := candidateSpans
		for ci, sp := range spans {
			if sp.on >= sp.off {
				continue
			}
			reasons, spanGap := rejectCheck(pts, sp)
			peakI := cluster[0].idx
			peakT := cluster[0].t
			if !merged && len(cluster) >= 1 {
				peakT = cluster[0].t
			}
			onMass, onTemp := interpMass(pts, sp.on)
			offMass, offTemp := interpMass(pts, sp.off)
			loss := onMass - offMass
			st := domain.StageCandidate{
				ID: StageID(group, ci, sp.on, sp.off, peakT, gasAt(bands, peakT)),
				Index: len(stages) + 1, OnsetS: sp.on, OffsetS: sp.off, PeakS: peakT,
				PeakMgPerMin: pts[peakI].DTGMgPerMin,
				OnsetTempC: onTemp, OffsetTempC: offTemp, PeakTempC: pts[peakI].TempC,
				LossMg: loss, LossPct: 100.0 * loss / initialOf(g, pts),
				Atmosphere: gasAt(bands, peakT), OverlapGroup: group,
				SpanGap: spanGap, UncertaintyS: 0,
			}
			if merged {
				st.UncertaintyS = avgSpacing(pts, cluster[0].idx, cluster[len(cluster)-1].idx)
				st.RejectedReasons = []string{
					"overlapping peaks: envelope vs valley-split both legal; no unique decomposition without operator adjudication",
				}
			}
			st.RejectedReasons = append(st.RejectedReasons, reasons...)
			altIDs = append(altIDs, st.ID)
			_ = onMass
			stages = append(stages, stages_len(stages))
		}
		if merged {
			for k := range stages {
				if stages[k].OverlapGroup == group {
					stages[k].Alternatives = altIDs
				}
			}
		}
		for _, p := range cluster {
			used[p.idx] = true
		}
		i = j
	}
	_ = used
	if hasGapSpan(stages) {
		status = demote(status)
		diags = append(diags, domain.Diagnostic{Scope: "artifact", Code: "stage_spans_gap",
			Message: "a candidate stage spans a sampling gap; it is retained but rejected for interpolation across the gap",
			Severity: domain.SeverityWarning})
	}
	docs := []*domain.Document{in.Samples, in.InitialMass}
	ih := inputsHash(append(docs, in.Gas), in.Params, "stages")
	id := ArtifactID("stages", ih)
	data := map[string]any{"stages": stages}
	return newArtifact(id, projectID, domain.ArtStageCandidates, status, data,
		prov([]*domain.Document{in.Samples, in.InitialMass, in.Gas}, in.Params, g.ParamsFP, ih, dtg.ID, gasArt.ID), diags)
}

func stages_len(s []domain.StageCandidate) int { return len(s) - 1 }

func initialOf(g *Graph, pts []domain.Point) float64 {
	if g.InitialMassMg > 0 {
		return g.InitialMassMg
	}
	if len(pts) > 0 {
		return pts[0].MassMg
	}
	return 1
}

func onsetOf(pts []domain.Point, idx int, thr float64) float64 {
	t, _ := boundary(pts, idx, -1, thr)
	return t
}

func offsetOf(pts []domain.Point, idx int, thr float64) float64 {
	t, _ := boundary(pts, idx, 1, thr)
	return t
}

func avgSpacing(pts []domain.Point, a, b int) float64 {
	if b <= a {
		return 1
	}
	d := pts[b].T - pts[a].T
	if d <= 0 {
		return 1
	}
	return d / float64(b-a)
}

func valleyIdx(pts []domain.Point, a, b int) int {
	best, bestV := a, pts[a].DTGMgPerMin
	for i := a; i <= b; i++ {
		if pts[i].DTGMgPerMin > bestV {
			bestV, best = pts[i].DTGMgPerMin, i
		}
	}
	return best
}

func splitSpan(pts []domain.Point, cluster []peak, valley int, thr float64) []span {
	on1 := onsetOf(pts, cluster[0].idx, thr)
	off1 := pts[valley].T
	on2 := pts[valley].T
	off2 := offsetOf(pts, cluster[len(cluster)-1].idx, thr)
	return []span{{on1, off1}, {on2, off2}}
}

func rejectCheck(pts []domain.Point, sp span) ([]string, bool) {
	var reasons []string
	spanGap := false
	for i := 1; i < len(pts); i++ {
		if pts[i].GapBefore && pts[i-1].T < sp.off && pts[i].T > sp.on {
			spanGap = true
			reasons = append(reasons, "candidate spans a sampling gap; no interpolation across the gap is allowed")
		}
	}
	return reasons, spanGap
}

func hasGapSpan(stages []domain.StageCandidate) bool {
	for _, s := range stages {
		if s.SpanGap {
			return true
		}
	}
	return false
}

func interpMass(pts []domain.Point, t float64) (float64, float64) {
	for i := range pts {
		if pts[i].T == t {
			return pts[i].MassMg, pts[i].TempC
		}
	}
	for i := 1; i < len(pts); i++ {
		if pts[i].GapBefore {
			continue
			}
		if t > pts[i-1].T && t < pts[i].T {
			fr := (t - pts[i-1].T) / (pts[i].T - pts[i-1].T)
			return pts[i-1].MassMg + fr*(pts[i].MassMg-pts[i-1].MassMg),
				pts[i-1].TempC + fr*(pts[i].TempC-pts[i-1].TempC)
		}
	}
	if len(pts) > 0 {
		return pts[len(pts)-1].MassMg, pts[len(pts)-1].TempC
	}
	return 0, 0
}

func bandsFromAny(raw []any) []domain.GasBand {
	var out []domain.GasBand
	for _, v := range raw {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		b := domain.GasBand{}
		b.FromS, _ = m["from_s"].(float64)
		b.ToS, _ = m["to_s"].(float64)
		b.ValveAtS, _ = m["valve_at_s"].(float64)
		b.Atmosphere, _ = m["atmosphere"].(string)
		b.DelayS, _ = m["delay_s"].(float64)
		out = append(out, b)
	}
	return out
}
