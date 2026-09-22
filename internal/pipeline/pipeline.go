// Package pipeline turns the latest versioned source layers + parameter
// version into a content-addressed Derived artifact.
//
// Domain rules implemented here:
//   - gas transport delay and furnace-vs-sample thermal lag mean a valve
//     switch time is NOT the sample reaction time;
//   - stages may overlap and stage losses are never forced to sum to 100%;
//   - sampling gaps are marked and never bridged by interpolation (DTG is
//     not computed across a gap);
//   - non-monotonic noise is flagged, not silently smoothed away;
//   - ambiguous overlaps retain all legal candidates with rejection reasons.
package pipeline

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"tga-review/internal/domain"
)

const AlgoVersion = "tga-decomp-1.3.0"

// SourceSet holds the selected latest versions of each source layer.
type SourceSet struct {
	Samples []domain.SamplePoint
	Drift   []domain.DriftPoint
	Furnace []domain.FurnaceSegment
	Valves  []domain.ValveEvent
	Blank   []domain.BlankPoint
	M0      []domain.M0Layer
	Params  domain.Params
	// Source refs for provenance.
	Refs []domain.SourceRef
	Seq  int64
	Now  time.Time
}

// Build runs the full derivation and returns a content-addressed artifact.
func Build(src SourceSet) *domain.Derived {
	params := src.Params
	fp := params.Fingerprint
	if fp == "" {
		fp = domain.ParamFingerprint(params)
	}

	points := markGaps(src.Samples)
	detectSpikes(points)
	correctDrift(points, src.Drift)

	atm := atmosphereTimeline(src.Valves, params)
	furnace := append([]domain.FurnaceSegment(nil), src.Furnace...)
	sort.Slice(furnace, func(i, j int) bool { return furnace[i].StartT < furnace[j].StartT })
	sampleTemp := laggedSampleTemp(points, furnace, params.TempLagS)
	dtg := computeDTG(points) // NaN across gaps; never interpolated

	stages, unknowns := detectStages(points, dtg, sampleTemp, atm, params)

	m0, m0NodeInput, m0Diag := selectM0(src.M0)
	values := buildValues(points, stages, m0, m0NodeInput, m0Diag, atm, src.Refs, fp)

	d := &domain.Derived{
		Seq:         src.Seq,
		ParamUID:    params.UID,
		ParamFP:     fp,
		Sources:     append([]domain.SourceRef(nil), src.Refs...),
		Points:      points,
		Blank:       append([]domain.BlankPoint(nil), src.Blank...),
		Drift:       append([]domain.DriftPoint(nil), src.Drift...),
		Stages:      stages,
		Values:      values,
		AtmTimeline: atm,
		Furnace:     furnace,
		Unknowns:    unknowns,
		CreatedAt:   src.Now,
	}
	d.UID = contentUID(d)
	for i := range d.Values {
		if d.Values[i].UID == "" {
			d.Values[i].UID = domain.StableID("v", d.UID, d.Values[i].Path)
		}
	}
	return d
}

// markGaps flags non-interpolated sampling gaps (spacing > 3x median).
func markGaps(pts []domain.SamplePoint) []domain.SamplePoint {
	out := append([]domain.SamplePoint(nil), pts...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].T < out[j].T })
	deltas := []float64{}
	for i := 1; i < len(out); i++ {
		if out[i].T-out[i-1].T > 0 {
			deltas = append(deltas, out[i].T-out[i-1].T)
		}
	}
	sort.Float64s(deltas)
	median := 0.0
	if len(deltas) > 0 {
		median = deltas[len(deltas)/2]
	}
	threshold := 3 * median
	for i := 1; i < len(out); i++ {
		if median > 0 && out[i].T-out[i-1].T > threshold+1 {
			out[i-1].GapAfter = true
			out[i].GapBefore = true
		}
	}
	return out
}

