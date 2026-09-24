// Package derive turns the six immutable raw strata into reviewable results:
// corrected TG series, DTG, furnace program interpolation, gas-at-sample
// timeline, overlapping stage candidates and residue calculation chains.
//
// Domain rules enforced here:
//
//   - Gas valve switch time != sample reaction time (gas transport delay).
//   - Furnace setpoint != sample temperature (thermal lag); a lagged program
//     is materialized explicitly.
//   - Stages may overlap and their mass losses are NOT forced to sum to 100%.
//   - Sampling gaps are never interpolated: any candidate crossing a gap is
//     reported as unknown and all legal alternatives are retained.
//   - Non-monotonic noise inside a band is flagged, not silently deleted.
//   - Every derived value carries algorithm version, parameter fingerprint
//     and direct source ids.
package derive

import (
	"sort"

	"tga-review/internal/domain"
	"tga-review/internal/events"
)

// Engine derives results against a fixed parameter revision.
type Engine struct {
	Params map[string]domain.Quantity
	FP     string
	Rev    int
}

func NewEngine(rev events.ParameterRevision) *Engine {
	p := DefaultParams()
	if rev.Revision > 0 {
		p = rev.Parameters
	}
	fp := rev.Fingerprint
	if fp == "" {
		fp = Fingerprint(p)
	}
	return &Engine{Params: p, FP: fp, Rev: rev.Revision}
}

func (e *Engine) pNum(name string) float64 {
	if q, ok := e.Params[name]; ok {
		if v, ok := q.SI(); ok {
			return v
		}
	}
	q := DefaultParams()[name]
	v, _ := q.SI()
	return v
}

// Point is a normalized sample point (seconds, kg, K).
type Point struct {
	Seq       int
	T         float64 // furnace time s
	RawMassKg float64
	TempK     float64
	CorrMassKg float64 // after drift + blank correction
	NoiseFlag bool
}

// Gap is a missing interval. No interpolation crosses it.
type Gap struct {
	FromSeq int     `json:"from_seq"`
	ToSeq   int     `json:"to_seq"`
	T0      float64 `json:"t0_s"`
	T1      float64 `json:"t1_s"`
	SpanS   float64 `json:"span_s"`
	Reason  string  `json:"reason"`
}

// DTGPoint is one derivative sample, fraction/s (negative during loss).
type DTGPoint struct {
	T        float64 `json:"t_s"`
	Value    float64 `json:"fraction_per_s"`
	InGap    bool    `json:"in_gap,omitempty"`
}

// Series is the corrected TG/DTG series with full provenance.
type Series struct {
	Points         []Point    `json:"points"`
	DTG            []DTGPoint `json:"dtg"`
	Gaps           []Gap      `json:"gaps"`
	Sources        []string   `json:"source_ids"`
	DriftModelID   string     `json:"drift_model_id,omitempty"`
	BlankID        string     `json:"blank_id,omitempty"`
	InitialMassID  string     `json:"initial_mass_id"`
	InitialMassKg  float64    `json:"initial_mass_kg"`
	AlgoVersion    string     `json:"algo_version"`
	ParamFP        string     `json:"param_fingerprint"`
	ParamRev       int        `json:"param_revision"`
	Notes          []string   `json:"notes"`
}

// Peak is a detected DTG negative excursion.
type Peak struct {
	ID          string  `json:"id"`
	T           float64 `json:"t_s"`
	TempK       float64 `json:"temp_k"`
	Depth       float64 `json:"fraction_per_s"`
	Prominence  float64 `json:"prominence"`
	LeftT       float64 `json:"left_t_s"`
	RightT      float64 `json:"right_t_s"`
	CrossesGap  bool    `json:"crosses_gap"`
	MergeGroup  int     `json:"merge_group,omitempty"`
	Sources     []string `json:"source_ids"`
	Unknown     bool    `json:"unknown,omitempty"`
	RejectReason string `json:"reject_reason,omitempty"`
}

// Stage is a mass-loss stage candidate. Stages CAN overlap.
type Stage struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	FromT         float64  `json:"from_t_s"`
	ToT           float64  `json:"to_t_s"`
	PeakIDs       []string `json:"peak_ids"`
	MassLossFrac  *float64 `json:"mass_loss_fraction,omitempty"`
	MassLossKg    *float64 `json:"mass_loss_kg,omitempty"`
	OverlapsWith  []string `json:"overlaps,omitempty"`
	CrossesGap    bool     `json:"crosses_gap"`
	Unknown       bool     `json:"unknown"`
	Alternatives  []string `json:"alternative_stage_ids,omitempty"`
	Status        string   `json:"status"` // candidate | rejected
	RejectReason  string   `json:"reject_reason,omitempty"`
	ReactionT0    float64  `json:"reaction_t0_s"` // gas-at-sample window
	ReactionT1    float64  `json:"reaction_t1_s"`
	Sources       []string `json:"source_ids"`
	AlgoVersion   string   `json:"algo_version"`
	ParamFP       string   `json:"param_fingerprint"`
	ParamRev      int      `json:"param_revision"`
}

