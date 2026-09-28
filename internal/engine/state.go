package engine

import (
	"fmt"
	"sort"

	"tga/internal/domain"
)

// currentSources is the set of head source versions feeding a recompute.
type currentSources struct {
	samples  *domain.Source
	drift    *domain.Source
	program  *domain.Source
	gas      *domain.Source
	blank    *domain.Source
	initMass *domain.Source
	params   domain.Params
}

// Engine is the in-memory, event-sourced aggregate. Store persists events; the
// Engine rebuilds this state by replaying them.
type Engine struct {
	Project     *domain.Project
	events      []*domain.Event
	sources     map[string]*domain.Source             // all versions by id
	heads       map[domain.SourceType]*domain.Source  // current head per type
	derivations map[string]*domain.Derivation         // all derived versions
	headDeriv   map[domain.DerivationKind]*domain.Derivation
	decisions   map[string]*domain.Decision
	judgements  []*domain.Judgement
	clock       Clock
}

func NewEngine(clock Clock) *Engine {
	return &Engine{
		sources:     map[string]*domain.Source{},
		heads:       map[domain.SourceType]*domain.Source{},
		derivations: map[string]*domain.Derivation{},
		headDeriv:   map[domain.DerivationKind]*domain.Derivation{},
		decisions:   map[string]*domain.Decision{},
		clock:       clock,
	}
}

func (e *Engine) Events() []*domain.Event          { return e.events }
func (e *Engine) Sources() map[string]*domain.Source { return e.sources }
func (e *Engine) Heads() map[domain.SourceType]*domain.Source { return e.heads }
func (e *Engine) Derivations() map[string]*domain.Derivation { return e.derivations }
func (e *Engine) HeadDeriv() map[domain.DerivationKind]*domain.Derivation { return e.headDeriv }
func (e *Engine) Decisions() map[string]*domain.Decision { return e.decisions }
func (e *Engine) Judgements() []*domain.Judgement { return e.judgements }

func (e *Engine) cur() currentSources {
	c := currentSources{params: domain.DefaultParams()}
	if s, ok := e.heads[domain.SourceParams]; ok && s.Params != nil {
		c.params = *s.Params
	}
	c.samples = e.heads[domain.SourceSamples]
	c.drift = e.heads[domain.SourceDrift]
	c.program = e.heads[domain.SourceProgram]
	c.gas = e.heads[domain.SourceGas]
	c.blank = e.heads[domain.SourceBlank]
	c.initMass = e.heads[domain.SourceInitMass]
	return c
}

// buildEvent constructs an event without applying it, so callers can fully
// populate type-specific payloads before commit. commit() is the only place an
// event is applied/appended, which keeps live and replay paths identical.
func (e *Engine) buildEvent(projectID string, typ domain.EventType, payload map[string]any, refs []string) *domain.Event {
	seq := int64(len(e.events) + 1)
	now := e.clock.Now()
	return &domain.Event{
		Seq: seq, ProjectID: projectID, Type: typ,
		Clock: now.Format("2006-01-02T15:04:05.000000Z07:00"), VClock: now.UnixNano(),
		Actor: "reviewer", Payload: payload, Refs: refs,
	}
}

func (e *Engine) next(typ domain.EventType, payload map[string]any, refs []string) *domain.Event {
	return e.commit(e.buildEvent(e.Project.ID, typ, payload, refs))
}

func (e *Engine) nextPID(projectID string, typ domain.EventType, payload map[string]any, refs []string) *domain.Event {
	return e.commit(e.buildEvent(projectID, typ, payload, refs))
}

func (e *Engine) commit(ev *domain.Event) *domain.Event {
	e.apply(ev)
	e.events = append(e.events, ev)
	if e.Project != nil {
		e.Project.HeadVClock = ev.VClock
	}
	return ev
}

// apply is the single reducer. Replay uses the exact same path, which is what
// makes time travel return the dependencies and conclusions as they were.
func (e *Engine) apply(ev *domain.Event) {
	switch ev.Type {
	case domain.EvProjectCreated:
		name, _ := ev.Payload["name"].(string)
		e.Project = &domain.Project{ID: ev.ProjectID, Name: name, CreatedVClock: ev.VClock, HeadVClock: ev.VClock}
	case domain.EvSourceImported:
		if src := sourceFromPayload(ev); src != nil {
			e.sources[src.ID] = src
			if prev := e.heads[src.Type]; prev == nil || src.Version > prev.Version {
				if prev != nil {
					// nothing to expire here: derived expiry is computed in recompute
				}
				e.heads[src.Type] = src
			}
		}
	case domain.EvParamRevised:
		if src := sourceFromPayload(ev); src != nil && src.Params != nil {
			e.sources[src.ID] = src
			e.heads[domain.SourceParams] = src
		}
	case domain.EvDerived:
		if ds := derivationsFromPayload(ev); ds != nil {
			for _, d := range ds {
				if old := e.headDeriv[d.Kind]; old != nil && old.ID != d.ID {
					old.Superseded = true
				}
				e.derivations[d.ID] = d
				e.headDeriv[d.Kind] = d
			}
		}
	case domain.EvJudgement:
		var js []*domain.Judgement
		if raw, ok := ev.Payload["judgements"]; ok {
			js = judgementsFromAny(raw, ev.Seq)
		}
		e.judgements = append(e.judgements, js...)
	case domain.EvDecision:
		if d := decisionFromPayload(ev); d != nil {
			e.decisions[d.ID] = d
		}
	case domain.EvConflict:
		if id, ok := ev.Payload["superseded_decision"].(string); ok {
			if d := e.decisions[id]; d != nil {
				d.Active = false
				if v, ok := ev.Payload["new_seq"].(float64); ok {
					d.SupersededBy = int64(v)
				}
			}
		}
	case domain.EvUndo:
		if id, ok := ev.Payload["target_decision"].(string); ok {
			if d := e.decisions[id]; d != nil {
				d.Active = false
				d.SupersededBy = ev.Seq
			}
		}
	}
}

// Replay rebuilds an engine from a persisted event slice.
func Replay(projectID string, events []*domain.Event, clock Clock) *Engine {
	e := NewEngine(clock)
	sort.SliceStable(events, func(a, b int) bool { return events[a].Seq < events[b].Seq })
	for _, ev := range events {
		if ev.ProjectID != projectID {
			continue
		}
		e.apply(ev)
		e.events = append(e.events, ev)
	}
	return e
}

// AtSeq replays through the given event and returns the historical state.
func AtSeq(projectID string, events []*domain.Event, seq int64, clock Clock) *Engine {
	var sub []*domain.Event
	for _, ev := range events {
		if ev.ProjectID == projectID && ev.Seq <= seq {
			sub = append(sub, ev)
		}
	}
	return Replay(projectID, sub, clock)
}

// snapshotAt returns a historical engine; used by the review "open old event".
func (e *Engine) SnapshotAt(seq int64) *Engine {
	return AtSeq(e.Project.ID, e.events, seq, e.clock)
}

func mustEventType(ev *domain.Event) string { return string(ev.Type) }

var _ = fmt.Sprintf
