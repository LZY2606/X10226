package domain

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// StableID returns a content-derived stable identifier with the given prefix.
func StableID(prefix string, parts ...any) string {
	h := sha1.New()
	enc := json.NewEncoder(h)
	for _, p := range parts {
		_ = enc.Encode(p)
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}

// ParamFingerprint canonicalizes the analysis parameters.
func ParamFingerprint(p Params) string {
	delays := map[string]float64{}
	for k, v := range p.GasDelayS {
		delays[k] = v
	}
	keys := make([]string, 0, len(delays))
	for k := range delays {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	type kv struct {
		K string  `json:"k"`
		V float64 `json:"v"`
	}
	list := make([]kv, 0, len(keys))
	for _, k := range keys {
		list = append(list, kv{k, delays[k]})
	}
	canon := struct {
		Delays        []kv    `json:"delays"`
		TempLagS      float64 `json:"temp_lag_s"`
		SmoothWindowS float64 `json:"smooth_window_s"`
		ProminencePct float64 `json:"prominence_pct"`
	}{list, p.TempLagS, p.SmoothWindowS, p.ProminencePct}
	return StableID("fp", canon)
}
