package project

import (
	"encoding/json"

	"tgareview/internal/bundle"
	"tgareview/internal/domain"
)

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func layerIn(kind, parent string, data json.RawMessage) bundle.LayerInput {
	return bundle.LayerInput{Kind: domain.LayerKind(kind), ParentRevision: parent, Data: data}
}
func bundleBundle(name string, layers ...bundle.LayerInput) *bundle.Bundle {
	return &bundle.Bundle{FormatVersion: bundle.FormatVersion, Name: name, Layers: layers}
}
