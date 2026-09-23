package core

import (
	"math"
	"sort"
)

const AlgoVersion = "tga-decomp/3"

// Params are the tunable analysis parameters. Every derived result carries a
// fingerprint of the effective parameters.
type Params struct {
	AtmosphereDelayS     float64 `json:"atmosphere_delay_s"`
	TempLagS             float64 `json:"temp_lag_s"`
	GapFactor            float64 `json:"gap_factor"`
	PeakThresholdMgPerS  float64 `json:"peak_threshold_mg_per_s"`
	OverlapWindowS       float64 `json:"overlap_window_s"`
	SmoothWindow         int     `json:"smooth_window"`
	NoiseEpsMg           float64 `json:"noise_eps_mg"`
}

func DefaultParams() Params {
	return Params{
		AtmosphereDelayS:    90,
		TempLagS:            45,
		GapFactor:           2.5,
		PeakThresholdMgPerS: 0.002,
		OverlapWindowS:      120,
		SmoothWindow:        5,
		NoiseEpsMg:          0.005,
	}
}

type ChainEntry struct {
	Step   string   `json:"step"`
	Expr   string   `json:"expr,omitempty"`
	Value  *float64 `json:"value,omitempty"`
	Unit   string   `json:"unit,omitempty"`
	Source string   `json:"source,omitempty"`
	Note   string   `json:"note,omitempty"`
}

type corrPoint struct {
	T     float64 `json:"t_s"`
	Raw   float64 `json:"raw_mg"`
	Blank float64 `json:"blank_mg"`
	Drift float64 `json:"drift_mg"`
	Corr  float64 `json:"corr_mg"`
}

type correctedValue struct {
	Points   []corrPoint  `json:"points"`
	Segments [][2]float64 `json:"segments_s"`
	Chain    []ChainEntry `json:"chain"`
}

type tempPoint struct {
	FurnaceT float64 `json:"furnace_t_s"`
	SampleT  float64 `json:"sample_t_s"`
	TempC    float64 `json:"temp_c"`
}

type sampleTempValue struct {
	Points []tempPoint  `json:"points"`
	LagS   float64      `json:"lag_s"`
	Basis  []string     `json:"basis"`
}

type atmospherePoint struct {
	ValveT     float64 `json:"valve_t_s"`
	EffectiveT float64 `json:"effective_at_sample_t_s"`
	Gas        string  `json:"gas"`
}

type atmosphereValue struct {
	Events  []atmospherePoint `json:"events"`
	DelayS  float64           `json:"delay_s"`
	Basis   []string          `json:"basis"`
}

type dtgPoint struct {
	T          float64 `json:"t_s"`
	Rate       float64 `json:"loss_rate_mg_per_s"`
	Noisy      bool    `json:"noisy,omitempty"`
	SegmentIdx int     `json:"segment_idx"`
}

type dtgValue struct {
	Points   []dtgPoint   `json:"points"`
	Segments [][2]float64 `json:"segments_s"`
	Chain    []ChainEntry `json:"chain"`
}

type stageValue struct {
	TStart         float64      `json:"t_start_s"`
	TEnd           float64      `json:"t_end_s"`
	TPeak          float64      `json:"t_peak_s"`
	PeakRate       float64      `json:"peak_rate_mg_per_s"`
	MassStart      *float64     `json:"mass_start_mg"`
	MassEnd        *float64     `json:"mass_end_mg"`
	LossMg         *float64     `json:"loss_mg"`
	LossPctInitial *float64     `json:"loss_pct_of_initial"`
	ResidueAfter   *float64     `json:"residue_after_pct"`
	Status         string       `json:"status"`
	Reasons        []string     `json:"reasons"`
	Basis          []string     `json:"basis"`
	Chain          []ChainEntry `json:"chain"`
}

type residueValue struct {
	ResiduePct    *float64     `json:"residue_pct"`
	FinalMassMg   *float64     `json:"final_mass_mg"`
	InitialMassMg *float64     `json:"initial_mass_mg"`
	Status        string       `json:"status"`
	Reasons       []string     `json:"reasons"`
	Chain         []ChainEntry `json:"chain"`
}

type Finding struct {
	Rule     string   `json:"rule"`
	Status   string   `json:"status"`
	Fragment string   `json:"fragment,omitempty"`
	Affected []string `json:"affected"`
	Note     string   `json:"note,omitempty"`
}

type findingsValue struct {
	Items []Finding `json:"items"`
}

func fptr(v float64) *float64 { return &v }

// interpolate does piecewise-linear interpolation within a sorted series and
// clamps at the ends. It never bridges two series: callers must ensure t is
// within one contiguous segment when gap crossing is forbidden.
func interp(pts []Point, t float64) float64 {
	if len(pts) == 1 {
		return pts[0].V
	}
	if t <= pts[0].T {
		return pts[0].V
	}
	last := pts[len(pts)-1]
	if t >= last.T {
		return last.V
	}
	i := sort.Search(len(pts)-1, func(i int) bool { return pts[i+1].T >= t })
	a, b := pts[i], pts[i+1]
	if b.T == a.T {
		return a.V
	}
	return a.V + (b.V-a.V)*(t-a.T)/(b.T-a.T)
}

