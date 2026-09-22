// Package compute implements derivation algorithm "tga-decomp-v1".
// Rules of record:
//   - valve switch time != sample reaction time (atmosphere delay + temp lag);
//   - stages may overlap and stage losses are NOT forced to sum to 100%;
//   - data gaps are never bridged by interpolation;
//   - when a split is not unique, ALL legal candidates are retained.
package compute

import (
	"fmt"
	"math"
	"sort"
	"time"

	"tgasys/internal/domain"
	"tgasys/internal/ingest"
)

const AlgoVersion = "tga-decomp-v1"

// FixedSeed controls any deterministic tie breaking (no RNG is otherwise
// needed; kept explicit per project testing rules).
const FixedSeed = int64(20260919)

// Engine derives results from the current layers and parameters.
type Engine struct{}

func New() *Engine { return &Engine{} }

// Input bundles everything a derivation may directly depend on.
type Input struct {
	Layers ingest.LayerSet
	Params *domain.Parameters
}

// Derive performs the full pipeline and returns a result with findings.
func (e *Engine) Derive(in Input, projectID, resultID string, sources []domain.SourceRef, now time.Time) *domain.Result {
	res := &domain.Result{
		ID: resultID, ProjectID: projectID,
		AlgoVersion: AlgoVersion,
		ParamID:     in.Params.ID,
		ParamFingerprint: in.Params.Fingerprint,
		DirectSources: sources,
		CreatedAt:   now,
	}
	layers := in.Layers
	p := in.Params
	res.Issues = append(res.Issues, layers.Issues...)

	// 1. Initial mass layer (latest revised/tare wins, chain keeps all).
	initMass, initChain, initIssues := chooseInitialMass(layers.Initial)
	res.Issues = append(res.Issues, initIssues...)

	// 2. Drift + blank correction on raw sample masses.
	drift := fitDrift(layers.DriftRows, layers.Samples)
	blank := fitBlank(layers.Blank)
	corr := make([]domain.SamplePoint, len(layers.Samples))
	copy(corr, layers.Samples)
	nonMono := 0
	for i := range corr {
		m := corr[i].M.Value
		orig := m
		switch p.DriftModel {
		case "linear":
			m -= drift.at(corr[i].T)
		case "none":
		}
		if p.BlankSubtract && blank != nil {
			m -= blank.at(corr[i].T)
		}
		corr[i].M.Value = m
		if i > 0 {
			if m > corr[i-1].M.Value+1e-9 {
				nonMono++
			}
		}
		_ = orig
	}
	if nonMono > 0 && !p.NoiseMonotonic {
		res.Findings = append(res.Findings, domain.Finding{
			Code: "noise.non_monotonic", Severity: "info",
			Message: fmt.Sprintf("%d mass upticks retained (non-monotonic noise, no forced descent)", nonMono),
			Data:    map[string]any{"count": nonMono},
		})
	}

	// 3. Gaps are detected from raw sampling; never interpolated.
	gaps := detectGaps(corr)
	res.Gaps = gaps
	if len(gaps) > 0 {
		for _, g := range gaps {
			res.Findings = append(res.Findings, domain.Finding{
				Code: "sampling.gap", Severity: "warning",
				Message: fmt.Sprintf("data gap %.0fs..%.0fs not interpolated", g.StartT, g.EndT),
				StartT:  &g.StartT, EndT: &g.EndT,
				Data:    map[string]any{"reason": g.Reason},
			})
		}
	}

	// 4. Furnace setpoint + lagged sample-side temperature.
	furnace := buildFurnace(layers.Furnace, p.TempLagTauS, corr)
	res.Furnace = furnace

	// 5. Atmosphere segments: reaction time = valve time + delay.
	atm, atmFindings := buildAtmosphere(layers.Gas, p.AtmDelayS, furnace)
	res.AtmSegments = atm
	res.Findings = append(res.Findings, atmFindings...)

	// 6. TG % series (no crossing interpolation across gaps).
	if initMass > 0 {
		tg := buildTG(corr, initMass, gaps)
		res.TG = tg
		res.DTG = buildDTG(tg, gaps, p.DTGWindowS)
	} else {
		res.Issues = append(res.Issues, domain.Issue{
			Code: "initial.missing", Severity: "error",
			Message: "no usable initial mass; TG percentages cannot be computed",
		})
	}

	// 7. Peaks and stage candidates (overlap aware).
	stages, stageFindings := buildStages(res.DTG, res.TG, atm, furnace, gaps, p, resultID)
	res.Stages = stages
	res.Findings = append(res.Findings, stageFindings...)

	// 8. Residues with full calculation chains.
	residues := buildResidues(res.TG, initMass, corr, layers.Initial, initChain, drift, blank, p, gaps)
	res.Residues = residues

	res.Samples = corr
	sortIssues(res.Issues)
	return res
}