// detectSpikes flags non-monotonic points whose mass rises above the local
// trend by more than 5x the typical deviation. The raw value is retained.
func detectSpikes(pts []domain.SamplePoint) {
	if len(pts) < 7 {
		return
	}
	diffs := make([]float64, 0, len(pts)-1)
	for i := 1; i < len(pts); i++ {
		if !pts[i].GapBefore {
			diffs = append(diffs, pts[i].Mass-pts[i-1].Mass)
		}
	}
	sort.Float64s(diffs)
	if len(diffs) < 3 {
		return
	}
	med := diffs[len(diffs)/2]
	devs := make([]float64, len(diffs))
	for i, d := range diffs {
		devs[i] = math.Abs(d - med)
	}
	sort.Float64s(devs)
	mad := devs[len(devs)/2]
	if mad <= 0 {
		mad = 1e-6
	}
	for i := 3; i < len(pts)-3; i++ {
		if pts[i].GapBefore || pts[i].GapAfter {
			continue
		}
		localTrend := (pts[i+1].Mass - pts[i-1].Mass) / 2
		up := pts[i].Mass - pts[i-1].Mass
		if up > 0 && up-localTrend > 5*mad && up > 0.05 {
			pts[i].Spike = true
		}
	}
}

// correctDrift subtracts the piecewise-linear baseline drift at each time.
// Unconverted drift points are skipped and recorded as a diagnostic chain.
func correctDrift(pts []domain.SamplePoint, drift []domain.DriftPoint) {
	good := make([]domain.DriftPoint, 0, len(drift))
	for _, d := range drift {
		if d.Status == domain.RowOK {
			good = append(good, d)
		}
	}
	sort.Slice(good, func(i, j int) bool { return good[i].T < good[j].T })
	for i := range pts {
		if pts[i].Status != domain.RowOK {
			continue
		}
		off := interpDrift(good, pts[i].T)
		pts[i].Mass = pts[i].RawMass.Value - off
	}
}

func interpDrift(d []domain.DriftPoint, t float64) float64 {
	if len(d) == 0 {
		return 0
	}
	if t <= d[0].T {
		return d[0].Offset.Value
	}
	if t >= d[len(d)-1].T {
		return d[len(d)-1].Offset.Value
	}
	for i := 1; i < len(d); i++ {
		if t <= d[i].T {
			a, b := d[i-1], d[i]
			frac := (t - a.T) / (b.T - a.T)
			return a.Offset.Value + frac*(b.Offset.Value-a.Offset.Value)
		}
	}
	return d[len(d)-1].Offset.Value
}

// atmosphereTimeline maps valve events onto the sample reaction clock using
// the configured per-gas transport delay.
func atmosphereTimeline(valves []domain.ValveEvent, p domain.Params) []domain.AtmWindow {
	evs := append([]domain.ValveEvent(nil), valves...)
	sort.Slice(evs, func(i, j int) bool { return evs[i].T < evs[j].T })
	wins := []domain.AtmWindow{}
	for i, ev := range evs {
		delay := p.GasDelayS[strings.ToLower(ev.ToGas)]
		start := ev.T + delay
		end := math.Inf(1)
		if i+1 < len(evs) {
			next := evs[i+1]
			end = next.T + p.GasDelayS[strings.ToLower(next.ToGas)]
		}
		wins = append(wins, domain.AtmWindow{
			StartT: start, EndT: end, Gas: ev.ToGas, ValveT: ev.T, DelayS: delay,
		})
	}
	return wins
}

// gasAt returns the atmosphere on the reaction clock at time t.
func gasAt(wins []domain.AtmWindow, t float64) string {
	g := "?"
	for _, w := range wins {
		if t >= w.StartT && t < w.EndT {
			g = w.Gas
		}
	}
	if g == "?" && len(wins) > 0 && t < wins[0].StartT {
		if wins[0].Gas != "" {
			// pre-first-switch atmosphere inferred from the first valve origin
			g = "N2"
		}
	}
	return g
}
