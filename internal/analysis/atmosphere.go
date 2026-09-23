package analysis

import (
	"fmt"
	"sort"

	"tga-review/internal/domain"
)

// MapAtmosphere converts valve switch times into sample reaction windows.
// reaction center = valve time + atmos delay; the temperature lag gives an
// uncertainty band. Valve time is therefore never treated as reaction time.
func (e *Engine) MapAtmosphere(in Input) AtmosPayload {
	var windows []ReactionWindow
	var diags []domain.Diagnostic
	gasByName := map[string]bool{}
	for i, g := range in.Gas {
		center := g.TimeSec + in.Params.AtmosDelaySec
		hw := in.Params.TempLagSec
		w := ReactionWindow{
			GasEventID: g.ID, Gas: g.Gas, ValveS: g.TimeSec, CenterS: center,
			HalfWidthS: hw, LowS: center - hw, HighS: center + hw,
		}
		windows = append(windows, w)
		gasByName[g.Gas] = true
		if hw > 0 || in.Params.AtmosDelaySec > 0 {
			diags = append(diags, domain.Diagnostic{Code: "VALVE_NOT_REACTION_TIME", Severity: domain.SevInfo,
				Ref: g.ID,
				Message: fmt.Sprintf("gas event %d (%s): valve %.1fs, reaction window [%.1f, %.1f]s (delay %.1fs, lag %.1fs)",
					i, g.Gas, g.TimeSec, w.LowS, w.HighS, in.Params.AtmosDelaySec, hw)})
		}
	}
	// derive nominal spans at window centers for labeling
	sort.Slice(windows, func(i, j int) bool { return windows[i].CenterS < windows[j].CenterS })
	var spans []GasSpan
	for i, w := range windows {
		end := 1e12
		if i+1 < len(windows) {
			end = windows[i+1].CenterS
		}
		spans = append(spans, GasSpan{Gas: w.Gas, StartS: w.CenterS, EndS: end})
	}
	return AtmosPayload{Windows: windows, Spans: spans, Diagnostics: diags}
}

// gasAtTime labels a stage: if the stage interval intersects more than one
// uncertainty band, the atmosphere is considered uncertain.
func gasAtTime(atmos AtmosPayload, startS, endS float64) (string, bool) {
	var hit []string
	for _, s := range atmos.Spans {
		if intervalsOverlap(startS, endS, s.StartS, s.EndS) {
			hit = append(hit, s.Gas)
		}
	}
	// uncertainty: any window band intersecting the stage interior
	uncertain := false
	for _, w := range atmos.Windows {
		if intervalsOverlap(startS, endS, w.LowS, w.HighS) {
			// stage crosses the uncertainty band of a switch
			if startS < w.HighS && endS > w.LowS &&
				((startS < w.LowS) || (endS > w.HighS)) {
				uncertain = true
			}
		}
	}
	uniq := unique(hit)
	label := "unknown"
	if len(uniq) == 1 {
		label = uniq[0]
	} else if len(uniq) > 1 {
		label = uniq[0] + "/" + uniq[1]
		uncertain = true
	}
	if len(uniq) > 2 {
		label = "mixed"
		uncertain = true
	}
	return label, uncertain
}

func intervalsOverlap(a1, a2, b1, b2 float64) bool {
	return a1 < b2 && b1 < a2
}

func unique(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
