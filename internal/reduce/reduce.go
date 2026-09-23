// Package reduce is the event-sourced state machine. Raw inputs are append
// only; revisions are new events with parent links. Derived artifacts are
// deterministic snapshots (candidate.shown): changing one direct source only
// materializes a new snapshot for results that actually changed, and old
// snapshots stay readable as history, marked superseded rather than deleted.
package reduce

import (
	"encoding/json"
	"fmt"
	"sort"

	"tga-archive/internal/compute"
	"tga-archive/internal/domain"
)

// Clock is the virtual time source used when appending events.
type Clock interface{ Now() string }

// Snapshot is one materialized derived result with direct-source fingerprint.
type Snapshot struct {
	EventSeq     int            `json:"event_seq"`
	Result       compute.Result `json:"result"`
	Direct       []string       `json:"direct_sources"`
	ParamFP      string         `json:"param_fp"`
	SupersededBy int            `json:"superseded_by,omitempty"`
}

// DecisionState is the lifecycle view of one adjudication.
type DecisionState struct {
	EventSeq int             `json:"event_seq"`
	ID       string          `json:"id"`
	Decision domain.Decision `json:"decision"`
	Status   string          `json:"status"` // active | undone | lost
	UndoneBy int             `json:"undone_by,omitempty"`
	LostTo   int             `json:"lost_to,omitempty"`
}

// Artifact tracks a stable node identity across history.
type Artifact struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	FirstSeq  int    `json:"first_seq"`
	LatestSeq int    `json:"latest_seq"`
}

// State is the reduced view at one point in the log.
type State struct {
	ProjectID string
	Name      string
	Params    domain.Params
	Rows      []compute.Row
	rawSeen   map[string]bool

	Snaps     []*Snapshot
	snapByKey map[string]*Snapshot
	Decisions []*DecisionState
	Artifacts map[string]Artifact
	LatestSeq int
	eventIDs  []string
}

// NewState returns an empty state for a project.
func NewState(projectID, name string) *State {
	return &State{
		ProjectID: projectID,
		Name:      name,
		Params:    domain.DefaultParams(),
		rawSeen:   map[string]bool{},
		snapByKey: map[string]*Snapshot{},
		Artifacts: map[string]Artifact{},
	}
}

