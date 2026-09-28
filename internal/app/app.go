// Package app orchestrates the event-sourced TGA review workflow: importing
// the six immutable raw layers, running versioned derivations, recording human
// decisions/conflicts/undo, and serving both current and historical views.
package app

import (
	"context"
	"encoding/json"

	"tgareview/internal/domain"
	"tgareview/internal/store"
)

// App is the application service.
type App struct {
	St *store.Store
}

// New constructs an App over a store.
func New(st *store.Store) *App { return &App{St: st} }

// CreateProject creates a project with genesis event and default parameters.
func (a *App) CreateProject(ctx context.Context, id, name string) (*domain.Project, error) {
	now := a.St.Clock().Now()
	p := &domain.Project{ID: id, Name: name, CreatedAt: now}
	if err := a.St.CreateProject(ctx, *p); err != nil {
		return nil, err
	}
	tx, err := a.St.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ev, err := a.St.AppendEventTx(ctx, tx, id, domain.EvParamsRevised, "system",
		"初始参数 v1（算法 "+domain.AlgoVersion+"）", map[string]any{
			"version": 1, "params": domain.DefaultParams(), "algo": domain.AlgoVersion,
		})
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(domain.DefaultParams())
	if err := a.St.SetKV(ctx, tx, id, kvParams, string(b), ev.Seq); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

const kvParams = "params"

// CurrentParams returns the effective parameter set, reconstructing from the
// journal when the KV projection is absent (e.g. freshly imported bundle).
func (a *App) CurrentParams(ctx context.Context, projectID string) (domain.Params, error) {
	if v, _, err := a.St.GetKV(ctx, projectID, kvParams); err == nil {
		var p domain.Params
		if err := json.Unmarshal([]byte(v), &p); err == nil {
			return p, nil
		}
	}
	return paramsAtSeq(ctx, a.St, projectID, 1<<62)
}
