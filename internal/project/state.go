package project

import (
	"sort"

	"tgareview/internal/domain"
	"tgareview/internal/engine"
)

// State is the projection of a project event prefix.
type State struct {
	ProjectID   string
	Name        string
	Description string
	Seq         int64

	HeadRev   map[domain.LayerKind]string // layer kind -> head rev id
	RevParent map[string]string           // rev id -> parent
	RevKind   map[string]domain.LayerKind

	Params       engine.Params
	ParamsRevID  string
	ParamsParent string

	// Artifacts produced up to this prefix, keyed by artifact id.
	Artifacts map[string]*engine.Artifact
	// Order of artifact creation (basis sequence).
	ArtifactOrder []string
	// BasisSeq maps artifact id to the seq after which it was produced.
	BasisSeq map[string]int64

	// Decisions: latest live decision per target id.
	Decisions map[string]*Decision
	// Decisions in historical order (including undone/superseded).
	DecisionLog []*Decision
}

// Decision is a human verdict.
type Decision struct {
	ID         string
	Seq        int64
	ArtifactID string
	TargetID   string
	Kind       string
	Value      string
	Rationale  string
	Supersedes string
	ConflictOf string
	Undone     bool
}

func newState(id, name, desc string) *State {
	return &State{
		ProjectID: id, Name: name, Description: desc,
		HeadRev:   map[domain.LayerKind]string{},
		RevParent: map[string]string{},
		RevKind:   map[string]domain.LayerKind{},
		Params:    engine.DefaultParams(),
		Artifacts: map[string]*engine.Artifact{},
		BasisSeq:  map[string]int64{},
		Decisions: map[string]*Decision{},
	}
}

// CurrentArtifactID returns the artifact computed from the current head basis
// and current params, if present in this projection.
func (s *State) CurrentArtifactID() string {
	if len(s.ArtifactOrder) == 0 {
		return ""
	}
	// ArtifactOrder is append-only; the last one matching the current basis is
	// the current artifact. Determinism guarantees at most one such id.
	want := s.BasisKey()
	for i := len(s.ArtifactOrder) - 1; i >= 0; i-- {
		id := s.ArtifactOrder[i]
		if s.Artifacts[id].ParamsFingerprint == s.Params.Fingerprint() && keyOf(s.Artifacts[id].Basis) == want {
			return id
		}
	}
	return ""
}

// BasisKey is the stable key of the current head revisions.
func (s *State) BasisKey() string {
	b := engine.Basis{}
	for k, v := range s.HeadRev {
		b[string(k)] = v
	}
	return keyOf(b)
}

func keyOf(b engine.Basis) string {
	keys := make([]string, 0, len(b))
	for k := range b {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for _, k := range keys {
		out += k + "=" + b[k] + ";"
	}
	return out
}

// IsStale reports whether an artifact was computed from anything other than
// the current head revisions / current params.
func (s *State) IsStale(art *engine.Artifact) bool {
	return art.ParamsFingerprint != s.Params.Fingerprint() || keyOf(art.Basis) != s.BasisKey()
}

// DecisionFor returns the live decision attached to a candidate/residue.
func (s *State) DecisionFor(targetID string) *Decision {
	d := s.Decisions[targetID]
	if d == nil || d.Undone {
		return nil
	}
	return d
}
