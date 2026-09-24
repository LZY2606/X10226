package tga

import (
	"fmt"
	"math"
	"sort"
	"time"
)

const AlgoVersion = "tg.eng.v1"

type Params struct {
	AlgoVersion       string  `json:"algo_version"`
	AtmDelayS         float64 `json:"atm_delay_s"`
	LagTauS           float64 `json:"lag_tau_s"`
	GapFactor         float64 `json:"gap_factor"`
	NoiseSigmaMg      float64 `json:"noise_sigma_mg"`
	MinPeakPromPctMin float64 `json:"min_peak_prom_pct_min"`
	MinStageLossPct   float64 `json:"min_stage_loss_pct"`
	OverlapWindowS    float64 `json:"overlap_window_s"`
	DTGHalfWindow     int     `json:"dtg_half_window"`
}

func DefaultParams() Params {
	return Params{
		AlgoVersion:       AlgoVersion,
		AtmDelayS:         20,
		LagTauS:           8,
		GapFactor:         4,
		NoiseSigmaMg:      0.02,
		MinPeakPromPctMin: 0.4,
		MinStageLossPct:   0.5,
		OverlapWindowS:    300,
		DTGHalfWindow:     15,
	}
}

func (p Params) Fingerprint() string {
	return Hash("params", canonicalJSON(p))
}

type Point struct {
	T        Measure `json:"t"`
	Mass     Measure `json:"mass"`
	MassRaw  Measure `json:"mass_raw"`
	Temp     Measure `json:"temp"`
	Flagged  bool    `json:"flagged,omitempty"`
	GapAfter bool    `json:"gap_after,omitempty"`
}

type Peak struct {
	ID    string  `json:"id"`
	T     Measure `json:"t"`
	Temp  Measure `json:"temp"`
	Rate  Measure `json:"rate"`
	Group int     `json:"group"`
}

type GasSeg struct {
	Gas        string  `json:"gas"`
	TValve     Measure `json:"t_valve"`
	TEffective Measure `json:"t_effective"`
	Flow       Measure `json:"flow"`
}

type Stage struct {
	Name       string  `json:"name"`
	TStart     Measure `json:"t_start"`
	TEnd       Measure `json:"t_end"`
	MassStart  Measure `json:"mass_start"`
	MassEnd    Measure `json:"mass_end"`
	LossPct    QVal    `json:"loss_pct"`
	Atmosphere string  `json:"atmosphere"`
	Overlapped bool    `json:"overlapped,omitempty"`
}

type Candidate struct {
	ID            string                `json:"id"`
	RunID         string                `json:"run_id"`
	Index         int                   `json:"index"`
	Kind          string                `json:"kind"`
	AlgoVersion   string                `json:"algo_version"`
	ParamsFP      string                `json:"params_fp"`
	Sources       map[SourceKind]string `json:"sources"`
	Stages        []Stage               `json:"stages"`
	ResiduePct    QVal                  `json:"residue_pct"`
	SumPct        QVal                  `json:"sum_pct"`
	Viable        bool                  `json:"viable"`
	RejectReasons []string              `json:"reject_reasons,omitempty"`
	Warnings      []string              `json:"warnings,omitempty"`
}

type Judgment struct {
	ID     string   `json:"id"`
	Code   string   `json:"code"`
	Detail string   `json:"detail"`
	Refs   []string `json:"refs,omitempty"`
}

type Run struct {
	ID          string                `json:"id"`
	AlgoVersion string                `json:"algo_version"`
	ParamsFP    string                `json:"params_fp"`
	Inputs      map[SourceKind]string `json:"inputs"`
	Points      []Point               `json:"points"`
	DTG         []float64             `json:"dtg"`
	DTGOK       []bool                `json:"dtg_ok"`
	Peaks       []Peak                `json:"peaks"`
	GasSegs     []GasSeg              `json:"gas_segs"`
	Candidates  []*Candidate          `json:"candidates"`
	Judgments   []Judgment            `json:"judgments"`
	Gaps        [][2]float64          `json:"gaps"`
	NFlagged    int                   `json:"n_flagged"`
	At          time.Time             `json:"at"`

	segments [][2]int
}

type Inputs struct {
	Samples   []SamplePoint
	Drift     []DriftPoint
	Furnace   []FurnacePoint
	Gas       []GasEvent
	Blank     []BlankPoint
	Initial   *Measure
	SourceIDs map[SourceKind]string
}

func median(xs []float64) float64 {
	cp := append([]float64(nil), xs...)
	sort.Float64s(cp)
	if len(cp) == 0 {
		return 0
	}
	return cp[len(cp)/2]
}

func interpTS(xs, ys []float64, x float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	if x <= xs[0] {
		return ys[0]
	}
	if x >= xs[len(xs)-1] {
		return ys[len(ys)-1]
	}
	i := sort.Search(len(xs), func(i int) bool { return xs[i] >= x })
	if xs[i] == x {
		return ys[i]
	}
	a, b := i-1, i
	return ys[a] + (ys[b]-ys[a])*(x-xs[a])/(xs[b]-xs[a])
}

