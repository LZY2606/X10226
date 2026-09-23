package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"tga-lineage/internal/domain"
)

// Param names (kept stable; referenced by UI and fixtures).
const (
	PGasDelay       = "gas_delay_s"
	PLagBasis       = "temp_lag_basis"
	PGapFactor      = "gap_factor"
	PSmoothWindow   = "smooth_window_s"
	PPeakProminence = "peak_prominence_mg"
	PDriftOrder     = "drift_poly_order"
)

// DefaultParams returns the v1 default parameter set.
func DefaultParams() map[string]domain.Param {
	return map[string]domain.Param{
		PGasDelay:       {Value: 0, Unit: "s"},
		PLagBasis:       {Value: 1, Unit: "flag:1=sample_temp,0=furnace_setpoint"},
		PGapFactor:      {Value: 3, Unit: "x median dt"},
		PSmoothWindow:   {Value: 20, Unit: "s"},
		PPeakProminence: {Value: 0.01, Unit: "mg"},
		PDriftOrder:     {Value: 1, Unit: "polynomial order"},
	}
}

// Fingerprint is a stable content hash over parameter name/value/unit triples.
func Fingerprint(params map[string]domain.Param) string {
	names := make([]string, 0, len(params))
	for k := range params {
		names = append(names, k)
	}
	sort.Strings(names)
	type triple struct {
		N string  `json:"n"`
		V float64 `json:"v"`
		U string  `json:"u"`
	}
	arr := make([]triple, 0, len(names))
	for _, n := range names {
		p := params[n]
		arr = append(arr, triple{N: n, V: p.Value, U: p.Unit})
	}
	b, _ := json.Marshal(arr)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

func num(params map[string]domain.Param, name string, fallback float64) float64 {
	if p, ok := params[name]; ok {
		return p.Value
	}
	return fallback
}
