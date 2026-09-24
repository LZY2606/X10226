package pipeline

import (
	"fmt"
	"math"

	"tga/internal/domain"
)

type variant struct {
	stages []Stage
	note   string
}

// buildCandidates turns peak groups into legal stage decompositions.
//
//   - A single peak group yields one stage.
//   - A group with k>=2 overlapping sub-peaks yields k candidate decompositions:
//     the merged envelope as one stage, and every resolution into its sub-peaks
//     (2-sub-peak case => merged + split; 3 => merged + all pairings are kept
//     only when each split boundary sits at a detected saddle).
//   - Stages may overlap in time and losses are NOT forced to sum to 100%.
//   - A group touching a sampling gap produces unknown mass endpoints: the
//     candidate stays legal but carries unknown/partial status.
func buildCandidates(an *Analysis, layers *domain.ImportedLayers, p domain.AnalysisParams) {
	im := activeInitialMass(layers)
	groups := an.PeakGroups
	// Each multi-subpeak group contributes a variant index; cartesian product
	// gives every legal decomposition.
	type variant struct {
		stages []Stage
		note   string
	}
	perGroup := make([][]variant, len(groups))
	for gi, g := range groups {
		merged := an.stageFromWindow(layers, g.MergedStartS, g.MergedEndS, g.MergedApexS, g.ID, fmt.Sprintf("G%d", gi+1), im, 0, g.TouchesGap)
		if len(g.SubPeaks) <= 1 {
			perGroup[gi] = []variant{{stages: []Stage{merged}}}
			continue
		}
		vars := []variant{{stages: []Stage{merged}, note: "merged-overlap"}}
		// split variant at the detected saddle(s)
		var split []Stage
		for si, sp := range g.SubPeaks {
			st := an.stageFromWindow(layers, sp.StartS, sp.EndS, sp.ApexS, g.ID,
				fmt.Sprintf("G%d.%d", gi+1, si+1), im, si, g.TouchesGap)
			st.Overlaps = true
			if si < len(g.SubPeaks)-1 {
				st.BoundaryNote = fmt.Sprintf("boundary at saddle t=%.1fs", g.SubPeaks[si].EndS)
			}
			split = append(split, st)
		}
		vars = append(vars, variant{stages: split, note: "saddle-split"})
		perGroup[gi] = vars
	}
	combos := combine(perGroup)
	for ci, combo := range combos {
		cand := StageCandidate{ID: hnum("cand", an.AnalysisID, ci), Label: fmt.Sprintf("C%d", ci+1), Status: "candidate"}
		var total float64
		allKnown := true
		tags := ""
		for _, v := range combo {
			cand.Stages = append(cand.Stages, v.stages...)
			if v.note != "" {
				tags += v.note + ";"
			}
		}
		for _, st := range cand.Stages {
			if st.Known {
				total += st.MassLossPct
			} else {
				allKnown = false
			}
		}
		cand.TotalLossPct = round3(total)
		cand.TotalKnown = allKnown
		if tags != "" {
			cand.RejectReason = "" // kept as legal; tag is visible per-stage
		}
		// Legal candidates with unknown mass due to gaps are explicitly flagged,
		// never silently accepted or dropped.
		for _, st := range cand.Stages {
			if !st.Known {
				cand.Status = "partial"
			}
		}
		an.Candidates = append(an.Candidates, cand)
	}
}

func combine(perGroup [][]variant) [][]variant {
	out := [][]variant{{}}
	for _, opts := range perGroup {
		var next [][]variant
		for _, prefix := range out {
			for _, o := range opts {
				cp := make([]variant, 0, len(prefix)+1)
				cp = append(cp, prefix...)
				cp = append(cp, o)
				next = append(next, cp)
			}
		}
		out = next
	}
	if len(out) == 1 && len(out[0]) == 0 {
		return nil
	}
	return out
}

// stageFromWindow reads corrected mass at window endpoints inside one segment.
// If an endpoint falls inside a sampling gap (no bracket point), Known=false.
func (an *Analysis) stageFromWindow(layers *domain.ImportedLayers, startS, endS, apexS float64, groupID, label string, im *domain.InitialMassLayer, subIdx int, touchesGap bool) Stage {
	st := Stage{ID: hnum("stg", groupID, label), GroupID: groupID, Label: label,
		StartS: round2(startS), EndS: round2(endS), ApexS: round2(apexS), Known: true}
	m0, ok0 := an.massAt(startS)
	m1, ok1 := an.massAt(endS)
	if !ok0 || !ok1 || touchesGap {
		st.Known = false
		st.BoundaryNote = appendNote(st.BoundaryNote, "stage endpoint adjacent to sampling gap; mass loss unknown, no interpolation")
	}
	st.StartMassMG = round4(m0)
	st.EndMassMG = round4(m1)
	st.MassLossMG = round4(m0 - m1)
	if im != nil && im.Mass.Valid && st.Known {
		st.MassLossPct = round3((m0 - m1) / im.Mass.Value * 100)
	}
	st.GasAtValve, st.GasAtReaction = gasAt(an, apexS, layers)
	return st
}

