// Package service is the event-sourced application layer. It rehydrates a
// project from the immutable event log, runs the versioned engine and applies
// decisions, and exposes the review view model.
package service

import (
	"encoding/json"

	"tga-lineage/internal/domain"
	"tga-lineage/internal/engine"
)

// Project is the replayed state of one project at a given head event.
type Project struct {
	ID        string
	Name      string
	CreatedAt string

	events      []domain.Event
	fragments   map[domain.FragmentKind]*domain.Fragment
	fragHistory map[domain.FragmentKind][]*domain.Fragment
	fragVersion map[domain.FragmentKind]int
	params      map[string]domain.Param
	paramVer    int
	paramFP     string
	runs        []*domain.RunRecord
	// allNodesByRun: nodes from each run keyed by run number.
	runNodes map[int][]domain.Node
	// currentNodeIndex: node id -> node from latest run that produced it.
	current map[string]domain.Node
	// stale: node id -> run where it became stale.
	staleAt map[string]int
	// manual nodes emitted via decisions, keyed by decision id.
	manualNodes map[int]domain.Node
	decisions   []*domain.Decision
	conflicts   []*domain.Conflict
	headEvent   int
}

func newProject() *Project {
	return &Project{
		fragments:   map[domain.FragmentKind]*domain.Fragment{},
		fragHistory: map[domain.FragmentKind][]*domain.Fragment{},
		fragVersion: map[domain.FragmentKind]int{},
		params:      engine.DefaultParams(),
		runNodes:    map[int][]domain.Node{},
		current:     map[string]domain.Node{},
		staleAt:    map[string]int{},
		manualNodes: map[int]domain.Node{},
	}
}

// Replay rebuilds a project from its event log.
func Replay(evs []domain.Event) *Project {
	p := newProject()
	for _, e := range evs {
		p.apply(e)
	}
	return p
}

// ReplayAt rebuilds state truncated at event id <= at (historical checkout).
func ReplayAt(evs []domain.Event, at int) *Project {
	p := newProject()
	for _, e := range evs {
		if e.ID > at {
			break
		}
		p.apply(e)
	}
	return p
}

func (p *Project) apply(e domain.Event) {
	p.headEvent = e.ID
	var pl domain.Payloads
	_ = json.Unmarshal(e.Payload, &pl)
	switch e.Kind {
	case domain.EvProjectCreated:
		p.ID = e.ProjectID
		p.Name = pl.Name
		p.CreatedAt = e.At
	case domain.EvFragmentAdded:
		if pl.Fragment != nil {
			f := *pl.Fragment
			p.fragments[f.Kind] = &f
			p.fragHistory[f.Kind] = append(p.fragHistory[f.Kind], &f)
			p.fragVersion[f.Kind] = f.Version
		}
	case domain.EvParamSet:
		if pl.ParamSet != nil {
			p.paramVer = pl.ParamSet.Version
			p.paramFP = pl.ParamSet.Fingerprint
			p.params = cloneParams(pl.ParamSet.Params)
		}
	case domain.EvDerived:
		if pl.Run != nil {
			p.applyRun(pl.Run)
		}
	case domain.EvDecision:
		if pl.Decision != nil {
			d := *pl.Decision
			p.decisions = append(p.decisions, &d)
			p.applyDecision(&d, e.ID)
		}
	case domain.EvDecisionUndo:
		if pl.Undo != nil {
			for _, d := range p.decisions {
				if d.ID == pl.Undo.DecisionID {
					d.Retracted = true
					d.RetractReason = pl.Undo.Reason
				}
			}
			delete(p.manualNodes, pl.Undo.DecisionID)
		}
	case domain.EvConflict:
		if pl.Conflict != nil {
			c := *pl.Conflict
			p.conflicts = append(p.conflicts, &c)
			if w, ok := p.current[c.Winner]; ok {
				w.Status = "accepted"
				p.current[c.Winner] = w
			}
			loser := c.NodeA
			if c.NodeA == c.Winner {
				loser = c.NodeB
			}
			if l, ok := p.current[loser]; ok {
				l.Status = "rejected_conflict"
				p.current[loser] = l
			}
		}
	case domain.EvNote:
	}
}

func (p *Project) applyRun(r *domain.RunRecord) {
	p.runs = append(p.runs, r)
	fresh := map[string]bool{}
	nodes := make([]domain.Node, len(r.Nodes))
	copy(nodes, r.Nodes)
	for i := range nodes {
		nodes[i].Status = initialStatus(nodes[i])
	}
	p.runNodes[r.Number] = nodes
	for _, n := range nodes {
		fresh[n.ID] = true
		if old, ok := p.current[n.ID]; ok {
			_ = old
		}
		p.current[n.ID] = n
	}
	// mark earlier-current nodes stale if they depend on a source the new run
	// supersedes and were not re-produced.
	newSources := map[string]bool{}
	for _, n := range nodes {
		for _, s := range n.SourceIDs {
			newSources[s] = true
		}
	}
	for id, n := range p.current {
		if fresh[id] {
			continue
		}
		for _, s := range n.SourceIDs {
			if sameSourceNewer(s, newSources) {
				p.staleAt[id] = r.Number
				delete(p.current, id)
				break
			}
		}
	}
	// re-apply non-retracted decisions that touched candidates that still exist
	for _, d := range p.decisions {
		if d.Retracted {
			continue
		}
		if _, exists := p.current[d.NodeID]; exists {
			p.applyDecision(d, 0)
		}
	}
}

func initialStatus(n domain.Node) string {
	if n.Status != "" {
		return n.Status
	}
	return "current"
}

// sameSourceNewer checks whether src (e.g. src:sample_points:v2) is superseded
// by an entry in fresh with the same source kind and equal-or-newer version.
func sameSourceNewer(src string, fresh map[string]bool) bool {
	kind, ver, ok := parseSourceTag(src)
	if !ok {
		return false
	}
	for f := range fresh {
		fk, fv, fok := parseSourceTag(f)
		if fok && fk == kind && fv >= ver && f != src {
			return true
		}
	}
	return false
}

func parseSourceTag(s string) (string, int, bool) {
	if len(s) < 5 || s[:4] != "src:" {
		return "", 0, false
	}
	rest := s[4:]
	i := len(rest) - 1
	for i >= 0 && rest[i] != ':' {
		i--
	}
	if i < 0 || i+2 >= len(rest) || rest[i+1] != 'v' {
		return "", 0, false
	}
	kind := rest[:i]
	v := 0
	for _, ch := range rest[i+2:] {
		if ch < '0' || ch > '9' {
			return "", 0, false
		}
		v = v*10 + int(ch-'0')
	}
	return kind, v, true
}

func cloneParams(in map[string]domain.Param) map[string]domain.Param {
	out := make(map[string]domain.Param, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
