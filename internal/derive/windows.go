package derive

import (
	"sort"

	"tga-archive/internal/domain"
)

// buildWindows turns valve events into sample-experienced reaction windows.
// Valve switch time != sample reaction time: transport delay shifts by
// ValveDelayS and measured thermal lag is reported separately.
func buildWindows(valvesSP, furnaceSP *domain.SourcePayload, pts []Point, p domain.Params) []ReactionWindow {
	if valvesSP == nil {
		return nil
	}
	valves := append([]domain.ValveEvent(nil), valvesSP.Valves...)
	sort.SliceStable(valves, func(i, j int) bool {
		return valves[i].Time.Valid && valves[j].Time.Valid && valves[i].Time.Value < valves[j].Time.Value
	})
	var out []ReactionWindow
	for i, v := range valves {
		startT := v.Time.Value + p.ValveDelayS
		endT := inf()
		if i+1 < len(valves) {
			endT = valves[i+1].Time.Value + p.ValveDelayS
		} else if len(pts) > 0 {
			endT = pts[len(pts)-1].Time.Value
		}
		valveTemp := tempAt(pts, v.Time.Value)
		startTemp := tempAt(pts, startT)
		furnaceTemp := furnaceAt(furnaceSP, v.Time.Value)
		lag := invalid("K")
		if furnaceTemp.Valid && valveTemp.Valid {
			lag = measure(furnaceTemp.Value-valveTemp.Value, "K")
		}
		w := ReactionWindow{
			ID:                 domain.HashID("win", valvesSP.SourceID, v.Time.Value, v.Gas, p.ValveDelayS),
			Gas:                v.Gas,
			ValveAt:            v.Time,
			StartAt:            measure(startT, domain.UnitSecond),
			EndAt:              measure(endT, domain.UnitSecond),
			ValveTemp:          valveTemp,
			StartTemp:          startTemp,
			FurnaceTempAtValve: furnaceTemp,
			MeasuredLagK:       lag,
			Delay:              measure(p.ValveDelayS, domain.UnitSecond),
		}
		if lag.Valid && lag.Value > p.ReferenceTempLagK {
			w.Note = "measured thermal lag exceeds reference; valve switch precedes sample experience"
		} else {
			w.Note = "transport delay applied; valve time is not reaction time"
		}
		out = append(out, w)
	}
	return out
}

func tempAt(pts []Point, t float64) domain.Measure {
	best := -1
	for i := range pts {
		if !pts[i].Time.Valid || !pts[i].Temp.Valid {
			continue
		}
		if pts[i].Time.Value <= t {
			best = i
		}
	}
	if best < 0 {
		return invalid(domain.UnitKelvin)
	}
	return pts[best].Temp
}

func furnaceAt(furnaceSP *domain.SourcePayload, t float64) domain.Measure {
	if furnaceSP == nil {
		return invalid(domain.UnitKelvin)
	}
	for _, seg := range furnaceSP.Furnace {
		if !seg.StartTime.Valid || !seg.EndTime.Valid {
			continue
		}
		if t >= seg.StartTime.Value-1e-9 && t <= seg.EndTime.Value+1e-9 {
			if seg.Kind == "hold" || seg.EndTime.Value == seg.StartTime.Value {
				return seg.EndTemp
			}
			frac := (t - seg.StartTime.Value) / (seg.EndTime.Value - seg.StartTime.Value)
			return measure(seg.StartTemp.Value+frac*(seg.EndTemp.Value-seg.StartTemp.Value), domain.UnitKelvin)
		}
	}
	return invalid(domain.UnitKelvin)
}

func inf() float64 {
	// finite sentinel; callers always overwrite the last window
	return 1e18
}
