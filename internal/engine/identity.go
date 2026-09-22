package engine

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"tga-review/internal/domain"
)

// fingerprint hashes the parameters that influence a layer.
func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:])[:12]
}

// stableID yields an id that is identical when the logical family, algorithm
// and full content (sources + deps + payload) are identical. Round-tripping
// export/import therefore preserves identity, order and provenance.
func stableID(family string, payload any, sourceIDs, deps []string) string {
	all := append(append(append([]string{}, sourceIDs...), deps...), AlgoVersion)
	sort.Strings(all)
	hb, _ := json.Marshal(payload)
	h := sha1.Sum([]byte(family + "|" + joinSorted(all) + "|" + string(hb)))
	return fmt.Sprintf("art-%s-%s", family, hex.EncodeToString(h[:])[:14])
}

func joinSorted(xs []string) string {
	cp := append([]string{}, xs...)
	sort.Strings(cp)
	out := ""
	for _, x := range cp {
		out += x + ","
	}
	return out
}

func mkArtifact(snap *Snapshot, kind, family, label string, sourceIDs, deps []string, payload any) domain.Artifact {
	raw, _ := json.Marshal(payload)
	pv := paramView{}
	if snap != nil {
		pv = paramView{
			AtmosDelayS:  snap.Params.AtmosDelayS,
			TempLagC:     snap.Params.TempLagC,
			DriftKind:    snap.Params.DriftKind,
			DriftRateMgH: snap.Params.DriftRateMgH,
			Seq:          snap.Params.Seq,
		}
	}
	paramFP := fingerprint(pv)
	id := stableID(family, payload, sourceIDs, deps)
	return domain.Artifact{
		ID: id, ProjectID: "", Kind: kind, Family: family, Label: label,
		AlgoVersion: AlgoVersion, ParamFP: paramFP,
		SourceIDs: sourceIDs, DependsOn: deps, Payload: raw,
	}
}

// paramView decides which parameters affect all layers; layers that depend on
// fewer parameters still embed the same fingerprint, but the service treats
// dependency invalidation at family granularity (see DependsOn + source ids).
// (paramView used in JSON fingerprint below)
type paramView struct {
	AtmosDelayS  float64 `json:"atmos_delay_s"`
	TempLagC     float64 `json:"temp_lag_c"`
	DriftKind    string  `json:"drift_kind"`
	DriftRateMgH float64 `json:"drift_rate_mg_h"`
	Seq          int64   `json:"seq"`
}

func segID(i int) string { return fmt.Sprintf("seg-%d", i) }
