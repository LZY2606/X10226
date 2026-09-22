package service

import (
	"context"
	"encoding/json"
	"time"

	"tgareview/internal/domain"
	"tgareview/internal/engine"
	"tgareview/internal/store"
)

// ProjectView bundles head state, view and timeline for the review page.
type ProjectView struct {
	Meta      store.ProjectMeta             `json:"meta"`
	View      *domain.DerivedView           `json:"view"`
	Events    []TimelineEvent               `json:"events"`
	Conflicts []domain.Conflict             `json:"conflicts"`
	Params    domain.Params                 `json:"params"`
	Initial   *domain.WeightRow             `json:"initial_weight,omitempty"`
	Batches   []domain.BatchImportedPayload `json:"batches"`
}

// TimelineEvent decorates an event with a payload summary for the UI.
type TimelineEvent struct {
	Seq     int64          `json:"seq"`
	EventID string         `json:"event_id"`
	Type    string         `json:"type"`
	Actor   string         `json:"actor"`
	At      time.Time      `json:"at"`
	Undone  bool           `json:"undone"`
	Payload map[string]any `json:"payload"`
}

// ListProjects returns project headers.
func (s *Service) ListProjects(ctx context.Context) ([]store.ProjectMeta, error) {
	return s.st.ListProjects(ctx)
}

// HeadView returns the complete head review bundle.
func (s *Service) HeadView(ctx context.Context, pid string) (*ProjectView, error) {
	head, err := s.st.HeadSeq(ctx, pid)
	if err != nil {
		return nil, err
	}
	return s.ViewAt(ctx, pid, head)
}

// ViewAt returns the bundle as it was at an arbitrary event. Old events retain
// their dependency versions and visible conclusions (not the newest compute).
func (s *Service) ViewAt(ctx context.Context, pid string, seq int64) (*ProjectView, error) {
	head, err := s.st.HeadSeq(ctx, pid)
	if err != nil {
		return nil, err
	}
	if seq <= 0 || seq > head {
		seq = head
	}
	stateData, err := s.st.StateAt(ctx, pid, seq)
	if err != nil {
		return nil, err
	}
	var st engine.State
	if err := json.Unmarshal(stateData, &st); err != nil {
		return nil, err
	}
	metas, err := s.st.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	var meta store.ProjectMeta
	for _, m := range metas {
		if m.ID == pid {
			meta = m
		}
	}
	tl := make([]TimelineEvent, 0, len(st.Events))
	undone := map[string]bool{}
	for _, ev := range st.Events {
		if ev.Type == domain.EvUndo {
			var u domain.UndoPayload
			_ = json.Unmarshal(ev.Payload, &u)
			undone[u.UndoneEventID] = true
		}
	}
	for _, ev := range st.Events {
		var pl map[string]any
		_ = json.Unmarshal(ev.Payload, &pl)
		tl = append(tl, TimelineEvent{
			Seq: ev.Seq, EventID: ev.EventID, Type: string(ev.Type),
			Actor: ev.Actor, At: ev.At, Undone: undone[ev.EventID], Payload: pl,
		})
	}
	var confs []domain.Conflict
	for _, c := range st.Conflicts {
		confs = append(confs, c)
	}
	pv := &ProjectView{
		Meta: meta, View: st.View, Events: tl, Conflicts: confs,
		Params: st.Params, Batches: st.Batches,
	}
	for i := range st.Weights {
		if st.Weights[i].Active {
			w := st.Weights[i]
			pv.Initial = &w
		}
	}
	return pv, nil
}

// RawEvents returns the immutable event log (used by casepack narrative undo).
func (s *Service) RawEvents(ctx context.Context, pid string) ([]domain.Event, error) {
	return s.st.Events(ctx, pid)
}