// splitSegments partitions a time series at sampling gaps.
func splitSegments(ts []float64, gapFactor float64) ([]int, [][2]float64) {
	if len(ts) < 2 {
		return []int{0}, nil
	}
	dts := make([]float64, len(ts)-1)
	for i := range dts {
		dts[i] = ts[i+1] - ts[i]
	}
	sorted := append([]float64{}, dts...)
	sort.Float64s(sorted)
	median := sorted[len(sorted)/2]
	bounds := []int{0}
	var segs [][2]float64
	for i, dt := range dts {
		if median > 0 && dt > gapFactor*median {
			segs = append(segs, [2]float64{ts[bounds[len(bounds)-1]], ts[i]})
			bounds = append(bounds, i+1)
		}
	}
	segs = append(segs, [2]float64{ts[bounds[len(bounds)-1]], ts[len(ts)-1]})
	return bounds, segs
}

func smooth(vals []float64, w int) []float64 {
	if w < 2 || len(vals) < w {
		return vals
	}
	out := make([]float64, len(vals))
	half := w / 2
	for i := range vals {
		lo, hi := i-half, i+half
		if lo < 0 {
			lo = 0
		}
		if hi >= len(vals) {
			hi = len(vals) - 1
		}
		sum := 0.0
		for j := lo; j <= hi; j++ {
			sum += vals[j]
		}
		out[i] = sum / float64(hi-lo+1)
	}
	return out
}

type parsedInputs struct {
	samples []Point
	blank   []Point
	furnace []Point
	valves  []ValveEvent
	drift   DriftSpec
	initMg  float64
	ids     map[string]string
	diags   []Finding
}

func loadInputs(s *Store, asof int64) (parsedInputs, []Finding) {
	var in parsedInputs
	in.ids = map[string]string{}
	var findings []Finding
	addDiag := func(kind, id string, ds []Diagnostic) {
		for _, d := range ds {
			findings = append(findings, Finding{
				Rule: "parse_diagnostic:" + kind, Status: "unconvertible",
				Fragment: d.Raw, Affected: []string{id},
				Note:     d.Reason,
			})
		}
	}
	if o, ok, _ := s.LatestObject(KindSamples, asof); ok {
		in.ids[KindSamples] = o.ID
		if pts, ds, err := ParseSeries(o.Content, "mass"); err == nil {
			in.samples = pts
			addDiag(KindSamples, o.ID, ds)
		} else {
			findings = append(findings, Finding{Rule: "parse_diagnostic:samples", Status: "error",
				Fragment: o.Content, Affected: []string{o.ID}, Note: err.Error()})
		}
	}
	if o, ok, _ := s.LatestObject(KindBlank, asof); ok {
		in.ids[KindBlank] = o.ID
		if pts, ds, err := ParseSeries(o.Content, "mass"); err == nil {
			in.blank = pts
			addDiag(KindBlank, o.ID, ds)
		} else {
			findings = append(findings, Finding{Rule: "parse_diagnostic:blank", Status: "error",
				Fragment: o.Content, Affected: []string{o.ID}, Note: err.Error()})
		}
	}
	if o, ok, _ := s.LatestObject(KindFurnace, asof); ok {
		in.ids[KindFurnace] = o.ID
		if pts, ds, err := ParseSeries(o.Content, "temp"); err == nil {
			in.furnace = pts
			addDiag(KindFurnace, o.ID, ds)
		} else {
			findings = append(findings, Finding{Rule: "parse_diagnostic:furnace", Status: "error",
				Fragment: o.Content, Affected: []string{o.ID}, Note: err.Error()})
		}
	}
	if o, ok, _ := s.LatestObject(KindValves, asof); ok {
		in.ids[KindValves] = o.ID
		if vs, ds, err := ParseValves(o.Content); err == nil {
			in.valves = vs
			addDiag(KindValves, o.ID, ds)
		} else {
			findings = append(findings, Finding{Rule: "parse_diagnostic:valves", Status: "error",
				Fragment: o.Content, Affected: []string{o.ID}, Note: err.Error()})
		}
	}
	if o, ok, _ := s.LatestObject(KindDrift, asof); ok {
		in.ids[KindDrift] = o.ID
		if sp, _, err := ParseDrift(o.Content); err == nil {
			in.drift = sp
		} else {
			findings = append(findings, Finding{Rule: "parse_diagnostic:drift", Status: "error",
				Fragment: o.Content, Affected: []string{o.ID}, Note: err.Error()})
		}
	}
	if o, ok, _ := s.LatestObject(KindInitialMass, asof); ok {
		in.ids[KindInitialMass] = o.ID
		if mg, _, err := ParseInitialMass(o.Content); err == nil {
			in.initMg = mg
		} else {
			findings = append(findings, Finding{Rule: "parse_diagnostic:initial_mass", Status: "error",
				Fragment: o.Content, Affected: []string{o.ID}, Note: err.Error()})
		}
	}
	// Revision auditability.
	objs, _ := s.Objects(KindInitialMass, asof)
	if len(objs) > 1 {
		findings = append(findings, Finding{
			Rule: "initial_mass_revision", Status: "revised",
			Fragment: objs[len(objs)-1].Content,
			Affected: []string{objs[0].ID, objs[len(objs)-1].ID},
			Note:     "初重存在修订版本；旧派生结果保留但标记过期",
		})
	}
	return in, findings
}
