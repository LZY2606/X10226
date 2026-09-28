// Package derive implements the TGA derivation pipeline: normalization,
// balance-drift & blank correction, DTG (no interpolation across sampling
// gaps), valve->reaction time mapping (atmosphere delay + temperature lag),
// DTG peak detection (overlap-aware), stage candidates (may overlap, never
// forced to sum to 100%), and final residue with an expandable calculation
// chain.
package derive

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"tgareview/internal/domain"
)

// AlgoVersion mirrors domain.AlgoVersion for callers.
const AlgoVersion = domain.AlgoVersion

// Point is a corrected TG sample in canonical units.
type Point struct {
	Line     int     `json:"line"`
	TS       float64 `json:"t_s"`
	T        float64 `json:"temp_c"`
	MassRaw  float64 `json:"mass_raw_mg"`
	MassCorr float64 `json:"mass_corr_mg"`
	MassPct  float64 `json:"mass_pct"`
	Segment  int     `json:"segment"`
	GapAfter bool    `json:"gap_after"`
	NonMonot bool    `json:"non_monotonic_noise"`
}

// DTGPoint is one derivative sample; DTG is undefined at gap edges.
type DTGPoint struct {
	Line      int     `json:"line"`
	TS        float64 `json:"t_s"`
	T         float64 `json:"temp_c"`
	DTGPctMin float64 `json:"dtg_pct_per_min"`
	Segment   int     `json:"segment"`
	Defined   bool    `json:"defined"`
}

// Peak is a DTG peak with its prominence.
type Peak struct {
	Line          int     `json:"line"`
	TS            float64 `json:"t_s"`
	T             float64 `json:"temp_c"`
	DTG           float64 `json:"dtg_pct_per_min"`
	Prominence    float64 `json:"prominence_pct_per_min"`
	OverlapsWith  int     `json:"overlaps_with_line,omitempty"`
	Note          string  `json:"note,omitempty"`
}

// AtmoEvent maps a valve switch onto the sample reaction timeline.
type AtmoEvent struct {
	Line        int     `json:"line"`
	ValveTS     float64 `json:"valve_t_s"`
	ReactionTS  float64 `json:"reaction_t_s"`
	FurnaceTemp float64 `json:"furnace_temp_c"`
	SampleTemp  float64 `json:"sample_temp_c"`
	Gas         string  `json:"gas"`
	Flow        float64 `json:"flow_mL_min"`
	Note        string  `json:"note"`
}

// FurnaceSeg is a normalized furnace program segment.
type FurnaceSeg struct {
	StartS  float64 `json:"start_s"`
	EndS    float64 `json:"end_s"`
	TargetC float64 `json:"target_c"`
	Rate    float64 `json:"rate_c_min"`
}

// TGResult is artifact ArtCorrectedTG.
type TGResult struct {
	Basis        string        `json:"coordinate_basis"`
	Points       []Point       `json:"points"`
	Gaps         [][2]float64  `json:"gaps"`
	InitialMass  float64       `json:"initial_mass_mg"`
	FinalMass    float64       `json:"final_mass_corr_mg"`
	M0Source     string        `json:"m0_source"`
	BlankApplied bool          `json:"blank_applied"`
	DriftApplied bool          `json:"drift_applied"`
	Chain        []domain.ChainNode `json:"chain"`
	Diagnostics  []string      `json:"diagnostics"`
}

// DTGResult is artifact ArtDTG.
type DTGResult struct {
	Series []DTGPoint         `json:"series"`
	Peaks  []Peak             `json:"peaks"`
	Chain  []domain.ChainNode `json:"chain"`
}

// AtmoResult is artifact ArtAtmoTimeline.
type AtmoResult struct {
	DelayS  float64           `json:"atmo_delay_s"`
	LagS    float64           `json:"temp_lag_s"`
	Events  []AtmoEvent       `json:"events"`
	Segment []FurnaceSeg      `json:"furnace_program"`
	Chain   []domain.ChainNode `json:"chain"`
}

// CandidatesResult groups stage and residue candidates.
type CandidatesResult struct {
	Stages   []domain.Candidate `json:"stages"`
	Residues []domain.Candidate `json:"residues"`
	Chain    []domain.ChainNode `json:"chain"`
	Unknowns []string           `json:"unknowns"`
}

// Inputs bundles the current source versions needed for a derivation.
type Inputs struct {
	Sample, Drift, Furnace, Gas, Blank, InitialMass *domain.Source
}

func (in Inputs) IDs() []string {
	var out []string
	for _, s := range []*domain.Source{in.Sample, in.Drift, in.Furnace, in.Gas, in.Blank, in.InitialMass} {
		if s != nil {
			out = append(out, s.ID)
		}
	}
	sort.Strings(out)
	return out
}

func r3(v float64) float64 { return math.Round(v*1000) / 1000 }

// furnaceTemp gives furnace temperature at time t from measured points or the
// programmed segments; no interpolation across segment boundaries.
func furnaceTemp(segs []FurnaceSeg, pts []Point, t float64) float64 {
	if i := sort.Search(len(pts), func(i int) bool { return pts[i].TS >= t }); i < len(pts) {
		if math.Abs(pts[i].TS-t) < 1e-9 {
			return pts[i].T
		}
		if i > 0 {
			a, b := pts[i-1], pts[i]
			if b.Segment == a.Segment && b.TS-a.TS > 0 {
				return a.T + (t-a.TS)*(b.T-a.T)/(b.TS-a.TS)
			}
		}
	}
	var startTemp float64
	for _, s := range segs {
		if t >= s.StartS && t <= s.EndS {
			return startTemp + (t-s.StartS)*s.Rate/60
		}
		if t > s.EndS {
			startTemp = s.TargetC
		}
	}
	return math.NaN()
}

func notef(format string, a ...any) string { return fmt.Sprintf(format, a...) }
