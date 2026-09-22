// Package bundle defines the offline, TGA-specific exchange format and the
// validation rules for importing it. A bundle is either fully committed,
// partially committed (accepted layer fragments + rejected-fragment
// diagnostics retained), or fatally rejected before anything is written.
package bundle

import (
	"encoding/json"
	"fmt"

	"tgareview/internal/domain"
)

const FormatVersion = "tga.bundle/v1"

// Bundle is the offline exchange envelope.
type Bundle struct {
	FormatVersion string       `json:"format_version"`
	ProjectID     string       `json:"project_id,omitempty"`
	Name          string       `json:"name"`
	Description   string       `json:"description,omitempty"`
	Layers        []LayerInput `json:"layers"`
}

// LayerInput is one submitted layer revision. ParentRevision expresses the
// version chain; empty means the layer's first revision.
type LayerInput struct {
	Kind           domain.LayerKind `json:"kind"`
	ParentRevision string           `json:"parent_revision,omitempty"`
	SourceFile     string           `json:"source_file,omitempty"`
	Data           json.RawMessage  `json:"data"`
}

// FragmentDiagnostic records a rejected piece while keeping the import alive.
type FragmentDiagnostic struct {
	LayerKind domain.LayerKind `json:"layer_kind"`
	Row       *int             `json:"row,omitempty"`
	Field     string           `json:"field,omitempty"`
	Message   string           `json:"message"`
	Raw       string           `json:"raw,omitempty"`
}

// Result is the outcome of validating a bundle.
type Result struct {
	Bundle        *Bundle
	Accepted      []LayerInput
	Fatal         error
	Diagnostics   []FragmentDiagnostic
	RejectedKinds map[domain.LayerKind]bool
	// Partial is true when at least one fragment was accepted and at least
	// one fatal-layer/diagnostic failure occurred.
	Partial bool
}

// Parse decodes the envelope, distinguishing fatal format errors.
func Parse(raw json.RawMessage) (*Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, &domain.FatalFormatError{Msg: "bundle is not valid JSON: " + err.Error()}
	}
	if b.FormatVersion != FormatVersion {
		return nil, &domain.FatalFormatError{Msg: fmt.Sprintf("unsupported format_version %q (want %s)", b.FormatVersion, FormatVersion)}
	}
	if b.Name == "" {
		return nil, &domain.FatalFormatError{Msg: "bundle.name is required"}
	}
	if len(b.Layers) == 0 {
		return nil, &domain.FatalFormatError{Msg: "bundle.layers must contain at least one layer"}
	}
	seen := map[domain.LayerKind]bool{}
	for i := range b.Layers {
		k := b.Layers[i].Kind
		if !k.Valid() {
			return nil, &domain.FatalFormatError{Msg: fmt.Sprintf("layers[%d]: unknown layer kind %q", i, k)}
		}
		if seen[k] {
			return nil, &domain.FatalFormatError{Msg: fmt.Sprintf("layers[%d]: duplicate layer %q in one submission; submit revisions separately", i, k)}
		}
		seen[k] = true
		if len(b.Layers[i].Data) == 0 {
			return nil, &domain.FatalFormatError{Msg: fmt.Sprintf("layers[%d] %s: empty data", i, k)}
		}
	}
	return &b, nil
}

// Validate parses every layer, keeping accepted fragments and collecting
// per-row diagnostics. A layer whose structure is invalid becomes a rejected
// layer but does not abort the other layers (half-failure).
func Validate(b *Bundle) *Result {
	r := &Result{Bundle: b, RejectedKinds: map[domain.LayerKind]bool{}}
	for _, in := range b.Layers {
		l, err := domain.ParseLayer(in.Kind, in.SourceFile, in.Data)
		if err != nil {
			r.RejectedKinds[in.Kind] = true
			r.Diagnostics = append(r.Diagnostics, FragmentDiagnostic{
				LayerKind: in.Kind,
				Message:   err.Error(),
			})
			continue
		}
		for _, d := range l.Diagnostics {
			r.Diagnostics = append(r.Diagnostics, FragmentDiagnostic{LayerKind: in.Kind, Message: d})
		}
		// Collect field-level unconvertible diagnostics (points remain in place).
		switch in.Kind {
		case domain.SamplePoints:
			for _, p := range l.Samples {
				for fld, f := range map[string]domain.Field{"time_s": p.TimeS, "temp_c": p.TempC, "mass_mg": p.MassMg} {
					for _, d := range f.Diagnostics {
						row := p.Index
						r.Diagnostics = append(r.Diagnostics, FragmentDiagnostic{LayerKind: in.Kind, Row: &row, Field: fld, Message: d, Raw: f.Raw})
					}
				}
			}
		case domain.InitialMass:
			for _, d := range l.InitialMass.MassMg.Diagnostics {
				r.Diagnostics = append(r.Diagnostics, FragmentDiagnostic{LayerKind: in.Kind, Field: "mass_mg", Message: d, Raw: l.InitialMass.MassMg.Raw})
			}
		}
		r.Accepted = append(r.Accepted, in)
	}
	r.Partial = len(r.Accepted) > 0 && (len(r.RejectedKinds) > 0 || len(r.Diagnostics) > 0)
	return r
}
