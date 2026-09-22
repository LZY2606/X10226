package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"tgaaudit/internal/domain"
	"tgaaudit/internal/units"
)

// CurrentBatches maps section kind to the currently applicable batch.
type CurrentBatches map[string]*domain.Batch

func (c CurrentBatches) ids() []string {
	kinds := make([]string, 0, len(c))
	for k := range c {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	ids := make([]string, 0, len(kinds))
	for _, k := range kinds {
		ids = append(ids, c[k].ID)
	}
	return ids
}

// colView is a numeric/string column extracted from a RawSection.
type colView struct {
	values []*float64
	texts  []string
	raws   []string
	unit   string
	basis  string
}

func view(sec *domain.RawSection, key string) colView {
	v := colView{}
	idx := -1
	for i, c := range sec.Columns {
		if normalize(c.Name) == key {
			idx = i
			v.unit = c.Unit
			v.basis = c.Basis
			break
		}
	}
	if idx < 0 {
		return v
	}
	for _, row := range sec.Rows {
		if idx < len(row) {
			cell := row[idx]
			v.values = append(v.values, cell.Value)
			v.raws = append(v.raws, cell.Raw)
			v.texts = append(v.texts, strings.TrimSpace(cell.Raw))
		} else {
			v.values = append(v.values, nil)
			v.raws = append(v.raws, "")
			v.texts = append(v.texts, "")
		}
	}
	return v
}

func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "_")
	for _, suf := range []string{"_s", "_mg", "_c"} {
		s = strings.TrimSuffix(s, suf)
	}
	switch s {
	case "time", "t", "furnace_time":
		return "t"
	case "mass", "m", "sample_mass":
		return "mass"
	case "sample_temperature", "sample_temp", "ts":
		return "ts"
	case "furnace_temperature", "furnace_temp", "tf", "setpoint":
		return "tf"
	case "gas", "atmosphere":
		return "gas"
	case "event", "valve_event", "valve":
		return "event"
	case "slope", "drift_rate", "drift":
		return "slope"
	case "intercept":
		return "intercept"
	case "t0", "start":
		return "t0"
	case "t1", "end":
		return "t1"
	case "at":
		return "at"
	case "note", "comment":
		return "note"
	}
	return s
}

func toSeconds(v *float64, unit string) *float64 {
	if v == nil {
		return nil
	}
	u := strings.TrimSpace(unit)
	switch u {
	case "", "s", "sec":
		out := *v
		return &out
	case "min":
		out := *v * 60
		return &out
	case "ms":
		out := *v / 1000
		return &out
	}
	q := units.Normalise(units.Quantity{Value: *v, Unit: u, OK: true})
	if q.OK {
		out := q.Value
		return &out
	}
	return nil
}

func toKg(v *float64, unit string) *float64 {
	if v == nil {
		return nil
	}
	switch strings.TrimSpace(unit) {
	case "mg":
		out := *v * 1e-6
		return &out
	case "g":
		out := *v * 1e-3
		return &out
	case "", "kg":
		out := *v
		return &out
	}
	q := units.Normalise(units.Quantity{Value: *v, Unit: unit, OK: true})
	if q.Unit == "kg" {
		out := q.Value
		return &out
	}
	return nil
}

func toKelvin(v *float64, unit string) *float64 {
	if v == nil {
		return nil
	}
	q := units.Quantity{Value: *v, Unit: strings.TrimSpace(unit), OK: true}
	if k, ok := units.NormaliseTemperature(q); ok {
		return &k
	}
	return nil
}

// interpolateAt estimates furnace temperature at time t ONLY from the furnace
// program (piecewise linear setpoints). It never bridges sample-data gaps.
func interpolateAt(ts, tf []float64, t float64) *float64 {
	if len(ts) == 0 {
		return nil
	}
	if t <= ts[0] {
		v := tf[0]
		return &v
	}
	if t >= ts[len(ts)-1] {
		v := tf[len(tf)-1]
		return &v
	}
	i := sort.Search(len(ts), func(i int) bool { return ts[i] >= t })
	if i == 0 {
		v := tf[0]
		return &v
	}
	if ts[i] == t {
		v := tf[i]
		return &v
	}
	frac := (t - ts[i-1]) / (ts[i] - ts[i-1])
	v := tf[i-1] + frac*(tf[i]-tf[i-1])
	return &v
}

// gasAt returns the gas name active at time t given valve switches.
func gasAt(switches []gasSwitch, t float64) string {
	cur := ""
	for _, sw := range switches {
		if sw.t <= t+1e-9 {
			cur = sw.to
		} else {
			break
		}
	}
	return cur
}

type gasSwitch struct {
	t     float64
	from  string
	to    string
	event string
}

// ParamFingerprint is the stable hash of the effective parameter set.
func ParamFingerprint(p domain.Params) string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

// SetID identifies the exact inputs+parameters of a derivation.
func SetID(b CurrentBatches, p domain.Params) string {
	h := sha256.New()
	for _, id := range b.ids() {
		fmt.Fprintf(h, "%s\n", id)
	}
	pj, _ := json.Marshal(p)
	h.Write(pj)
	sum := h.Sum(nil)
	return "set_" + hex.EncodeToString(sum[:14])
}

func nodeID(kind string, parts ...string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|", kind)
	for _, p := range parts {
		fmt.Fprintf(h, "%s|", p)
	}
	sum := h.Sum(nil)
	return kind + "-" + hex.EncodeToString(sum[:12])
}
