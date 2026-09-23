package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"tga-lineage/internal/clocks"
	"tga-lineage/internal/domain"
	"tga-lineage/internal/engine"
	"tga-lineage/internal/importer"
	"tga-lineage/internal/store"
)

// Service is the application facade over the event store.
type Service struct {
	Store *store.Store
	Clock clocks.Clock
}

func New(st *store.Store, c clocks.Clock) *Service {
	if c == nil {
		c = clocks.Real{}
	}
	return &Service{Store: st, Clock: c}
}

type FormatError struct{ Msg string }

func (e *FormatError) Error() string { return e.Msg }

// CreateProject appends the project_created seed event.
func (s *Service) CreateProject(ctx context.Context, id, name string) error {
	if _, err := s.loadOrErr(ctx, id); err == nil {
		return fmt.Errorf("project %q already exists", id)
	}
	return s.append(ctx, id, domain.EvProjectCreated, domain.Payloads{Name: name})
}

// AddFragment parses and commits one layer version, then reruns derivation.
// A structural format error returns *FormatError and commits nothing.
func (s *Service) AddFragment(ctx context.Context, projectID string, kind domain.FragmentKind, filename string, r io.Reader) (*domain.Fragment, error) {
	p, err := s.loadOrErr(ctx, projectID)
	if err != nil {
		return nil, err
	}
	frag, perr := importer.Parse(kind, filename, r)
	if perr != nil {
		if importer.IsFormatError(perr) {
			return nil, &FormatError{Msg: perr.Error()}
		}
		return nil, perr
	}
	frag.Version = p.fragVersion[kind] + 1
	if err := s.append(ctx, projectID, domain.EvFragmentAdded, domain.Payloads{Fragment: frag}); err != nil {
		return nil, err
	}
	if err := s.derive(ctx, projectID); err != nil {
		return nil, err
	}
	return frag, nil
}

// SetParams records a parameter revision (new version + parent) and reruns.
func (s *Service) SetParams(ctx context.Context, projectID string, params map[string]domain.Param, reason string) error {
	p, err := s.loadOrErr(ctx, projectID)
	if err != nil {
		return err
	}
	merged := cloneParams(p.params)
	for k, v := range params {
		merged[k] = v
	}
	ps := &domain.ParamSet{
		Version: p.paramVer + 1, Parent: p.paramVer,
		Params: merged, Fingerprint: engine.Fingerprint(merged), Reason: reason,
	}
	if err := s.append(ctx, projectID, domain.EvParamSet, domain.Payloads{ParamSet: ps}); err != nil {
		return err
	}
	return s.derive(ctx, projectID)
}

// DecideRequest is a reviewer accept/reject/adjust.
type DecideRequest struct {
	NodeID string
	Action string
	Reason string
	Adjust *domain.Adjustment
}

