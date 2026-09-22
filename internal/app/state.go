// Package app folds the append-only event log into review state and runs the
// deterministic derivation. Folding up to any seq reproduces exactly the
// visible conclusions and dependency versions that existed at that event.
package app

import (
	"encoding/json"

	"tga-archive/internal/derive"
	"tga-archive/internal/domain"
	"tga-archive/internal/store"
)

type LayerSource struct {
	Current  *domain.SourcePayload `json:"current"`
	Replaced []string              `json:"replaced"` // source ids superseded
}

type RulingRec struct {
	Event       domain.Envelope      `json:"event"`
	Payload     domain.RulingPayload `json:"payload"`
	Active      bool                 `json:"active"`
	UndoneBy    string               `json:"undoneBy,omitempty"`
	ConflictIDs []string             `json:"conflicts,omitempty"`
}

type ConflictRec struct {
	Event   domain.Envelope        `json:"event"`
	Payload domain.ConflictPayload `json:"payload"`
}

type State struct {
	Project      store.ProjectInfo                 `json:"project"`
	Layers       map[string]*LayerSource           `json:"layers"`
	Params       domain.Params                     `json:"params"`
	ParamVersion string                            `json:"paramVersion"`
	ParamHistory []domain.ParamsPayload            `json:"paramHistory"`
	Rulings      map[string]*RulingRec             `json:"rulings"`
	Conflicts    []ConflictRec                     `json:"conflicts"`
	Diagnostics  []domain.Diagnostic               `json:"diagnostics"`
	Report       derive.Report                    `json:"report"`
	SeenSeq      int64                            `json:"seenSeq"`
}

func newState(info store.ProjectInfo) *State {
	return &State{
		Project: info,
		Layers:  map[string]*LayerSource{},
		Params:  domain.DefaultParams(),
		Rulings: map[string]*RulingRec{},
	}
}

func (s *State) source(layer string) *domain.SourcePayload {
	if ls, ok := s.Layers[layer]; ok && ls != nil {
		return ls.Current
	}
	return nil
}

func (s *State) inputs() derive.Inputs {
	return derive.Inputs{
		Samples:     s.source(domain.LayerSamples),
		Drift:       s.source(domain.LayerDrift),
		Furnace:     s.source(domain.LayerFurnace),
		Valves:      s.source(domain.LayerGas),
		Blank:       s.source(domain.LayerBlank),
		InitialMass: s.source(domain.LayerInitialMass),
	}
}

// Fold replays events into state. Every fold is pure: no DB writes.
func Fold(info store.ProjectInfo, events []domain.Envelope) *State {
	s := newState(info)
	for _, ev := range events {
		s.apply(ev)
		s.SeenSeq = ev.Seq
	}
	recompute(s)
	return s
}

func (s *State) apply(ev domain.Envelope) {
	switch ev.Type {
	case domain.EvProjectCreated:
		// metadata already in s.Project; nothing else to do
	case domain.EvSourceSubmitted:
		var p domain.SourcePayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return
		}
		ls := s.Layers[p.Layer]
		if ls == nil {
			ls = &LayerSource{}
			s.Layers[p.Layer] = ls
		}
		if p.Accepted {
			// revision: new version supersedes the current one
			if ls.Current != nil {
				ls.Replaced = append(ls.Replaced, ls.Current.SourceID)
			}
			cur := p
			ls.Current = &cur
		} else {
			// half-failure: raw fragment is recorded but never becomes current
			s.Diagnostics = append(s.Diagnostics, p.Diagnostics...)
		}
	case domain.EvParamsRevised:
		var p domain.ParamsPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return
		}
		s.Params = p.Params
		s.ParamVersion = p.ParamVersion
		s.ParamHistory = append(s.ParamHistory, p)
	case domain.EvRuling:
		var p domain.RulingPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return
		}
		rec := &RulingRec{Event: ev, Payload: p, Active: true}
		// conflicting rulings on the same candidate are both retained
		if prior, ok := s.Rulings[p.CandidateID]; ok && prior.Active && prior.Payload.Decision != p.Decision {
			prior.ConflictIDs = append(prior.ConflictIDs, p.RulingID)
			rec.ConflictIDs = append(rec.ConflictIDs, prior.Payload.RulingID)
			s.Diagnostics = append(s.Diagnostics, domain.Diagnostic{
				Severity: domain.SevWarning, Code: "ruling_conflict", Fragment: p.CandidateID,
				Message: "candidate has conflicting active rulings; resolution required",
			})
		}
		s.Rulings[p.RulingID] = rec
	case domain.EvConflictResolved:
		var p domain.ConflictPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return
		}
		for id, r := range s.Rulings {
			if r.Payload.CandidateID == p.CandidateID && id != p.KeptRuling && r.Active {
				r.Active = false
				r.ConflictIDs = append(r.ConflictIDs, "resolved:"+p.KeptRuling)
			}
		}
		s.Conflicts = append(s.Conflicts, ConflictRec{Event: ev, Payload: p})
	case domain.EvUndo:
		var p domain.UndoPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return
		}
		if r, ok := s.Rulings[p.RulingID]; ok {
			r.Active = false
			r.UndoneBy = ev.ID
		}
	}
}

func recompute(s *State) {
	r := derive.Run(s.inputs(), s.Params, s.effectiveParamVersion())
	// apply ruling statuses onto candidates (history-aware)
	byKey := map[string]int{}
	for i, c := range r.Candidates {
		byKey[c.Key] = i
	}
	for _, rec := range s.Rulings {
		idx, ok := byKey[rec.Payload.CandidateID]
		if !ok {
			continue
		}
		if rec.Active {
			if rec.Payload.Decision == "accept" {
				r.Candidates[idx].Status = "ruled_accepted"
			} else {
				r.Candidates[idx].Status = "ruled_rejected"
			}
		}
	}
	// conflict state marker
	for _, rec := range s.Rulings {
		if rec.Active && len(rec.ConflictIDs) > 0 {
			if idx, ok := byKey[rec.Payload.CandidateID]; ok {
				if r.Candidates[idx].Status == "ruled_accepted" {
					r.Candidates[idx].Status = "ruled_accepted_conflicted"
				} else if r.Candidates[idx].Status == "ruled_rejected" {
					r.Candidates[idx].Status = "ruled_rejected_conflicted"
				}
			}
		}
	}
	s.Report = r
}

func (s *State) effectiveParamVersion() string {
	if s.ParamVersion != "" {
		return s.ParamVersion
	}
	return "params:default-v1"
}
