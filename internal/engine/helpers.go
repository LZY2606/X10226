package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"tgareview/internal/domain"
)

func resolveInitialMass(src *domain.Source, r *Report) *float64 {
	if src == nil {
		r.Notes = append(r.Notes, ReportNote{"warning", "initial_mass_missing",
			"no initial mass source has been imported"})
		return nil
	}
	var im domain.InitialMass
	if err := json.Unmarshal(src.Payload, &im); err != nil {
		r.Notes = append(r.Notes, ReportNote{"error", "initial_mass_corrupt", err.Error()})
		return nil
	}
	if !im.Mass.OK {
		r.Notes = append(r.Notes, ReportNote{"warning", "initial_mass_unconvertible",
			"initial mass kept raw: " + im.Mass.Text + " (" + im.Mass.Diag + ")"})
		return nil
	}
	v := im.Mass.Value
	return &v
}

func medianInterval(entries []entry) float64 {
	d := make([]float64, 0)
	for i := 1; i < len(entries); i++ {
		if entries[i].tOK && entries[i-1].tOK && entries[i].t > entries[i-1].t {
			d = append(d, entries[i].t-entries[i-1].t)
		}
	}
	if len(d) == 0 {
		return 0
	}
	sort.Float64s(d)
	return d[len(d)/2]
}

type segment struct{ from, to int }

func splitSegments(entries []entry, factor float64) []segment {
	var segs []segment
	start := -1
	med := medianInterval(entries)
	flush := func(end int) {
		if start >= 0 {
			segs = append(segs, segment{start, end})
			start = -1
		}
	}
	for i, e := range entries {
		if !e.tOK || e.mg == nil {
			flush(i - 1)
			continue
		}
		if start == -1 {
			start = i
			continue
		}
		prev := entries[i-1]
		if !prev.tOK || prev.mg == nil {
			// restart handled by start==-1 on bad row
			start = i
			continue
		}
		if med > 0 && factor > 0 && e.t-prev.t > factor*med {
			flush(i - 1)
			start = i
		}
	}
	flush(len(entries) - 1)
	return segs
}

func blankInterp(src *domain.Source, r *Report) [][2]float64 {
	if src == nil {
		return nil
	}
	var b domain.BlankCurve
	if err := json.Unmarshal(src.Payload, &b); err != nil {
		r.Notes = append(r.Notes, ReportNote{"error", "blank_corrupt", err.Error()})
		return nil
	}
	out := make([][2]float64, 0, len(b.Points))
	for _, p := range b.Points {
		if p.T.OK && p.M.OK && !p.Excluded {
			out = append(out, [2]float64{p.T.Value, p.M.Value})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func driftInterp(src *domain.Source, r *Report) [][2]float64 {
	if src == nil {
		return nil
	}
	var d domain.BalanceDrift
	if err := json.Unmarshal(src.Payload, &d); err != nil {
		r.Notes = append(r.Notes, ReportNote{"error", "drift_corrupt", err.Error()})
		return nil
	}
	out := make([][2]float64, 0)
	if d.Rate != nil && d.Rate.OK {
		// constant linear drift rate in mg/min: offset(t)=rate*t_min
		rate := d.Rate.Value
		return [][2]float64{{0, 0}, {1, rate / 60.0 * 1}}
	}
	for _, p := range d.Points {
		if p.T.OK && p.Offset.OK && !p.Excluded {
			out = append(out, [2]float64{p.T.Value, p.Offset.Value})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func buildProgram(src *domain.Source, tempLagC float64) []ProgStep {
	if src == nil {
		return nil
	}
	var pr domain.FurnaceProgram
	if err := json.Unmarshal(src.Payload, &pr); err != nil {
		return nil
	}
	var t float64
	out := make([]ProgStep, 0, len(pr.Segments))
	for _, sg := range pr.Segments {
		st := ProgStep{UID: fmt.Sprintf("%s-%d", domain.UIDProg, sg.Index), Index: sg.Index,
			Kind: sg.Kind, StartTS: t}
		st.StartTempC = sg.StartTemp.Value - tempLagC
		st.EndTempC = sg.EndTemp.Value - tempLagC
		dur := 0.0
		if sg.Kind == "ramp" && sg.Rate.OK && sg.Rate.Value != 0 {
			dur = math.Abs(sg.EndTemp.Value-sg.StartTemp.Value) / sg.Rate.Value * 60
		} else if sg.EndT.OK && sg.StartT.OK {
			dur = sg.EndT.Value - sg.StartT.Value
		}
		t += dur
		st.EndTS = t
		if dur > 0 {
			st.RateCMin = (st.EndTempC - st.StartTempC) * 60 / dur
		}
		st.Gas = sg.Gas
		out = append(out, st)
	}
	return out
}

func buildSwitches(src *domain.Source, delayS float64) []GasSwitch {
	if src == nil {
		return nil
	}
	var g domain.GasEvents
	if err := json.Unmarshal(src.Payload, &g); err != nil {
		return nil
	}
	out := make([]GasSwitch, 0, len(g.Events))
	for _, ev := range g.Events {
		if !ev.At.OK {
			continue
		}
		out = append(out, GasSwitch{
			UID:     fmt.Sprintf("%s-%d", domain.UIDGas, ev.Index),
			Index:   ev.Index,
			ValveTS: ev.At.Value,
			EffTS:   ev.At.Value + delayS,
			From:    ev.From, To: ev.To,
			DelayS: delayS, RawText: ev.At.Text,
		})
	}
	return out
}

// entry is a normalized sample row shared by build and stage detection.
type entry struct {
	idx  int
	t    float64
	tOK  bool
	furn *float64
	samp *float64
	mg   *float64
	rawM string
	rawT string
}
