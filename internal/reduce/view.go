package reduce

import (
	"sort"

	"tga-archive/internal/compute"
)

// CurrentSnapshot returns the newest materialized derived snapshot.
func (s *State) CurrentSnapshot() *Snapshot {
	if len(s.Snaps) == 0 {
		return nil
	}
	return s.Snaps[len(s.Snaps)-1]
}

// SnapshotAt returns the newest snapshot materialized at or before seq.
func SnapshotAt(snaps []*Snapshot, seq int) *Snapshot {
	var best *Snapshot
	for _, sn := range snaps {
		if sn.EventSeq <= seq {
			best = sn
		}
	}
	return best
}

// Staleness describes whether a value visible in an old snapshot is current.
type Staleness struct {
	ValueID    string `json:"value_id"`
	Kind       string `json:"kind"`
	FirstSeq   int    `json:"first_seq"`
	LatestSeq  int    `json:"latest_seq"`
	CurrentSeq int    `json:"current_seq"`
}

// IsCurrent reports whether the node identity still exists in the newest
// snapshot. Expired values remain viewable but are labelled stale.
func (s *State) IsCurrent(id string) (Staleness, bool) {
	a, ok := s.Artifacts[id]
	if !ok {
		return Staleness{}, false
	}
	cur := 0
	if len(s.Snaps) > 0 {
		cur = s.Snaps[len(s.Snaps)-1].EventSeq
	}
	return Staleness{ValueID: id, Kind: a.Kind, FirstSeq: a.FirstSeq,
		LatestSeq: a.LatestSeq, CurrentSeq: cur}, a.LatestSeq == cur
}

// DirectDependents reports current artifacts whose provenance includes the
// given raw source id: the "affected objects" list for a changed direct
// source. Unrelated results are not invalidated.
func DirectDependents(sn *Snapshot, rawID string) []string {
	if sn == nil {
		return nil
	}
	var out []string
	for _, c := range sn.Result.Curve {
		if refsContain(c.Node.Sources, rawID) {
			out = append(out, c.Node.ID)
		}
	}
	for _, st := range sn.Result.Stages {
		if refsContain(stageSources(st), rawID) {
			out = append(out, st.ID)
		}
	}
	for _, r := range sn.Result.Residues {
		for _, n := range r.Chain {
			if refsContain(n.Sources, rawID) {
				out = append(out, n.ID)
			}
		}
	}
	sort.Strings(out)
	return out
}

func refsContain(refs []compute.Ref, rawID string) bool {
	for _, r := range refs {
		if r.ID == rawID {
			return true
		}
	}
	return false
}

func stageSources(st compute.StageCandidate) []compute.Ref { return st.Sources }

// EventIDs returns the project-scoped event id order (for tests).
func (s *State) EventIDs() []string { return append([]string{}, s.eventIDs...) }
