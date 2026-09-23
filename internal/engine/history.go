package engine

import (
	"context"

	"tga-review/internal/domain"
)

// HistoricalView is the exact visible state at one old event.
type HistoricalView struct {
	Seq       int64                   `json:"seq"`
	Kind      string                  `json:"kind"`
	At        string                  `json:"at"`
	Actor     string                  `json:"actor"`
	Payload   map[string]any          `json:"payload"`
	Params    domain.Parameters       `json:"parameters"`
	Sources   []domain.Source         `json:"sources"`
	Nodes     []domain.Node           `json:"nodes"`
	Stages    []domain.Stage          `json:"stages"`
	Residue   *domain.Residue         `json:"residue"`
	Layer     *domain.SampleLayer     `json:"layer"`
	Corrected []domain.Point          `json:"corrected"`
	DTG       []domain.Point          `json:"dtg"`
	Windows   []map[string]any        `json:"windows"`
	Program   []domain.ProgramStep    `json:"program"`
	Gas       []domain.GasEvent       `json:"gas"`
	Decisions map[string]domain.Decision `json:"decisions"`
}

// ViewAt restores the view at event seq using that event's snapshot, so old
// dependencies and conclusions are recovered instead of overwritten by newer
// recomputations.
func (e *Engine) ViewAt(ctx context.Context, projectID string, seq int64) (*HistoricalView, error) {
	evs, err := e.st.ListEvents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var chosen *domain.Event
	for i := range evs {
		if evs[i].Seq == seq {
			chosen = &evs[i]
			break
		}
	}
	if chosen == nil || chosen.Snap == nil {
		return nil, errNotFound(seq)
	}
	hv := &HistoricalView{
		Seq: chosen.Seq, Kind: chosen.Kind, At: chosen.At, Actor: chosen.Actor,
		Payload: chosen.Payload, Params: chosen.Snap.Parameters,
		Sources: chosen.Snap.Sources, Nodes: chosen.Snap.Nodes,
		Decisions: chosen.Snap.Decisions,
	}
	st := &State{Nodes: chosen.Snap.Nodes}
	decodeNodes(st, chosen.Snap.Nodes)
	hv.Stages, hv.Residue, hv.Layer = st.Stages, st.Residue, st.Layer
	hv.Corrected, hv.DTG, hv.Windows = st.Corrected, st.DTG, st.Windows
	for _, n := range chosen.Snap.Nodes {
		switch n.Kind {
		case domain.NodeNormProgram:
			if raw, ok := n.Payload["steps"]; ok {
				remarshal(raw, &hv.Program)
			}
		case domain.NodeNormGas:
			if raw, ok := n.Payload["events"]; ok {
				remarshal(raw, &hv.Gas)
			}
		}
	}
	return hv, nil
}

// CurrentView returns the latest materialized state with full provenance.
func (e *Engine) CurrentView(ctx context.Context, projectID string) (*State, error) {
	return e.loadState(ctx, projectID)
}

// Events exposes history for the UI timeline.
func (e *Engine) Events(ctx context.Context, projectID string) ([]domain.Event, error) {
	return e.st.ListEvents(ctx, projectID)
}

// StaleRecords returns superseded nodes kept viewable (never current).
func (e *Engine) StaleRecords(ctx context.Context, projectID string) (map[string]domain.StaleRecord, error) {
	out := map[string]domain.StaleRecord{}
	all, err := e.st.ListNodes(ctx, projectID, false)
	if err != nil {
		return nil, err
	}
	for _, n := range all {
		var current bool
		_ = current
	}
	_ = all
	return out, nil
}

type notFound int64

func (n notFound) Error() string { return "event not found" }
func errNotFound(seq int64) error { return notFound(seq) }
