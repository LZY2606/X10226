package reduce

import (
	"encoding/json"
	"fmt"

	"tga-archive/internal/compute"
	"tga-archive/internal/domain"
)

// eventIDs lives on State; add it lazily to keep struct docs tidy.
// (declared in state_extras.go)

// AppendOutcome returns events that must be stored for one user action.
type AppendOutcome struct {
	Events    []domain.Event
	Snapshots []*Snapshot
}

// assignID gives an event a stable identity: a project-scoped monotonic seq
// plus a content hash, so re-imported archives keep identical ids.
func assignID(ev *domain.Event, projectID string, seq int) {
	ev.Seq = seq
	ev.ID = domain.ContentID("ev", projectID, seq, ev.Type, string(ev.Payload), ev.At)
}

// CreateProject mints project id and its first event from stable seed.
func CreateProject(name, seed, note string, clock Clock) (string, domain.Event) {
	pid := domain.ContentID("proj", name, seed)
	pinfo := domain.ProjectInfo{Name: name, ClockSeed: seed, Note: note}
	ev := domain.Event{
		Seq:     1,
		Type:    domain.EvProjectCreated,
		At:      clock.Now(),
		Payload: domain.Canonical(pinfo),
	}
	ev.ID = domain.ContentID("ev", pid, 1, ev.Type, string(ev.Payload), ev.At)
	return pid, ev
}

// current computes the derived result for a state.
func (s *State) current() compute.Result {
	return compute.Run(s.Rows, s.Params)
}

// applyBundle computes, via a provisional replay, whether the import changes
// any derived output; if so it returns the import event plus one snapshot.
func appendBundle(s *State, b domain.Bundle, clock Clock) (AppendOutcome, error) {
	if b.Kind == "revision" && b.ParentEvent == "" {
		return AppendOutcome{}, fmt.Errorf("revision requires parent event")
	}
	next := s.nextSeq()
	ev := domain.Event{
		Type: domain.EvBundleImported,
		At:   clock.Now(),
	}
	if b.ParentEvent != "" {
		ev.ParentSeq = s.seqOf(b.ParentEvent)
	}
	ev.Payload = domain.Canonical(b)
	assignID(&ev, s.ProjectID, next)

	// provisional state after only the import
	prov := s.fork()
	if err := prov.apply(ev); err != nil {
		return AppendOutcome{}, err
	}
	res := prov.current()
	key := snapKey(res)
	if prev, ok := prov.snapByKey[key]; ok {
		// identical derived output already exists; no new candidate snapshot,
		// but the import (incl. rejected fragments) is still recorded.
		_ = prev
		return AppendOutcome{Events: []domain.Event{ev}}, nil
	}

	direct := append([]string{}, res.RawRefs...)
	snap := &Snapshot{EventSeq: next + 1, Result: res, Direct: direct, ParamFP: res.ParamFP}
	snapEvent := domain.Event{
		Type:      domain.EvCandidateShown,
		At:        clock.Now(),
		ParentSeq: next,
		Payload:   domain.Canonical(snap),
	}
	assignID(&snapEvent, s.ProjectID, next+1)
	return AppendOutcome{Events: []domain.Event{ev, snapEvent}, Snapshots: []*Snapshot{snap}}, nil
}

// fork returns an independent copy of the state for provisional replay.
func (s *State) fork() *State {
	cp := NewState(s.ProjectID, s.Name)
	cp.Params = s.Params
	cp.LatestSeq = s.LatestSeq
	cp.eventIDs = append(cp.eventIDs, s.eventIDs...)
	for k := range s.rawSeen {
		cp.rawSeen[k] = true
	}
	cp.Rows = append([]compute.Row{}, s.Rows...)
	cp.Decisions = append([]*DecisionState{}, s.Decisions...)
	for k, v := range s.Artifacts {
		cp.Artifacts[k] = v
	}
	for _, sn := range s.Snaps {
		c := *sn
		cp.Snaps = append(cp.Snaps, &c)
		cp.snapByKey[snapKey(sn.Result)] = &c
	}
	return cp
}

// ApplyBundle is the package entry point.
func (s *State) ApplyBundle(b domain.Bundle, clock Clock) (AppendOutcome, error) {
	return appendBundle(s, b, clock)
}

