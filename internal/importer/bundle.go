// Package importer ingests the layered TGA bundle. Each layer is validated
// independently: a malformed layer is retained as a fragment with diagnostics
// (partial import) while valid layers still land. Original representations
// are never overwritten; normalization lives only in the derived layer.
package importer

import (
	"encoding/json"
	"fmt"

	"tga-review/internal/domain"
)

// Bundle is the wire format accepted by POST /api/projects/{id}/import.
// Layers may be freely combined; any layer may also carry "_raw" style
// intentionally bad values to exercise diagnostics.
type Bundle struct {
	ProjectID string         `json:"project_id,omitempty"`
	Samples   *SamplesLayer  `json:"samples,omitempty"`
	Drift     *DriftLayer    `json:"drift,omitempty"`
	Furnace   *FurnaceLayer  `json:"furnace,omitempty"`
	Gas       *GasLayer      `json:"gas,omitempty"`
	Blank     *BlankLayer    `json:"blank,omitempty"`
	Initial   *InitialLayer  `json:"initial,omitempty"`
	Params    *ParamsLayer   `json:"params,omitempty"`
}

type SamplesLayer struct {
	Points []SampleRow `json:"points"`
}

// SampleRow keeps every cell as a string so the original representation
// ("12.30 mg", " 310.5", "n/a") survives into storage.
type SampleRow struct {
	Time string `json:"time"`
	Temp string `json:"temp"`
	Mass string `json:"mass"`
	// Optional explicit unit hints; when absent the parser infers/defaults.
	TimeUnit string `json:"time_unit,omitempty"`
	TempUnit string `json:"temp_unit,omitempty"`
	MassUnit string `json:"mass_unit,omitempty"`
}

type DriftLayer struct {
	Model     string  `json:"model"`
	RateMGMin float64 `json:"rate_mg_min,omitempty"`
	BlankRef  string  `json:"blank_ref,omitempty"`
	Raw       string  `json:"raw,omitempty"` // unparseable original spec kept verbatim
}

type FurnaceLayer struct {
	Segments []FurnaceRow `json:"segments"`
}
type FurnaceRow struct {
	Kind   string  `json:"kind"`
	Start  string  `json:"start"`
	End    string  `json:"end"`
	From   string  `json:"from"`
	To     string  `json:"to"`
}

type GasLayer struct {
	Events []GasRow `json:"events"`
}
type GasRow struct {
	Time     string  `json:"time"`
	Gas      string  `json:"gas"`
	FlowSCCM float64 `json:"flow_sccm,omitempty"`
	Note     string  `json:"note,omitempty"`
}

type BlankLayer struct {
	Points []BlankRow `json:"points"`
}
type BlankRow struct {
	Time string  `json:"time"`
	Mass string  `json:"mass"`
}

type InitialLayer struct {
	Records []InitialRow `json:"records"`
}
type InitialRow struct {
	Mass  string  `json:"mass"`
	Basis string  `json:"basis"`
	At    string  `json:"at"`
	Note  string  `json:"note,omitempty"`
}

type ParamsLayer struct {
	AtmosDelaySec float64 `json:"atmos_delay_s"`
	TempLagSec    float64 `json:"temp_lag_s"`
	OverlapMergeC float64 `json:"overlap_merge_c"`
	NoiseFloorMG  float64 `json:"noise_floor_mg"`
	PeakPromMGMin float64 `json:"peak_prom_mg_min"`
}

// LayerResult is the parsed outcome for one layer.
type LayerResult struct {
	Layer       string
	Present     bool
	Accepted    bool
	Source      domain.Source
	Domain      any // typed domain payload (RawPoint list, etc.)
	Diagnostics []domain.Diagnostic
}

// Parse strictly decodes the bundle envelope so format errors are explicit.
func Parse(data []byte) (*Bundle, []domain.Diagnostic) {
	var b Bundle
	var diags []domain.Diagnostic
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, []domain.Diagnostic{{
			Code: "BUNDLE_JSON", Severity: domain.SevError,
			Message: fmt.Sprintf("bundle is not valid JSON: %v", err),
		}}
	}
	return &b, diags
}
