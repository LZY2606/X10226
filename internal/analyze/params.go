package analyze

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// paramKeys maps CSV key -> setter; the set is closed, so a typo is an error
// instead of silently changing nothing.
var paramKeys = map[string]func(*Params, float64){
	"atmosphere_delay_min":  func(p *Params, v float64) { p.AtmosphereDelayMin = v },
	"temp_lag_offset_min":   func(p *Params, v float64) { p.TempLagOffsetMin = v },
	"min_stage_loss_pct":    func(p *Params, v float64) { p.MinStageLossPct = v },
	"nonmonotonic_tol_pct":  func(p *Params, v float64) { p.NonMonotonicTolPct = v },
	"dtg_window_min":        func(p *Params, v float64) { p.DTGWindowMin = v },
	"overlap_depth_pct":     func(p *Params, v float64) { p.OverlapDepthPct = v },
}

func decodeParams(m map[string]string, p Params) (Params, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		set, ok := paramKeys[k]
		if !ok {
			return p, fmt.Errorf("unknown parameter %q (allowed: %s)", k, strings.Join(allowedKeys(), ","))
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(m[k]), 64)
		if err != nil {
			return p, fmt.Errorf("parameter %s: %w", k, err)
		}
		set(&p, v)
	}
	return p, nil
}

func allowedKeys() []string {
	out := make([]string, 0, len(paramKeys))
	for k := range paramKeys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AsMap serializes params to the stored key,value representation.
func (p Params) AsMap() map[string]string {
	return map[string]string{
		"atmosphere_delay_min": strconv.FormatFloat(p.AtmosphereDelayMin, 'g', -1, 64),
		"temp_lag_offset_min":  strconv.FormatFloat(p.TempLagOffsetMin, 'g', -1, 64),
		"min_stage_loss_pct":   strconv.FormatFloat(p.MinStageLossPct, 'g', -1, 64),
		"nonmonotonic_tol_pct": strconv.FormatFloat(p.NonMonotonicTolPct, 'g', -1, 64),
		"dtg_window_min":       strconv.FormatFloat(p.DTGWindowMin, 'g', -1, 64),
		"overlap_depth_pct":    strconv.FormatFloat(p.OverlapDepthPct, 'g', -1, 64),
	}
}
