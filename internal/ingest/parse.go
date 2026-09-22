// Package ingest parses imported TGA source data. Two formats are accepted:
//   - JSON bundles (machine exchange format),
//   - a small, copy-paste friendly line DSL (see parse_text.go).
//
// The six source layers are parsed independently so that a failure in one
// layer ("import half-failure") never discards the others. Original textual
// representations are retained and normalisation happens here; values that
// cannot be converted stay in place with diagnostics.
package ingest

import (
	"encoding/json"
	"fmt"

	"tga-review/internal/domain"
)

// LayerStatus marks what happened while importing one layer.
const (
	StatusParsed = "parsed"
	StatusFailed = "failed"
	StatusAbsent = "absent"
)

// LayerResult is the outcome for one of the six source layers.
type LayerResult struct {
	Layer       string              `json:"layer"`
	Status      string              `json:"status"`
	Diagnostics []domain.Diagnostic `json:"diagnostics,omitempty"`
	// Payload is one of the typed *domain layer structs when Status==parsed.
	Payload any `json:"-"`
	// Fragment retains the raw submitted bytes/text for the layer, so a failed
	// import leaves an input fragment behind instead of vanishing.
	Fragment string `json:"fragment,omitempty"`
}

// Result is the outcome of parsing one submission.
type Result struct {
	Layers      []LayerResult       `json:"layers"`
	Diagnostics []domain.Diagnostic `json:"diagnostics,omitempty"`
}

// ParsedLayer returns the typed payload for a layer when present.
func (r *Result) ParsedLayer(name string) (any, bool) {
	for i := range r.Layers {
		if r.Layers[i].Layer == name && r.Layers[i].Status == StatusParsed {
			return r.Layers[i].Payload, true
		}
	}
	return nil, false
}

func diag(sev, code, msg, layer string, idx int, field string) domain.Diagnostic {
	return domain.Diagnostic{Severity: sev, Code: code, Message: msg, Layer: layer, Index: idx, Field: field}
}

// ParseJSON parses a JSON bundle. Unknown layers are rejected; malformed values
// are attached as diagnostics while the rest of the layer still parses.
func ParseJSON(data []byte) (*Result, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("payload is not a JSON object: %w", err)
	}
	res := &Result{}
	for _, name := range domain.Layers {
		body, ok := raw[name]
		if !ok {
			res.Layers = append(res.Layers, LayerResult{Layer: name, Status: StatusAbsent})
			continue
		}
		frag := string(body)
		lr := LayerResult{Layer: name, Fragment: frag}
		payload, diags, err := parseJSONLayer(name, body)
		lr.Diagnostics = append(lr.Diagnostics, diags...)
		if err != nil {
			lr.Status = StatusFailed
			lr.Diagnostics = append(lr.Diagnostics, diag(domain.SevError, "layer_json", err.Error(), name, 0, ""))
		} else {
			lr.Status = StatusParsed
			lr.Payload = payload
		}
		res.Layers = append(res.Layers, lr)
	}
	return res, nil
}