// Residue is a final (or per-stage) residual fraction with an expandable
// calculation chain.
type Residue struct {
	ID          string             `json:"id"`
	Label       string             `json:"label"`
	Value       *float64           `json:"fraction,omitempty"`
	Unknown     bool               `json:"unknown"`
	UnknownReason string           `json:"unknown_reason,omitempty"`
	WindowT0    float64            `json:"window_t0_s"`
	WindowT1    float64            `json:"window_t1_s"`
	Chain       []ChainStep        `json:"chain"`
	Sources     []string           `json:"source_ids"`
	AlgoVersion string             `json:"algo_version"`
	ParamFP     string             `json:"param_fingerprint"`
	ParamRev    int                `json:"param_revision"`
}

// ChainStep is one readable, sourced step of a calculation chain.
type ChainStep struct {
	Order   int     `json:"order"`
	Text    string  `json:"text"`
	Value   string  `json:"value,omitempty"`
	Unit    string  `json:"unit,omitempty"`
	Source  string  `json:"source_id,omitempty"`
}

// PeaksResult / StagesResult bundle candidates plus rejected alternatives.
type PeaksResult struct {
	Peaks    []Peak  `json:"peaks"`
	Rejected []Peak  `json:"rejected"`
	Sources  []string `json:"sources"`
}

type StagesResult struct {
	Stages []Stage `json:"stages"`
}

// BuildSeries normalizes raw points and applies drift/blank correction.
func (e *Engine) BuildSeries(samples []domain.RawRecord, drifts []domain.RawRecord,
	blanks []domain.RawRecord, initial domain.RawRecord) (Series, []string) {
	var notes []string
	var pts []Point
	src := map[string]bool{initial.ID: true}
	for _, r := range samples {
		src[r.ID] = true
		sp := r.Payload.(domain.SamplePoint)
		t, _ := sp.FurnaceTime.SI()
		m, _ := sp.Mass.SI()
		tk, _ := sp.Temperature.SI()
		pts = append(pts, Point{Seq: sp.Seq, T: t, RawMassKg: m, TempK: tk, CorrMassKg: m})
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].T < pts[j].T })

	im := initial.Payload.(domain.InitialMass)
	m0, _ := im.Mass.SI()

	// drift correction: prefer explicit drift stratum, else fit blank curve
	var drift domain.DriftCorrection
	var driftID, blankID string
	if len(drifts) > 0 {
		d := drifts[len(drifts)-1]
		drift = d.Payload.(domain.DriftCorrection)
		driftID = d.ID
		src[d.ID] = true
	}
	var blankPts []Point
	if len(blanks) > 0 {
		b := blanks[len(blanks)-1]
		blankID = b.ID
		src[b.ID] = true
		for _, r := range blanks {
			bp := r.Payload.(domain.BlankPoint)
			t, _ := bp.FurnaceTime.SI()
			m, _ := bp.Mass.SI()
			tk, _ := bp.Temperature.SI()
			blankPts = append(blankPts, Point{Seq: bp.Seq, T: t, RawMassKg: m, TempK: tk})
		}
		sort.Slice(blankPts, func(i, j int) bool { return blankPts[i].T < blankPts[j].T })
		if driftID == "" && len(blankPts) >= 2 {
			drift = fitLinearDrift(blankPts)
			notes = append(notes, "drift model fitted from blank curve (no explicit drift stratum)")
		}
	}

	band := e.pNum("nonmonotonic_band_mg")
	for i := range pts {
		corr := pts[i].RawMassKg
		if slope, ok := drift.Slope.SI(); ok {
			ic, _ := drift.Intercept.SI()
			corr -= slope*(pts[i].T) + ic
		}
		corr -= blankAt(blankPts, pts[i].T)
		pts[i].CorrMassKg = corr
		if i > 0 && pts[i].CorrMassKg > pts[i-1].CorrMassKg {
			deltaMg := (pts[i].CorrMassKg - pts[i-1].CorrMassKg) * 1e6
			if deltaMg <= band*1e3 {
				pts[i].NoiseFlag = true
				if i == 1 || !pts[i-1].NoiseFlag {
					notes = append(notes, "non-monotonic uptick within noise band flagged (not removed)")
				}
			}
		}
	}

	// gap detection: adjacent time delta exceeding max_gap_span OR missing seq
	gapSpan := e.pNum("max_gap_span_s")
	var gaps []Gap
	for i := 1; i < len(pts); i++ {
		span := pts[i].T - pts[i-1].T
		missing := pts[i].Seq != pts[i-1].Seq+1
		if span > gapSpan || missing {
			g := Gap{FromSeq: pts[i-1].Seq, ToSeq: pts[i].Seq, T0: pts[i-1].T, T1: pts[i].T, SpanS: span}
			switch {
			case missing && span > gapSpan:
				g.Reason = "missing sample sequence and time span exceeds threshold"
			case missing:
				g.Reason = "missing sample sequence (no interpolation)"
			default:
				g.Reason = "time span exceeds threshold (no interpolation)"
			}
			gaps = append(gaps, g)
			notes = append(notes, "sampling gap kept as gap; no interpolation across it")
		}
	}

	dtg := e.buildDTG(pts, gaps)

	var sources []string
	for id := range src {
		sources = append(sources, id)
	}
	sort.Strings(sources)
	return Series{
		Points: pts, DTG: dtg, Gaps: gaps, Sources: sources,
		DriftModelID: driftID, BlankID: blankID,
		InitialMassID: initial.ID, InitialMassKg: m0,
		AlgoVersion: AlgoVersion, ParamFP: e.FP, ParamRev: e.Rev,
		Notes: notes,
	}, sources
}