// massAt returns corrected mass exactly at an observed point time; it never
// interpolates. Window endpoints are always detected extrema (observed times).
func (an *Analysis) massAt(t float64) (float64, bool) {
	for i := range an.Points {
		if near(an.Points[i].TimeS, t) {
			return an.Points[i].CorrMassMG, true
		}
	}
	return 0, false
}

func activeInitialMass(layers *domain.ImportedLayers) *domain.InitialMassLayer {
	var best *domain.InitialMassLayer
	for i := range layers.InitialM {
		if layers.InitialM[i].Active {
			best = &layers.InitialM[i]
		}
	}
	return best
}

// buildResidue assembles the expandable calculation chain for the final residue.
func buildResidue(an *Analysis, layers *domain.ImportedLayers) {
	im := activeInitialMass(layers)
	chain := ResidueChain{ID: hnum("res", an.AnalysisID)}
	if im == nil || !im.Mass.Valid {
		chain.Known = false
		chain.UnknownReason = "no active valid initial mass; residue percent undetermined"
		chain.Nodes = append(chain.Nodes, CalcNode{
			NodeID: hnum(chain.ID, "noim"), Label: "initial mass",
			Diag: "no valid initial mass layer", Unknown: true,
		})
		an.Residue = chain
		return
	}
	an.InitialMassMG = &im.Mass.Value
	an.InitialMassRef = hnum("im", im.Line, im.Version)
	// Final mass = mean corrected mass over the last observed 30 s window of
	// the LAST segment; a gap at the tail widens the unknown window.
	last := an.Segments[len(an.Segments)-1]
	endT := an.Points[last.PointTo].TimeS
	from := endT - 30
	var sum float64
	var n int
	for i := last.PointFrom; i <= last.PointTo; i++ {
		if an.Points[i].TimeS >= from {
			sum += an.Points[i].CorrMassMG
			n++
		}
	}
	final := sum / float64(n)
	chain.Known = true
	chain.FinalMassMG = round4(final)
	chain.FinalWindowFrom = round2(from)
	chain.FinalWindowTo = round2(endT)
	chain.InitialMassMG = round4(im.Mass.Value)
	chain.ResiduePct = round3(final / im.Mass.Value * 100)
	fv := final
	iv := im.Mass.Value
	rp := final / im.Mass.Value * 100
	chain.Nodes = []CalcNode{
		{NodeID: hnum(chain.ID, "n0"), Label: "active initial mass (highest declared version)", Value: &iv, Unit: domain.UnitMilligram,
			Sources: []string{an.InitialMassRef}, Formula: fmt.Sprintf("initial v%d = %.4f mg (raw %q)", im.Version, im.Mass.Value, im.Mass.Raw)},
		{NodeID: hnum(chain.ID, "n1"), Label: "final plateau window", Unit: domain.UnitSecond,
			Value: ptr(30), Formula: fmt.Sprintf("last 30s of final segment [%.1f, %.1f], n=%d points", from, endT, n)},
		{NodeID: hnum(chain.ID, "n2"), Label: "final mass = mean(corrected mass) in window", Value: &fv, Unit: domain.UnitMilligram,
			Formula: "corrected = raw - drift(t) - blank(t); averaged, never interpolated"},
		{NodeID: hnum(chain.ID, "n3"), Label: "residue %", Value: &rp, Unit: domain.UnitPercent,
			Formula: "residue% = final mass / initial mass * 100"},
	}
	if last.PointTo-last.PointFrom+1 < 3 {
		chain.Known = false
		chain.UnknownReason = "final segment too short after sampling gap"
		chain.Nodes = append(chain.Nodes, CalcNode{NodeID: hnum(chain.ID, "non"), Label: "residue reliability", Unknown: true,
			Diag: chain.UnknownReason})
	}
	an.Residue = chain
}

func ptr(v float64) *float64 { return &v }

func appendNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }
