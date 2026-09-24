package derive

import (
	"fmt"
	"math"
	"sort"

	"tga-review/internal/domain"
)

// ProgramPoint is the furnace program as experienced by the sample: the
// setpoint curve is shifted by thermal lag.
type ProgramPoint struct {
	T         float64 `json:"t_s"`
	SetpointK float64 `json:"setpoint_k"`
	LaggedK   float64 `json:"sample_experienced_k"`
}

// GasAtSample is a valve event shifted by gas transport delay.
type GasAtSample struct {
	ValveT     float64          `json:"valve_t_s"`
	ArrivalT   float64          `json:"arrival_t_s"`
	ReactionT0 float64          `json:"reaction_t0_s"`
	Action     string           `json:"action"`
	Species    []domain.Species `json:"species"`
	SourceID   string           `json:"source_id"`
	Note       string           `json:"note,omitempty"`
}

// Timeline holds program and gas layers aligned to furnace seconds.
type Timeline struct {
	Program     []ProgramPoint `json:"program"`
	Gas         []GasAtSample  `json:"gas"`
	Sources     []string       `json:"sources"`
	AlgoVersion string         `json:"algo_version"`
	ParamFP     string         `json:"param_fp"`
	ParamRev    int            `json:"param_revision"`
}

// BuildTimeline materializes the lagged furnace program and the gas-at-sample
// timeline. Valve switch time is explicitly kept distinct from reaction time.
func (e *Engine) BuildTimeline(program, gas []domain.RawRecord) Timeline {
	lag := e.pNum("temp_lag_s")
	delay := e.pNum("gas_delay_s")
	src := map[string]bool{}

	var segs []programSeg
	for _, r := range program {
		ps := r.Payload.(domain.ProgramSegment)
		src[r.ID] = true
		t0, _ := ps.FromTime.SI()
		t1, _ := ps.ToTime.SI()
		k0, _ := ps.Setpoint0.SI()
		k1, _ := ps.Setpoint1.SI()
		segs = append(segs, programSeg{t0, t1, k0, k1})
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].t0 < segs[j].t0 })

	var prog []ProgramPoint
	if len(segs) > 0 {
		t := segs[0].t0
		end := segs[len(segs)-1].t1
		step := 10.0
		for t <= end+1e-9 {
			sp := setpointAt(segs, t)
			lagged := setpointAt(segs, math.Max(0, t-lag))
			prog = append(prog, ProgramPoint{T: t, SetpointK: sp, LaggedK: lagged})
			t += step
		}
		prog = append(prog, ProgramPoint{T: end, SetpointK: setpointAt(segs, end),
			LaggedK: setpointAt(segs, math.Max(0, end-lag))})
	}

	var gasLine []GasAtSample
	for _, r := range gas {
		ge := r.Payload.(domain.GasValveEvent)
		src[r.ID] = true
		t, _ := ge.FurnaceTime.SI()
		gasLine = append(gasLine, GasAtSample{
			ValveT: t, ArrivalT: t + delay, ReactionT0: t + delay,
			Action: ge.Action, Species: ge.Species, SourceID: r.ID, Note: ge.Note,
		})
	}
	sort.Slice(gasLine, func(i, j int) bool { return gasLine[i].ValveT < gasLine[j].ValveT })

	var sources []string
	for id := range src {
		sources = append(sources, id)
	}
	sort.Strings(sources)
	return Timeline{Program: prog, Gas: gasLine, Sources: sources,
		AlgoVersion: AlgoVersion, ParamFP: e.FP, ParamRev: e.Rev}
}

type programSeg struct{ t0, t1, sp0, sp1 float64 }

func setpointAt(segs []programSeg, t float64) float64 {
	for _, s := range segs {
		if t >= s.t0 && t <= s.t1 {
			if s.t1 == s.t0 {
				return s.sp1
			}
			f := (t - s.t0) / (s.t1 - s.t0)
			return s.sp0 + f*(s.sp1-s.sp0)
		}
	}
	if t < segs[0].t0 {
		return segs[0].sp0
	}
	return segs[len(segs)-1].sp1
}

