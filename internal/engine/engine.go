// Package engine is the event-sourced review state machine. It appends
// immutable events, replays them with a virtual clock and pure compute pipeline,
// materializes derived views and keeps full history for time travel.
package engine

import (
	"encoding/json"
	"fmt"
	"time"

	"tgareview/internal/domain"
)

// Clock is injectable; tests use fixed/virtual time.
type Clock func() time.Time

// State is the materialized project state at one event sequence.
type State struct {
	ProjectID string
	Name      string
	Origin    string
	Seed      int64
	Seq       int64
	HeadID    string

	Raw     []domain.RawPoint
	Drifts  []domain.DriftRow
	Blanks  []domain.BlankPoint
	Program []domain.ProgramStep
	Gas     []domain.GasEvent
	Weights []domain.WeightRow
	Params  domain.Params

	// Parent linkage of parameter/weight/drift revisions.
	ParamsParent string

	Events    []domain.Event
	Decisions map[string]domain.DecisionPayload // keyed by target id
	Conflicts map[string]domain.Conflict
	Undone    map[string]bool // event ids negated by undo
	Notes     []domain.NotePayload
	Batches   []domain.BatchImportedPayload

	View *domain.DerivedView
}

// NewState returns an empty state with default parameters.
func NewState() *State {
	return &State{
		Params:    domain.DefaultParams(),
		Decisions: map[string]domain.DecisionPayload{},
		Conflicts: map[string]domain.Conflict{},
		Undone:    map[string]bool{},
	}
}

// Clone deep-copies the state for snapshot isolation.
func (s *State) Clone() *State {
	b, _ := json.Marshal(s)
	var c State
	_ = json.Unmarshal(b, &c)
	return &c
}

// Apply appends one event to a cloned state and rematerializes the view.
// The caller assigns the stable event id and sequence before replay.
func Apply(prev *State, ev domain.Event) (*State, error) {
	if ev.Seq == 0 {
		ev.Seq = prev.Seq + 1
	}
	s := prev.Clone()
	s.Events = append(s.Events, ev)
	s.Seq = ev.Seq
	s.HeadID = ev.EventID
	if err := applyEvent(s, ev); err != nil {
		return nil, err
	}
	if s.Undone[ev.EventID] {
		// an undone event keeps history but contributes nothing
	}
	s.materialize()
	return s, nil
}

func decode[T any](ev domain.Event) (T, error) {
	var p T
	if len(ev.Payload) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		return p, fmt.Errorf("event %s payload: %w", ev.EventID, err)
	}
	return p, nil
}

func applyEvent(s *State, ev domain.Event) error {
	switch ev.Type {
	case domain.EvProjectCreated:
		p, err := decode[domain.ProjectCreatedPayload](ev)
		if err != nil {
			return err
		}
		s.ProjectID, s.Name, s.Origin, s.Seed = ev.ProjectID, p.Name, p.Origin, p.Seed

	case domain.EvBatchImported:
		p, err := decode[domain.BatchImportedPayload](ev)
		if err != nil {
			return err
		}
		s.Batches = append(s.Batches, p)

	case domain.EvSourceRow:
		p, err := decode[domain.SourceRowPayload](ev)
		if err != nil {
			return err
		}
		if p.Status != domain.RowOK {
			return nil // failed rows stay only as source-row history
		}
		return ingestRow(s, p, ev.EventID)

	case domain.EvParamsSet:
		p, err := decode[domain.ParamsSetPayload](ev)
		if err != nil {
			return err
		}
		s.Params = p.Params
		s.ParamsParent = p.ParentParams

	case domain.EvDecision:
		p, err := decode[domain.DecisionPayload](ev)
		if err != nil {
			return err
		}
		return applyDecision(s, p, ev.Seq)

	case domain.EvConflictResolved:
		p, err := decode[domain.ConflictResolvedPayload](ev)
		if err != nil {
			return err
		}
		if c, ok := s.Conflicts[p.ConflictID]; ok {
			c.Open = false
			c.ChosenID = p.ChosenID
			c.ResolvedSeq = ev.Seq
			s.Conflicts[p.ConflictID] = c
		}

	case domain.EvUndo:
		p, err := decode[domain.UndoPayload](ev)
		if err != nil {
			return err
		}
		s.Undone[p.UndoneEventID] = true
		// Remove the effect of the undone decision, if any.
		for k, d := range s.Decisions {
			if d.EventID == p.UndoneEventID {
				delete(s.Decisions, k)
			}
		}
		// Re-open a conflict if the undone event was its resolution: locate
		// the resolution sequence via the undone event id.
		for _, prior := range s.Events {
			if prior.EventID != p.UndoneEventID || prior.Type != domain.EvConflictResolved {
				continue
			}
			var rp domain.ConflictResolvedPayload
			if err := json.Unmarshal(prior.Payload, &rp); err != nil {
				return err
			}
			if c, ok := s.Conflicts[rp.ConflictID]; ok {
				c.Open = true
				c.ChosenID, c.ResolvedSeq = "", 0
				s.Conflicts[rp.ConflictID] = c
			}
		}

	case domain.EvNote:
		p, err := decode[domain.NotePayload](ev)
		if err != nil {
			return err
		}
		s.Notes = append(s.Notes, p)
	}
	return nil
}

// WeightsForView exposes active weight rows for stage recomputation.
func (s *State) WeightsForView() []domain.WeightRow { return s.Weights }
