package service

import (
	"tga-lineage/internal/domain"
	"tga-lineage/internal/engine"
)

// applyDecision mutates view statuses and emits manual-stage nodes. Conflicts
// are not resolved automatically here — the caller records a conflict event
// when two decisions contradict; statuses reflect the latest non-retracted
// decision until a conflict_resolved event chooses a winner.
func (p *Project) applyDecision(d *domain.Decision, eventID int) {
	n, ok := p.current[d.NodeID]
	if !ok {
		return
	}
	switch d.Action {
	case "accept":
		n.Status = "accepted"
		if n.Group != "" {
			collision := false
			for id, other := range p.current {
				if id != n.ID && other.Group == n.Group && other.Kind == n.Kind {
					if other.Status == "accepted" {
						other.Status = "conflicting"
						collision = true
					}
				}
			}
			if collision {
				n.Status = "conflicting"
			}
		}
		if d.Adjust != nil {
			manual := p.buildManual(n, d, eventID)
			p.manualNodes[d.ID] = manual
			n.Status = "superseded_by_manual"
		}
	case "reject":
		n.Status = "rejected"
	}
	p.current[n.ID] = n
}

func (p *Project) buildManual(parent domain.Node, d *domain.Decision, eventID int) domain.Node {
	var tg *domain.Node
	if t, ok := p.currentByKind("derived/drift_corrected"); ok {
		tg = &t
	}
	startT, endT := d.Adjust.StartT, d.Adjust.EndT
	if startT == 0 && endT == 0 {
		startT, _ = parent.Extra["start_t"].(float64)
		endT, _ = parent.Extra["end_t"].(float64)
	}
	m := engine.ManualStage(parent, tg, startT, endT, d.ID)
	return m
}

func (p *Project) currentByKind(kind string) (domain.Node, bool) {
	for _, n := range p.current {
		if n.Kind == kind {
			return n, true
		}
	}
	return domain.Node{}, false
}

// activeDecisions returns non-retracted decisions in chronological order.
func (p *Project) activeDecisions() []*domain.Decision {
	out := make([]*domain.Decision, 0, len(p.decisions))
	for _, d := range p.decisions {
		if !d.Retracted {
			out = append(out, d)
		}
	}
	return out
}

// manualList returns manual nodes in decision order.
func (p *Project) manualList() []domain.Node {
	var out []domain.Node
	for _, d := range p.activeDecisions() {
		if m, ok := p.manualNodes[d.ID]; ok {
			out = append(out, m)
		}
	}
	return out
}
