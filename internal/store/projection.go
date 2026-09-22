// Package store persists the append-only event log, raw source versions and
// derived nodes in SQLite, and folds events into a reviewable projection.
package store

import (
	"encoding/json"
	"fmt"
	"sort"

	"tga-review/internal/domain"
)

// State is a fold of the event log. Fold again up to a chosen sequence to
// revisit any past state; the fold is deterministic and never mutates the log.
type State struct {
	Project       *domain.Project
	Params        map[string]domain.Params // all revisions
	CurrentParams string
	Sources       map[string]map[string]*domain.SourceVersion // layer -> versionID -> version
	LayerCurrent  map[string]string                            // layer -> newest version id
	Diagnostics   []domain.Diagnostic
	Sessions      map[string]*domain.ImportSession
	Derived       map[string]*domain.DerivedNode
	Batches       map[string]*BatchMeta
	NodeBatch     map[string]string
	CurrentBatch  string
	Decisions     map[string]*domain.DecisionRecord
	Conflicts     []*domain.ConflictRecord
	ConflictByTarget map[string]*domain.ConflictRecord
	Events        []domain.Event
	UndoneEvents  map[string]string // event id -> undo event id
}

// BatchMeta summarizes one derivation batch.
type BatchMeta struct {
	ID           string
	EventSeq     int64
	ParamsID     string
	NodeIDs      []string
	Supersedes   []string
	Note         string
}

func NewState() *State {
	return &State{
		Params: map[string]domain.Params{},
		Sources: map[string]map[string]*domain.SourceVersion{},
		LayerCurrent: map[string]string{},
		Sessions: map[string]*domain.ImportSession{},
		Derived: map[string]*domain.DerivedNode{},
		Batches: map[string]*BatchMeta{},
		NodeBatch: map[string]string{},
		Decisions: map[string]*domain.DecisionRecord{},
		ConflictByTarget: map[string]*domain.ConflictRecord{},
		UndoneEvents: map[string]string{},
	}
}

// Fold applies one event to the state.
func (s *State) Fold(e domain.Event) error {
	s.Events = append(s.Events, e)
	switch e.Type {
	case domain.EvProjectCreated:
		var p domain.ProjectCreated
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		s.Project = &domain.Project{ID: e.ProjectID, Name: p.Name, Description: p.Description, Labels: p.Labels}
	case domain.EvImportBegan:
		var p domain.ImportBegan
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		s.Sessions[p.SessionID] = &domain.ImportSession{ID: p.SessionID, Status: domain.ImportBeganState, Layers: p.Layers, BeganAt: e.At}
	case domain.EvLayerCommitted:
		// SourceVersion payloads are loaded from the versions table on demand;
		// here we track order/current pointers from the event payload.
		var p domain.LayerCommitted
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if s.Sources[p.Layer] == nil {
			s.Sources[p.Layer] = map[string]*domain.SourceVersion{}
		}
		// mark previously current version superseded (not deleted)
		if old := s.LayerCurrent[p.Layer]; old != "" && old != p.VersionID {
			if v := s.Sources[p.Layer][old]; v != nil {
				v.Accepted = false
			}
		}
		s.LayerCurrent[p.Layer] = p.VersionID
	case domain.EvImportConcluded:
		var p domain.ImportConcluded
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		sess := s.Sessions[p.SessionID]
		if sess == nil {
			sess = &domain.ImportSession{ID: p.SessionID}
			s.Sessions[p.SessionID] = sess
		}
		sess.Status = p.Status
		sess.LayersOK = p.LayersOK
		sess.LayersBad = p.LayersBad
		sess.EndedAt = e.At
		sess.Summary = p.Summary
	case domain.EvParamsRevised:
		var p domain.ParamsRevised
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		// full Params object is stored in params table; the log records identity
		s.CurrentParams = p.ParamsID
	case domain.EvDerivedProduced:
		var p domain.DerivedProduced
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		s.Batches[p.BatchID] = &BatchMeta{ID: p.BatchID, EventSeq: e.Seq, ParamsID: p.Fingerprint.Hash(), NodeIDs: p.NodeIDs, Supersedes: p.Supersedes, Note: p.Note}
		s.CurrentBatch = p.BatchID
		for _, id := range p.NodeIDs {
			s.NodeBatch[id] = p.BatchID
		}
	case domain.EvDecision:
		var p domain.Decision
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		s.Decisions[e.ID] = &domain.DecisionRecord{ID: e.ID, EventID: e.ID, Seq: e.Seq, TargetType: p.TargetType, TargetID: p.TargetID, Action: p.Action, ParamsID: p.ParamsID, Reason: p.Reason, At: e.At, Actor: e.Actor}
		s.checkConflict(e.ID, p)
	case domain.EvConflictRaised:
		var p domain.ConflictRaised
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		c := &domain.ConflictRecord{ID: e.ID, EventID: e.ID, TargetID: p.TargetID, DecisionIDs: p.DecisionIDs, Reason: p.Reason}
		s.Conflicts = append(s.Conflicts, c)
		s.ConflictByTarget[p.TargetID] = c
	case domain.EvConflictResolved:
		var p domain.ConflictResolved
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		if c := s.ConflictByTarget[p.TargetID]; c != nil {
			c.Resolved = true
			c.WinnerDecision = p.WinnerDecision
			c.ResolutionNote = p.Note
		}
	case domain.EvUndo:
		var p domain.UndoPayload
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			return err
		}
		s.UndoneEvents[p.UndoneEventID] = e.ID
		if d := s.Decisions[p.UndoneEventID]; d != nil {
			d.Undone = true
		}
	case domain.EvProjectImported:
		// identity-preserving restore: nothing to fold beyond presence
	}
	return nil
}

