package domain

import (
	"encoding/json"
	"fmt"
)

func ApplyEvents(projectID string, events []Event) (*Project, error) {
	project := NewProject(projectID, "")
	for _, event := range events {
		if err := ApplyEvent(project, event); err != nil {
			return nil, err
		}
	}
	return project, nil
}

func ApplyEvent(project *Project, event Event) error {
	project.Events = append(project.Events, event)
	switch event.Type {
	case EventProjectCreated:
		var payload ProjectCreated
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		project.Name = payload.Name
	case EventSourcesCommitted:
		var payload SourcesCommitted
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		attempt := ImportAttempt{EventID: event.ID, BatchID: payload.BatchID, Status: payload.Status, OccurredAt: event.OccurredAt, Diagnostics: payload.Diagnostics}
		if payload.Status == "failed" {
			attempt.Status = "failed"
			for i := range payload.Fragments {
				attempt.FragmentIDs = append(attempt.FragmentIDs, fmt.Sprintf("%s:fragment:%d", payload.BatchID, i+1))
			}
			project.ImportAttempts = append(project.ImportAttempts, attempt)
			return nil
		}
		if len(payload.Sources) == 0 {
			return fmt.Errorf("commit %s has no sources", payload.BatchID)
		}
		for _, committed := range payload.Sources {
			if committed.ID == "" || committed.Layer == "" {
				return fmt.Errorf("source id and layer are required")
			}
			if _, exists := project.Sources[committed.ID]; exists {
				return fmt.Errorf("immutable source %s cannot be overwritten", committed.ID)
			}
			if committed.Version <= 0 {
				return fmt.Errorf("source %s version must be positive", committed.ID)
			}
			if committed.ParentID != "" {
				parent, exists := project.Sources[committed.ParentID]
				if !exists {
					return fmt.Errorf("source %s parent %s is unknown", committed.ID, committed.ParentID)
				}
				if parent.Layer != committed.Layer {
					return fmt.Errorf("revision parent must stay within layer %s", committed.Layer)
				}
				if committed.Version != parent.Version+1 {
					return fmt.Errorf("source %s must follow parent version", committed.ID)
				}
			} else if current, exists := project.CurrentSources[committed.Layer]; exists {
				if current.Version+1 != committed.Version {
					return fmt.Errorf("layer %s requires parent link for version %d", committed.Layer, committed.Version)
				}
			} else if committed.Version != 1 {
				return fmt.Errorf("first source for %s must be version 1", committed.Layer)
			}
			source := &Source{ID: committed.ID, Layer: committed.Layer, Version: committed.Version, ParentID: committed.ParentID, CommitEventID: event.ID, ContentType: committed.ContentType, Raw: committed.Content, OccurredAt: event.OccurredAt, Diagnostics: payload.Diagnostics}
			project.Sources[committed.ID] = source
			project.CurrentSources[committed.Layer] = source
			attempt.FragmentIDs = append(attempt.FragmentIDs, committed.ID)
		}
		attempt.Status = "committed"
		project.ImportAttempts = append(project.ImportAttempts, attempt)
	case EventDerivation:
		var payload DerivationRecorded
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		var graph Graph
		if err := json.Unmarshal(payload.Graph, &graph); err != nil {
			return err
		}
		graph.DerivationID = payload.DerivationID
		project.Graphs = append(project.Graphs, graph)
		for i := range graph.Nodes {
			node := graph.Nodes[i]
			node.DerivationID = payload.DerivationID
			project.Nodes[node.ID] = &node
			project.CurrentNodes[node.Key] = node.ID
		}
	case EventDecision:
		var payload DecisionMade
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		conflict := ""
		for _, prior := range project.DecisionLog {
			if prior.SubjectID == payload.SubjectID && prior.UndoneEventID == "" && prior.Verdict != payload.Verdict {
				conflict = prior.ID
				break
			}
		}
		decision := Decision{ID: payload.DecisionID, EventID: event.ID, SubjectID: payload.SubjectID, Verdict: payload.Verdict, Rationale: payload.Rationale, Actor: event.Actor, OccurredAt: event.OccurredAt, ConflictsWith: payload.ConflictsWith, BasisEventIDs: payload.BasisEventIDs, ParameterHash: payload.ParameterHash}
		if conflict != "" && decision.ConflictsWith == "" {
			decision.ConflictsWith = conflict
		}
		project.Decisions[payload.DecisionID] = &decision
		project.DecisionLog = append(project.DecisionLog, decision)
	case EventDecisionUndone:
		var payload DecisionUndone
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		decision, exists := project.Decisions[payload.DecisionID]
		if !exists {
			return fmt.Errorf("decision %s is unknown", payload.DecisionID)
		}
		decision.UndoneEventID = event.ID
		decision.UndoneAt = event.OccurredAt
	case EventProjectImported:
		// The marker is preserved in the event stream; all stable identities come from the imported events.
	default:
		return fmt.Errorf("unknown event type %q", event.Type)
	}
	return nil
}

func ProjectAt(events []Event, eventID string) (*Project, error) {
	prefix := []Event{}
	found := false
	for _, event := range events {
		prefix = append(prefix, event)
		if event.ID == eventID {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("event %s not found", eventID)
	}
	return ApplyEvents(events[0].ProjectID, prefix)
}

func CurrentGraph(project *Project) *Graph {
	if len(project.Graphs) == 0 {
		return nil
	}
	graph := project.Graphs[len(project.Graphs)-1]
	return &graph
}

func NodeIsCurrent(project *Project, nodeID string) bool {
	node, exists := project.Nodes[nodeID]
	if !exists {
		return false
	}
	return project.CurrentNodes[node.Key] == nodeID
}
