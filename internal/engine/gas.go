package engine

import (
	"sort"

	"tga-lineage/internal/domain"
)

type gasEvent struct {
	t        float64 // raw valve-switch time
	state    string
	flow     float64
	flowOK   bool
	reactionT float64 // valve time + configured atmosphere delay
}

func gasEventsFrom(frag *domain.Fragment, delayS float64) []gasEvent {
	var evs []gasEvent
	for _, r := range frag.Rows {
		var e gasEvent
		if c, ok := r.Original["time"]; ok && c.ParseOK {
			e.t = c.Value
		}
		if c, ok := r.Original["valve"]; ok && c.ParseOK {
			e.state = c.Raw
		}
		if c, ok := r.Original["flow"]; ok && c.ParseOK {
			e.flow = c.Value
			e.flowOK = true
		}
		if e.state != "" {
			e.reactionT = e.t + delayS
			evs = append(evs, e)
		}
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].t < evs[j].t })
	return evs
}

// atmosphereAt returns the atmosphere acting at a given sample-reaction time.
func atmosphereAt(evs []gasEvent, reactionT float64) string {
	state := "UNKNOWN"
	for _, e := range evs {
		if e.reactionT <= reactionT {
			state = e.state
		} else {
			break
		}
	}
	return state
}

// valveAt returns the valve state at raw valve time (the command timeline).
func valveAt(evs []gasEvent, valveT float64) string {
	state := "UNKNOWN"
	for _, e := range evs {
		if e.t <= valveT {
			state = e.state
		} else {
			break
		}
	}
	return state
}

type tareLayer struct {
	layer string
	mass  float64
	ok    bool
	row   int
	basis string
}

func tareLayersFrom(frag *domain.Fragment) []tareLayer {
	var ls []tareLayer
	for _, r := range frag.Rows {
		var l tareLayer
		if c, ok := r.Original["layer"]; ok && c.ParseOK {
			l.layer = c.Raw
		}
		if c, ok := r.Original["mass"]; ok && c.ParseOK {
			l.mass = c.Value
			l.ok = true
		}
		if c, ok := r.Original["basis"]; ok && c.ParseOK {
			l.basis = c.Raw
		}
		l.row = r.Index
		ls = append(ls, l)
	}
	sort.SliceStable(ls, func(i, j int) bool { return ls[i].row < ls[j].row })
	return ls
}

// currentInitialMass applies the newest "initial"/"revision" layer (append-only
// revisions), keeping the crucible tare separate.
func currentInitialMass(ls []tareLayer) (float64, int, bool) {
	mass, row, ok := 0.0, 0, false
	for _, l := range ls {
		if !l.ok {
			continue
		}
		if l.layer == "initial" || l.layer == "revision" {
			mass, row, ok = l.mass, l.row, true
		}
	}
	return mass, row, ok
}
