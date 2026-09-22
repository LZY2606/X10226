package engine

import (
	"math"
	"sort"

	"tga-review/internal/domain"
)

// shoulderFrac is the fraction of peak |DTG| at which we declare the excursion
// shoulder. It keeps two overlapping peaks from absorbing the whole segment
// while still tolerating the noisy floor around zero.
const shoulderFrac = 0.12

// BuildStages turns DTG peaks into decomposition stage candidates.
//
// Domain rules:
//   - Boundaries follow the excursion outward until |DTG| falls below
//     shoulderFrac of the peak magnitude, the rate turns non-negative, a
//     neighboring peak basin is entered, or a gap is hit.
//   - Overlapping peaks legitimately yield overlapping intervals; stages are
//     never normalized so losses sum to 100%.
//   - If the shoulder cannot be placed uniquely (segment edge reached while
//     still well below the recovery threshold), a second conservative
//     candidate clamped to the segment edge is emitted with its uncertainty,
//     so every legal candidate survives.
func BuildStages(projectID, batchID string, fp domain.Fingerprint, correctedNode, dtgNode, peaksNode, windowsNode, gapNode *domain.DerivedNode, initMg float64, p domain.Params) (*domain.DerivedNode, error) {
	corr, ok := correctedNode.Payload.(domain.CorrectedPayload)
	if !ok {
		return fail("corrected node payload mismatch")
	}
	dp, ok := dtgNode.Payload.(domain.DTGPayload)
	if !ok {
		return fail("dtg node payload mismatch")
	}
	pp, ok := peaksNode.Payload.(domain.PeaksPayload)
	if !ok {
		return fail("peaks node payload mismatch")
	}
	gp, ok := gapNode.Payload.(domain.GapMapPayload)
	if !ok {
		return fail("gap node payload mismatch")
	}
	segments := GapSegments(toCanonical(corr.Points), gp.Gaps)
	winGas := gasAt(windowsNode)
	peakRow := map[int]domain.Peak{}
	for _, pk := range pp.Peaks {
		peakRow[pk.Row] = pk
	}

	var stages []domain.Stage
	for _, pk := range pp.Peaks {
		idx := indexOfRow(corr.Points, pk.Row)
		if idx < 0 {
			continue
		}
		seg := segments[idx]
		threshold := math.Abs(pk.DTGMgMin) * shoulderFrac
		left, leftAmb := shoulder(dp.Points, idx, seg, -1, threshold, peakRow)
		right, rightAmb := shoulder(dp.Points, idx, seg, +1, threshold, peakRow)
		type alt struct {
			li, ri          int
			uncertaintyPct  float64
			note            string
		}
		alts := []alt{{li: left, ri: right}}
		if leftAmb || rightAmb {
			alts[0].uncertaintyPct = 15
			alts[0].note = "shoulder not unique: excursion reaches gap-bounded segment edge"
			li2, ri2 := segmentEdge(dp.Points, idx, seg)
			alts = append(alts, alt{li: li2, ri: ri2, uncertaintyPct: 5, note: "alternative clamped to gap-bounded segment edge"})
		}
		for ai, a := range alts {
			start, end := corr.Points[a.li], corr.Points[a.ri]
			loss := start.CorrectedMg - end.CorrectedMg
			pct := 0.0
			if initMg > 0 {
				pct = loss / initMg * 100
			}
			id, _ := domain.ContentID(domain.PrefixCandidate, "stage", struct {
				Dtg  string
				Peak string
				Alt  int
				L, R int
				FP   domain.Fingerprint
			}{dtgNode.ID, pk.ID, ai, start.Row, end.Row, fp})
			stages = append(stages, domain.Stage{
				ID: id, StartRow: start.Row, EndRow: end.Row,
				StartTMin: round6(start.TMin), EndTMin: round6(end.TMin),
				StartTK: round6(start.TK), EndTK: round6(end.TK),
				PeakID: pk.ID, GasContext: winGas(pk.TMin),
				MassLossMg: round6(loss), MassLossPct: round6(pct),
				Status: domain.StageCandidate,
				UncertaintyPct: a.uncertaintyPct, RejectReason: a.note,
			})
		}
	}

	for i := range stages {
		for j := i + 1; j < len(stages); j++ {
			if stages[i].StartTMin < stages[j].EndTMin && stages[j].StartTMin < stages[i].EndTMin {
				stages[i].Overlaps = append(stages[i].Overlaps, stages[j].ID)
				stages[j].Overlaps = append(stages[j].Overlaps, stages[i].ID)
			}
		}
	}
	sort.Slice(stages, func(i, j int) bool { return stages[i].StartTMin < stages[j].StartTMin })

	payload := domain.StagesPayload{Stages: stages}
	id, err := domain.ContentID(domain.PrefixDerived, string(domain.DKStage), struct {
		In string
		FP domain.Fingerprint
		P  domain.StagesPayload
	}{dtgNode.ID, fp, payload})
	if err != nil {
		return nil, err
	}
	return &domain.DerivedNode{
		ID: id, ProjectID: projectID, Kind: domain.DKStage, BatchID: batchID,
		Fingerprint: fp, InputNodeIDs: []string{correctedNode.ID, dtgNode.ID, peaksNode.ID},
		Payload: payload,
	}, nil
}

// shoulder walks away from the peak while the excursion remains active.
// It stops at: a gap, a non-negative/weak rate below the recovery threshold,
// or entry into another peak's basin. Returns (index, ambiguous).
func shoulder(pts []domain.DTGPoint, idx int, seg string, dir int, threshold float64, peaks map[int]domain.Peak) (int, bool) {
	j := idx
	ambiguous := false
	for {
		k := j + dir
		if k < 0 || k >= len(pts) || pts[k].SegmentID != seg {
			ambiguous = true
			break
		}
		if _, isPeak := peaks[pts[k].Row]; isPeak {
			break // entered another peak's basin
		}
		v := pts[k].DTGMgMin
		if v >= 0 || math.Abs(v) <= threshold {
			j = k
			break
		}
		j = k
	}
	return j, ambiguous
}

func segmentEdge(pts []domain.DTGPoint, idx int, seg string) (int, int) {
	li, ri := idx, idx
	for k := idx; k >= 0 && pts[k].SegmentID == seg; k-- {
		li = k
	}
	for k := idx; k < len(pts) && pts[k].SegmentID == seg; k++ {
		ri = k
	}
	return li, ri
}

func indexOfRow(pts []domain.CorrectedPoint, row int) int {
	for i := range pts {
		if pts[i].Row == row {
			return i
		}
	}
	return -1
}

func gasAt(windowsNode *domain.DerivedNode) func(float64) string {
	var wins []domain.ReactionWindow
	if windowsNode != nil {
		if wp, ok := windowsNode.Payload.(domain.ReactionWindowsPayload); ok {
			wins = wp.Windows
		}
	}
	return func(tMin float64) string {
		cur := ""
		for _, w := range wins {
			if tMin >= w.ReachTMin {
				cur = w.Gas
			}
		}
		return cur
	}
}