// Replay rebuilds a state from its stored events deterministically.
func Replay(events []domain.Event) (*State, error) {
	if len(events) == 0 {
		return nil, fmt.Errorf("no events")
	}
	var info domain.ProjectInfo
	if err := json.Unmarshal(events[0].Payload, &info); err != nil {
		return nil, err
	}
	pid := domain.ContentID("proj", info.Name, info.ClockSeed)
	s := NewState(pid, info.Name)
	for _, ev := range events[1:] {
		if err := s.apply(ev); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *State) nextSeq() int { return s.LatestSeq + 1 }

func (s *State) seqOf(eventID string) int {
	if eventID == "" {
		return 0
	}
	for seq, id := range s.eventIDs {
		if id == eventID {
			return seq
		}
	}
	return 0
}

func (s *State) apply(ev domain.Event) error {
	switch ev.Type {
	case domain.EvProjectCreated:
		s.LatestSeq = ev.Seq
		s.eventIDs = append(s.eventIDs, ev.ID)
	case domain.EvBundleImported:
		var b domain.Bundle
		if err := json.Unmarshal(ev.Payload, &b); err != nil {
			return err
		}
		s.ingestRows(b)
		s.LatestSeq = ev.Seq
		s.eventIDs = append(s.eventIDs, ev.ID)
	case domain.EvParamsRevised:
		var pl struct {
			Params domain.Params `json:"params"`
		}
		if err := json.Unmarshal(ev.Payload, &pl); err != nil {
			return err
		}
		s.Params = pl.Params
		s.LatestSeq = ev.Seq
		s.eventIDs = append(s.eventIDs, ev.ID)
	case domain.EvCandidateShown:
		var snap Snapshot
		if err := json.Unmarshal(ev.Payload, &snap); err != nil {
			return err
		}
		snap.EventSeq = ev.Seq
		cp := snap
		s.Snaps = append(s.Snaps, &cp)
		s.indexArtifact(&cp)
		s.LatestSeq = ev.Seq
		s.eventIDs = append(s.eventIDs, ev.ID)
	case domain.EvDecisionMade:
		var d domain.Decision
		if err := json.Unmarshal(ev.Payload, &d); err != nil {
			return err
		}
		s.Decisions = append(s.Decisions, &DecisionState{
			EventSeq: ev.Seq, ID: ev.ID, Decision: d, Status: "active",
		})
		s.LatestSeq = ev.Seq
		s.eventIDs = append(s.eventIDs, ev.ID)
	case domain.EvConflictResolved:
		var c domain.ConflictResolution
		if err := json.Unmarshal(ev.Payload, &c); err != nil {
			return err
		}
		for _, ds := range s.Decisions {
			if ds.ID == c.LostID {
				ds.Status = "lost"
				ds.LostTo = ev.Seq
			}
		}
		s.LatestSeq = ev.Seq
		s.eventIDs = append(s.eventIDs, ev.ID)
	case domain.EvDecisionUndone:
		var u domain.Undo
		if err := json.Unmarshal(ev.Payload, &u); err != nil {
			return err
		}
		for _, ds := range s.Decisions {
			if ds.ID == u.DecisionEventID {
				ds.Status = "undone"
				ds.UndoneBy = ev.Seq
			}
		}
		s.LatestSeq = ev.Seq
		s.eventIDs = append(s.eventIDs, ev.ID)
	default:
		return fmt.Errorf("unknown event type %q", ev.Type)
	}
	return nil
}

func (s *State) ingestRows(b domain.Bundle) {
	order := len(s.Rows)
	for _, r := range b.Rows {
		if !r.Accepted {
			continue
		}
		id := domain.RowID(r.Stratum, r.Raw, r.LineNo)
		if s.rawSeen[id] {
			continue
		}
		s.rawSeen[id] = true
		s.Rows = append(s.Rows, compute.Row{
			ID: id, Stratum: r.Stratum, LineNo: r.LineNo, Order: order, Fields: r.Fields,
		})
		order++
	}
}

func snapKey(res compute.Result) string {
	// Identity of the *derived output*: algo + parameter fingerprint + every
	// node id, sorted. Same inputs produce the same key, so a local change to
	// one direct source only yields a new key for results that actually moved.
	ids := make([]string, 0, len(res.Curve)+len(res.Stages)+len(res.Residues)+len(res.DTG)+len(res.Peaks)+len(res.GasSpans)+len(res.Gaps)+len(res.Program))
	for _, c := range res.Curve {
		ids = append(ids, c.Node.ID)
	}
	for _, d := range res.DTG {
		ids = append(ids, d.Node.ID)
	}
	for _, p := range res.Peaks {
		ids = append(ids, p.Node.ID)
	}
	for _, g := range res.GasSpans {
		ids = append(ids, g.Node.ID)
	}
	for _, g := range res.Gaps {
		ids = append(ids, g.Node.ID)
	}
	for _, p := range res.Program {
		ids = append(ids, p.Node.ID)
	}
	for _, st := range res.Stages {
		ids = append(ids, st.ID)
	}
	for _, r := range res.Residues {
		ids = append(ids, r.Node.ID)
	}
	for _, j := range res.Judgments {
		ids = append(ids, j.Code+j.Message)
	}
	sort.Strings(ids)
	return res.AlgoVersion + "|" + res.ParamFP + "|" + domain.ContentID("snapkey", ids)
}

func (s *State) indexArtifact(snap *Snapshot) {
	all := s.nodesOf(snap.Result)
	for id, kind := range all {
		a, ok := s.Artifacts[id]
		if !ok {
			a = Artifact{ID: id, Kind: kind, FirstSeq: snap.EventSeq}
		}
		a.LatestSeq = snap.EventSeq
		s.Artifacts[id] = a
	}
}

func (s *State) nodesOf(res compute.Result) map[string]string {
	out := map[string]string{}
	for _, c := range res.Curve {
		out[c.Node.ID] = "curve"
	}
	for _, d := range res.DTG {
		out[d.Node.ID] = "dtg"
	}
	for _, p := range res.Peaks {
		out[p.Node.ID] = "peak"
	}
	for _, g := range res.GasSpans {
		out[g.Node.ID] = "gas"
	}
	for _, g := range res.Gaps {
		out[g.Node.ID] = "gap"
	}
	for _, p := range res.Program {
		out[p.Node.ID] = "program"
	}
	for _, st := range res.Stages {
		out[st.ID] = "stage"
		out[st.Node.ID] = "stage-node"
	}
	for _, r := range res.Residues {
		out[r.Node.ID] = "residue"
		for _, n := range r.Chain {
			out[n.ID] = n.Kind
		}
	}
	return out
}
