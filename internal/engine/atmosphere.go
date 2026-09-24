package engine

import (
	"fmt"

	"tgaline/internal/domain"
)

// reactionWindows converts valve events into plausible sample-reaction
// intervals. Valve switch time != reaction time: transport delay and thermal
// lag are expressed as an interval [valve+delay, valve+lag], not a point.
func reactionWindows(events []gasEvent, p map[string]float64) []domain.ReactionWindow {
	delay := p["atmo_delay_s"]
	lag := p["atmo_lag_s"]
	if lag < delay {
		lag = delay
	}
	out := make([]domain.ReactionWindow, 0, len(events))
	for _, e := range events {
		state := "切入"
		if !e.on {
			state = "切出"
		}
		rw := domain.ReactionWindow{
			ID:          reactionID(e.gas, e.t, e.t+delay, e.t+lag),
			FromEventID: e.id, Gas: e.gas, ValveS: e.t,
			Window: domain.Window{StartS: e.t + delay, EndS: e.t + lag},
			DelayS: delay, LagS: lag,
			Note: fmt.Sprintf("%s 阀动作 @ %.1fs 不等于样品反应时间；反应可能落在 +%.0f~+%.0fs 的 %s 窗口",
				e.gas, e.t, delay, lag, state),
		}
		out = append(out, rw)
	}
	return out
}

// overlappingWindows reports gas reaction windows that can claim a peak,
// i.e. the peak center lies inside the [valve+delay, valve+lag] interval.
func windowsCovering(wins []domain.ReactionWindow, t float64) []domain.ReactionWindow {
	var hit []domain.ReactionWindow
	for _, w := range wins {
		if t >= w.Window.StartS && t <= w.Window.EndS {
			hit = append(hit, w)
		}
	}
	return hit
}
