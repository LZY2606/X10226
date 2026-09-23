// Package analyze implements the tgax-stage-v1 domain calculations:
// balance drift correction, blank/buoyancy subtraction, sampling gaps (never
// interpolated), non-monotonic noise marking, DTG peaks, gas-delay/temperature
// lag reaction mapping, overlapping stage candidate families and residue chains.
package analyze

import (
	"fmt"
	"math"
	"sort"

	"tgax/internal/model"
)

const AlgoVersion = "tgax-stage-v1.0"

type Point struct {
	I         int                        `json:"i"`
	Line      int                        `json:"line"`
	T         float64                    `json:"t"` // s, furnace time
	TempF     *float64                   `json:"temp_f"`
	RawM      *float64                   `json:"raw_m"`
	Corrected *float64                   `json:"corrected_m"`
	BlankM    *float64                   `json:"blank_m"`
	DriftM    *float64                   `json:"drift_m"`
	DTG       *float64                   `json:"dtg"` // mg/min
	GapBefore bool                       `json:"gap_before"`
	Noisy     bool                       `json:"noisy"`
	Values    map[string]model.ValueNode `json:"-"`
}

type Gap struct {
	AfterIndex int     `json:"after_index"`
	T1, T2     float64 `json:"t1"`
	Dt         float64 `json:"dt"`
	Reason     string  `json:"reason"`
}

type GasInterval struct {
	Gas        string  `json:"gas"`
	ValveAt    float64 `json:"valve_at"`
	ReactsAt   float64 `json:"reacts_at"`
	UntilValve float64 `json:"until_valve"`
	UntilReact float64 `json:"until_react"`
	DelayS     float64 `json:"delay_s"`
}

type StageCandidate struct {
	ID           string   `json:"id"`
	PeakT        float64  `json:"peak_t"`
	StartT       float64  `json:"start_t"`
	EndT         float64  `json:"end_t"`
	PeakTempC    *float64 `json:"peak_temp_c"`
	StartMassM   *float64 `json:"start_mass_mg"`
	EndMassM     *float64 `json:"end_mass_mg"`
	MassLossMg   *float64 `json:"mass_loss_mg"`
	MassLossPct  *float64 `json:"mass_loss_pct"`
	DTGPeak      *float64 `json:"dtg_peak_mg_per_min"`
	Gas          string   `json:"gas"`
	Family       string   `json:"family"` // primary | overlap-left | overlap-right
	OverlapsWith []string `json:"overlaps_with,omitempty"`
	BoundedByGap bool     `json:"bounded_by_gap"`
	Unknown      []string `json:"unknown,omitempty"`
	RejectReason string   `json:"reject_reason,omitempty"`
	SourceIDs    []string `json:"source_ids"`
	Current      bool     `json:"current"`
}

type Residue struct {
	ID         string      `json:"id"`
	Label      string      `json:"label"`
	AtT        *float64    `json:"at_t"`
	AtTempC    *float64    `json:"at_temp_c"`
	MassMg     *float64    `json:"mass_mg"`
	Mass0Mg    *float64    `json:"mass0_mg"`
	Percent    *float64    `json:"percent"`
	Unknown    []string    `json:"unknown,omitempty"`
	ChainSteps []ChainStep `json:"chain_steps"`
	SourceIDs  []string    `json:"source_ids"`
	Current    bool        `json:"current"`
}

type ChainStep struct {
	ValueID string         `json:"value_id"`
	Label   string         `json:"label"`
	Expr    string         `json:"expr"`
	Q       model.Quantity `json:"q"`
	Sources []string       `json:"sources"`
}

type Result struct {
	Points       []Point              `json:"points"`
	Gaps         []Gap                `json:"gaps"`
	NonMonotonic []int                `json:"non_monotonic_indices"`
	GasIntervals []GasInterval        `json:"gas_intervals"`
	Furnace      []model.FurnacePoint `json:"furnace,omitempty"`
	Candidates   []StageCandidate     `json:"candidates"`
	Residues     []Residue            `json:"residues"`
	Values       []model.ValueNode    `json:"values"`
	Assertions   []model.Assertion    `json:"assertions"`
	Diagnostics  []model.Diagnostic   `json:"diagnostics"`
	Mass0Mg      *float64             `json:"mass0_mg,omitempty"`
	InputsOK     bool                 `json:"inputs_ok"`
	ParamFP      string               `json:"param_fp"`
	SourceKey    string               `json:"source_key"`
}

func fptr(v float64) *float64 { return &v }

func hasGap(gaps []Gap, i int) bool {
	for _, g := range gaps {
		if g.AfterIndex == i {
			return true
		}
	}
	return false
}

