package app

import "github.com/example/tga-evidence/internal/domain"

type Snapshot struct {
	ProjectID      string                 `json:"project_id"`
	Name           string                 `json:"name"`
	Sequence       int                    `json:"sequence"`
	Sources        []SourceView           `json:"sources"`
	CurrentSources map[string]SourceView  `json:"current_sources"`
	Nodes          []NodeView             `json:"nodes"`
	CurrentGraphID string                 `json:"current_graph_id"`
	Stages         []NodeView             `json:"stages"`
	Rules          []NodeView             `json:"rules"`
	Decisions      []DecisionView         `json:"decisions"`
	ImportAttempts []domain.ImportAttempt `json:"import_attempts"`
	Events         []EventView            `json:"events"`
	Diagnostics    []domain.Diagnostic    `json:"diagnostics"`
}

type SourceView struct {
	ID            string              `json:"id"`
	Layer         string              `json:"layer"`
	Version       int                 `json:"version"`
	ParentID      string              `json:"parent_id"`
	CommitEventID string              `json:"commit_event_id"`
	ContentType   string              `json:"content_type"`
	Current       bool                `json:"current"`
	Raw           any                 `json:"raw"`
	Diagnostics   []domain.Diagnostic `json:"diagnostics"`
}

type NodeView struct {
	domain.ValueNode
	Current  bool          `json:"current"`
	Decision *DecisionView `json:"decision,omitempty"`
}

type DecisionView struct {
	domain.Decision
	Current bool `json:"current"`
}

type EventView struct {
	ID            string `json:"id"`
	Sequence      int64  `json:"sequence"`
	ParentEventID string `json:"parent_event_id"`
	Type          string `json:"type"`
	OccurredAt    string `json:"occurred_at"`
	Actor         string `json:"actor"`
}

func SnapshotFromProject(project *domain.Project) Snapshot {
	snapshot := Snapshot{ProjectID: project.ID, Name: project.Name, Sequence: len(project.Events), CurrentSources: map[string]SourceView{}}
	for _, source := range project.Sources {
		view := sourceView(source, project.CurrentSources[source.Layer] != nil && project.CurrentSources[source.Layer].ID == source.ID)
		snapshot.Sources = append(snapshot.Sources, view)
	}
	for layer, source := range project.CurrentSources {
		snapshot.CurrentSources[layer] = sourceView(source, true)
	}
	if graph := domain.CurrentGraph(project); graph != nil {
		snapshot.CurrentGraphID = graph.DerivationID
		for i := range graph.Nodes {
			node := graph.Nodes[i]
			node.DerivationID = graph.DerivationID
			view := NodeView{ValueNode: node, Current: true}
			attachDecision(&view, project)
			snapshot.Nodes = append(snapshot.Nodes, view)
			if node.Kind == "stage_candidate" || node.Kind == "overlapping_shoulder_candidate" {
				snapshot.Stages = append(snapshot.Stages, view)
			}
			if node.Kind == "machine_readable_rule" {
				snapshot.Rules = append(snapshot.Rules, view)
			}
		}
	}
	// Preserve superseded nodes in the API with stale=false/current=false, but do not let them appear as present values.
	for _, node := range project.Nodes {
		if domain.NodeIsCurrent(project, node.ID) {
			continue
		}
		view := NodeView{ValueNode: *node, Current: false}
		attachDecision(&view, project)
		snapshot.Nodes = append(snapshot.Nodes, view)
		if node.Kind == "stage_candidate" || node.Kind == "overlapping_shoulder_candidate" {
			snapshot.Stages = append(snapshot.Stages, view)
		}
	}
	for _, decision := range project.DecisionLog {
		view := DecisionView{Decision: decision, Current: decision.UndoneEventID == ""}
		snapshot.Decisions = append(snapshot.Decisions, view)
	}
	snapshot.ImportAttempts = project.ImportAttempts
	for _, event := range project.Events {
		snapshot.Events = append(snapshot.Events, EventView{ID: event.ID, Sequence: event.Sequence, ParentEventID: event.ParentEventID, Type: event.Type, OccurredAt: event.OccurredAt, Actor: event.Actor})
	}
	return snapshot
}

func sourceView(source *domain.Source, current bool) SourceView {
	var raw any
	_ = decodeRaw(source.Raw, &raw)
	return SourceView{ID: source.ID, Layer: source.Layer, Version: source.Version, ParentID: source.ParentID, CommitEventID: source.CommitEventID, ContentType: source.ContentType, Current: current, Raw: raw, Diagnostics: source.Diagnostics}
}

func decodeRaw(raw []byte, target *any) error {
	return jsonUnmarshal(raw, target)
}

func attachDecision(view *NodeView, project *domain.Project) {
	for i := len(project.DecisionLog) - 1; i >= 0; i-- {
		decision := project.DecisionLog[i]
		if decision.SubjectID == view.ID && decision.UndoneEventID == "" {
			copy := DecisionView{Decision: decision, Current: true}
			view.Decision = &copy
			return
		}
	}
}