func (r *Run) addJudgment(code, detail string, refs []string) {
	r.Judgments = append(r.Judgments, Judgment{ID: Hash(r.ID, code, detail), Code: code, Detail: detail, Refs: refs})
}

// RunEngine executes algorithm tg.eng.v1. Output is deterministic: same
// inputs, parameters and run id always produce the same points, peaks,
// candidates, judgments and value ids.
func RunEngine(p Params, in Inputs, runID string, at time.Time) *Run {
	if p.AlgoVersion == "" {
		p = DefaultParams()
	}
	r := &Run{
		ID:          runID,
		AlgoVersion: p.AlgoVersion,
		ParamsFP:    p.Fingerprint(),
		Inputs:      in.SourceIDs,
		Points:      []Point{},
		Peaks:       []Peak{},
		GasSegs:     []GasSeg{},
		Candidates:  []*Candidate{},
		Judgments:   []Judgment{},
		At:          at,
	}
	n := len(in.Samples)
	if n == 0 || in.Initial == nil {
		r.addJudgment("input.incomplete", "缺少原始采样点或样品初重，无法派生候选；数据缺口不跨越插值", nil)
		return r
	}
	m0 := in.Initial.Value

	var driftAt func(float64) float64
	switch {
	case len(in.Drift) > 0:
		xs, ys := make([]float64, len(in.Drift)), make([]float64, len(in.Drift))
		for i, d := range in.Drift {
			xs[i], ys[i] = d.T.Value, d.Drift.Value
		}
		driftAt = func(t float64) float64 { return interpTS(xs, ys, t) }
	case len(in.Blank) > 0:
		xs, ys := make([]float64, len(in.Blank)), make([]float64, len(in.Blank))
		for i, b := range in.Blank {
			xs[i], ys[i] = b.T.Value, b.Mass.Value
		}
		base := ys[0]
		driftAt = func(t float64) float64 { return interpTS(xs, ys, t) - base }
	default:
		driftAt = func(float64) float64 { return 0 }
	}

	r.Points = make([]Point, n)
	for i, sp := range in.Samples {
		r.Points[i] = Point{
			T:       sp.T,
			MassRaw: sp.Mass,
			Temp:    sp.Temp,
			Mass:    Measure{Value: sp.Mass.Value - driftAt(sp.T.Value), Unit: UnitMg, Raw: sp.Mass.Raw},
		}
	}

	dts := make([]float64, 0, n-1)
	for i := 1; i < n; i++ {
		dts = append(dts, r.Points[i].T.Value-r.Points[i-1].T.Value)
	}
	medDT := median(dts)
	for i := 1; i < n; i++ {
		dt := r.Points[i].T.Value - r.Points[i-1].T.Value
		if medDT > 0 && dt > p.GapFactor*medDT {
			r.Points[i-1].GapAfter = true
			r.Gaps = append(r.Gaps, [2]float64{r.Points[i-1].T.Value, r.Points[i].T.Value})
		}
	}
	for _, g := range r.Gaps {
		r.addJudgment("gap.no-interp",
			fmt.Sprintf("采样缺口 [%.0f, %.0f) s 不跨越插值；落入缺口的候选边界必须拒绝", g[0], g[1]),
			[]string{string(SrcSamples)})
	}

	r.segments = [][2]int{}
	segStart := 0
	lastGood := 0
	for i := 0; i < n; i++ {
		if i > 0 && r.Points[i-1].GapAfter {
			segStart, lastGood = i, i
			continue
		}
		if i > segStart && r.Points[i].Mass.Value > r.Points[lastGood].Mass.Value+p.NoiseSigmaMg {
			r.Points[i].Flagged = true
			r.NFlagged++
			continue
		}
		lastGood = i
	}
	if r.NFlagged > 0 {
		r.addJudgment("noise.nonmonotonic",
			fmt.Sprintf("检出 %d 个超过噪声阈值 %.3g mg 的非单调点：已标记并排除出端点/DTG，原始点保留", r.NFlagged, p.NoiseSigmaMg),
			[]string{string(SrcSamples)})
	}

	start := 0
	for i := 1; i < n; i++ {
		if r.Points[i-1].GapAfter {
			r.segments = append(r.segments, [2]int{start, i - 1})
			start = i
		}
	}
	r.segments = append(r.segments, [2]int{start, n - 1})

	r.DTG = make([]float64, n)
	r.DTGOK = make([]bool, n)
	w := p.DTGHalfWindow
	for i := w; i < n-w; i++ {
		a, b := i-w, i+w
		crossGap, flaggedInWindow := false, false
		for j := a; j < b; j++ {
			if r.Points[j].GapAfter {
				crossGap = true
			}
		}
		for j := a; j <= b; j++ {
			if r.Points[j].Flagged {
				flaggedInWindow = true
			}
		}
		if crossGap || flaggedInWindow {
			continue
		}
		dt := r.Points[b].T.Value - r.Points[a].T.Value
		if dt <= 0 {
			continue
		}
		r.DTG[i] = (r.Points[a].Mass.Value-r.Points[b].Mass.Value)/dt*60/m0*100
		r.DTGOK[i] = true
	}
	r.detectPeaks(p)
	r.buildGasSegs(p, in.Gas)
	r.buildCandidates(p, m0)
	return r
}

