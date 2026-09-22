// Package analyze implements algorithm version "tga-stage-v1":
//
//   - raw points are normalized to min / degC / mg (raw text stays upstream);
//   - balance baseline drift and the blank curve are interpolated at sample
//     times and subtracted (they never remove non-monotonic signal);
//   - DTG is a central difference per contiguous segment, never across a
//     declared sampling gap;
//   - loss intervals grow around DTG minima and overlapping peaks split into
//     multiple *legal* candidate decompositions; candidates never span a gap;
//   - gas valve times are translated to sample-reaction time using an
//     atmosphere delay and temperature lag, so a valve switch is evidence,
//     not a stage boundary;
//   - stages may overlap and stage losses are not forced to sum to 100%;
//   - candidates, rejection reasons, error/unknown states are first-class.
package analyze

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"tga-review/internal/model"
)

// AlgorithmVersion is the frozen algorithm id stamped on every derived run.
const AlgorithmVersion = "tga-stage-v1"

// Params are the tunable inputs. Values carry no unit conversions; the unit
// basis is documented per key so fingerprints are meaningful.
type Params struct {
	// AtmosphereDelayMin is the gas transport delay after a valve switch.
	AtmosphereDelayMin float64 `json:"atmosphere_delay_min"`
	// TempLagOffsetMin shifts the reaction estimate for furnace temperature lag.
	TempLagOffsetMin float64 `json:"temp_lag_offset_min"`
	// MinStageLossPct rejects a candidate whose stage loss is below this share.
	MinStageLossPct float64 `json:"min_stage_loss_pct"`
	// NonMonotonicTolPct flags a mass uptick above this size as noise, not growth.
	NonMonotonicTolPct float64 `json:"nonmonotonic_tol_pct"`
	// DTGWindowMin is the minimum separation of two distinct DTG minima.
	DTGWindowMin float64 `json:"dtg_window_min"`
	// OverlapDepthPct: a valley above this fraction of the smaller flanking
	// peak is treated as overlapping peaks (two legal candidates are emitted).
	OverlapDepthPct float64 `json:"overlap_depth_pct"`
}

// DefaultParams returns the baseline parameter set.
func DefaultParams() Params {
	return Params{
		AtmosphereDelayMin:  0.8,
		TempLagOffsetMin:   0.2,
		MinStageLossPct:     2.0,
		NonMonotonicTolPct:  0.35,
		DTGWindowMin:        1.2,
		OverlapDepthPct:     55.0,
	}
}

// FromMap parses a parameter map; missing keys take defaults, unknown keys error.
func FromMap(m map[string]string) (Params, error) {
	p := DefaultParams()
	return decodeParams(m, p)
}

// Fingerprint returns the stable parameter fingerprint.
func (p Params) Fingerprint() string {
	keys := []string{
		"atmosphere_delay_min", "temp_lag_offset_min", "min_stage_loss_pct",
		"nonmonotonic_tol_pct", "dtg_window_min", "overlap_depth_pct",
	}
	vals := map[string]float64{
		"atmosphere_delay_min": p.AtmosphereDelayMin,
		"temp_lag_offset_min":  p.TempLagOffsetMin,
		"min_stage_loss_pct":   p.MinStageLossPct,
		"nonmonotonic_tol_pct": p.NonMonotonicTolPct,
		"dtg_window_min":       p.DTGWindowMin,
		"overlap_depth_pct":    p.OverlapDepthPct,
	}
	b, _ := json.Marshal(struct {
		Alg string `json:"alg"`
		V   map[string]float64 `json:"v"`
	}{AlgorithmVersion, pickMap(vals, keys)})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12])
}

func pickMap(m map[string]float64, keys []string) map[string]float64 {
	out := map[string]float64{}
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}

// Inputs is the typed snapshot of active source versions for one run.
type Inputs struct {
	Samples  []model.SamplePoint
	Gaps     []model.Gap
	Drift    []model.DriftPoint
	Furnace  []model.FurnaceSeg
	Gas      []model.GasEvent
	Blank    []model.BlankPoint
	Initial  []model.InitialMass
	Version  map[model.SourceKind]string // kind -> source version id
	RawSHA   map[model.SourceKind]string // kind -> raw sha256
}

// InputFingerprint hashes the active raw documents; identical input reuses runs.
func (in Inputs) Fingerprint() string {
	keys := make([]string, 0, len(in.RawSHA))
	for k := range in.RawSHA {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	h := sha256.New()
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(in.RawSHA[model.SourceKind(k)]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:12])
}
