package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"tga-lineage/internal/domain"
	"tga-lineage/internal/store"
)

// BundleFormat is the offline export envelope.
const BundleFormat = "tga-lineage/bundle/v1"

// Bundle is a self-contained project archive.
type Bundle struct {
	Format    string         `json:"format"`
	ExportedAt string        `json:"exported_at"`
	Projects  []BundleProject `json:"projects"`
}

type BundleProject struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	CreatedAt string         `json:"created_at"`
	Events    []domain.Event `json:"events"`
}

// ExportBundle writes every project with its complete event log (ordered ids
// preserved), so identities, ordering and parent relationships survive a
// delete-and-reimport cycle.
func (s *Service) ExportBundle(ctx context.Context, w io.Writer) error {
	projects, events, err := s.Store.Snapshot(ctx)
	if err != nil {
		return err
	}
	b := Bundle{Format: BundleFormat, ExportedAt: s.Clock.Now().Format(time.RFC3339Nano)}
	byProject := map[string]*BundleProject{}
	for _, pr := range projects {
		bp := &BundleProject{ID: pr.ID, Name: pr.Name, CreatedAt: pr.CreatedAt}
		byProject[pr.ID] = bp
		b.Projects = append(b.Projects, *bp)
	}
	for _, e := range events {
		bp := byProject[e.ProjectID]
		bp.Events = append(bp.Events, e)
	}
	// refill projects with populated events
	for i := range b.Projects {
		b.Projects[i] = *byProject[b.Projects[i].ID]
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(b)
}

// ImportBundle restores projects. Existing project ids are rejected so an
// export→delete→import reproduces the identical log.
func (s *Service) ImportBundle(ctx context.Context, r io.Reader) ([]string, error) {
	var b Bundle
	if err := json.NewDecoder(r).Decode(&b); err != nil {
		return nil, fmt.Errorf("invalid bundle: %w", err)
	}
	if b.Format != BundleFormat {
		return nil, fmt.Errorf("unsupported bundle format %q", b.Format)
	}
	var restored []string
	for _, bp := range b.Projects {
		row := store.ProjectRow{ID: bp.ID, Name: bp.Name, CreatedAt: bp.CreatedAt}
		if err := s.Store.CreateProject(ctx, row); err != nil {
			return restored, err
		}
		for _, e := range bp.Events {
			e.ProjectID = bp.ID
			if err := s.Store.AppendEvent(ctx, e); err != nil {
				return restored, err
			}
		}
		restored = append(restored, bp.ID)
	}
	return restored, nil
}

// ListProjects returns the project directory.
func (s *Service) ListProjects(ctx context.Context) ([]store.ProjectRow, error) {
	return s.Store.ListProjects(ctx)
}


var _ = store.ProjectRow{}
