package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/example/tga-evidence/internal/domain"
)

type EventStore interface {
	CreateProject(ctx context.Context, event domain.Event) error
	AppendEvents(ctx context.Context, projectID string, events []domain.Event) (int64, error)
	ListEvents(ctx context.Context, projectID string) ([]domain.Event, error)
	ListProjectIDs(ctx context.Context) ([]string, error)
	ProjectExists(ctx context.Context, projectID string) (bool, error)
	ImportProject(ctx context.Context, create domain.Event, events []domain.Event) error
}

type InputSource struct {
	ID          string         `json:"id"`
	Layer       string         `json:"layer"`
	ParentID    string         `json:"parent_id,omitempty"`
	ContentType string         `json:"content_type"`
	Content     map[string]any `json:"content"`
}

type CommitRequest struct {
	BatchID     string          `json:"batch_id"`
	ParentBatch string          `json:"parent_batch,omitempty"`
	Reason      string          `json:"reason,omitempty"`
	Scope       string          `json:"scope,omitempty"`
	Sources     []InputSource   `json:"sources"`
	Raw         json.RawMessage `json:"raw,omitempty"`
	Actor       string          `json:"actor,omitempty"`
}

type DecisionRequest struct {
	SubjectID string `json:"subject_id"`
	Verdict   string `json:"verdict"`
	Rationale string `json:"rationale"`
	Actor     string `json:"actor,omitempty"`
}

type ParameterAdjustment struct {
	Parameters map[string]any `json:"parameters"`
	Reason     string         `json:"reason,omitempty"`
	Actor      string         `json:"actor,omitempty"`
}

type Service struct {
	store EventStore
	clock Clock
	ids   *SequencedID
	seed  uint64
	mu    sync.Mutex
}

func NewService(store EventStore, clock Clock, seed uint64) *Service {
	if clock == nil {
		clock = SystemClock{}
	}
	return &Service{store: store, clock: clock, seed: seed, ids: NewSequencedID(clock, seed)}
}

func (service *Service) CreateProject(ctx context.Context, name, description string) (string, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	projectID := service.ids.New("proj", "", name, description)
	payload, _ := json.Marshal(domain.ProjectCreated{Name: name, Description: description})
	event := domain.Event{ID: service.ids.New("evt", projectID, name), ProjectID: projectID, Type: domain.EventProjectCreated, OccurredAt: service.clock.Now().Format(timeFormat), Actor: "operator", Payload: payload}
	if err := service.store.CreateProject(ctx, event); err != nil {
		return "", err
	}
	return projectID, nil
}

const timeFormat = "2006-01-02T15:04:05.000000Z"

func (service *Service) LoadProject(ctx context.Context, projectID string) (*domain.Project, error) {
	events, err := service.store.ListEvents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, errors.New("project not found")
	}
	return domain.ApplyEvents(projectID, events)
}

func (service *Service) CommitSources(ctx context.Context, projectID string, request CommitRequest) ([]domain.Event, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	project, err := service.LoadProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	diagnostics, committed, fatal := validateAndCommit(project, request)
	if fatal != nil {
		fragments := []json.RawMessage{}
		if request.Raw != nil {
			fragments = append(fragments, request.Raw)
		}
		failed := service.makeEvent(projectID, domain.EventSourcesCommitted, domain.SourcesCommitted{BatchID: request.BatchID, ParentBatch: request.ParentBatch, Reason: request.Reason, Scope: request.Scope, Status: "failed", Diagnostics: diagnostics, Fragments: fragments}, actor(request.Actor))
		if _, appendErr := service.store.AppendEvents(ctx, projectID, []domain.Event{failed}); appendErr != nil {
			return nil, appendErr
		}
		return []domain.Event{failed}, fatal
	}
	commitEvent := service.makeEvent(projectID, domain.EventSourcesCommitted, domain.SourcesCommitted{BatchID: request.BatchID, ParentBatch: request.ParentBatch, Reason: request.Reason, Scope: request.Scope, Status: "committed", Sources: committed, Diagnostics: diagnostics}, actor(request.Actor))
	derived, derivationDiagnostics := domain.Derive(project)
	graph := domain.Graph{DerivationID: derived.DerivationID, Nodes: derived.Nodes, Bindings: derived.Bindings}
	graphRaw, _ := json.Marshal(graph)
	derivationEvent := service.makeEvent(projectID, domain.EventDerivation, domain.DerivationRecorded{DerivationID: derived.DerivationID, Graph: graphRaw}, "system:tga-derive")
	if len(derivationDiagnostics) > 0 {
		// Non-fatal derivation diagnostics remain attached to nodes; the graph itself is valid historical evidence.
	}
	events := []domain.Event{commitEvent, derivationEvent}
	if _, err := service.store.AppendEvents(ctx, projectID, events); err != nil {
		return nil, err
	}
	return events, nil
}

