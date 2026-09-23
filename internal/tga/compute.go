package tga

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

// Derived kinds.
const (
	KindCorrected    = "corrected_curve"
	KindDTG          = "dtg"
	KindAtmosphere   = "atmosphere_timeline"
	KindCandidate    = "stage_candidate"
	KindFinalResidue = "final_residue"
)

// Judgment is a machine-readable domain judgment. The UI renders these and
// they are replayable evidence, not free text.
type Judgment struct {
	Rule     string     `json:"rule,omitempty"`
	Detail   string     `json:"detail,omitempty"`
	ValveT   float64    `json:"valve_t_s,omitempty"`
	EffT     float64    `json:"effective_t_s,omitempty"`
	FurnaceT float64    `json:"furnace_t_s,omitempty"`
	SampleT  float64    `json:"sample_t_s,omitempty"`
	GapStart float64    `json:"gap_start_s,omitempty"`
	GapEnd   float64    `json:"gap_end_s,omitempty"`
	WindowA  [2]float64 `json:"window_a_s,omitempty"`
	WindowB  [2]float64 `json:"window_b_s,omitempty"`
	OverlapS float64    `json:"overlap_s,omitempty"`
	Observed float64    `json:"observed_sum_pct,omitempty"`
	PeakT    float64    `json:"peak_t_s,omitempty"`
	Reason   string     `json:"reason,omitempty"`
}

// ChainStep is one step of the readable calculation chain of a result.
type ChainStep struct {
	N       int    `json:"n"`
	Formula string `json:"formula"`
	Inputs  string `json:"inputs"`
	Value   string `json:"value"`
	Unit    string `json:"unit"`
	Source  string `json:"source_ids"`
}

type CPt struct {
	I       int     `json:"i"`
	T       float64 `json:"t_s"`
	M       float64 `json:"m_mg"`
	MOK     bool    `json:"m_ok"`
	Temp    float64 `json:"temp_c"`
	TempOK  bool    `json:"temp_ok"`
	RawM    float64 `json:"raw_m_mg"`
	Drift   float64 `json:"drift_mg"`
	Blank   float64 `json:"blank_mg"`
	GapNext bool    `json:"gap_next"`
}

type Corrected struct {
	Status    string     `json:"status"`
	Errors    []string   `json:"errors,omitempty"`
	Points    []CPt      `json:"points"`
	Judgments []Judgment `json:"judgments,omitempty"`
}

type DPt struct {
	I       int        `json:"i"`
	TSpan   [2]float64 `json:"t_span_s"`
	TCenter float64    `json:"t_center_s"`
	Rate    float64    `json:"rate_mg_per_s"`
	SmRate  float64    `json:"smoothed_mg_per_s"`
	Segment int        `json:"segment"`
}

type DTG struct {
	Status    string     `json:"status"`
	Errors    []string   `json:"errors,omitempty"`
	Points    []DPt      `json:"points"`
	Judgments []Judgment `json:"judgments,omitempty"`
}

type AtmInterval struct {
	T0     float64 `json:"t0_s"`
	T1     float64 `json:"t1_s"`
	Gas    string  `json:"gas"`
	Flow   float64 `json:"flow_mL_min"`
	ValveT float64 `json:"valve_t_s"`
}

type Atmosphere struct {
	Status    string       `json:"status"`
	Errors    []string     `json:"errors,omitempty"`
	Intervals []AtmInterval `json:"intervals"`
	Judgments []Judgment   `json:"judgments,omitempty"`
}

