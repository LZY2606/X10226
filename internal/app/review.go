package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/example/tga-evidence/internal/domain"
)

type Archive struct {
	Schema     string         `json:"schema"`
	ProjectID  string         `json:"project_id"`
	Name       string         `json:"name"`
	ExportedAt string         `json:"exported_at"`
	Events     []domain.Event `json:"events"`
}

func (service *Service) Decide(ctx context.Context, projectID string, request DecisionRequest) (domain.Event, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if request.SubjectID == "" || request.Verdict == "" {
		return domain.Event{}, errors.New("subject_id and verdict are required")
	}
	if request.Verdict != "accepted" && request.Verdict != "rejected" && request.Verdict != "parameter_adjusted" {
		return domain.Event{}, fmt.Errorf("unsupported verdict %q", request.Verdict)
	}
	project, err := service.LoadProject(ctx, projectID)
	if err != nil {
		return domain.Event{}, err
	}
	node, exists := project.Nodes[request.SubjectID]
	if !exists {
		return domain.Event{}, fmt.Errorf("subject %s is not a derived result", request.SubjectID)
	}
	basis := []string{}
	lastEventID := project.Events[len(project.Events)-1].ID
	basis = append(basis, lastEventID, node.DerivationID)
	conflict := ""
	for i := len(project.DecisionLog) - 1; i >= 0; i-- {
		prior := project.DecisionLog[i]
		if prior.SubjectID == request.SubjectID && prior.UndoneEventID == "" && prior.Verdict != request.Verdict {
			conflict = prior.ID
			break
		}
	}
	payload := domain.DecisionMade{DecisionID: service.ids.New("dec", projectID, request.SubjectID, request.Verdict), SubjectID: request.SubjectID, Verdict: request.Verdict, Rationale: request.Rationale, BasisEventIDs: basis, ParameterHash: node.ParameterHash, ConflictsWith: conflict}
	event := service.makeEvent(projectID, domain.EventDecision, payload, actor(request.Actor))
	_, err = service.store.AppendEvents(ctx, projectID, []domain.Event{event})
	return event, err
}

func (service *Service) UndoDecision(ctx context.Context, projectID, decisionID, reason, actorName string) (domain.Event, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	project, err := service.LoadProject(ctx, projectID)
	if err != nil {
		return domain.Event{}, err
	}
	if _, exists := project.Decisions[decisionID]; !exists {
		return domain.Event{}, fmt.Errorf("decision %s is unknown", decisionID)
	}
	payload := domain.DecisionUndone{DecisionID: decisionID, Reason: reason}
	event := service.makeEvent(projectID, domain.EventDecisionUndone, payload, actor(actorName))
	_, err = service.store.AppendEvents(ctx, projectID, []domain.Event{event})
	return event, err
}

func (service *Service) AdjustParameters(ctx context.Context, projectID string, adjustment ParameterAdjustment) ([]domain.Event, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	project, err := service.LoadProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	current := project.CurrentSources[domain.LayerAnalysisParameters]
	if current == nil {
		return nil, errors.New("initial analysis parameters must be imported before adjustment")
	}
	var base domain.AnalysisParameters
	if err := json.Unmarshal(current.Raw, &base); err != nil {
		return nil, err
	}
	merged, err := mergeParameters(base, adjustment.Parameters)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(merged)
	request := CommitRequest{BatchID: service.ids.New("batch", projectID, "parameters", raw), Reason: adjustment.Reason, Scope: "analysis_parameters_only", Actor: adjustment.Actor, Sources: []InputSource{{
		ID: service.ids.New("src", projectID, domain.LayerAnalysisParameters, raw), Layer: domain.LayerAnalysisParameters, ParentID: current.ID, ContentType: "application/json", Content: mustMap(raw),
	}}}
	service.mu.Unlock()
	events, err := service.CommitSources(ctx, projectID, request)
	service.mu.Lock()
	return events, err
}

func (service *Service) Export(ctx context.Context, projectID string) (Archive, error) {
	project, err := service.LoadProject(ctx, projectID)
	if err != nil {
		return Archive{}, err
	}
	name := project.Name
	return Archive{Schema: "tga-evidence-archive/v1", ProjectID: projectID, Name: name, ExportedAt: service.clock.Now().Format(timeFormat), Events: project.Events}, nil
}

func (service *Service) ImportArchive(ctx context.Context, archive Archive, preserveID bool) (string, []domain.Event, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	if len(archive.Events) == 0 {
		return "", nil, errors.New("archive has no events")
	}
	if archive.Events[0].Type != domain.EventProjectCreated {
		return "", nil, errors.New("archive must begin with project_created")
	}
	projectID := archive.ProjectID
	if !preserveID {
		projectID = service.ids.New("proj", "", archive.ProjectID, len(archive.Events))
	}
	exists, err := service.store.ProjectExists(ctx, projectID)
	if err != nil {
		return "", nil, err
	}
	if exists {
		return "", nil, fmt.Errorf("project %s already exists", projectID)
	}
	events := make([]domain.Event, len(archive.Events))
	markerPayload, _ := json.Marshal(domain.ProjectImported{OriginalProjectID: archive.ProjectID, EventCount: len(archive.Events)})
	marker := domain.Event{ID: service.ids.New("evt", projectID, "project_imported", archive.ProjectID), ProjectID: projectID, Type: domain.EventProjectImported, OccurredAt: service.clock.Now().Format(timeFormat), Actor: "system:import", Payload: markerPayload}
	events = append(events, marker)
	for i, event := range archive.Events {
		event.ProjectID = projectID
		events[i] = event
	}
	if err := service.store.ImportProject(ctx, events[0], events[1:]); err != nil {
		return "", nil, err
	}
	return projectID, events, nil
}

func (service *Service) StateAtEvent(ctx context.Context, projectID, eventID string) (*domain.Project, error) {
	events, err := service.store.ListEvents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return domain.ProjectAt(events, eventID)
}

func mergeParameters(base domain.AnalysisParameters, patch map[string]any) (domain.AnalysisParameters, error) {
	raw, err := json.Marshal(base)
	if err != nil {
		return base, err
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		return base, err
	}
	for key, value := range patch {
		object[key] = value
	}
	mergedRaw, _ := json.Marshal(object)
	var merged domain.AnalysisParameters
	if err := json.Unmarshal(mergedRaw, &merged); err != nil {
		return base, err
	}
	return merged, nil
}

func mustMap(raw json.RawMessage) map[string]any {
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	return value
}