func (r *Run) detectPeaks(p Params) {
	n := len(r.Points)
	w2 := 10
	type rawPeak struct {
		idx  int
		rate float64
	}
	var raws []rawPeak
	for i := 0; i < n; i++ {
		if !r.DTGOK[i] {
			continue
		}
		isMax := true
		leftMin, rightMin := math.Inf(1), math.Inf(1)
		for j := i - w2; j <= i + w2; j++ {
			if j < 0 || j >= n || !r.DTGOK[j] {
				continue
			}
			if r.DTG[j] > r.DTG[i] {
				isMax = false
				break
			}
			if j < i && r.DTG[j] < leftMin {
				leftMin = r.DTG[j]
			}
			if j > i && r.DTG[j] < rightMin {
				rightMin = r.DTG[j]
			}
		}
		if !isMax {
			continue
		}
		prom := r.DTG[i] - math.Max(leftMin, rightMin)
		if prom < p.MinPeakPromPctMin {
			continue
		}
		raws = append(raws, rawPeak{i, r.DTG[i]})
	}
	var merged []rawPeak
	for _, rp := range raws {
		if len(merged) > 0 && rp.idx-merged[len(merged)-1].idx <= w2 {
			if rp.rate > merged[len(merged)-1].rate {
				merged[len(merged)-1] = rp
			}
			continue
		}
		merged = append(merged, rp)
	}
	for i, rp := range merged {
		pt := r.Points[rp.idx]
		r.Peaks = append(r.Peaks, Peak{
			ID:    Hash(r.ID, "peak", fmt.Sprint(i)),
			T:     Measure{Value: pt.T.Value, Unit: UnitS},
			Temp:  Measure{Value: pt.Temp.Value, Unit: UnitC},
			Rate:  Measure{Value: rp.rate, Unit: UnitPctMin},
			Group: -1,
		})
	}
	group := 0
	for i := 0; i < len(r.Peaks); i++ {
		if r.Peaks[i].Group != -1 {
			continue
		}
		members := []int{i}
		for j := i + 1; j < len(r.Peaks); j++ {
			last := members[len(members)-1]
			if r.Peaks[j].T.Value-r.Peaks[last].T.Value < p.OverlapWindowS {
				members = append(members, j)
			}
		}
		if len(members) > 1 {
			for _, m := range members {
				r.Peaks[m].Group = group
			}
			group++
		}
	}
}

func (r *Run) buildGasSegs(p Params, gas []GasEvent) {
	sorted := append([]GasEvent(nil), gas...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].T.Value < sorted[j].T.Value })
	for i, g := range sorted {
		id := Hash(r.ID, "gas", fmt.Sprint(i), g.Gas)
		_ = id
		r.GasSegs = append(r.GasSegs, GasSeg{
			Gas:        g.Gas,
			TValve:     Measure{Value: g.T.Value, Unit: UnitS, Raw: g.T.Raw},
			TEffective: Measure{Value: g.T.Value + p.AtmDelayS, Unit: UnitS},
			Flow:       g.Flow,
		})
	}
	if p.AtmDelayS > 0 && len(r.GasSegs) > 0 {
		r.addJudgment("atmosphere.delay",
			fmt.Sprintf("阀切换时间不等于样品反应时间：样品侧生效时间 = 阀时间 + %.0f s（温度滞后 τ=%.0f s），气氛标注按生效时间", p.AtmDelayS, p.LagTauS),
			[]string{string(SrcGas)})
	}
}

func (r *Run) atmosphereAt(t float64) string {
	gas := ""
	for _, g := range r.GasSegs {
		if g.TEffective.Value <= t {
			gas = g.Gas
		}
	}
	return gas
}

func (r *Run) insideGap(t float64) bool {
	for _, g := range r.Gaps {
		if t > g[0] && t < g[1] {
			return true
		}
	}
	return false
}

func (r *Run) massAt(t float64) (float64, bool) {
	for _, seg := range r.segments {
		if t < r.Points[seg[0]].T.Value || t > r.Points[seg[1]].T.Value {
			continue
		}
		idx := seg[0]
		best := math.Abs(r.Points[seg[0]].T.Value - t)
		for i := seg[0] + 1; i <= seg[1]; i++ {
			d := math.Abs(r.Points[i].T.Value - t)
			if d < best {
				best, idx = d, i
			}
		}
		for d := 0; d <= seg[1]-seg[0]; d++ {
			if idx-d >= seg[0] && !r.Points[idx-d].Flagged {
				return r.Points[idx-d].Mass.Value, true
			}
			if idx+d <= seg[1] && !r.Points[idx+d].Flagged {
				return r.Points[idx+d].Mass.Value, true
			}
		}
		return 0, false
	}
	return 0, false
}