// Analyze runs the v1 calculation. Input versions may be nil for layers that
// failed to import; corresponding outputs are marked unknown rather than faked.
func Analyze(p model.Params, samples, drift, furnace, valves, blank, mass *model.InputVersion) *Result {
	r := &Result{ParamFP: ParamFingerprint(p), SourceKey: srcKey(samples, drift, furnace, valves, blank, mass)}
	if furnace != nil {
		r.Furnace = furnace.Furnace
	}
	r.Values = []model.ValueNode{}

	// --- normalize samples ---
	type sp struct {
		p  Point
		ok bool
	}
	var live []sp
	if samples == nil || len(samples.Samples) == 0 {
		r.Diagnostics = append(r.Diagnostics, model.Diagnostic{Scope: "SAMPLES", Code: "no_samples", Message: "采样层缺失或为空，无法分析"})
		return r.finalize(p, map[string]string{})
	}
	for _, s := range samples.Samples {
		pt := Point{I: s.Index, Line: s.Line, Values: map[string]model.ValueNode{}}
		if s.T.Value != nil {
			pt.T = *s.T.Value
		}
		pt.TempF = s.Temp.Value
		pt.RawM = s.M.Value
		live = append(live, sp{pt, s.T.Status == "ok" && s.M.Status == "ok"})
	}

	// --- gaps: median delta, never interpolate ---
	med := medianDelta(live)
	var goodIdx []int
	for j := range live {
		if j > 0 {
			prev := -1
			for k := j - 1; k >= 0; k-- {
				if live[k].ok {
					prev = k
					break
				}
			}
			if prev >= 0 && live[j].ok {
				dt := live[j].p.T - live[prev].p.T
				if dt > p.GapFactor*med && med > 0 {
					r.Gaps = append(r.Gaps, Gap{AfterIndex: live[prev].p.I, T1: live[prev].p.T, T2: live[j].p.T, Dt: dt,
						Reason: "相邻采样间隔超过 gap_factor×中位间隔；缺口处不插值"})
					live[j].p.GapBefore = true
				}
			}
		}
		if live[j].ok {
			goodIdx = append(goodIdx, j)
		}
	}

	// --- furnace program -> sample temperature (with temp lag) ---
	for j := range live {
		if live[j].ok && live[j].p.TempF == nil && furnace != nil {
			if v := furnaceAt(furnace.Furnace, live[j].p.T-p.TempLagS); v != nil {
				live[j].p.TempF = v
			}
		}
	}

	// --- balance drift correction ---
	for j := range live {
		if !live[j].ok || live[j].p.RawM == nil {
			continue
		}
		raw := *live[j].p.RawM
		if drift != nil && drift.Drift != nil && drift.Drift.Rate.Value != nil && drift.Drift.T0.Value != nil {
			d := drift.Drift.Rate.Value
			corr := raw - d*(live[j].p.T-*drift.Drift.T0.Value)
			live[j].p.DriftM = fptr(d * (live[j].p.T - *drift.Drift.T0.Value))
			live[j].p.Corrected = fptr(corr)
		} else {
			c := raw
			live[j].p.Corrected = &c
		}
	}

	// --- blank/buoyancy subtraction (anchored to first usable point) ---
	if blank != nil && len(blank.Blank) > 0 {
		anchor := anchorBlank(blank.Blank)
		if anchor != nil {
			for j := range live {
				if live[j].ok && live[j].p.Corrected != nil && live[j].p.TempF != nil {
					b := blankInterp(blank.Blank, *live[j].p.TempF)
					if b != nil {
						rel := *b - *anchor
						live[j].p.BlankM = fptr(rel)
						c := *live[j].p.Corrected - rel
						live[j].p.Corrected = &c
					}
				}
			}
			r.Assertions = append(r.Assertions, model.Assertion{
				ID: "ast-blank-applied", Code: "BLANK_CORRECTION_APPLIED", Severity: "info",
				Text: fmt.Sprintf("空白曲线按首可用点 %.3f mg 锚定后逐点扣除（温度插值，跨缺口不用于质量）", *anchor)})
		}
	}
	if drift == nil || drift.Drift == nil || drift.Drift.Rate.Value == nil {
		r.Assertions = append(r.Assertions, model.Assertion{ID: "ast-no-drift", Code: "DRIFT_MODEL_MISSING", Severity: "warning",
			Text: "缺少天平漂移模型，校正质量=原始质量，残留率可能含系统偏差"})
	}

	// --- DTG: central differences within contiguous segments only ---
	for n := 1; n < len(goodIdx); n++ {
		a, b := goodIdx[n-1], goodIdx[n]
		pa, pb := live[a].p, live[b].p
		if pb.GapBefore {
			continue // do not differentiate across a gap
		}
		if pa.Corrected == nil || pb.Corrected == nil {
			continue
		}
		dt := pb.T - pa.T
		if dt <= 0 {
			continue
		}
		d := (*pb.Corrected - *pa.Corrected) / dt * 60
		live[b].p.DTG = &d
	}

	// --- non-monotonic noise: positive excursions beyond noise band ---
	band := p.NoiseBandMgPerMin
	for j := range live {
		if live[j].p.DTG != nil && *live[j].p.DTG > band {
			live[j].p.Noisy = true
			r.NonMonotonic = append(r.NonMonotonic, live[j].p.I)
		}
	}
	if len(r.NonMonotonic) > 0 {
		r.Assertions = append(r.Assertions, model.Assertion{ID: "ast-noise", Code: "NON_MONOTONIC_NOISE", Severity: "warning",
			Detail: map[string]any{"indices": r.NonMonotonic, "band_mg_per_min": band},
			Text:   fmt.Sprintf("%d 个点出现超过 %.3f mg/min 的非单调增重噪声，保留原点并标记，不做平滑删除", len(r.NonMonotonic), band)})
	}
	if len(r.Gaps) > 0 {
		r.Assertions = append(r.Assertions, model.Assertion{ID: "ast-gaps", Code: "SAMPLING_GAP", Severity: "warning",
			Detail: map[string]any{"count": len(r.Gaps)},
			Text:   fmt.Sprintf("检测到 %d 处采样缺口；缺口两侧不插值，跨缺口的峰边界标记未知", len(r.Gaps))})
	}

	r.Points = make([]Point, len(live))
	for j := range live {
		r.Points[j] = live[j].p
	}

	// --- gas valve timeline -> sample reaction timeline ---
	r.GasIntervals = gasIntervals(valves, p)

	// --- DTG peak detection with overlap candidate families ---
	r.Candidates = detectStages(r, p)

	// --- residues with expandable calculation chains ---
	r.Residues = computeResidues(r, mass)

	r.InputsOK = true
	idmap := map[string]string{}
	return r.finalize(p, idmap)
}