type Candidate struct {
	Status      string      `json:"status"`
	Reason      string      `json:"reason,omitempty"`
	Window      [2]float64  `json:"window_s"`
	PeakT       float64     `json:"peak_t_s"`
	PeakRate    float64     `json:"peak_rate_mg_per_s"`
	TempStart   float64     `json:"temp_start_c"`
	TempPeak    float64     `json:"temp_peak_c"`
	TempEnd     float64     `json:"temp_end_c"`
	AtmStart    string      `json:"atm_start"`
	AtmPeak     string      `json:"atm_peak"`
	AtmEnd      string      `json:"atm_end"`
	MassLossPct float64     `json:"mass_loss_pct"`
	LossStatus  string      `json:"loss_status"`
	Chain       []ChainStep `json:"chain,omitempty"`
	Judgments   []Judgment  `json:"judgments,omitempty"`
}

type Candidates struct {
	Status     string      `json:"status"`
	Errors     []string    `json:"errors,omitempty"`
	Candidates []Candidate `json:"candidates"`
}

type FinalResidue struct {
	Status      string      `json:"status"`
	Errors      []string    `json:"errors,omitempty"`
	ResiduePct  float64     `json:"residue_pct"`
	T           float64     `json:"t_s"`
	Temp        float64     `json:"temp_c"`
	Chain       []ChainStep `json:"chain,omitempty"`
	ObservedSum float64     `json:"observed_loss_sum_pct"`
	Judgments   []Judgment  `json:"judgments,omitempty"`
}

// Node is one computed derived value before persistence wrapping.
type Node struct {
	Kind    string
	Payload any
	Sources []string
	ParamFP string
	ID      string
}

// PipelineResult is the five-node result of a recompute.
type PipelineResult struct {
	Corrected  *Node
	DTG        *Node
	Atmosphere *Node
	Candidates *Node
	Residue    *Node
}

// ParamsFingerprint hashes the algorithm version and parameter set.
func ParamsFingerprint(p Params) string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(append([]byte(AlgoVersion+"|"), b...))
	return hex.EncodeToString(sum[:12])
}

// Canonical serializes derived payloads deterministically for content ids.
func Canonical(v any) []byte {
	b, _ := json.Marshal(v)
	var a any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	_ = dec.Decode(&a)
	a = sortAny(a)
	out, _ := json.Marshal(a)
	return out
}

func sortAny(v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(x))
		for _, k := range keys {
			out[k] = sortAny(x[k])
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = sortAny(x[i])
		}
		return out
	}
	return v
}

// ContentID derives the stable identity of a derived node: kind, algo
// version, parameter fingerprint, direct sources and payload content.
func ContentID(kind, algo, fp string, sources []string, payload any) string {
	ss := append([]string{}, sources...)
	sort.Strings(ss)
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%s|", kind, algo, fp, stringsJoin(ss))
	h.Write(Canonical(payload))
	sum := h.Sum(nil)
	return "der-" + hex.EncodeToString(sum[:14])
}

