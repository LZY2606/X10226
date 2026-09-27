// Package ingest parses and validates the six strata of a TGA import.
//
// Original representations are always preserved. The parser separates clean
// layers from rejected layers ("half failure"): a payload that contains some
// malformed layers still commits the clean ones and records diagnostics for
// the rest instead of failing the whole transaction.
package ingest

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"tga-review/internal/domain"
)

// Payload is the on-the-wire import document. Each stratum is independent.
type Payload struct {
	Schema      string                               `json:"schema"`
	Version     string                               `json:"payload_version,omitempty"`
	Samples     []domain.SamplePoint                 `json:"samples,omitempty"`
	Drift       *domain.DriftModel                   `json:"drift,omitempty"`
	Furnace     []domain.FurnaceSegment              `json:"furnace,omitempty"`
	Gas         []domain.GasEvent                    `json:"gas,omitempty"`
	Blank       []domain.BlankPoint                  `json:"blank,omitempty"`
	InitialMass *domain.InitialMassStatement         `json:"initial_mass,omitempty"`
}

// LayerResult reports what happened to one stratum.
type LayerResult struct {
	Layer      string          `json:"layer"`
	Accepted   bool            `json:"accepted"`
	RecordCount int            `json:"record_count"`
	Normalised json.RawMessage `json:"normalised,omitempty"`
	Diagnostics []string       `json:"diagnostics"`
}

// Report is the outcome of parsing one payload.
type Report struct {
	SchemaOK bool          `json:"schema_ok"`
	Layers   []LayerResult `json:"layers"`
	Errors   []string      `json:"errors"`
}

// HasAcceptedLayer reports partial success.
func (r *Report) HasAcceptedLayer() bool {
	for _, l := range r.Layers {
		if l.Accepted {
			return true
		}
	}
	return false
}

// AcceptedLayers returns the accepted layer names in canonical order.
func (r *Report) AcceptedLayers() []string {
	var out []string
	for _, l := range r.Layers {
		if l.Accepted {
			out = append(out, l.Layer)
		}
	}
	return out
}

// Parse decodes raw JSON and validates every layer independently.
// Raw representations on every Quantity survive validation regardless.
func Parse(raw []byte) (*Payload, *Report) {
	rep := &Report{Layers: []LayerResult{}}
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		rep.Errors = append(rep.Errors, fmt.Sprintf("invalid json: %v", err))
		return nil, rep
	}
	if p.Schema != "tga-import/1" {
		rep.Errors = append(rep.Errors, "missing or unsupported schema: expected \"tga-import/1\"")
	}
	rep.SchemaOK = len(rep.Errors) == 0

	check := func(layer string, ok bool, n int, norm any, diags []string) {
		lr := LayerResult{Layer: layer, Accepted: ok && len(diags) == 0, RecordCount: n, Diagnostics: diags}
		if lr.Accepted && norm != nil {
			b, _ := json.Marshal(norm)
			lr.Normalised = b
		}
		rep.Layers = append(rep.Layers, lr)
	}

	if len(p.Samples) > 0 {
		n, diags := normaliseSamples(p.Samples)
		check(domain.LayerSamples, n > 0, n, p.Samples, diags)
	}
	if p.Drift != nil {
		diags := normaliseDrift(p.Drift)
		check(domain.LayerDrift, true, 1, p.Drift, diags)
	}
	if len(p.Furnace) > 0 {
		n, diags := normaliseFurnace(p.Furnace)
		check(domain.LayerFurnace, n > 0, n, p.Furnace, diags)
	}
	if len(p.Gas) > 0 {
		n, diags := normaliseGas(p.Gas)
		check(domain.LayerGas, n > 0, n, p.Gas, diags)
	}
	if len(p.Blank) > 0 {
		n, diags := normaliseBlank(p.Blank)
		check(domain.LayerBlank, n > 0, n, p.Blank, diags)
	}
	if p.InitialMass != nil {
		diags := normaliseInitial(p.InitialMass)
		check(domain.LayerInitialMass, true, 1, p.InitialMass, diags)
	}
	sort.SliceStable(rep.Layers, func(i, j int) bool {
		return layerOrder(rep.Layers[i].Layer) < layerOrder(rep.Layers[j].Layer)
	})
	return &p, rep
}

func layerOrder(l string) int {
	for i, x := range domain.AllLayers() {
		if x == l {
			return i
		}
	}
	return 99
}
