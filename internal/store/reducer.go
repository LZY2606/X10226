package store

import (
	"encoding/json"

	"tga/internal/domain"
)

func (s *Store) reduce(p *Project, ev *domain.Event) error {
	switch ev.Type {
	case "project_created":
		p.Name, _ = ev.Payload["name"].(string)
		p.CreatedAt = ev.At
	case "source_committed":
		var src domain.Source
		if err := remarshal(ev.Payload["source"], &src); err != nil {
			return err
		}
		src.CommittedAt = ev.At
		p.Sources[src.Layer] = append(p.Sources[src.Layer], &src)
	case "import_batch_recorded":
		var b ImportBatch
		if err := remarshal(ev.Payload["batch"], &b); err != nil {
			return err
		}
		b.CommittedAt = ev.At
		p.Batches = append(p.Batches, &b)
	case "params_added":
		var pr domain.Params
		if err := remarshal(ev.Payload["params"], &pr); err != nil {
			return err
		}
		p.Params[pr.Fingerprint] = pr
	case "analysis_created":
		var a domain.Analysis
		if err := remarshal(ev.Payload["analysis"], &a); err != nil {
			return err
		}
		a.CreatedAt = ev.At
		p.Analyses = append(p.Analyses, &a)
	case "decision_made":
		var d domain.Decision
		if err := remarshal(ev.Payload["decision"], &d); err != nil {
			return err
		}
		d.AtEventSeq = ev.Seq
		d.CreatedAt = ev.At
		p.Decisions = append(p.Decisions, &d)
	case "decision_undone":
		target, _ := ev.Payload["decision_id"].(string)
		for _, d := range p.Decisions {
			if d.ID == target {
				d.Undone = true
				d.UndoneBy, _ = ev.Payload["undo_id"].(string)
			}
		}
	case "project_deleted":
		// projection marker; row deletion happens via event-time filter
		p.Name = ""
	}
	return nil
}

func remarshal(v any, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// EffectiveDecision returns the latest non-undone decision for a target.
func (p *Project) EffectiveDecision(targetID string) *domain.Decision {
	var d *domain.Decision
	for _, dec := range p.Decisions {
		if dec.TargetID == targetID && !dec.Undone {
			d = dec
		}
	}
	return d
}

// DecisionHistory returns the full adjudication trail for a target.
func (p *Project) DecisionHistory(targetID string) []*domain.Decision {
	var out []*domain.Decision
	for _, dec := range p.Decisions {
		if dec.TargetID == targetID {
			out = append(out, dec)
		}
	}
	return out
}
