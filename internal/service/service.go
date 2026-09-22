// Package service coordinates the event log, state machine and store. It is
// the only layer that commits events and assigns stable, content-derived ids.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"tgareview/internal/domain"
	"tgareview/internal/engine"
	"tgareview/internal/parse"
	"tgareview/internal/store"
)

// Service holds the database and a virtual clock.
type Service struct {
	st  *store.Store
	now engine.Clock
	mu  sync.Mutex
}

// New creates a service. If now is nil, wall clock is used (tests inject time).
func New(st *store.Store, now engine.Clock) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{st: st, now: now}
}

// CreateProject starts a new review project and records the seed.
func (s *Service) CreateProject(ctx context.Context, name, origin string, seed int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pid := domain.StableID("project", origin, name, fmt.Sprint(seed))[:16]
	pl := domain.ProjectCreatedPayload{Name: name, Origin: origin, Seed: seed}
	state := engine.NewState()
	state.ProjectID, state.Name, state.Origin, state.Seed = pid, name, origin, seed
	if _, err := s.commit(ctx, state, domain.EvProjectCreated, "", "", pl); err != nil {
		return "", err
	}
	return pid, nil
}

// commit builds, persists and returns an event from a typed payload. Multi-row
// imports call it repeatedly against one in-memory state so sequence numbers
// advance within a batch.
func (s *Service) commit(ctx context.Context, state *engine.State,
	typ domain.EventType, actor, parentID string, payload any,
) (domain.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Event{}, err
	}
	seq := state.Seq + 1
	id := domain.StableID("event", state.ProjectID, string(typ), fmt.Sprint(seq),
		string(raw))
	at := s.now()
	if fix, ok := payload.(eventIDFixer); ok {
		fix.FixEventID(id)
		if fp, ok := fix.(payloadCarrier); ok {
			raw, _ = json.Marshal(fp.PayloadValue())
		}
	}
	ev := domain.Event{
		ProjectID: state.ProjectID, Seq: seq, Type: typ, ParentID: parentID,
		At: at, Actor: actorOr(actor), EventID: id, Payload: raw,
	}
	next, err := engine.Apply(state, ev)
	if err != nil {
		return domain.Event{}, err
	}
	*state = *next
	stateJSON, viewJSON, err := s.serialize(state)
	if err != nil {
		return domain.Event{}, err
	}
	return s.st.AppendEvent(ctx, ev, stateJSON, viewJSON)
}

func (s *Service) serialize(state *engine.State) ([]byte, []byte, error) {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return nil, nil, err
	}
	viewJSON, err := json.Marshal(state.View)
	if err != nil {
		return nil, nil, err
	}
	return stateJSON, viewJSON, nil
}

func actorOr(a string) string {
	if a == "" {
		return "reviewer"
	}
	return a
}

// loadState returns the head materialized state of a project.
func (s *Service) loadState(ctx context.Context, pid string) (*engine.State, error) {
	head, err := s.st.HeadSeq(ctx, pid)
	if err != nil {
		return nil, err
	}
	if head == 0 {
		return nil, fmt.Errorf("project %s: %w", pid, store.ErrNotFound)
	}
	data, err := s.st.StateAt(ctx, pid, head)
	if err != nil {
		return nil, err
	}
	var st engine.State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

var _ = parse.Format

// eventIDFixer lets a payload receive its own stable event id before commit.
type eventIDFixer interface {
	FixEventID(string)
}

type payloadCarrier interface{ PayloadValue() any }

// decisionWrap carries a DecisionPayload and stamps its own event id.
type decisionWrap struct{ d *domain.DecisionPayload }

func (w decisionWrap) FixEventID(id string) { w.d.EventID = id }
func (w decisionWrap) PayloadValue() any    { return *w.d }
