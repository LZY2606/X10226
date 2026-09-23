// Package engine is the review state machine. It appends events with full
// snapshots, recomputes only nodes affected by changed inputs and preserves
// superseded results so history can be revisited without overwrite.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"tga-review/internal/clock"
	"tga-review/internal/derive"
	"tga-review/internal/domain"
	"tga-review/internal/ingest"
	"tga-review/internal/store"
)

// DefaultParameters is the v1 parameter set.
func DefaultParameters() domain.Parameters {
	p := domain.Parameters{
		Version: 1, GasDelaySeconds: 30, TempLagSeconds: 45,
		PeakWindowSeconds: 120, OverlapTolSeconds: 60, GapMaxSeconds: 90,
		MassUnit: "mg", TimeUnit: "s", TempUnit: "C",
	}
	p.Fingerprint = ingest.Fingerprint(derive.AlgorithmVersion, p)
	return p
}

// Engine ties clock, store and derivation together.
type Engine struct {
	st    *store.Store
	clk   clock.Clock
	actor string
}

// New constructs an engine.
func New(st *store.Store, clk clock.Clock) *Engine {
	return &Engine{st: st, clk: clk, actor: "reviewer"}
}

// SetActor labels subsequent events (UI reviewer / replay / tests).
func (e *Engine) SetActor(a string) { e.actor = a }

// CreateProject makes a new review project and seeds v1 parameters.
func (e *Engine) CreateProject(ctx context.Context, id, name string, seed int64) error {
	if err := e.st.CreateProject(ctx, domain.Project{
		ID: id, Name: name, CreatedAt: e.clk.Now(), Seed: seed}); err != nil {
		return err
	}
	snap := &domain.Snapshot{Seq: 0, Parameters: DefaultParameters(),
		Flags: map[string]string{}}
	return e.append(ctx, domain.EvProjectCreated,
		map[string]any{"project_id": id, "name": name, "seed": seed}, snap)
}

// State is the materialized current view of a project.
type State struct {
	Project   domain.Project
	Sources   []domain.Source
	Current   map[string]*domain.Source
	Params    domain.Parameters
	Nodes     []domain.Node
	Stages    []domain.Stage
	Residue   *domain.Residue
	Layer     *domain.SampleLayer
	Corrected []domain.Point
	DTG       []domain.Point
	Windows   []map[string]any
	Events    []domain.Event
	StageOrder []string
}

// Event mirrors domain.Event with decoded payload convenience.
type Event = domain.Event

// loadState reads events and replays to the latest snapshot.
func (e *Engine) loadState(ctx context.Context, projectID string) (*State, error) {
	proj, err := e.st.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	evs, err := e.st.ListEvents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	st := &State{Project: *proj, Current: map[string]*domain.Source{}, Events: evs}
	if len(evs) == 0 {
		return nil, fmt.Errorf("project %q not initialized", projectID)
	}
	last := evs[len(evs)-1].Snap
	st.Params = last.Parameters
	st.StageOrder = last.StageOrder
	stored, err := e.st.ListSources(ctx, projectID)
	if err != nil {
		return nil, err
	}
	st.Sources = stored
	// materialize current accepted version per kind (highest version accepted)
	byKind := map[string][]domain.Source{}
	for i := range stored {
		byKind[stored[i].Kind] = append(byKind[stored[i].Kind], stored[i])
	}
	for kind, list := range byKind {
		sort.Slice(list, func(a, b int) bool { return list[a].Version > list[b].Version })
		// latest version is authoritative even if rejected-only during half failure
		st.Current[kind] = &list[0]
	}
	nodes, err := e.st.ListNodes(ctx, projectID, true)
	if err != nil {
		return nil, err
	}
	st.Nodes = nodes
	decodeNodes(st, nodes)
	return st, nil
}

func decodeNodes(st *State, nodes []domain.Node) {
	for _, n := range nodes {
		switch n.Kind {
		case domain.NodeStages:
			if raw, ok := n.Payload["stages"]; ok {
				st.Stages = decodeStages(raw)
			}
		case domain.NodeResidue:
			if raw, ok := n.Payload["residue"]; ok {
				var r domain.Residue
				remarshal(raw, &r)
				st.Residue = &r
			}
		case domain.NodeNormSample:
			if raw, ok := n.Payload["layer"]; ok {
				var l domain.SampleLayer
				remarshal(raw, &l)
				st.Layer = &l
			}
		case domain.NodeCorrected:
			if raw, ok := n.Payload["series"]; ok {
				remarshal(raw, &st.Corrected)
			}
		case domain.NodeDTG:
			if raw, ok := n.Payload["series"]; ok {
				remarshal(raw, &st.DTG)
			}
		case domain.NodeReactWindow:
			if raw, ok := n.Payload["windows"]; ok {
				remarshal(raw, &st.Windows)
			}
		}
	}
}

func decodeStages(raw any) []domain.Stage {
	b, _ := json.Marshal(raw)
	var out []domain.Stage
	_ = json.Unmarshal(b, &out)
	return out
}

func remarshal(raw any, dst any) {
	b, _ := json.Marshal(raw)
	_ = json.Unmarshal(b, dst)
}

func (e *Engine) append(ctx context.Context, kind string, payload map[string]any, snap *domain.Snapshot) error {
	_, err := e.st.AppendEvent(ctx, &domain.Event{
		ProjectID: snapProject(payload), Kind: kind, At: e.clk.Tick(),
		Actor: e.actor, Payload: payload, Snap: snap})
	return err
}

func snapProject(p map[string]any) string {
	if v, ok := p["project_id"].(string); ok {
		return v
	}
	return ""
}