func stringsJoin(s []string) string {
	out := ""
	for i, v := range s {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}

// Inputs are the current versions of each layer; nil means the layer is
// absent (failed import or never submitted).
type Inputs struct {
	Raw      *RawDoc
	Drift    *RawDoc
	Blank    *RawDoc
	Furnace  *RawDoc
	Gas      *RawDoc
	InitMass *RawDoc
	Params   Params
	DocIDs   map[string]string
}

func (in Inputs) idFor(layer string) string {
	if id, ok := in.DocIDs[layer]; ok {
		return id
	}
	return "missing:" + layer
}

func mkNode(kind, fp string, sources []string, payload any) *Node {
	n := &Node{Kind: kind, Payload: payload, Sources: sources, ParamFP: fp}
	n.ID = ContentID(kind, AlgoVersion, fp, sources, payload)
	return n
}

// ---- interpolation helpers ---------------------------------------------

func interpSegs(segs []Seg, t float64) (float64, bool) {
	if len(segs) == 0 {
		return 0, false
	}
	for i, s := range segs {
		t0, t1 := s.T0.V, s.T1.V
		if t < t0 {
			if i == 0 {
				return s.V0.V, true
			}
			continue
		}
		if t <= t1 {
			if t1 == t0 {
				return s.V1.V, true
			}
			f := (t - t0) / (t1 - t0)
			return s.V0.V + f*(s.V1.V-s.V0.V), true
		}
	}
	last := segs[len(segs)-1]
	return last.V1.V, true
}

func interpPoints(pts []Point, t float64) (float64, bool) {
	if len(pts) == 0 {
		return 0, false
	}
	for i := 1; i < len(pts); i++ {
		t0 := pts[i-1].T.V
		t1 := pts[i].T.V
		if t >= t0 && t <= t1 {
			if !pts[i-1].M.OK || !pts[i].M.OK {
				return 0, false
			}
			if t1 == t0 {
				return pts[i].M.V, true
			}
			f := (t - t0) / (t1 - t0)
			return pts[i-1].M.V + f*(pts[i].M.V-pts[i-1].M.V), true
		}
	}
	last := pts[len(pts)-1]
	if !last.M.OK {
		return 0, false
	}
	return last.M.V, true
}

func interpCP(pts []CPt, t float64) (float64, bool) {
	for i := 1; i < len(pts); i++ {
		t0, t1 := pts[i-1].T, pts[i].T
		if t >= t0 && t <= t1 {
			if t1 == t0 || !pts[i-1].MOK || !pts[i].MOK {
				if !pts[i-1].MOK || !pts[i].MOK {
					return 0, false
				}
				return pts[i].M, true
			}
			f := (t - t0) / (t1 - t0)
			return pts[i-1].M + f*(pts[i].M-pts[i-1].M), true
		}
	}
	if len(pts) > 0 && pts[len(pts)-1].MOK {
		return pts[len(pts)-1].M, true
	}
	return 0, false
}

func interpTempCP(pts []CPt, t float64) (float64, bool) {
	for i := 1; i < len(pts); i++ {
		t0, t1 := pts[i-1].T, pts[i].T
		if t >= t0 && t <= t1 {
			if t1 == t0 || !pts[i-1].TempOK || !pts[i].TempOK {
				if !pts[i-1].TempOK || !pts[i].TempOK {
					return 0, false
				}
				return pts[i].Temp, true
			}
			f := (t - t0) / (t1 - t0)
			return pts[i-1].Temp + f*(pts[i].Temp-pts[i-1].Temp), true
		}
	}
	if len(pts) > 0 && pts[len(pts)-1].TempOK {
		return pts[len(pts)-1].Temp, true
	}
	return 0, false
}

// ---- corrected curve ----------------------------------------------------

func computeCorrected(in Inputs) *Corrected {
	c := &Corrected{Status: "ok"}
	if in.Raw == nil {
		c.Status = "error"
		c.Errors = append(c.Errors, "raw_points layer missing (import absent or rejected)")
		return c
	}
	var bad []string
	for _, p := range in.Raw.Points {
		if !p.M.OK {
			bad = append(bad, fmt.Sprintf("t=%s", p.T.Text))
		}
	}
	if len(bad) > 0 {
		c.Errors = append(c.Errors, fmt.Sprintf("%d unparseable mass values retained in place and excluded from computation", len(bad)))
	}
	if in.Drift == nil {
		c.Errors = append(c.Errors, "drift_correction layer missing; drift set to 0 with an error mark")
	}
	if in.Blank == nil {
		c.Errors = append(c.Errors, "blank_curve layer missing; blank set to 0 with an error mark")
	}
	for i, p := range in.Raw.Points {
		cp := CPt{I: i, T: p.T.V, RawM: p.M.V, MOK: p.M.OK, Temp: p.Temp.V, TempOK: p.Temp.OK}
		if in.Drift != nil && p.T.OK {
			if d, ok := interpSegs(in.Drift.Segments, p.T.V); ok {
				cp.Drift = d
			}
		}
		if in.Blank != nil && p.T.OK {
			if b, ok := interpPoints(in.Blank.Points, p.T.V); ok {
				cp.Blank = b
			}
		}
		if p.M.OK {
			cp.M = p.M.V - cp.Drift - cp.Blank
		}
		c.Points = append(c.Points, cp)
	}
	// gap detection: gaps exist between raw points, never interpolated.
	seg := 0
	for i := 0; i+1 < len(c.Points); i++ {
		dt := c.Points[i+1].T - c.Points[i].T
		if dt > in.Params.MaxGapS {
			c.Points[i].GapNext = true
			c.Judgments = append(c.Judgments, Judgment{
				Rule: "gap_no_interpolation",
				Detail: fmt.Sprintf("sampling gap %.1fs exceeds max_gap %.1fs; no interpolation across it", dt, in.Params.MaxGapS),
				GapStart: c.Points[i].T, GapEnd: c.Points[i+1].T,
			})
		}
	}
	_ = seg
	if len(c.Errors) > 0 {
		c.Status = "partial"
	}
	return c
}

// ---- DTG ----------------------------------------------------------------

func computeDTG(in Inputs, corr *Corrected) *DTG {
	d := &DTG{Status: "ok"}
	if corr.Status == "error" {
		d.Status = "error"
		d.Errors = append(d.Errors, "cannot compute DTG: corrected curve unavailable")
		return d
	}
	d.Judgments = append(d.Judgments, corr.Judgments...)
	seg := 0
	for i := 0; i+1 < len(corr.Points); i++ {
		a, b := corr.Points[i], corr.Points[i+1]
		if b.T-a.T > in.Params.MaxGapS {
			seg++
			continue // never differentiate across a gap
		}
		if !a.MOK || !b.MOK {
			continue
		}
		rate := (b.M - a.M) / (b.T - a.T)
		d.Points = append(d.Points, DPt{
			I: i, TSpan: [2]float64{a.T, b.T}, TCenter: (a.T + b.T) / 2,
			Rate: rate, Segment: seg,
		})
	}
	// boxcar smoothing within the same segment only
	half := in.Params.SmoothWindowS / 2
	for i := range d.Points {
		var sum, n float64
		for j := range d.Points {
			if d.Points[j].Segment != d.Points[i].Segment {
				continue
			}
			if math.Abs(d.Points[j].TCenter-d.Points[i].TCenter) <= half {
				sum += d.Points[j].Rate
				n++
			}
		}
		d.Points[i].SmRate = sum / n
	}
	if len(corr.Errors) > 0 {
		d.Status = "partial"
		d.Errors = append(d.Errors, corr.Errors...)
	}
	return d
}

// ---- atmosphere ----------------------------------------------------------

func computeAtmosphere(in Inputs) *Atmosphere {
	a := &Atmosphere{Status: "ok"}
	if in.Gas == nil {
		a.Status = "error"
		a.Errors = append(a.Errors, "gas_events layer missing")
		return a
	}
	delay := in.Params.AtmosphereDelayS
	evs := append([]GasEvent{}, in.Gas.Gas...)
	sort.Slice(evs, func(i, j int) bool { return evs[i].T.V < evs[j].T.V })
	for i := range evs {
		t0 := evs[i].T.V
		t1 := math.Inf(1)
		if i+1 < len(evs) {
			t1 = evs[i+1].T.V + delay
		}
		a.Intervals = append(a.Intervals, AtmInterval{
			T0: t0 + delay, T1: t1, Gas: evs[i].Gas, Flow: evs[i].Flow.V, ValveT: t0,
		})
		if i > 0 && delay != 0 {
			a.Judgments = append(a.Judgments, Judgment{
				Rule: "atmosphere_delay", ValveT: t0, EffT: t0 + delay,
				Detail: fmt.Sprintf("valve switch at furnace %.1fs reaches the sample at %.1fs (delay %.0fs); valve time is not reaction time", t0, t0+delay, delay),
			})
		}
	}
	return a
}

func gasAt(a *Atmosphere, t float64) string {
	if a.Status == "error" {
		return "unknown"
	}
	cur := "unknown"
	for _, iv := range a.Intervals {
		if t >= iv.T0 && t < iv.T1 {
			return iv.Gas
		}
		if t >= iv.T0 {
			cur = iv.Gas
		}
	}
	return cur
}

// ---- stage candidates ----------------------------------------------------

func computeCandidates(in Inputs, dt *DTG, corr *Corrected, atm *Atmosphere) *Candidates {
	c := &Candidates{Status: "ok"}
	if dt.Status == "error" {
		c.Status = "error"
		c.Errors = append(c.Errors, "cannot find stages: DTG unavailable")
		return c
	}
	if in.Furnace == nil {
		c.Errors = append(c.Errors, "furnace_program layer missing; temperatures shown as recorded")
	}
	lag := in.Params.TempLagS
	if lag != 0 {
		c.Candidates = nil
	}
	type pk struct {
		t, raw, sm float64
	}
	var rawMin, smMin []pk
	pts := dt.Points
	seg := -2
	for i := 1; i+1 < len(pts); i++ {
		if pts[i].Segment != seg {
			rawMin, smMin, seg = nil, nil, pts[i].Segment
		}
		if pts[i].Segment != pts[i-1].Segment || pts[i].Segment != pts[i+1].Segment {
			continue
		}
		if pts[i].Rate <= pts[i-1].Rate && pts[i].Rate <= pts[i+1].Rate {
			rawMin = append(rawMin, pk{pts[i].TCenter, pts[i].Rate, pts[i].SmRate})
		}
		if pts[i].SmRate <= pts[i-1].SmRate && pts[i].SmRate <= pts[i+1].SmRate {
			smMin = append(smMin, pk{pts[i].TCenter, pts[i].Rate, pts[i].SmRate})
		}
	}
	_ = rawMin
	// accepted peaks come from the smoothed signal; rejected minima are kept
	// as evidence of non-monotonic noise / sub-threshold bumps.
	var smoothedMins []pk
	for i := 1; i+1 < len(pts); i++ {
		if pts[i].Segment != pts[i-1].Segment || pts[i].Segment != pts[i+1].Segment {
			continue
		}
		if pts[i].SmRate <= pts[i-1].SmRate && pts[i].SmRate <= pts[i+1].SmRate {
			smoothedMins = append(smoothedMins, pk{pts[i].TCenter, pts[i].Rate, pts[i].SmRate})
		}
	}
	mergeGap := 20.0
	var accepted []pk
	for _, m := range smoothedMins {
		if len(accepted) > 0 && math.Abs(m.t-accepted[len(accepted)-1].t) < mergeGap {
			if m.sm < accepted[len(accepted)-1].sm {
				accepted[len(accepted)-1] = m
			}
			continue
		}
		accepted = append(accepted, m)
	}
	// rejected minima: raw local minima that smoothing/prominence removes
	rejSet := map[float64]bool{}
	for i := 1; i+1 < len(pts); i++ {
		if pts[i].Segment != pts[i-1].Segment || pts[i].Segment != pts[i+1].Segment {
			continue
		}
		if pts[i].Rate <= pts[i-1].Rate && pts[i].Rate <= pts[i+1].Rate {
			near := false
			for _, a := range accepted {
				if math.Abs(a.t-pts[i].TCenter) < mergeGap {
					near = true
				}
			}
			if !near {
				rejSet[pts[i].TCenter] = true
			}
		}
	}
	_ = rejSet
	for _, m := range accepted {
		if -m.sm < in.Params.MinProminence {
			c.Candidates = append(c.Candidates, makeRej(in, m, "local minimum below minimum prominence; treated as non-monotonic noise", corr, atm))
			continue
		}
		c.Candidates = append(c.Candidates, makeAcc(in, m, corr, atm))
	}
	for i := 1; i+1 < len(pts); i++ {
		if !rejSet[pts[i].TCenter] {
			continue
		}
		m := pk{pts[i].TCenter, pts[i].Rate, pts[i].SmRate}
		c.Candidates = append(c.Candidates, makeRej(in, m, "raw local minimum removed by smoothing: non-monotonic noise", corr, atm))
	}
	// noise minima are emitted in scan order; sort everything by time
	sort.Slice(c.Candidates, func(i, j int) bool { return c.Candidates[i].PeakT < c.Candidates[j].PeakT })
	appendOverlapJudgments(c)
	if lag != 0 {
		c.Judgments = append(c.Judgments, Judgment{
			Rule: "temperature_lag", FurnaceT: 0, SampleT: 0,
			Detail: fmt.Sprintf("sample temperature lags the furnace program by %.0fs; reaction times are mapped with the lag, not equated with furnace time", lag),
		})
	}
	if len(dt.Errors) > 0 {
		c.Status = "partial"
		c.Errors = append(c.Errors, dt.Errors...)
	}
	return c
}

func windowFor(in Inputs, m struct {
	t, raw, sm float64
}, dt *DTG) [2]float64 {
	idx := -1
	for i := range dt.Points {
		if dt.Points[i].TCenter == m.t {
			idx = i
			break
		}
	}
	level := m.sm * in.Params.StageThreshold
	lo, hi := idx, idx
	for lo-1 >= 0 && dt.Points[lo-1].Segment == dt.Points[idx].Segment && dt.Points[lo-1].SmRate < level {
		lo--
	}
	for hi+1 < len(dt.Points) && dt.Points[hi+1].Segment == dt.Points[idx].Segment && dt.Points[hi+1].SmRate < level {
		hi++
	}
	return [2]float64{dt.Points[lo].TSpan[0], dt.Points[hi].TSpan[1]}
}

func tempAt(in Inputs, corr *Corrected, t float64) (float64, bool) {
	if in.Furnace != nil {
		if tt, ok := interpSegs(in.Furnace.Segments, t-in.Params.TempLagS); ok {
			return tt, true
		}
	}
	if tt, ok := interpTempCP(corr.Points, t); ok {
		return tt, false
	}
	return 0, false
}

func makeAcc(in Inputs, m struct {
	t, raw, sm float64
}, corr *Corrected, atm *Atmosphere) Candidate {
	dt := &DTG{}
	_ = dt
	return Candidate{}
}

func makeRej(in Inputs, m struct {
	t, raw, sm float64
}, reason string, corr *Corrected, atm *Atmosphere) Candidate {
	tp, _ := tempAt(inForRej(in), corr, m.t)
	return Candidate{
		Status: "rejected", Reason: reason,
		Window: [2]float64{m.t, m.t}, PeakT: m.t, PeakRate: m.raw, TempPeak: tp,
		AtmPeak: gasAt(atm, m.t),
		Judgments: []Judgment{{Rule: "candidate_rejected", PeakT: m.t, Reason: reason}},
	}
}

// inForRej placeholder replaced by storing params on tempAt callers.
func inForRej(in Inputs) Inputs { return in }

func appendOverlapJudgments(c *Candidates) {
	var acc []int
	for i := range c.Candidates {
		if c.Candidates[i].Status == "accepted" {
			acc = append(acc, i)
		}
	}
	for x := 0; x < len(acc); x++ {
		for y := x + 1; y < len(acc); y++ {
			a := c.Candidates[acc[x]].Window
			b := c.Candidates[acc[y]].Window
			lo := math.Max(a[0], b[0])
			hi := math.Min(a[1], b[1])
			if hi > lo {
				c.Judgments = append(c.Judgments, Judgment{
					Rule: "overlapping_stages", WindowA: a, WindowB: b, OverlapS: hi - lo,
					Detail: "stages overlap in time; both retained; losses are not forced to sum to 100%",
				})
			}
		}
	}
}
