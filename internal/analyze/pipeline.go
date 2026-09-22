package analyze

import (
	"math"
	"sort"

	"tga-review/internal/model"
)

// Point is one normalized sample point with derived columns.
type Point struct {
	Seq                            int
	TimeMin                        float64
	TempC                          float64
	RawMassMg                      float64
	DriftMg                        float64 // interpolated balance drift at t
	BlankMg                        float64 // interpolated blank signal at t
	CorrectedMg                    float64
	TGPct                          float64 // corrected mass / initial mass
	DTGPctMin                      float64 // %/min central diff; 0 at segment edges
	Segment                        int     // contiguous segment id; gaps break segments
	NonMonotonic                   bool    // corrected mass uptick beyond tolerance
	NonMonotonicDeltaPct           float64
}

// Finding is a machine-readable diagnostic emitted by the algorithm.
type Finding struct {
	Code     string                 `json:"code"`
	Severity string                 `json:"severity"` // info|warn|reject
	Subject  string                 `json:"subject"`
	Message  string                 `json:"message"`
	Detail   map[string]interface{} `json:"detail,omitempty"`
}

// CorrectionStep documents one correction value with its provenance chain.
type CorrectionStep struct {
	Seq     int     `json:"seq"`
	TimeMin float64 `json:"time_min"`
	RawMg   float64 `json:"raw_mg"`
	DriftMg float64 `json:"drift_mg"`
	BlankMg float64 `json:"blank_mg"`
	CorrMg  float64 `json:"corrected_mg"`
	InitialMg float64 `json:"initial_mg"`
	TGPct   float64 `json:"tg_pct"`
}

// RunResult is the full first-class output of one analysis run.
type RunResult struct {
	Params           Params            `json:"params"`
	ParamFingerprint string            `json:"param_fingerprint"`
	InputFingerprint string            `json:"input_fingerprint"`
	InitialMassMg    *float64          `json:"initial_mass_mg"`
	InitialBasis     string            `json:"initial_basis"`
	InitialVersionID string            `json:"initial_version_id"`
	InitialStatus    string            `json:"initial_status"` // ok|unknown|error
	Points           []Point           `json:"points"`
	Corrections      []CorrectionStep  `json:"corrections"`
	Gaps             []model.Gap       `json:"gaps"`
	FurnaceTemp      []Anchor          `json:"furnace_temp"`
	GasReaction      []GasAnchor       `json:"gas_reaction"`
	Candidates       []Candidate       `json:"candidates"`
	Rejections       []Rejection       `json:"rejections"`
	Residue          Residue           `json:"residue"`
	Findings         []Finding         `json:"findings"`
}

// Anchor is a furnace segment aligned to the sample time axis.
type Anchor struct {
	TimeMin float64 `json:"time_min"`
	TempC   float64 `json:"temp_c"`
	Kind    string  `json:"kind"`
	SourceSeq int   `json:"source_seq"`
}

// GasAnchor pairs a commanded valve time with the estimated reaction time.
type GasAnchor struct {
	ValveTimeMin    float64 `json:"valve_time_min"`
	ReactionTimeMin float64 `json:"reaction_time_min"`
	Gas             string  `json:"gas"`
	Action          string  `json:"action"`
	DelayMin        float64 `json:"delay_min"`
	TempLagMin      float64 `json:"temp_lag_min"`
	SourceSeq       int     `json:"source_seq"`
}

// interp linearly interpolates nodes (xs sorted ascending) at x. Outside the
// node range the nearest end value is held (baseline extrapolation, clearly
// flagged by the caller when used).
func interp(xs, ys []float64, x float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	if x <= xs[0] {
		return ys[0]
	}
	last := len(xs) - 1
	if x >= xs[last] {
		return ys[last]
	}
	i := sort.Search(last, func(i int) bool { return xs[i] >= x })
	if xs[i] == x {
		return ys[i]
	}
	lo := i - 1
	frac := (x - xs[lo]) / (xs[i] - xs[lo])
	return ys[lo] + frac*(ys[i]-ys[lo])
}

type node struct{ t, v float64 }

func sortedNodes(ts, vs []float64) ([]float64, []float64) {
	ns := make([]node, len(ts))
	for i := range ts {
		ns[i] = node{ts[i], vs[i]}
	}
	sort.Slice(ns, func(i, j int) bool { return ns[i].t < ns[j].t })
	outT := make([]float64, len(ns))
	outV := make([]float64, len(ns))
	for i, n := range ns {
		outT[i], outV[i] = n.t, n.v
	}
	return outT, outV
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
