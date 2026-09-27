package compute

import (
	"fmt"
	"math"
	"sort"

	"tga/internal/domain"
)

const noiseJumpMg = 0.002 // |positive mass jump| above this is retained noise

// Inputs gathers the exact direct sources for one derivation run.
type Inputs struct {
	Raw     []domain.RawPoint
	Init    *domain.InitialMass
	Drift   *domain.DriftSpec
	Furnace []domain.FurnaceSegment
	Gas     []domain.GasEvent
	Blank   []domain.BlankPoint
	Params  domain.Params
}

// Derive runs the frozen algorithm pipeline. It never returns partial series
// silently: missing inputs and unusable values become findings/gaps, and
// unbridgeable gaps stay holes (Present=false) that no interpolation crosses.
func Derive(in Inputs) domain.AnalysisResult {
	res := domain.AnalysisResult{
		ParamsFingerprint: in.Params.Fingerprint,
		AlgoVersion:       domain.AlgoVersion,
		StageSumEnforced:  false,
	}
	if in.Init == nil || !validV(in.Init.Mass) {
		res.Findings = append(res.Findings, domain.Finding{Code: "NO_INITIAL_MASS",
			Severity: "error", Message: "initial mass missing or unconvertible; TG% cannot be normalized"})
		return res
	}
	pts, gaps := buildPoints(in, &res)
	res.Gaps = gaps
	furnaceTempSeries(in, pts)
	correctDrift(in, pts, &res)
	normalize(in, pts)
	sampleTemp(in, pts)
	dtg(in, pts, &res)
	markNoise(pts, &res)
	segments := contiguousSegments(pts)
	peaks := findPeaks(pts, in.Params, segments)
	stages := buildStages(in, pts, peaks, segments, &res)
	res.Stages = stages
	res.Residues = buildResidues(in, pts, segments, &res)
	res.ValveLinks = linkValves(in, pts, &res)
	res.Series = pts
	sum := 0.0
	for _, s := range stages {
		sum += s.MassLossPct
	}
	res.StageSumPct = sum
	res.Findings = append(res.Findings, domain.Finding{Code: "STAGE_SUM_NOT_ENFORCED",
		Severity: "info",
		Message:  fmt.Sprintf("stage mass losses sum to %.3f%%; stages may overlap and are not forced to 100%%", sum),
		Data:     map[string]any{"sum_pct": round(sum, 4)}})
	return res
}

func validV(v domain.Value) bool { return v.Unit != "" }

type pt = domain.SeriesPoint

func buildPoints(in Inputs, res *domain.AnalysisResult) ([]pt, []domain.Gap) {
	var out []pt
	var gaps []domain.Gap
	gapThreshold := medianDt(in.Raw) * 2.5
	lastValidT := math.NaN()
	lastValidRow := 0
	for _, rp := range in.Raw {
		p := pt{Row: rp.Row}
		if rp.Time.Unit != "" {
			p.Time = rp.Time.Value
		}
		if rp.Temp.Unit != "" {
			p.FurnaceT = rp.Temp.Value
			p.SampleT = rp.Temp.Value
		}
		if rp.Mass.Unit != "" {
			p.MassCorr = rp.Mass.Value
			p.Present = true
		} else {
			p.Present = false
		}
		if !rp.Valid && rp.Mass.Unit == "" {
			// whole-row or mass failure: contributes a hole
		}
		if p.Present {
			if !math.IsNaN(lastValidT) && gapThreshold > 0 && p.Time-lastValidT > gapThreshold {
				gap := domain.Gap{FromRow: lastValidRow, ToRow: p.Row,
					FromT: lastValidT, ToT: p.Time, Reason: "sampling_gap"}
				gaps = append(gaps, gap)
				res.Findings = append(res.Findings, domain.Finding{
					Code: "DATA_GAP", Severity: "warning",
					Message: fmt.Sprintf("%.0fs sampling gap (rows %d-%d) is not bridged by interpolation",
						p.Time-lastValidT, lastValidRow, p.Row),
					T: lastValidT,
					Data: map[string]any{"from_t": round(lastValidT, 3), "to_t": round(p.Time, 3),
						"from_row": lastValidRow, "to_row": p.Row}})
			}
			lastValidT = p.Time
			lastValidRow = p.Row
		}
		out = append(out, p)
	}
	// mark gap-before flag on first present point after each gap, and clear
	// derived values inside holes so no drawing/interpolation crosses them.
	for i := range out {
		for _, g := range gaps {
			if out[i].Row == g.ToRow {
				out[i].GapBefore = true
			}
			if out[i].Row > g.FromRow && out[i].Row < g.ToRow {
				out[i].Present = false
			}
		}
	}
	return out, gaps
}

func medianDt(raw []domain.RawPoint) float64 {
	var ts []float64
	for _, r := range raw {
		if r.Time.Unit != "" {
			ts = append(ts, r.Time.Value)
		}
	}
	var dts []float64
	for i := 1; i < len(ts); i++ {
		if ts[i] > ts[i-1] {
			dts = append(dts, ts[i]-ts[i-1])
		}
	}
	sort.Float64s(dts)
	if len(dts) == 0 {
		return 0
	}
	return dts[len(dts)/2]
}
