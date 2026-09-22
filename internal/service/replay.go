package service

import (
	"context"
	"encoding/json"
	"sort"

	"tga/internal/domain"
)

// ReplayView is the materialized state visible at one point in history.
type ReplayView struct {
	AtSeq       int64                             `json:"at_seq"`
	Event       *domain.HistoryEvent              `json:"event"`
	RawVersions map[string][]domain.RawSubmission `json:"raw_versions"`
	Params      []domain.Params                   `json:"params"`
	Derived     []domain.DerivedNode              `json:"derived"`
	Decisions   []domain.Decision                 `json:"decisions"`
}

// Replay reconstructs the exact visible state up to and including the history
// event at seq. It never recomputes with newest inputs; stored derived nodes
// are filtered by their creation event so the conclusions of the time are
// restored with their original dependency versions.
func (s *Service) Replay(ctx context.Context, projectID string, seq int64) (*ReplayView, error) {
	hist, err := s.st.History(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if seq <= 0 || seq > int64(len(hist)) {
		seq = int64(len(hist))
	}
	view := &ReplayView{AtSeq: seq, RawVersions: map[string][]domain.RawSubmission{}}

	created := map[string]int64{} // layer object ID -> created at seq
	paramAt := map[string]int64{}
	derivedAt := map[string]int64{}
	decisionAt := map[string]int64{}
	undoneAt := map[string]int64{}

	for i := int64(0); i < seq; i++ {
		h := hist[i]
		switch h.Type {
		case "raw_committed":
			var pl struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(h.Payload, &pl)
			created[pl.ID] = h.Seq
		case "params_committed":
			var pl struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(h.Payload, &pl)
			paramAt[pl.ID] = h.Seq
		case "derived":
			var pl struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(h.Payload, &pl)
			if _, seen := derivedAt[pl.ID]; !seen {
				derivedAt[pl.ID] = h.Seq
			}
		case "decision", "conflict":
			var d domain.Decision
			_ = json.Unmarshal(h.Payload, &d)
			if _, seen := decisionAt[d.ID]; !seen {
				decisionAt[d.ID] = h.Seq
			}
		case "undo":
			var pl struct {
				UndoneDecision string `json:"undone_decision"`
			}
			_ = json.Unmarshal(h.Payload, &pl)
			undoneAt[pl.UndoneDecision] = h.Seq
		}
	}
	view.Event = &hist[seq-1]

	raws, _ := s.st.AllRaws(ctx, projectID)
	for _, r := range raws {
		if created[r.ID] != 0 && created[r.ID] <= seq {
			view.RawVersions[string(r.Kind)] = append(view.RawVersions[string(r.Kind)], r)
		}
	}
	for k := range view.RawVersions {
		sort.Slice(view.RawVersions[k], func(a, b int) bool { return view.RawVersions[k][a].Version < view.RawVersions[k][b].Version })
	}
	params, _ := s.st.AllParams(ctx, projectID)
	for _, p := range params {
		if paramAt[p.ID] != 0 && paramAt[p.ID] <= seq {
			view.Params = append(view.Params, p)
		}
	}
	derived, _ := s.st.AllDerived(ctx, projectID)
	for _, d := range derived {
		if derivedAt[d.ID] != 0 && derivedAt[d.ID] <= seq {
			// at this point in time nothing was stale yet unless explicitly
			// superseded by an earlier derived snapshot
			view.Derived = append(view.Derived, d)
		}
	}
	decs, _ := s.st.AllDecisions(ctx, projectID)
	for _, d := range decs {
		if decisionAt[d.ID] != 0 && decisionAt[d.ID] <= seq {
			if u, ok := undoneAt[d.ID]; ok && u <= seq {
				continue // undo already effective at this point
			}
			view.Decisions = append(view.Decisions, d)
		}
	}
	return view, nil
}
