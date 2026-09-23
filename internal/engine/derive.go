package engine

import (
	"math"
	"sort"

	"tga-audit/internal/domain"
)

// Inputs is the immutable snapshot a derivation consumes. All numeric fields
// are canonical units (mg, s, deg C).
type Inputs struct {
	Params  Params
	Points  []domain.SamplePoint
	Drift   *domain.DriftModel
	Furnace []domain.FurnaceSegment
	Gas     []domain.GasEvent
	Blank   []domain.BlankPoint
	M0      *domain.M0Estimate
	// SourceRevs maps stream kind to the revision id these records came from,
	// so each derived value can name its direct sources.
	SourceRevs map[domain.Kind]string
}

// Gap marks a sampling discontinuity. Derivation never interpolates across it.
type Gap struct {
	AfterIndex  int     `json:"after_index"`
	BeforeIndex int     `json:"before_index"`
	FromS       float64 `json:"from_s"`
	ToS         float64 `json:"to_s"`
	DurationS   float64 `json:"duration_s"`
}

// DerivedPoint is the corrected-series view with DTG and quality flags.
type DerivedPoint struct {
	Index        int     `json:"index"`
	T            float64 `json:"t_s"`
	FurnaceT     float64 `json:"furnace_t_c"`
	SampleT      float64 `json:"sample_t_c"`
	RawMassMg    float64 `json:"raw_mass_mg"`
	CorrMassMg   float64 `json:"corr_mass_mg"`
	DTG          float64 `json:"dtg_ug_per_s"` // negative during loss
	GapAfter     bool    `json:"gap_after"`
	NonMonotonic bool    `json:"non_monotonic"`
	Usable       bool    `json:"usable"`
}

// Result is one complete derivation against one parameter fingerprint.
type Result struct {
	Params      Params         `json:"params"`
	Fingerprint string         `json:"fingerprint"`
	Algorithm   string         `json:"algorithm_version"`
	Points      []DerivedPoint `json:"points"`
	Gaps        []Gap          `json:"gaps"`
	Segments    []Segment      `json:"segments"`
	Candidates  []Candidate    `json:"candidates"`
	GasTimeline []GasReaction  `json:"gas_timeline"`
	Residue     *Residue       `json:"residue"`
	Diagnostics []string       `json:"diagnostics"`
}

// Segment is a maximal run of usable points between gaps/boundaries.
type Segment struct {
	ID           string  `json:"id"`
	StartIndex   int     `json:"start_index"`
	EndIndex     int     `json:"end_index"`
	StartS       float64 `json:"start_s"`
	EndS         float64 `json:"end_s"`
	BoundedByGap bool    `json:"bounded_by_gap"`
}

// GasReaction maps a valve command to the estimated sample-reaction window.
// Valve switch time != sample reaction time by construction.
type GasReaction struct {
	Index       int     `json:"index"`
	ValveAtS    float64 `json:"valve_at_s"`
	ReactionAtS float64 `json:"reaction_at_s"`
	FromGas     string  `json:"from_gas"`
	ToGas       string  `json:"to_gas"`
	DelayS      float64 `json:"delay_s"`
	ThermalLagC float64 `json:"thermal_lag_c"`
	SourceKey   string  `json:"source_key"`
}

// Candidate is one legal stage interpretation (DTG peak / overlap cluster).
type Candidate struct {
	Key               string   `json:"key"`
	Kind              string   `json:"kind"` // "peak" or "overlap_cluster"
	PeakIndex         int      `json:"peak_index"`
	BoundaryL         int      `json:"boundary_l_index"`
	BoundaryR         int      `json:"boundary_r_index"`
	PeakT             float64  `json:"peak_t_s"`
	PeakSampleT       float64  `json:"peak_sample_t_c"`
	PeakDTGUgPerS     float64  `json:"peak_dtg_ug_per_s"`
	MassLossMg        float64  `json:"mass_loss_mg"`
	MassLossPct       *float64 `json:"mass_loss_pct,omitempty"` // nil if m0 unknown
	OverlapMemberKeys []string `json:"overlap_member_keys,omitempty"`
	CrossesGap        bool     `json:"crosses_gap"`
	ForcedByGap       bool     `json:"forced_by_gap"`
	LinkedGasKey      string   `json:"linked_gas_key,omitempty"`
	RejectedReason    string   `json:"rejected_reason,omitempty"`
	MemberKeys        []string `json:"member_keys"` // direct source point keys
}

