package engine

import (
	"encoding/json"
	"math"
	"sort"

	"tgaline/internal/domain"
)

// point is one accepted sample measurement in canonical units.
type point struct {
	t float64 // s
	T float64 // K
	m float64 // mg (raw balance, uncorrected)
}

// layerInput extracts typed rows from a layer (nil-safe).
type layerInput struct {
	sample  []point
	drift   []kv
	furnace []kv
	gas     []gasEvent
	blank   []kv
	m0      *initialMass
}

type kv struct{ t, v float64 }

type gasEvent struct {
	t    float64
	gas  string
	on   bool
	flow float64
	id   string
}

type initialMass struct {
	mg      float64
	atS     *float64
	source  string
	rowNo   int
	layerID string
}

// NoiseRecord documents one non-monotonic excursion as first-class data.
type NoiseRecord struct {
	Index  int     `json:"index"`
	TimeS  float64 `json:"time_s"`
	JumpMG float64 `json:"jump_mg"`
	Kind   string  `json:"kind"` // uptick (mass gains) | isolated_spike
	Note   string  `json:"note"`
}

type noiseReport struct {
	Records     []NoiseRecord `json:"records"`
	ThresholdMG float64       `json:"threshold_mg"`
}

func extractInput(cur domain.CurrentLayers) layerInput {
	var in layerInput
	if l := cur[domain.LayerSample]; l != nil {
		for _, r := range l.Rows {
			if r.Status != domain.RowAccepted || r.TimeQ == nil || r.MassQ == nil || r.TQ == nil {
				continue
			}
			if !r.TimeQ.ConvOK || !r.MassQ.ConvOK || !r.TQ.ConvOK {
				continue
			}
			in.sample = append(in.sample, point{
				t: r.TimeQ.Value, T: r.TQ.Value, m: r.MassQ.Value,
			})
		}
		sort.Slice(in.sample, func(i, j int) bool { return in.sample[i].t < in.sample[j].t })
	}
	if l := cur[domain.LayerDrift]; l != nil {
		for _, r := range l.Rows {
			if r.Status != domain.RowAccepted || r.TimeQ == nil || r.MassQ == nil {
				continue
			}
			if r.TimeQ.ConvOK && r.MassQ.ConvOK {
				in.drift = append(in.drift, kv{r.TimeQ.Value, r.MassQ.Value})
			}
		}
		sort.Slice(in.drift, func(i, j int) bool { return in.drift[i].t < in.drift[j].t })
	}
	if l := cur[domain.LayerFurnace]; l != nil {
		for _, r := range l.Rows {
			if r.Status != domain.RowAccepted || r.TimeQ == nil || r.TQ == nil {
				continue
			}
			if r.TimeQ.ConvOK && r.TQ.ConvOK {
				in.furnace = append(in.furnace, kv{r.TimeQ.Value, r.TQ.Value})
			}
		}
		sort.Slice(in.furnace, func(i, j int) bool { return in.furnace[i].t < in.furnace[j].t })
	}
	if l := cur[domain.LayerGas]; l != nil {
		for _, r := range l.Rows {
			if r.Status != domain.RowAccepted || r.TimeQ == nil || r.MassQ == nil {
				continue
			}
			if !r.TimeQ.ConvOK || !r.MassQ.ConvOK {
				continue
			}
			in.gas = append(in.gas, gasEvent{
				t: r.TimeQ.Value, gas: r.Extra["gas"],
				on: r.Extra["state"] == "on", flow: r.MassQ.Value,
				id: rowNodeID("gas", l.ID, r.LineNo),
			})
		}
		sort.Slice(in.gas, func(i, j int) bool { return in.gas[i].t < in.gas[j].t })
	}
	if l := cur[domain.LayerBlank]; l != nil {
		for _, r := range l.Rows {
			if r.Status != domain.RowAccepted || r.TimeQ == nil || r.MassQ == nil {
				continue
			}
			if r.TimeQ.ConvOK && r.MassQ.ConvOK {
				in.blank = append(in.blank, kv{r.TimeQ.Value, r.MassQ.Value})
			}
		}
		sort.Slice(in.blank, func(i, j int) bool { return in.blank[i].t < in.blank[j].t })
	}
	if l := cur[domain.LayerInitial]; l != nil {
		// stratified initial mass: latest version, choose its first accepted row
		// that is anchored to the earliest time (or the only row).
		var best *domain.LayerRow
		for i := range l.Rows {
			r := &l.Rows[i]
			if r.Status != domain.RowAccepted || r.MassQ == nil || !r.MassQ.ConvOK {
				continue
			}
			if best == nil {
				best = r
				continue
			}
			if r.TimeQ != nil && r.TimeQ.ConvOK &&
				(best.TimeQ == nil || r.TimeQ.Value < best.TimeQ.Value) {
				best = r
			}
		}
		if best != nil {
			im := initialMass{mg: best.MassQ.Value, source: best.Extra["source"],
				rowNo: best.LineNo, layerID: l.ID}
			if best.TimeQ != nil && best.TimeQ.ConvOK {
				tv := best.TimeQ.Value
				im.atS = &tv
			}
			in.m0 = &im
		}
	}
	return in
}

func rowNodeID(kind, layerID string, lineNo int) string {
	return kind + ":" + layerID + ":" + itoa(lineNo)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// interp performs piecewise-linear interpolation ONLY between neighboring
// table points. It never extrapolates (ok=false outside the support range),
// and callers must never use it across a sampling gap.
func interp(table []kv, t float64) (float64, bool) {
	if len(table) == 0 {
		return 0, false
	}
	if t < table[0].t || t > table[len(table)-1].t {
		return 0, false
	}
	i := sort.Search(len(table), func(i int) bool { return table[i].t >= t })
	if i < len(table) && math.Abs(table[i].t-t) < 1e-12 {
		return table[i].v, true
	}
	if i == 0 || i >= len(table) {
		return 0, false
	}
	a, b := table[i-1], table[i]
	if b.t == a.t {
		return a.v, true
	}
	return a.v + (b.v-a.v)*(t-a.t)/(b.t-a.t), true
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