func medianDelta(live []struct {
	p  Point
	ok bool
}) float64 {
	var ds []float64
	var prev float64
	have := false
	for _, e := range live {
		if !e.ok {
			continue
		}
		if have {
			if d := e.p.T - prev; d > 0 {
				ds = append(ds, d)
			}
		}
		prev, have = e.p.T, true
	}
	sort.Float64s(ds)
	if len(ds) == 0 {
		return 0
	}
	return ds[len(ds)/2]
}

func furnaceAt(fp []model.FurnacePoint, t float64) *float64 {
	var ts, vs []float64
	for _, f := range fp {
		if f.T.Value != nil && f.Temp.Value != nil {
			ts = append(ts, *f.T.Value)
			vs = append(vs, *f.Temp.Value)
		}
	}
	for i := 1; i < len(ts); i++ {
		if t >= ts[i-1] && t <= ts[i] {
			if ts[i] == ts[i-1] {
				return &vs[i-1]
			}
			v := vs[i-1] + (vs[i]-vs[i-1])*(t-ts[i-1])/(ts[i]-ts[i-1])
			return &v
		}
	}
	if len(ts) > 0 {
		if t <= ts[0] {
			return &vs[0]
		}
		v := vs[len(vs)-1]
		return &v
	}
	return nil
}

func anchorBlank(bp []model.BlankPoint) *float64 {
	var best float64
	got := false
	for _, b := range bp {
		if b.M.Value != nil {
			if !got || *b.M.Value < best {
			}
			best, got = *b.M.Value, true
			break
		}
	}
	if got {
		return &best
	}
	return nil
}

func blankInterp(bp []model.BlankPoint, temp float64) *float64 {
	var ts, ms []float64
	for _, b := range bp {
		if b.Temp.Value != nil && b.M.Value != nil {
			ts = append(ts, *b.Temp.Value)
			ms = append(ms, *b.M.Value)
		}
	}
	for i := 1; i < len(ts); i++ {
		if temp >= ts[i-1] && temp <= ts[i] && ts[i] > ts[i-1] {
			v := ms[i-1] + (ms[i]-ms[i-1])*(temp-ts[i-1])/(ts[i]-ts[i-1])
			return &v
		}
	}
	if len(ms) > 0 {
		if temp <= ts[0] {
			return &ms[0]
		}
		v := ms[len(ms)-1]
		return &v
	}
	return nil
}

func gasIntervals(valves *model.InputVersion, p model.Params) []GasInterval {
	if valves == nil {
		return nil
	}
	type ve struct {
		at  float64
		gas string
		ok  bool
	}
	var events []ve
	for _, v := range valves.Valves {
		if v.At.Value != nil {
			events = append(events, ve{*v.At.Value, v.Gas, true})
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].at < events[j].at })
	var out []GasInterval
	for i, e := range events {
		delay := p.AtmosphereDelayS[e.gas]
		var untilValve float64
		if i+1 < len(events) {
			untilValve = events[i+1].at
		} else {
			untilValve = math.Inf(1)
		}
		nextDelay := 0.0
		if i+1 < len(events) {
			nextDelay = p.AtmosphereDelayS[events[i+1].gas]
		}
		g := GasInterval{Gas: e.gas, ValveAt: e.at, ReactsAt: e.at + delay,
			UntilValve: untilValve, UntilReact: untilValve + nextDelay, DelayS: delay}
		out = append(out, g)
	}
	if len(out) > 0 {
		var switched []float64
		for _, g := range out {
			if g.DelayS > 0 {
				switched = append(switched, g.DelayS)
			}
		}
		if len(switched) > 0 {
			out0 := out
			_ = out0
		}
	}
	return out
}

func gasAt(gs []GasInterval, reactT float64) string {
	gas := "unknown"
	for _, g := range gs {
		if reactT >= g.ReactsAt && reactT < g.UntilReact {
			gas = g.Gas
		}
	}
	return gas
}
