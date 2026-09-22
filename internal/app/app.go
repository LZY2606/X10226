// Package app orchestrates the use cases: it validates imports, appends
// immutable events, runs the versioned engine and handles decisions, conflict
// resolution and undo against a replayable store.
package app

import (
	"context"
	"encoding/json"
	"fmt"

	"tga-review/internal/clock"
	"tga-review/internal/domain"
	"tga-review/internal/engine"
	"tga-review/internal/ident"
	"tga-review/internal/importer"
	"tga-review/internal/store"
)

type Service struct {
	Store *store.Store
	Clock clock.Clock
}

func New(st *store.Store, c clock.Clock) *Service {
	if c == nil {
		c = clock.Wall{}
	}
	return &Service{Store: st, Clock: c}
}

func (s *Service) nowISO() string { return s.Clock.Now().Format("2006-01-02T15:04:05.000000000Z07:00") }

func (s *Service) appendEvent(ctx context.Context, projectID, typ, reviewer string, payload any, undoes, conflictOf string) (domain.Event, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return domain.Event{}, err
	}
	seq, err := s.Store.NextSeq(ctx, projectID)
	if err != nil {
		return domain.Event{}, err
	}
	ev := domain.Event{
		ID: ident.EventID(projectID, seq), Seq: seq, ProjectID: projectID,
		Type: typ, OccurredAt: s.nowISO(), Reviewer: reviewer,
		Undoes: undoes, ConflictOf: conflictOf, Payload: b,
	}
	return ev, s.Store.AppendEvent(ctx, ev)
}

// CreateProject creates a project with stable id derived from its name and
// emits the project_created event.
func (s *Service) CreateProject(ctx context.Context, name string) (string, error) {
	id := ident.ProjectID(name)
	exists, err := s.Store.ProjectExists(ctx, id)
	if err != nil {
		return "", err
	}
	if exists {
		return id, nil
	}
	if err := s.Store.CreateProject(ctx, id, name, s.nowISO()); err != nil {
		return "", err
	}
	if _, err := s.appendEvent(ctx, id, domain.EvtProjectCreated, "system", map[string]string{"name": name}, "", ""); err != nil {
		return "", err
	}
	return id, nil
}

// ImportResult is returned to callers after a submission.
type ImportResult struct {
	Batch    domain.Batch
	Parsed   *importer.Result
	Accepted []string
	Rejected []domain.RejectedLayer
}

// Import parses and persists one submission, recording the raw bytes and the
// accepted/rejected layer decision as a batch_imported event.
func (s *Service) Import(ctx context.Context, projectID, sourceRef string, payload []byte) (*ImportResult, error) {
	exists, err := s.Store.ProjectExists(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("unknown project %s", projectID)
	}
	parsed, perr := importer.Parse(projectID, sourceRef, payload)
	if perr != nil {
		return nil, perr
	}
	bid := ident.BatchID(projectID, sourceRef, payload)
	b := domain.Batch{
		ID: bid, CreatedAt: s.nowISO(), Kind: "layer", SourceRef: sourceRef,
		AcceptedLayers: parsed.AcceptedLayers, RejectedLayers: parsed.Rejected,
		Payload: payload,
	}
	if len(parsed.Rejected) > 0 {
		b.Note = "half-failure: rejected layer(s) retained as diagnostics; siblings accepted"
	}
	if err := s.Store.InsertBatch(ctx, projectID, b, parsed); err != nil {
		return nil, err
	}
	evPayload, _ := json.Marshal(map[string]any{
		"batch_id":        b.ID,
		"source_ref":      sourceRef,
		"accepted_layers": b.AcceptedLayers,
		"rejected_layers": b.RejectedLayers,
	})
	_ = evPayload
	if _, err := s.appendEvent(ctx, projectID, domain.EvtBatchImported, "importer",
		json.RawMessage(evPayload), "", ""); err != nil {
		return nil, err
	}
	return &ImportResult{Batch: b, Parsed: parsed, Accepted: b.AcceptedLayers, Rejected: b.RejectedLayers}, nil
}

// ReviseParams stores a new parameter version linked to its parent fingerprint.
func (s *Service) ReviseParams(ctx context.Context, projectID, reviewer, reason string, p domain.Params) (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	st, err := s.Store.Replay(ctx, projectID, 0)
	if err != nil {
		return "", err
	}
	payload := domain.ParamsRevisedPayload{
		ParentFingerprint: ident.Fingerprint(st.Params),
		NewFingerprint:    ident.Fingerprint(p),
		Params:            p,
		Reason:            reason,
	}
	ev, err := s.appendEvent(ctx, projectID, domain.EvtParamsRevised, reviewer, payload, "", "")
	return ev.ID, err
}

// RunPipeline produces a new derived run from current raw layers and params.
// The full derived set is embedded in the event, so replaying an old event
// shows exactly what that run concluded.
func (s *Service) RunPipeline(ctx context.Context, projectID, reviewer string) (domain.DerivedSet, error) {
	st, err := s.Store.Replay(ctx, projectID, 0)
	if err != nil {
		return domain.DerivedSet{}, err
	}
	layers, err := s.Store.Layers(ctx, projectID)
	if err != nil {
		return domain.DerivedSet{}, err
	}
	runNo := len(st.Runs) + 1
	runID := ident.RunID(projectID, runNo)
	eng := engine.New(projectID)
	var parents []string
	if st.CurrentRun != nil {
		parents = append(parents, st.CurrentRun.RunID)
	}
	set, err := eng.Run(engine.Inputs{
		Samples:    layers.Samples,
		Drift:      layers.Drift,
		Program:    layers.Program,
		Gas:        layers.Gas,
		Blank:      layers.Blank,
		Initial:    layers.Initial,
		Params:     st.Params,
		RunID:      runID,
		ParentRuns: parents,
	})
	if err != nil {
		return domain.DerivedSet{}, err
	}
	if _, err := s.appendEvent(ctx, projectID, domain.EvtRunProduced, reviewer,
		domain.RunProducedPayload{Derived: set}, "", ""); err != nil {
		return domain.DerivedSet{}, err
	}
	return set, nil
}
