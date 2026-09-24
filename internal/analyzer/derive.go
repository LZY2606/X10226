// Package analyzer implements the versioned TGA derivation pipeline.
//
// Domain rules encoded here:
//   - Gas delay and thermal lag mean valve-switch time != sample reaction
//     time.
//   - Stages may overlap and their percentage losses are not forced to sum
//     to 100.
//   - Sampling gaps split the series; no interpolation crosses a gap.
//   - When overlapping peaks cannot be uniquely resolved, all legal
//     candidates are kept (with rejected reasons recorded too).
package analyzer

import (
	"fmt"
	"math"
	"sort"

	"github.com/example/tga/internal/domain"
)

type finder struct {
	params domain.Params
	snap   *Snapshot
}

// Run executes the derivation pipeline deterministically over the layered
// snapshot.
func Run(snap *Snapshot, p domain.Params) *domain.AnalysisRunPayload {
	f := &finder{params: p, snap: snap}

	out := &domain.AnalysisRunPayload{
		AlgorithmVersion: domain.AlgorithmVersion,
		ParamVersion:     "", // set by engine
		InitialMass:      snap.InitialMass,
		InitialMassSource: snap.InitialRef,
	}
	out.SourceBatches = make([]string, 0, len(snap.Batches))
	for _, b := range snap.Batches {
		out.SourceBatches = append(out.SourceBatches, b.ID)
	}
	out.ParamFingerprint = FingerprintParams(p)

	// 1. Build points sorted in time, splitting at sampling gaps.
	points, segments, gapCount := f.buildSeries()
	out.Series.Points = points
	out.Series.Segments = segments
	out.Series.GasWindows = f.gasWindows()
	out.Series.FurnaceProgram = f.furnaceProgram()

	// 2. Diagnostics for missing layers and unconvertible values.
	out.Diagnostics = append(out.Diagnostics, snap.Diagnostics...)
	out.Unconvertibles = append(out.Unconvertibles, snap.Unconvertibles...)
	for _, c := range snap.Unconvertibles {
		out.Findings = append(out.Findings, domain.Finding{
			Code: domain.FindUnconvertible, Severity: "warning",
			Message: fmt.Sprintf("value %q at %s:%d retained, excluded from derivation",
				c.Raw, c.Section, c.Line),
			Evidence: map[string]interface{}{"section": c.Section, "line": c.Line, "raw": c.Raw},
		})
	}
	if len(snap.Batches) > 0 {
		for _, b := range snap.Batches {
			if b.Partial {
				out.Findings = append(out.Findings, domain.Finding{
					Code: domain.FindPartialImport, Severity: "warning",
					Message: "partial import batch " + b.ID + " committed; only valid rows derived",
					Evidence: map[string]interface{}{"batch_id": b.ID},
				})
			}
		}
	}

	// 3. Machine-readable domain judgments (always present, evidence-bearing).
	out.Findings = append(out.Findings,
		domain.Finding{
			Code: domain.FindGasDelay, Severity: "info",
			Message: fmt.Sprintf("valve events shifted by gas delay %.0f s before they reach the sample",
				p.GasDelayS),
			Evidence: map[string]interface{}{"gas_delay_s": p.GasDelayS},
		},
		domain.Finding{
			Code: domain.FindThermalLag, Severity: "info",
			Message: fmt.Sprintf("sample temperature lags program with first-order tau %.0f s",
				p.ThermalTauS),
			Evidence: map[string]interface{}{"thermal_tau_s": p.ThermalTauS},
		},
		domain.Finding{
			Code: domain.FindOverlappingStages, Severity: "info",
			Message: "stages may overlap; percentage losses are reported independently and are not normalized to sum to 100",
		},
	)
	if gapCount > 0 {
		out.Findings = append(out.Findings, domain.Finding{
			Code: domain.FindGapNoInterp, Severity: "warning",
			Message: fmt.Sprintf("%d sampling gap(s) split the series; no interpolation crosses a gap", gapCount),
			Evidence: map[string]interface{}{"gap_count": gapCount, "gap_max_step_s": p.GapMaxStepS},
		})
	}
	if snap.HasBlankSection && p.DriftMode != "none" {
		out.Findings = append(out.Findings, domain.Finding{
			Code: domain.FindBaselineDrift, Severity: "info",
			Message: fmt.Sprintf("balance baseline drift corrected from blank curve using %q mode", p.DriftMode),
			Evidence: map[string]interface{}{"mode": p.DriftMode},
		})
	}

	// 4. DTG smoothing + differentiation within segments only.
	nonMonotonic := f.computeDTG(points, segments)
	if nonMonotonic > 0 {
		out.Findings = append(out.Findings, domain.Finding{
			Code: domain.FindNonMonotonic, Severity: "warning",
			Message: fmt.Sprintf("%d non-monotonic point(s) flagged as noise; they are never interpolated away", nonMonotonic),
			Evidence: map[string]interface{}{"count": nonMonotonic},
		})
	}

	// 5. Peak detection within segments, then grouping of overlapping peaks.
	peaks, groups := f.detectPeaks(points, segments)
	out.Peaks = peaks

	// 6. Stages and the full candidate family (legal, rejected, unresolved).
	stages, candidates := f.buildStagesAndCandidates(points, peaks, groups)
	out.Stages = stages
	out.Candidates = candidates

	// 7. Residues with expandable calculation chains.
	out.Residues = f.residues(points, segments)

	// 8. Independent total (not a normalized stage sum).
	if len(points) > 0 && snap.InitialMass.ParseError == "" && snap.InitialMass.Value > 0 {
		first := firstMass(points)
		last := lastMass(points, segments)
		if first != nil && last != nil {
			out.TotalLossPct = (first.CorrectedMass() - last.CorrectedMass()) /
				snap.InitialMass.Value * 100
		}
	}
	out.StagesSumTo100 = false // by construction: independent, possibly overlapping

	out.RunID = computeRunID(out)
	return out
}

// corrected accessor handled via point helpers below.

func firstMass(pts []domain.Point) *domain.Point {
	for i := range pts {
		if pts[i].CorrectedMassMg != nil {
			return &pts[i]
		}
	}
	return nil
}

func lastMass(pts []domain.Point, segs [][]int) *domain.Point {
	for si := len(segs) - 1; si >= 0; si-- {
		for j := len(segs[si]) - 1; j >= 0; j-- {
			p := &pts[segs[si][j]]
			if p.CorrectedMassMg != nil {
				return p
			}
		}
	}
	return nil
}

func (p *domain.Point) CorrectedMass() float64 {
	if p.CorrectedMassMg != nil {
		return *p.CorrectedMassMg
	}
	return math.NaN()
}