func fitLinearDrift(pts []Point) domain.DriftCorrection {
	n := float64(len(pts))
	var sx, sy, sxx, sxy float64
	for _, p := range pts {
		x := p.T / 60 // slope expressed per minute
		y := p.RawMassKg * 1e6
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
	}
	slopeMgPerMin := (n*sxy - sx*sy) / (n*sxx - sx*sx)
	icMg := (sy - slopeMgPerMin*sx) / n
	return domain.DriftCorrection{
		Slope:     domain.Q(formatNum(slopeMgPerMin), "mg/min", "drift_slope_fitted"),
		Intercept: domain.Q(formatNum(icMg), "mg", "drift_intercept_fitted"),
		Source:    "blank_fit",
	}
}

func blankAt(blank []Point, t float64) float64 {
	if len(blank) == 0 {
		return 0
	}
	if t <= blank[0].T {
		return blank[0].RawMassKg
	}
	last := blank[len(blank)-1]
	if t >= last.T {
		return last.RawMassKg
	}
	for i := 1; i < len(blank); i++ {
		if t <= blank[i].T {
			a, b := blank[i-1], blank[i]
			f := (t - a.T) / (b.T - a.T)
			return a.RawMassKg + f*(b.RawMassKg-a.RawMassKg)
		}
	}
	return last.RawMassKg
}

// buildDTG uses centered differences but emits no derivative across gaps.
func (e *Engine) buildDTG(pts []Point, gaps []Gap) []DTGPoint {
	win := int(e.pNum("dtg_window_points"))
	if win < 1 {
		win = 1
	}
	raw := make([]float64, len(pts))
	for i := 1; i < len(pts)-1; i++ {
		if inAnyGap(gaps, pts[i-1].T, pts[i+1].T) {
			raw[i] = 0
			continue
		}
		dm := pts[i+1].CorrMassKg - pts[i-1].CorrMassKg
		dt := pts[i+1].T - pts[i-1].T
		if dt > 0 {
			raw[i] = dm / dt
		}
	}
	out := make([]DTGPoint, len(pts))
	for i := range pts {
		var sum float64
		n := 0
		for k := i - win; k <= i+win; k++ {
			if k >= 1 && k <= len(pts)-2 && !inAnyGap(gaps, pts[k-1].T, pts[k+1].T) {
				sum += raw[k]
				n++
			}
		}
		val := 0.0
		if n > 0 {
			val = sum / float64(n)
		}
		out[i] = DTGPoint{T: pts[i].T, Value: val,
			InGap: inAnyGap(gaps, pts[max0(i-1)].T, pts[min2(len(pts)-1, i+1)].T)}
	}
	return out
}

func inAnyGap(gaps []Gap, t0, t1 float64) bool {
	for _, g := range gaps {
		if t0 < g.T1 && t1 > g.T0 {
			return true
		}
	}
	return false
}

func max0(i int) int {
	if i < 0 {
		return 0
	}
	return i
}
func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
