package project

import (
	"context"
	"fmt"

	"tga-lineage/internal/analysis"
	"tga-lineage/internal/model"
)

// rebuild projects a State from the full event stream (or until seq). It
// never reruns the analysis: derived_committed events carry their artifacts,
// so historical conclusions replay byte-identically regardless of the
// algorithm binary currently installed.
func (s *Service) rebuild(ctx context.Context, pid string, until int64) (*State, error) {
	var st *State
	err := s.store.Stream(ctx, pid, until, func(ev model.Event) error {
		if st == nil {
			return fmt.Errorf("event seq %d precedes project creation", ev.Seq)
		}
		st.Events = append(st.Events, ev)
		st.Seq = ev.Seq
		switch p := ev.Payload.(type) {
		case model.LayerPayload:
			l := p.Layer
			st.Layers[l.Kind] = &l
		case model.ParamsPayload:
			pp := p.Params
			st.Params = &pp
		case model.DerivedPayload:
			st.InputFP = p.InputFP
			for k := range st.CurrentUIDs {
				delete(st.CurrentUIDs, k)
			}
			for i := range p.Artifacts {
				a := p.Artifacts[i]
				if _, exists := st.Arts[a.UID]; !exists {
					st.ArtOrder = append(st.ArtOrder, a.UID)
				}
				st.Arts[a.UID] = &a
				st.CurrentUIDs[a.UID] = true
			}
		case model.DecisionPayload:
			st.Decisions = append(st.Decisions, p.Decision)
		case model.JudgmentPayload:
			st.Judgments = append(st.Judgments, p.Judgments...)
		case model.CounterPayload:
			st.Counters = append(st.Counters, p.Counter)
		case model.NotePayload:
			// rendered as plain event in history
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, fmt.Errorf("project %s has no events", pid)
	}
	return st, nil
}

// analysisInput materializes the pipeline inputs from a state.
func analysisInput(st *State) analysis.Input {
	in := analysis.Input{Params: model.DefaultParams("")}
	if st.Params != nil {
		in.Params = *st.Params
	}
	in.Samples = st.Layers[model.LayerSamples]
	in.Blank = st.Layers[model.LayerBlank]
	in.Drift = st.Layers[model.LayerDrift]
	in.Furnace = st.Layers[model.LayerFurnace]
	in.Valves = st.Layers[model.LayerValves]
	in.Initial = st.Layers[model.LayerInitial]
	return in
}