// Decide records a decision. Two accepts inside one candidate group also write
// a conflict_resolved event (the newest accept initially wins; the winner can
// be changed with ResolveConflict).
func (s *Service) Decide(ctx context.Context, projectID string, req DecideRequest) (*domain.Decision, error) {
	p, err := s.loadOrErr(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if _, ok := p.findNode(req.NodeID); !ok {
		return nil, fmt.Errorf("node %q not found", req.NodeID)
	}
	switch req.Action {
	case "accept", "reject":
	default:
		return nil, fmt.Errorf("unknown action %q", req.Action)
	}
	d := &domain.Decision{
		ID: nextDecisionID(p), NodeID: req.NodeID, Action: req.Action,
		Reason: req.Reason, Adjust: req.Adjust,
	}
	if err := s.append(ctx, projectID, domain.EvDecision, domain.Payloads{Decision: d}); err != nil {
		return nil, err
	}
	// conflict detection for mutually-exclusive accepted candidates
	if req.Action == "accept" {
		winnerNode, _ := p.findNode(req.NodeID)
		grp := winnerNode.Group
		if grp != "" {
			for _, ad := range p.activeDecisions() {
				if ad.NodeID == req.NodeID {
					continue
				}
				an, ok := p.findNode(ad.NodeID)
				if !ok || an.Group != grp {
					continue
				}
				cf := &domain.Conflict{
					ID: nextConflictID(p), NodeA: an.ID, NodeB: req.NodeID,
					Winner: req.NodeID,
					Reason: "新决定与已接受候选冲突；暂以最新接受为准，可在冲突面板重新裁定" + reasonSuffix2(req.Reason),
				}
				if err := s.append(ctx, projectID, domain.EvConflict, domain.Payloads{Conflict: cf}); err != nil {
					return nil, err
				}
			}
		}
	}
	return d, nil
}

// Undo retracts a decision (the retraction stays in history).
func (s *Service) Undo(ctx context.Context, projectID string, decisionID int, reason string) error {
	if _, err := s.loadOrErr(ctx, projectID); err != nil {
		return err
	}
	return s.append(ctx, projectID, domain.EvDecisionUndo,
		domain.Payloads{Undo: &domain.UndoRecord{DecisionID: decisionID, Reason: reason}})
}

// ResolveConflict records a human resolution between two nodes.
func (s *Service) ResolveConflict(ctx context.Context, projectID string, nodeA, nodeB, winner, reason string) error {
	p, err := s.loadOrErr(ctx, projectID)
	if err != nil {
		return err
	}
	cf := &domain.Conflict{ID: nextConflictID(p), NodeA: nodeA, NodeB: nodeB, Winner: winner,
		Reason: reason}
	return s.append(ctx, projectID, domain.EvConflict, domain.Payloads{Conflict: cf})
}

// Note appends a free-form review note.
func (s *Service) Note(ctx context.Context, projectID, text string) error {
	if _, err := s.loadOrErr(ctx, projectID); err != nil {
		return err
	}
	return s.append(ctx, projectID, domain.EvNote, domain.Payloads{Text: text})
}

func (s *Service) derive(ctx context.Context, projectID string) error {
	p, err := s.loadOrErr(ctx, projectID)
	if err != nil {
		return err
	}
	runNo := len(p.runs) + 1
	nodes := engine.Run(engine.Input{Fragments: p.fragments, Params: p.params})
	// annotate node derived event only after we know id; compute stale ids:
	// simulate which current nodes are superseded (service state pre-run)
	stale := []string{}
	newSources := map[string]bool{}
	for _, n := range nodes {
		for _, t := range n.SourceIDs {
			newSources[t] = true
		}
	}
	fresh := map[string]bool{}
	for _, n := range nodes {
		fresh[n.ID] = true
	}
	for id, n := range p.current {
		if fresh[id] {
			continue
		}
		for _, src := range n.SourceIDs {
			if sameSourceNewer(src, newSources) {
				stale = append(stale, id)
				break
			}
		}
	}
	rec := &domain.RunRecord{
		Number: runNo, Algorithm: engine.AlgorithmVersion,
		ParamFP: engine.Fingerprint(p.params), Nodes: nodes, StaleIDs: stale,
	}
	evID, err := s.appendGetID(ctx, projectID, domain.EvDerived, domain.Payloads{Run: rec})
	if err != nil {
		return err
	}
	rec.EventID = evID
	return nil
}

func nextDecisionID(p *Project) int {
	return len(p.decisions) + 1
}

func nextConflictID(p *Project) int {
	return len(p.conflicts) + 1
}

func reasonSuffix2(r string) string {
	if r == "" {
		return ""
	}
	return "（" + r + "）"
}

// View rebuilds the read model (optionally at a historical event id).
func (s *Service) View(ctx context.Context, projectID string, at int) (*ViewModel, []domain.Event, error) {
	evs, err := s.Store.Events(ctx, projectID, 0)
	if err != nil {
		return nil, nil, err
	}
	var p *Project
	if at > 0 {
		p = ReplayAt(evs, at)
	} else {
		p = Replay(evs)
	}
	p.headEvent = atOrHead(evs, at)
	vm := p.View(evs)
	vm.Historical = at > 0 && at < headID(evs)
	return vm, evs, nil
}

func atOrHead(evs []domain.Event, at int) int {
	if at > 0 {
		return at
	}
	return headID(evs)
}

func headID(evs []domain.Event) int {
	if len(evs) == 0 {
		return 0
	}
	return evs[len(evs)-1].ID
}

// Chain builds the calculation chain at current head or a past event.
func (s *Service) Chain(ctx context.Context, projectID, nodeID string, at int) ([]domain.ChainItem, error) {
	evs, err := s.Store.Events(ctx, projectID, 0)
	if err != nil {
		return nil, err
	}
	p := ReplayAt(evs, at)
	return p.Chain(nodeID), nil
}

func (s *Service) append(ctx context.Context, projectID string, kind domain.EventKind, pl domain.Payloads) error {
	_, err := s.appendGetID(ctx, projectID, kind, pl)
	return err
}

func (s *Service) appendGetID(ctx context.Context, projectID string, kind domain.EventKind, pl domain.Payloads) (int, error) {
	b, err := json.Marshal(pl)
	if err != nil {
		return 0, err
	}
	maxID, err := s.Store.MaxEventID(ctx, projectID)
	if err != nil {
		return 0, err
	}
	e := domain.Event{ID: maxID + 1, ProjectID: projectID, Kind: kind, At: s.Clock.Now().Format("2006-01-02T15:04:05.000Z"), Payload: b}
	return e.ID, s.Store.AppendEvent(ctx, e)
}

func (s *Service) loadOrErr(ctx context.Context, id string) (*Project, error) {
	evs, err := s.Store.Events(ctx, id, 0)
	if err != nil {
		return nil, err
	}
	if len(evs) == 0 {
		return nil, fmt.Errorf("project %q not found", id)
	}
	return Replay(evs), nil
}