// checkConflict raises a logical conflict when a new decision contradicts the
// latest non-undone decision on the same target (accept then reject, etc.).
func (s *State) checkConflict(eventID string, d domain.Decision) {
	var prior *domain.DecisionRecord
	for _, rec := range s.Decisions {
		if rec.Undone || rec.TargetID != d.TargetID || rec.TargetType != d.TargetType {
			continue
		}
		if prior == nil || rec.Seq > prior.Seq {
			prior = rec
		}
	}
	if prior != nil && contradicts(prior.Action, d.Action) {
		c := &domain.ConflictRecord{
			ID: "implicit-" + eventID, EventID: eventID, TargetID: d.TargetID,
			DecisionIDs: []string{prior.ID, eventID},
			Reason: fmt.Sprintf("%s then %s on same target", prior.Action, d.Action),
		}
		s.Conflicts = append(s.Conflicts, c)
		s.ConflictByTarget[d.TargetID] = c
	}
}

func contradicts(a, b string) bool {
	if a == b {
		return false
	}
	return (a == "accept" || a == "reject") && (b == "accept" || b == "reject")
}

// DecisionFor returns the latest non-undone decision for a target.
func (s *State) DecisionFor(targetID string) *domain.DecisionRecord {
	var best *domain.DecisionRecord
	for _, d := range s.Decisions {
		if d.Undone || d.TargetID != targetID {
			continue
		}
		if best == nil || d.Seq > best.Seq {
			best = d
		}
	}
	return best
}

// IsNodeCurrent reports whether a derived node belongs to the newest batch.
// Stale nodes remain fully viewable but must never be presented as current.
func (s *State) IsNodeCurrent(nodeID string) bool {
	bid := s.NodeBatch[nodeID]
	return bid != "" && bid == s.CurrentBatch
}

// SortedBatches returns batches oldest first.
func (s *State) SortedBatches() []*BatchMeta {
	out := make([]*BatchMeta, 0, len(s.Batches))
	for _, b := range s.Batches {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventSeq < out[j].EventSeq })
	return out
}

// AsOf folds only events with seq <= seq (inclusive), enabling history revisit.
func LoadStateAt(events []domain.Event, seq int64) (*State, error) {
	s := NewState()
	for _, e := range events {
		if e.Seq > seq {
			break
		}
		if err := s.Fold(e); err != nil {
			return nil, err
		}
	}
	return s, nil
}
