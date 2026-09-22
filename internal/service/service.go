// Package service orchestrates imports, versioned raw/parameter layers,
// derivation, stale propagation, human decisions/conflicts/undo and replay.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"tga/internal/derive"
	"tga/internal/domain"
	"tga/internal/store"
)

// Service is the application facade.
type Service struct {
	st  *store.Store
	now func() time.Time
}

// New constructs a service. The clock is injectable for virtual time in tests.
func New(st *store.Store, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{st: st, now: now}
}

func (s *Service) clock() store.Clock {
	return func() string { return s.now().UTC().Format(time.RFC3339Nano) }
}

// CreateProject creates a project and its genesis event.
func (s *Service) CreateProject(ctx context.Context, name, slug string) (*domain.Project, error) {
	if slug == "" {
		slug = slugify(name)
	}
	ts := s.clock()
	sum := sha256.Sum256([]byte(slug + ":" + ts))
	id := "tga:prj:" + hex.EncodeToString(sum[:])[:16]
	p := domain.Project{ID: id, Name: name, Slug: slug, CreatedTS: ts}
	if err := s.st.Create(ctx, p); err != nil {
		return nil, err
	}
	if _, err := s.st.Event(ctx, id, "project_created", map[string]any{"name": name, "slug": slug}, nil, s.clock()); err != nil {
		return nil, err
	}
	return &p, nil
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	repl := strings.NewReplacer(" ", "-", "/", "-", ":", "-", "_", "-")
	s = repl.Replace(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	return strings.Trim(out, "-")
}

// LayerRequest is one submitted input layer (raw bytes are stored verbatim).
type LayerRequest struct {
	Kind    domain.SourceKind
	Payload json.RawMessage
}

// ImportLayers commits the six stratified layers independently. A malformed
// layer is quarantined as a partial-failure fragment while other layers still
// commit (import half-failure), exactly as required.
func (s *Service) ImportLayers(ctx context.Context, projectID, seed string, layers []LayerRequest) (*domain.ImportReport, error) {
	if _, err := s.st.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	rep := &domain.ImportReport{Accepted: map[string]int{}, ProjectID: projectID}
	importRef := domain.ImportRef(projectID+":"+seed, len(seed))
	rep.ImportRef = importRef
	for _, lr := range layers {
		if err := validateLayer(lr); err != nil {
			rep.Failed = append(rep.Failed, domain.ImportFailure{
				Kind: lr.Kind, Fragment: string(lr.Payload), Code: "FORMAT_ERROR",
				Message: err.Error(), Status: "quarantined",
			})
			rep.Partial = true
			continue
		}
		digest := domain.DigestOf(json.RawMessage(canonical(lr.Payload)))
		existing, _ := s.st.RawsByKind(ctx, projectID, lr.Kind)
		version := len(existing) + 1
		var parent string
		if version > 1 {
			parent = existing[version-2].ID
		}
		id := domain.RawID(projectID, lr.Kind, version, digest)
		sub := domain.RawSubmission{
			ID: id, ProjectID: projectID, Kind: lr.Kind, Version: version,
			ParentID: parent, Payload: json.RawMessage(canonical(lr.Payload)),
			Digest: digest, ImportRef: importRef,
		}
		if _, _, err := s.st.CommitRaw(ctx, sub, s.clock()); err != nil {
			return nil, err
		}
		rep.Accepted[string(lr.Kind)] = 1
	}
	if err := s.st.RecordImport(ctx, projectID, *rep, 0, s.clock()); err != nil {
		return nil, err
	}
	if err := s.invalidateDerived(ctx, projectID); err != nil {
		return nil, err
	}
	return rep, nil
}

func canonical(b []byte) []byte {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return b
	}
	out, err := json.Marshal(v)
	if err != nil {
		return b
	}
	return out
}

func validateLayer(lr LayerRequest) error {
	if !knownKind(lr.Kind) {
		return fmt.Errorf("unknown source kind %q", lr.Kind)
	}
	var v any
	if err := json.Unmarshal(lr.Payload, &v); err != nil {
		return fmt.Errorf("invalid JSON for %s: %w", lr.Kind, err)
	}
	switch lr.Kind {
	case domain.KindSamples:
		var pts []domain.SamplePoint
		if err := json.Unmarshal(lr.Payload, &pts); err != nil {
			return err
		}
		if len(pts) < 2 {
			return fmt.Errorf("samples require at least 2 points")
		}
	case domain.KindDrift:
		var pts []domain.DriftPoint
		if err := json.Unmarshal(lr.Payload, &pts); err != nil {
			return err
		}
	case domain.KindProgram:
		var pts []domain.ProgramSegment
		if err := json.Unmarshal(lr.Payload, &pts); err != nil {
			return err
		}
		if len(pts) == 0 {
			return fmt.Errorf("furnace program requires at least 1 segment")
		}
	case domain.KindGasEvent:
		var pts []domain.GasValveEvent
		if err := json.Unmarshal(lr.Payload, &pts); err != nil {
			return err
		}
	case domain.KindBlank:
		var pts []domain.BlankPoint
		if err := json.Unmarshal(lr.Payload, &pts); err != nil {
			return err
		}
	case domain.KindInitialMass:
		var im domain.InitialMass
		if err := json.Unmarshal(lr.Payload, &im); err != nil {
			return err
		}
		if im.Value.Raw == "" {
			return fmt.Errorf("initial mass requires raw value")
		}
	}
	return nil
}

func knownKind(k domain.SourceKind) bool {
	switch k {
	case domain.KindSamples, domain.KindDrift, domain.KindProgram, domain.KindGasEvent, domain.KindBlank, domain.KindInitialMass:
		return true
	}
	return false
}

var _ = derive.AlgorithmVersion
