package store

import (
	"context"

	"tga-review/internal/domain"
)

// ImportBundle restores a project from an archive, inserting rows in dependency
// order. An existing project id is rejected so immutable history is never
// silently overwritten.
func (s *Store) ImportBundle(ctx context.Context, b *Bundle) error {
	if err := b.VerifyChecksums(); err != nil {
		return err
	}
	existing, err := s.GetProject(ctx, b.Project.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		return errf("project %s already exists; delete the local database before re-importing", b.Project.ID)
	}
	if err := s.CreateProject(ctx, domain.Project{
		ID: b.Project.ID, Name: b.Project.Name, Description: b.Project.Description, CreatedAt: b.Project.CreatedAt,
	}); err != nil {
		return err
	}
	for name, v := range b.Counters {
		if _, err := s.db.ExecContext(ctx, "INSERT INTO counters(project_id,name,value) VALUES(?,?,?)", b.Project.ID, name, v); err != nil {
			return err
		}
	}
	for _, src := range b.Sources {
		if err := s.InsertSource(ctx, src); err != nil {
			return err
		}
	}
	for _, ses := range b.Sessions {
		if err := s.InsertSession(ctx, ses); err != nil {
			return err
		}
	}
	for _, pr := range b.Params {
		var p domain.Params
		if err := jsonUnmarshal(pr.JSON, &p); err != nil {
			return err
		}
		if err := s.InsertParams(ctx, b.Project.ID, pr.Version, pr.FP, pr.ParentFP, p, pr.CreatedAt); err != nil {
			return err
		}
	}
	for _, e := range b.Events {
		if err := s.AppendEvent(ctx, domain.Event{
			ProjectID: b.Project.ID, Seq: e.Seq, ID: e.ID, Type: e.Type, Payload: e.Payload, CreatedAt: e.CreatedAt,
		}); err != nil {
			return err
		}
	}
	for _, n := range b.Nodes {
		if err := s.InsertNode(ctx, n, n.CreatedEventSeq); err != nil {
			return err
		}
	}
	for _, d := range b.Decisions {
		if err := s.InsertDecision(ctx, d); err != nil {
		return err
		}
	}
	for _, c := range b.Conflicts {
		if err := s.InsertConflict(ctx, c); err != nil {
			return err
		}
	}
	for _, a := range b.Archive {
		if err := s.InsertArchiveEntry(ctx, a); err != nil {
			return err
		}
	}
	return nil
}