// ReviseParams records a parameter revision and re-materializes candidates.
func (s *State) ReviseParams(p domain.Params, reason string, clock Clock) (AppendOutcome, error) {
	type payload struct {
		Params domain.Params `json:"params"`
		Reason string        `json:"reason"`
	}
	next := s.nextSeq()
	ev := domain.Event{Type: domain.EvParamsRevised, At: clock.Now(),
		Payload: domain.Canonical(payload{Params: p, Reason: reason})}
	assignID(&ev, s.ProjectID, next)

	prov := s.fork()
	if err := prov.apply(ev); err != nil {
		return AppendOutcome{}, err
	}
	res := prov.current()
	key := snapKey(res)
	out := AppendOutcome{Events: []domain.Event{ev}}
	if _, ok := prov.snapByKey[key]; ok {
		return out, nil
	}
	snap := &Snapshot{EventSeq: next + 1, Result: res, Direct: append([]string{}, res.RawRefs...), ParamFP: res.ParamFP}
	snapEvent := domain.Event{Type: domain.EvCandidateShown, At: clock.Now(), ParentSeq: next,
		Payload: domain.Canonical(snap)}
	assignID(&snapEvent, s.ProjectID, next+1)
	out.Events = append(out.Events, snapEvent)
	out.Snapshots = []*Snapshot{snap}
	return out, nil
}

// RecordDecision appends a human adjudication against candidate identities.
func (s *State) RecordDecision(d domain.Decision, clock Clock) (domain.Event, error) {
	if len(d.TargetIDs) == 0 {
		return domain.Event{}, fmt.Errorf("decision needs at least one target")
	}
	if d.Verdict != "accepted" && d.Verdict != "rejected" && d.Verdict != "adjusted" {
		return domain.Event{}, fmt.Errorf("invalid verdict %q", d.Verdict)
	}
	for _, t := range d.TargetIDs {
		for _, ds := range s.Decisions {
			if ds.Status == "active" && contains(ds.Decision.TargetIDs, t) && ds.Decision.Verdict != d.Verdict {
				d.ConflictWith = ds.ID
			}
		}
	}
	next := s.nextSeq()
	ev := domain.Event{Type: domain.EvDecisionMade, At: clock.Now(), Payload: domain.Canonical(d)}
	assignID(&ev, s.ProjectID, next)
	s.Decisions = append(s.Decisions, &DecisionState{EventSeq: next, ID: ev.ID, Decision: d, Status: "active"})
	return ev, nil
}

// ResolveConflict keeps both contradictory decisions but marks one lost.
func (s *State) ResolveConflict(c domain.ConflictResolution, clock Clock) (domain.Event, error) {
	var lost int
	for i, ds := range s.Decisions {
		if ds.ID == c.LostID {
			lost = i
		}
	}
	foundW, foundL := false, false
	for _, ds := range s.Decisions {
		if ds.ID == c.WonID {
			foundW = true
		}
		if ds.ID == c.LostID {
			foundL = true
		}
	}
	if !foundW {
		return domain.Event{}, fmt.Errorf("winning decision not found")
	}
	if !foundL {
		return domain.Event{}, fmt.Errorf("losing decision not found")
	}
	next := s.nextSeq()
	ev := domain.Event{Type: domain.EvConflictResolved, At: clock.Now(), Payload: domain.Canonical(c)}
	assignID(&ev, s.ProjectID, next)
	s.Decisions[lost].Status = "lost"
	s.Decisions[lost].LostTo = next
	return ev, nil
}

// UndoDecision reverses a decision without deleting any history.
func (s *State) UndoDecision(u domain.Undo, clock Clock) (domain.Event, error) {
	var ds *DecisionState
	for _, x := range s.Decisions {
		if x.ID == u.DecisionEventID {
			ds = x
		}
	}
	if ds == nil {
		return domain.Event{}, fmt.Errorf("unknown decision")
	}
	next := s.nextSeq()
	ev := domain.Event{Type: domain.EvDecisionUndone, At: clock.Now(), Payload: domain.Canonical(u)}
	assignID(&ev, s.ProjectID, next)
	ds.Status = "undone"
	ds.UndoneBy = next
	return ev, nil
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// decodeSnapshot is a small helper retained for tests/debugging.
func decodeSnapshot(raw json.RawMessage) (*Snapshot, error) {
	var sn Snapshot
	if err := json.Unmarshal(raw, &sn); err != nil {
		return nil, err
	}
	return &sn, nil
}