// ChainNode is one step in an expandable residue calculation chain.
type ChainNode struct {
	ID         string  `json:"id"`
	Step       string  `json:"step"`
	Value      float64 `json:"value"`
	Unit       string  `json:"unit"`
	SourceKey  string  `json:"source_key,omitempty"`
	SourceKind string  `json:"source_kind,omitempty"`
	Note       string  `json:"note,omitempty"`
	Unknown    bool    `json:"unknown,omitempty"`
}

// Residue is the final residue result with full calculation chain.
type Residue struct {
	ValuePct       *float64    `json:"value_pct"` // nil => unknown, not a zero
	FinalMassMg    *float64    `json:"final_mass_mg"`
	InitialMassMg  *float64    `json:"initial_mass_mg"`
	Status         string      `json:"status"` // "ok","unknown_m0","gap_before_final"
	Chain          []ChainNode `json:"chain"`
	UncertaintyPct float64     `json:"uncertainty_pct"`
	StageSumNote   string      `json:"stage_sum_note"`
}

// Derive runs the full pipeline. It tolerates unusable source points (they are
// flagged, never interpolated over).
func Derive(in Inputs) Result {
	r := Result{Params: in.Params, Fingerprint: Fingerprint(in.Params), Algorithm: AlgorithmVersion}
	usable := usablePoints(in.Points)
	if len(usable) < 2 {
		r.Diagnostics = append(r.Diagnostics, "insufficient_usable_points")
		return r
	}

	// 1. Drift correction and base series.
	pts := make([]DerivedPoint, 0, len(usable))
	for _, p := range usable {
		corr := p.Mass.Numeric
		if in.Drift != nil && in.Drift.Slope.Normalized {
			corr = p.Mass.Numeric - (in.Drift.Slope.Numeric*p.Elapsed.Numeric + driftIntercept(in.Drift))
		}
		pts = append(pts, DerivedPoint{
			Index: p.Index, T: p.Elapsed.Numeric, FurnaceT: p.FurnaceT.Numeric,
			SampleT: p.SampleT.Numeric, RawMassMg: p.Mass.Numeric, CorrMassMg: corr, Usable: true,
		})
	}

	// 2. Gaps (never bridged).
	r.Gaps = findGaps(pts, in.Params.GapFactor)
	gapAfter := map[int]bool{}
	for _, g := range r.Gaps {
		gapAfter[g.AfterIndex] = true
	}
	for i := range pts {
		pts[i].GapAfter = gapAfter[pts[i].Index]
		if i > 0 && pts[i].CorrMassMg > pts[i-1].CorrMassMg*(1+in.Params.NonMonotonicRel) && !pts[i-1].GapAfter {
			pts[i].NonMonotonic = true
		}
	}

	// 3. DTG (centered, confined within gap-bounded runs; endpoints one-sided).
	computeDTG(pts, r.Gaps, in.Params.SmoothingWindowS)

	// 4. Segments (gap-bounded).
	r.Segments = buildSegments(pts, r.Gaps)

	// 5. Atmosphere reaction timeline (delay + thermal lag).
	r.GasTimeline = buildGasTimeline(in.Gas, in.Params)

	// 6. Candidates: peaks plus overlap clusters; gap-aware boundaries.
	r.Candidates = findCandidates(pts, r.Gaps, in.Params, r.GasTimeline, in.SourceRevs)

	// 7. Mass loss percentages require m0; otherwise candidates stay valid but
	// percentages are unknown first-class state.
	var m0 *float64
	if in.M0 != nil && in.M0.Mass.Normalized {
		v := in.M0.Mass.Numeric
		m0 = &v
	}
	assignMassLoss(r.Candidates, pts, m0)

	// 8. Final residue with expandable chain.
	r.Residue = buildResidue(pts, r.Gaps, in, m0)

	r.Points = pts
	return r
}

func driftIntercept(d *domain.DriftModel) float64 {
	if d.Intercept.Normalized {
		return d.Intercept.Numeric
	}
	return 0
}

func usablePoints(in []domain.SamplePoint) []domain.SamplePoint {
	out := make([]domain.SamplePoint, 0, len(in))
	for _, p := range in {
		if p.Elapsed.Normalized && p.FurnaceT.Normalized && p.SampleT.Normalized && p.Mass.Normalized {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Elapsed.Numeric < out[j].Elapsed.Numeric })
	return out
}

func medianInterval(pts []DerivedPoint) float64 {
	d := make([]float64, 0, len(pts)-1)
	for i := 1; i < len(pts); i++ {
		d = append(d, pts[i].T-pts[i-1].T)
	}
	sort.Float64s(d)
	if len(d) == 0 {
		return math.NaN()
	}
	return d[len(d)/2]
}

