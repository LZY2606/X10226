package service

import (
	"context"
	"encoding/json"
	"fmt"

	"tgareview/internal/domain"
	"tgareview/internal/engine"
	"tgareview/internal/store"
)

// ExportProject returns the portable archive of a project.
func (s *Service) ExportProject(ctx context.Context, pid string) (*store.ExportedProject, error) {
	return s.st.Export(ctx, pid)
}

// DeleteProject removes a project (used after export in the acceptance flow).
func (s *Service) DeleteProject(ctx context.Context, pid string) error {
	return s.st.DeleteProject(ctx, pid)
}

// ImportArchive imports an archive verbatim and rebuilds every snapshot by
// replaying the log through the state machine, so identity, order, provenance
// and time-travel views survive export -> delete -> import.
func (s *Service) ImportArchive(ctx context.Context, a *store.ExportedProject) (string, error) {
	if a.Format != "tga-review-archive-1" {
		return "", fmt.Errorf("unsupported archive format %q", a.Format)
	}
	if err := s.st.CreateProject(ctx, a.ProjectID, a.Name, a.Origin, a.Seed, s.now()); err != nil {
		return "", err
	}
	state := engine.NewState()
	for _, ev := range a.Events {
		var pc domain.ProjectCreatedPayload
		if ev.Type == domain.EvProjectCreated {
			_ = json.Unmarshal(ev.Payload, &pc)
			state.ProjectID = a.ProjectID
			state.Name, state.Origin, state.Seed = a.Name, a.Origin, a.Seed
		}
		next, err := engine.Apply(state, ev)
		if err != nil {
			return "", fmt.Errorf("replay event %d: %w", ev.Seq, err)
		}
		state = next
		stateJSON, viewJSON, err := s.serialize(state)
		if err != nil {
			return "", err
		}
		if err := s.insertReplayed(ctx, ev, stateJSON, viewJSON); err != nil {
			return "", err
		}
	}
	return a.ProjectID, nil
}

func (s *Service) insertReplayed(ctx context.Context, ev domain.Event, stateJSON, viewJSON []byte) error {
	_, err := s.st.AppendEvent(ctx, ev, stateJSON, viewJSON)
	return err
}
