package ingest

import (
	"fmt"
	"strings"

	"tga-archive/internal/domain"
)

// ParseLayer parses one layer fragment. It ALWAYS returns a payload (accepted
// or not): a half-failed import keeps raw text, diagnostics and layer so the
// event history records exactly what happened.
func ParseLayer(layer, label, rawText string, parentSourceID string) domain.SourcePayload {
	p := domain.SourcePayload{Layer: layer, SourceID: "", Label: label, RawText: rawText, ParentSourceID: parentSourceID}
	p.SourceID = domain.HashID("src", layer, rawText)
	switch layer {
	case domain.LayerSamples:
		p.Samples, p.Gaps, p.Diagnostics, p.Accepted = ParseSamples(rawText)
	case domain.LayerDrift:
		p.Drift, p.Diagnostics, p.Accepted = ParseDrift(rawText)
	case domain.LayerFurnace:
		p.Furnace, p.Diagnostics, p.Accepted = ParseFurnace(rawText)
	case domain.LayerGas:
		p.Valves, p.Diagnostics, p.Accepted = ParseGas(rawText)
	case domain.LayerBlank:
		p.Blank, p.Diagnostics, p.Accepted = ParseBlank(rawText)
	case domain.LayerInitialMass:
		p.InitialMass, p.Diagnostics, p.Accepted = ParseInitialMass(rawText)
	default:
		p.Accepted = false
		p.Diagnostics = []domain.Diagnostic{{Severity: domain.SevError, Code: "unknown_layer", Layer: layer, Message: "no parser for layer " + layer}}
	}
	for i := range p.Diagnostics {
		if p.Diagnostics[i].Layer == "" {
			p.Diagnostics[i].Layer = layer
		}
	}
	return p
}

// ParseGas is an alias kept for naming symmetry.
func ParseGas(text string) ([]domain.ValveEvent, []domain.Diagnostic, bool) {
	return ParseValves(text)
}

// ImportBundle parses a multi-section document. Recognized sections are parsed
// independently; one bad section never aborts the others (half-failure).
// Unknown sections are preserved as rejected fragments. Stray content outside
// sections is reported as a diagnostic.
func ImportBundle(text string) []domain.SourcePayload {
	sections := SplitSections(text)
	var out []domain.SourcePayload
	for _, sec := range sections {
		body := strings.TrimSpace(sec.Body)
		if body == "" {
			out = append(out, emptyPayload(sec.Name))
			continue
		}
		known := isKnownLayer(sec.Name)
		p := ParseLayer(sec.Name, sec.Name, body, "")
		if !known {
			p.Accepted = false
		}
		out = append(out, p)
	}
	return out
}

func emptyPayload(layer string) domain.SourcePayload {
	return domain.SourcePayload{
		Layer:    layer,
		SourceID: domain.HashID("src", layer, ""),
		Label:    layer,
		RawText:  "",
		Accepted: false,
		Diagnostics: []domain.Diagnostic{{
			Severity: domain.SevError,
			Code:     "empty_section",
			Layer:    layer,
			Message:  fmt.Sprintf("section %s is empty", layer),
		}},
	}
}

func isKnownLayer(layer string) bool {
	switch layer {
	case domain.LayerSamples, domain.LayerDrift, domain.LayerFurnace, domain.LayerGas, domain.LayerBlank, domain.LayerInitialMass:
		return true
	}
	return false
}

// FormatError describes a single malformed fragment in machine-readable form.
type FormatError struct {
	Code     string `json:"code"`
	Layer    string `json:"layer"`
	Fragment string `json:"fragment"`
	Message  string `json:"message"`
}

// DiagnosticsToErrors maps diagnostics for API responses.
func DiagnosticsToErrors(ds []domain.Diagnostic) []FormatError {
	var fe []FormatError
	for _, d := range ds {
		fe = append(fe, FormatError{Code: d.Code, Layer: d.Layer, Fragment: d.Fragment, Message: d.Message})
	}
	return fe
}