func (service *Service) makeEvent(projectID, eventType string, payload any, actorName string) domain.Event {
	raw, _ := json.Marshal(payload)
	return domain.Event{ID: service.ids.New("evt", projectID, eventType, payload), ProjectID: projectID, Type: eventType, OccurredAt: service.clock.Now().Format(timeFormat), Actor: actorName, Payload: raw}
}

func actor(value string) string {
	if value == "" {
		return "reviewer"
	}
	return value
}

func validateAndCommit(project *domain.Project, request CommitRequest) ([]domain.Diagnostic, []domain.CommittedSource, error) {
	if request.BatchID == "" {
		return nil, nil, fmt.Errorf("batch_id is required")
	}
	if len(request.Sources) == 0 {
		return []domain.Diagnostic{{Code: "empty_batch", Severity: "error", Path: "sources", Message: "no source fragments were present"}}, nil, fmt.Errorf("import batch is empty")
	}
	var diagnostics []domain.Diagnostic
	committed := make([]domain.CommittedSource, 0, len(request.Sources))
	seenLayers := map[string]bool{}
	for index, source := range request.Sources {
		path := fmt.Sprintf("sources[%d]", index)
		if source.ID == "" || source.Layer == "" {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "missing_source_identity", Severity: "error", Path: path, Message: "id and layer are required"})
			continue
		}
		if !knownLayer(source.Layer) {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "unknown_layer", Severity: "error", Path: path + ".layer", Message: "unsupported layer: " + source.Layer, Raw: source.Layer})
			continue
		}
		if _, exists := project.Sources[source.ID]; exists {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "immutable_source_overwrite", Severity: "error", Path: path + ".id", Message: "source id already exists; create a revision instead", Raw: source.ID})
			continue
		}
		if seenLayers[source.Layer] {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "duplicate_layer_in_batch", Severity: "error", Path: path + ".layer", Message: "one batch can contain a layer once", Raw: source.Layer})
			continue
		}
		seenLayers[source.Layer] = true
		current := project.CurrentSources[source.Layer]
		version := 1
		parentID := source.ParentID
		if current != nil {
			if parentID == "" {
				parentID = current.ID
			}
			if parentID != current.ID {
				diagnostics = append(diagnostics, domain.Diagnostic{Code: "non_head_revision", Severity: "error", Path: path + ".parent_id", Message: "revision must descend from current source", Raw: parentID})
				continue
			}
			version = current.Version + 1
		} else if parentID != "" {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "orphan_parent", Severity: "error", Path: path + ".parent_id", Message: "parent cannot precede first version", Raw: parentID})
			continue
		}
		content, err := json.Marshal(source.Content)
		if err != nil {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "invalid_json", Severity: "error", Path: path + ".content", Message: err.Error()})
			continue
		}
		if !json.Valid(content) {
			diagnostics = append(diagnostics, domain.Diagnostic{Code: "invalid_json", Severity: "error", Path: path + ".content", Message: "content is not valid JSON"})
			continue
		}
		parsed, parseDiagnostics := domain.ParseLayer(source.Layer, content)
		for i := range parseDiagnostics {
			parseDiagnostics[i].Path = path + ".content." + parseDiagnostics[i].Path
			diagnostics = append(diagnostics, parseDiagnostics[i])
			if parseDiagnostics[i].Severity == "error" {
				continue
			}
		}
		_ = parsed
		committed = append(committed, domain.CommittedSource{ID: source.ID, Layer: source.Layer, Version: version, ParentID: parentID, ContentType: defaultContentType(source.ContentType), Content: content})
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity == "error" {
			return diagnostics, nil, fmt.Errorf("import rejected: %s", diagnostic.Code)
		}
	}
	if len(committed) != len(request.Sources) {
		return diagnostics, nil, fmt.Errorf("import rejected before any fragment was committed")
	}
	return diagnostics, committed, nil
}

func knownLayer(layer string) bool {
	for _, candidate := range domain.Layers() {
		if layer == candidate {
			return true
		}
	}
	return false
}

func defaultContentType(value string) string {
	if value != "" {
		return value
	}
	return "application/json"
}