// BuildStages turns peaks into stage candidates. Stage bounds may overlap;
// each crossing of a sampling gap yields unknown mass loss plus alternatives.
func (e *Engine) BuildStages(s Series, pr PeaksResult, tl Timeline, gas []domain.RawRecord) StagesResult {
	minWidth := e.pNum("stage_min_width_s")
	delay := e.pNum("gas_delay_s")
	lag := e.pNum("temp_lag_s")

	var stages []Stage
	for i, pk := range pr.Peaks {
		from, to := pk.LeftT, pk.RightT
		label := fmt.Sprintf("S%d", i+1)
		st := Stage{
			Label: label, FromT: from, ToT: to, PeakIDs: []string{pk.ID},
			CrossesGap: pk.CrossesGap, Sources: append([]string{}, pk.Sources...),
			AlgoVersion: AlgoVersion, ParamFP: e.FP, ParamRev: e.Rev,
			ReactionT0: from + delay, ReactionT1: to + delay,
		}
		if strings.HasPrefix(pk.ID, "") {
		}
		st.ID = e.derID("stage", map[string]any{
			"peaks": st.PeakIDs, "from": from, "to": to,
		})
		if to-from < minWidth {
			st.Status = "rejected"
			st.RejectReason = fmt.Sprintf("width %.0fs below minimum %.0fs", to-from, minWidth)
			stages = append(stages, st)
			continue
		}
		st.Status = "candidate"
		if pk.CrossesGap {
			st.Unknown = true
			// legal alternatives: clipped before gap, clipped after gap
			for _, g := range s.Gaps {
				if from < g.T1 && to > g.T0 {
					a := st
					a.Label = label + "a"
					a.ToT = g.T0
					a.CrossesGap = false
					a.ID = e.derID("stage", map[string]any{
						"peaks": st.PeakIDs, "from": from, "to": g.T0, "clip": "before_gap",
					})
					a.Status = "candidate"
					if loss, kg, ok := massLoss(s, a.FromT, a.ToT); ok {
						a.MassLossFrac = &loss
						a.MassLossKg = &kg
					}
					a.Alternatives = nil
					stages = append(stages, a)

					b := st
					b.Label = label + "b"
					b.FromT = g.T1
					b.CrossesGap = false
					b.ID = e.derID("stage", map[string]any{
						"peaks": st.PeakIDs, "from": g.T1, "to": to, "clip": "after_gap",
					})
					b.Status = "candidate"
					if loss, kg, ok := massLoss(s, b.FromT, b.ToT); ok {
						b.MassLossFrac = &loss
						b.MassLossKg = &kg
					}
					stages = append(stages, b)
				}
			}
			st.Status = "rejected_as_unknown_span"
			st.RejectReason = "stage spans a sampling gap; mass loss not interpolated; see clipped alternatives"
			stages = append(stages, st)
			continue
		}
		if loss, kg, ok := massLoss(s, from, to); ok {
			st.MassLossFrac = &loss
			st.MassLossKg = &kg
		} else {
			st.Unknown = true
			st.Status = "rejected_as_unknown_span"
			st.RejectReason = "endpoint mass unavailable"
		}
		stages = append(stages, st)
	}

	// annotate overlaps explicitly (no forced 100% sum)
	for i := range stages {
		for j := range stages {
			if i == j || stages[i].Unknown || stages[j].Unknown {
				continue
			}
			if stages[i].Status != "candidate" || stages[j].Status != "candidate" {
				continue
			}
			if stages[i].FromT < stages[j].ToT && stages[j].FromT < stages[i].ToT {
				stages[i].OverlapsWith = append(stages[i].OverlapsWith, stages[j].ID)
			}
		}
	}
	sort.Slice(stages, func(i, j int) bool {
		if stages[i].FromT != stages[j].FromT {
			return stages[i].FromT < stages[j].FromT
		}
		return stages[i].Label < stages[j].Label
	})
	_ = lag
	return StagesResult{Stages: stages}
}

// massLoss returns corrected-mass loss fraction relative to initial mass.
// It NEVER interpolates: both endpoint times must coincide with observed
// points (within tolerance).
func massLoss(s Series, t0, t1 float64) (frac, kg float64, ok bool) {
	m0, ok0 := massAtObserved(s, t0)
	m1, ok1 := massAtObserved(s, t1)
	if !ok0 || !ok1 || s.InitialMassKg <= 0 {
		return 0, 0, false
	}
	return (m0 - m1) / s.InitialMassKg, m0 - m1, true
}

func massAtObserved(s Series, t float64) (float64, bool) {
	const tol = 0.5
	for _, p := range s.Points {
		if math.Abs(p.T-t) <= tol {
			return p.CorrMassKg, true
		}
	}
	return 0, false
}
