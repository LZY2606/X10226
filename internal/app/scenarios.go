package app

import (
	"context"
	"fmt"

	"tga-review/internal/domain"
)

// Scenario is one counterexample: it changes exactly ONE direct source
// (or one parameter vector) and the runner verifies that the blast radius is
// limited to the nodes that genuinely depend on it.
type Scenario struct {
	Key          string `json:"key"`
	Title        string `json:"title"`
	ChangedLayer domain.SourceLayer `json:"changed_layer,omitempty"`
	ChangedParam bool   `json:"changed_param,omitempty"`
	Run          func(ctx context.Context, svc *Service, projectID string) (ScenarioOutcome, error)
}

type ScenarioOutcome struct {
	Status        string         `json:"status"` // adopted | rejected
	ChangedSource string         `json:"changed_source"`
	Before        map[string]any `json:"before"`
	After         map[string]any `json:"after"`
	Rules         []domain.RuleEvent `json:"rules"`
	Scope         []string       `json:"affected_node_kinds"`
	Note          string         `json:"note"`
}

// snapshotNodeIDs returns current node id keyed by kind before a change.
func (s *Service) snapshotNodeIDs(ctx context.Context, projectID string) map[string]string {
	out := map[string]string{}
	for _, k := range []domain.NodeKind{
		domain.NodeCorrectedSeries, domain.NodeFurnaceTrace, domain.NodeGasTimeline,
		domain.NodeDTG, domain.NodeStages, domain.NodeResidue,
	} {
		if n, _ := s.st.CurrentNode(ctx, projectID, k); n != nil {
			out[string(k)] = n.ID
		}
	}
	return out
}

// RunScenario executes a registered scenario inside the live project, writes
// an archive entry and appends a machine-readable scenario event.
func (s *Service) RunScenario(ctx context.Context, projectID string, sc Scenario) (*domain.ArchiveEntry, error) {
	before := s.snapshotNodeIDs(ctx, projectID)
	outcome, err := sc.Run(ctx, s, projectID)
	if err != nil {
		return nil, err
	}
	after := s.snapshotNodeIDs(ctx, projectID)
	// scope = kinds whose node id changed
	var scope []string
	for k := range after {
		if before[k] != after[k] {
			scope = append(scope, k)
		}
	}
	outcome.Scope = scope
	outcome.Before = mergeSnap(outcome.Before, before)
	outcome.After = mergeSnap(outcome.After, after)

	// rules produced by the latest derived event
	rules := s.latestRules(ctx, projectID)
	if len(outcome.Rules) > 0 {
		rules = outcome.Rules
	}
	now := s.now()
	id := domain.ContentID("arc", map[string]any{
		"project": projectID, "scenario": sc.Key, "after": after, "at": now,
	})
	entry := domain.ArchiveEntry{
		ID: id, ProjectID: projectID, Scenario: sc.Key, Title: sc.Title,
		Status: outcome.Status, ChangedSource: outcome.ChangedSource,
		Before: outcome.Before, After: outcome.After, Rules: rules,
		Scope: scope, Adopted: outcome.Status == "adopted", CreatedAt: now,
	}
	if err := s.st.InsertArchiveEntry(ctx, entry); err != nil {
		return nil, err
	}
	seq, err := s.nextEventSeq(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.st.AppendEvent(ctx, domain.Event{
		ProjectID: projectID, Seq: seq,
		ID: domain.ContentID(domain.KindEvent, map[string]any{"p": projectID, "seq": seq, "id": id}),
		Type: domain.EvtScenarios, CreatedAt: now,
		Payload: map[string]any{
			"scenario": sc.Key, "title": sc.Title, "entries": []map[string]string{{"id": id}},
			"status": outcome.Status, "scope": scope, "note": outcome.Note,
		},
	}); err != nil {
		return nil, err
	}
	return &entry, nil
}

func mergeSnap(custom map[string]any, ids map[string]string) map[string]any {
	out := map[string]any{"node_ids": ids}
	for k, v := range custom {
		out[k] = v
	}
	return out
}

func (s *Service) latestRules(ctx context.Context, projectID string) []domain.RuleEvent {
	events, _ := s.st.Events(ctx, projectID)
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Type == domain.EvtDerived {
			raw, _ := jsonMarshal(events[i].Payload["rules"])
			var rules []domain.RuleEvent
			if err := jsonUnmarshal2(raw, &rules); err == nil && len(rules) > 0 {
				return rules
			}
		}
	}
	return nil
}

var _ = fmt.Sprintf

// NodeIDs exposes current node ids for scenario before/after comparisons.
func (s *Service) NodeIDs(ctx context.Context, projectID string) map[string]string {
	return s.snapshotNodeIDs(ctx, projectID)
}
