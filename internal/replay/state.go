package replay

import (
	"sort"

	"tga-review/internal/domain"
)

// State is one projected view of the event stream (current or as-of a seq).
type State struct {
	ProjectID  string
	Name       string
	AsOfSeq    int64
	Sources    map[string]*domain.Source // latest version per stratum
	AllSources []*domain.Source
	Nodes      map[string]*domain.Node
	NodeOrder  []string
	CurrentIDs map[string]bool // nodes derived from the current source set
	Judgments  []domain.Judgment
	Decisions  []domain.Decision
	Conflicts  []ConflictView
	ImportBatches []ImportReport
	CreatedTS  string
}

// ConflictView pairs mutually exclusive adjudications with their resolution.
type ConflictView struct {
	Key          string   `json:"key"`
	EventIDs     []string `json:"event_ids"`
	NodeKeys     []string `json:"node_keys"`
	Message      string   `json:"message"`
	ResolvedBy   string   `json:"resolved_by,omitempty"`
	Resolution   string   `json:"resolution,omitempty"`
	Open         bool     `json:"open"`
	Seq          int64    `json:"seq"`
}

// ImportReport records an import attempt, including partial failures.
type ImportReport struct {
	EventID        string            `json:"event_id"`
	Seq            int64             `json:"seq"`
	VirtualTS      string            `json:"virtual_ts"`
	AcceptedKinds  []string          `json:"accepted_kinds"`
	Rejected       []RejectedRecord  `json:"rejected"`
	Diagnostics    map[string][]string `json:"diagnostics,omitempty"`
}

// RejectedRecord keeps the rejected input fragment in place with a reason.
type RejectedRecord struct {
	Stratum string `json:"stratum"`
	Index   int    `json:"index,omitempty"`
	Raw     string `json:"raw,omitempty"`
	Reason  string `json:"reason"`
}

// OrderedNodes returns nodes in stable creation/group order.
func (s *State) OrderedNodes() []*domain.Node {
	out := make([]*domain.Node, 0, len(s.NodeOrder))
	for _, id := range s.NodeOrder {
		if n, ok := s.Nodes[id]; ok {
			out = append(out, n)
		}
	}
	return out
}

// SourceVersion returns the current version number of a stratum.
func (s *State) SourceVersion(kind string) int {
	if src := s.Sources[kind]; src != nil {
		return src.Version
	}
	return 0
}

func sortStrata(ks []string) []string {
	out := append([]string{}, ks...)
	sort.SliceStable(out, func(i, j int) bool {
		return rank(out[i]) < rank(out[j])
	})
	return out
}

func rank(k string) int {
	for i, x := range domain.StratumKinds {
		if x == k {
			return i
		}
	}
	return len(domain.StratumKinds)
}