func findGaps(pts []DerivedPoint, factor float64) []Gap {
	if factor <= 0 {
		factor = 4
	}
	med := medianInterval(pts)
	var gaps []Gap
	for i := 1; i < len(pts); i++ {
		dt := pts[i].T - pts[i-1].T
		if dt > factor*med && med > 0 {
			gaps = append(gaps, Gap{
				AfterIndex: pts[i-1].Index, BeforeIndex: pts[i].Index,
				FromS: pts[i-1].T, ToS: pts[i].T, DurationS: dt,
			})
		}
	}
	return gaps
}

func computeDTG(pts []DerivedPoint, gaps []Gap, windowS float64) {
	gapBefore := map[int]bool{}
	for _, g := range gaps {
		gapBefore[g.BeforeIndex] = true
	}
	raw := make([]float64, len(pts))
	// local derivative, one-sided at gap boundaries.
	for i := range pts {
		switch {
		case i == 0 || gapBefore[pts[i].Index]:
			if i+1 < len(pts) && !pts[i].GapAfter {
				raw[i] = 1000 * (pts[i+1].CorrMassMg - pts[i].CorrMassMg) / (pts[i+1].T - pts[i].T)
			}
		case pts[i].GapAfter:
			raw[i] = 1000 * (pts[i].CorrMassMg - pts[i-1].CorrMassMg) / (pts[i].T - pts[i-1].T)
		default:
			raw[i] = 1000 * (pts[i+1].CorrMassMg - pts[i-1].CorrMassMg) / (pts[i+1].T - pts[i-1].T)
		}
	}
	// centered boxcar smoothing, but never averaging across a gap.
	half := windowS / 2
	for i := range pts {
		var sum float64
		var n int
		for j := i; j >= 0; j-- {
			if pts[i].T-pts[j].T > half {
				break
			}
			if j < i && (pts[j].GapAfter || gapBefore[pts[j+1].Index]) {
				break
			}
			sum += raw[j]
			n++
		}
		for j := i + 1; j < len(pts); j++ {
			if pts[j].T-pts[i].T > half {
				break
			}
			if pts[j-1].GapAfter {
				break
			}
			sum += raw[j]
			n++
		}
		if n > 0 {
			pts[i].DTG = sum / float64(n)
		}
	}
}

func buildSegments(pts []DerivedPoint, gaps []Gap) []Segment {
	var segs []Segment
	start := 0
	gapAfter := map[int]int{}
	for _, g := range gaps {
		gapAfter[g.AfterIndex] = g.BeforeIndex
	}
	for i := 0; i < len(pts); i++ {
		if _, bounded := gapAfter[pts[i].Index]; bounded {
			segs = append(segs, Segment{
				ID:         segmentKey(pts[start].Index, pts[i].Index),
				StartIndex: pts[start].Index, EndIndex: pts[i].Index,
				StartS: pts[start].T, EndS: pts[i].T, BoundedByGap: true,
			})
			start = i + 1
		}
	}
	if start < len(pts) {
		segs = append(segs, Segment{
			ID:         segmentKey(pts[start].Index, pts[len(pts)-1].Index),
			StartIndex: pts[start].Index, EndIndex: pts[len(pts)-1].Index,
			StartS: pts[start].T, EndS: pts[len(pts)-1].T,
		})
	}
	return segs
}

func segmentKey(a, b int) string { return "seg:" + itoa(a) + "-" + itoa(b) }

func buildGasTimeline(gas []domain.GasEvent, p Params) []GasReaction {
	out := make([]GasReaction, 0, len(gas))
	for _, g := range gas {
		if !g.At.Normalized {
			continue
		}
		out = append(out, GasReaction{
			Index: g.Index, ValveAtS: g.At.Numeric,
			ReactionAtS: g.At.Numeric + p.GasDelayS,
			FromGas:     g.FromGas, ToGas: g.ToGas,
			DelayS: p.GasDelayS, ThermalLagC: p.ThermalLagC,
			SourceKey: "gas#" + itoa(g.Index),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ValveAtS < out[j].ValveAtS })
	return out
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	n := 0
	for i > 0 {
		b[n] = byte('0' + i%10)
		n++
		i /= 10
	}
	if neg {
		b[n] = '-'
		n++
	}
	for l, r := 0, n-1; l < r; l, r = l+1, r-1 {
		b[l], b[r] = b[r], b[l]
	}
	return string(b[:n])
}