func sortIssues(is []domain.Issue) {
	sort.SliceStable(is, func(i, j int) bool {
		if is[i].Severity != is[j].Severity {
			return is[i].Severity < is[j].Severity
		}
		return is[i].Code < is[j].Code
	})
}

func chooseInitialMass(ims []ingest.InitialMass) (float64, []domain.ChainStep, []domain.Issue) {
	var issues []domain.Issue
	chain := []domain.ChainStep{{
		Order: 0, Op: "initial.layer_scan",
		Detail: fmt.Sprintf("scan %d initial-mass statements, latest revised/tare wins by order", len(ims)),
		Output: "selection pending",
	}}
	best := -1
	for i, im := range ims {
		chain = append(chain, domain.ChainStep{
			Order: i + 1, Op: "initial.statement",
			Detail: fmt.Sprintf("statement #%d basis=%s mass=%s", i+1, im.Basis, im.M.Raw),
			Output: formatMass(im.M),
		})
		if im.M.Unconvertible {
			issues = append(issues, domain.Issue{Code: "initial.unconvertible", Severity: "error", Message: "initial mass kept raw: " + im.M.Diagnostic})
			continue
		}
		if best < 0 {
			best = i
			continue
		}
		// revised > tare > declared; same basis -> later order wins.
		if rank(im.Basis) >= rank(ims[best].Basis) {
			best = i
		}
	}
	if best < 0 {
		chain = append(chain, domain.ChainStep{Order: len(chain), Op: "initial.select", Detail: "no usable statement", Output: "unknown"})
		return 0, chain, issues
	}
	chosen := ims[best]
	chain = append(chain, domain.ChainStep{
		Order: len(chain), Op: "initial.select",
		Detail: fmt.Sprintf("selected statement #%d (%s)", best+1, chosen.Basis),
		Output: fmt.Sprintf("%.6f mg", chosen.M.Value),
	})
	return chosen.M.Value, chain, issues
}

func rank(b string) int {
	switch b {
	case "revised":
		return 3
	case "tare":
		return 2
	default:
		return 1
	}
}

func formatMass(q domain.Q) string {
	if q.Unconvertible {
		return "raw:" + q.Raw + " (" + q.Diagnostic + ")"
	}
	return fmt.Sprintf("%.6f mg", q.Value)
}

func detectGaps(pts []domain.SamplePoint) []domain.Gap {
	var gaps []domain.Gap
	if len(pts) < 3 {
		return gaps
	}
	dts := make([]float64, 0, len(pts)-1)
	for i := 1; i < len(pts); i++ {
		dts = append(dts, pts[i].T-pts[i-1].T)
	}
	sorted := append([]float64{}, dts...)
	sort.Float64s(sorted)
	median := sorted[len(sorted)/2]
	threshold := median * 4
	if threshold < 30 {
		threshold = 30
	}
	for i := 1; i < len(pts); i++ {
		dt := pts[i].T - pts[i-1].T
		missing := false
		for _, sp := range pts {
			if noteHas(sp.Note, "missing") {
				_ = sp
			}
		}
		_ = missing
		if dt > threshold {
			gaps = append(gaps, domain.Gap{
				StartT: pts[i-1].T, EndT: pts[i].T,
				Reason: fmt.Sprintf("interval %.1fs > 4x median %.1fs", dt, median),
			})
		}
		if noteHas(pts[i].Note, "gap") || noteHas(pts[i].Note, "missing") {
			gaps = append(gaps, domain.Gap{
				StartT: pts[i-1].T, EndT: pts[i].T,
				Reason: "explicit gap marker in sample note",
			})
		}
	}
	return gaps
}

func noteHas(note, word string) bool {
	return contains(note, word)
}

func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func inGap(t float64, gaps []domain.Gap) bool {
	for _, g := range gaps {
		if t > g.StartT && t < g.EndT {
			return true
		}
	}
	return false
}

func nearGapEdge(t float64, gaps []domain.Gap) bool {
	for _, g := range gaps {
		if math.Abs(t-g.StartT) < 1e-9 || math.Abs(t-g.EndT) < 1e-9 {
			return true
		}
	}
	return false
}
