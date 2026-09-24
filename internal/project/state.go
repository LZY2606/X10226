// Package project is the application service: it turns commands into journal
// events, materialises state by replaying the log, marks stale derived results
// without deleting them, and implements time travel to any past event.
package project

import (
	"encoding/json"
	"sort"

	"tga/internal/domain"
	"tga/internal/pipeline"
	"tga/internal/store"
)

type SourceCommit struct {
	Version int                       `json:"version"`
	Parent  int                       `json:"parent"`
	Partial bool                      `json:"partial"`
	Status  string                    `json:"status"` // committed / partial / rejected
	Layers  *domain.ImportedLayers    `json:"layers"`
	Reason  string                    `json:"reason,omitempty"`
	RefIDs  map[string]string         `json:"ref_ids"`
}

type ParamsCommit struct {
	Version int                    `json:"version"`
	Parent  int                    `json:"parent"`
	Params  domain.AnalysisParams  `json:"params"`
	Reason  string                 `json:"reason"`
}

type AnalysisRef struct {
	AnalysisID string `json:"analysis_id"`
	SourceV    int    `json:"source_version"`
	ParamsV    int    `json:"params_version"`
	Seq        int64  `json:"seq"`
}

type Decision struct {
	ID         string `json:"id"`
	Candidate  string `json:"candidate"`
	AnalysisID string `json:"analysis_id"`
	Action     string `json:"action"` // accept / reject / adjust
	Note       string `json:"note"`
	Seq        int64  `json:"seq"`
	Active     bool   `json:"active"`
	Supersedes string `json:"supersedes,omitempty"`
	UndoOf     string `json:"undo_of,omitempty"`
}

type Adjustment struct {
	CandidateID string             `json:"candidate_id"`
	Params      *domain.AnalysisParams `json:"params,omitempty"`
	Stages      []AdjustedStage    `json:"stages,omitempty"`
	Note        string             `json:"note"`
}

type AdjustedStage struct {
	Label  string   `json:"label"`
	StartS *float64 `json:"start_s,omitempty"`
	EndS   *float64 `json:"end_s,omitempty"`
}

// State is a materialisation at some seq.
type State struct {
	Seq          int64
	Name         string
	Deleted      bool
	Sources      []SourceCommit
	Params       []ParamsCommit
	Analyses     []AnalysisRef
	Decisions    []Decision
	Conflict     *Conflict
}

type Conflict struct {
	Seq       int64  `json:"seq"`
	Earlier   string `json:"earlier_decision"`
	Later     string `json:"later_decision"`
	Candidate string `json:"candidate_group"`
	Resolution string `json:"resolution"` // superseded / flagged
}

func currentSource(st *State) *SourceCommit {
	for i := len(st.Sources) - 1; i >= 0; i-- {
		if st.Sources[i].Status == "committed" || st.Sources[i].Status == "partial" {
			return &st.Sources[i]
		}
	}
	return nil
}

func currentParams(st *State) *ParamsCommit {
	if len(st.Params) == 0 {
		return nil
	}
	return &st.Params[len(st.Params)-1]
}

// Materialise replays events up to upToSeq (0 = head).
func Materialise(evs []store.EventRow, upToSeq int64) (*State, error) {
	st := &State{}
	for _, e := range evs {
		if upToSeq > 0 && e.Seq > upToSeq {
			break
		}
		st.Seq = e.Seq
		switch e.Kind {
		case domain.EvtProjectCreated:
			var p struct{ Name string `json:"name"` }
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				return nil, err
			}
			st.Name = p.Name
		case domain.EvtProjectDeleted:
			st.Deleted = true
		case domain.EvtSourceCommitted:
			var sc SourceCommit
			if err := json.Unmarshal(e.Payload, &sc); err != nil {
				return nil, err
			}
			st.Sources = append(st.Sources, sc)
		case domain.EvtParamsRevised:
			var pc ParamsCommit
			if err := json.Unmarshal(e.Payload, &pc); err != nil {
				return nil, err
			}
			st.Params = append(st.Params, pc)
		case domain.EvtAnalysisRun:
			var a AnalysisRef
			if err := json.Unmarshal(e.Payload, &a); err != nil {
				return nil, err
			}
			a.Seq = e.Seq
			st.Analyses = append(st.Analyses, a)
		case domain.EvtDecisionMade:
			var d Decision
			if err := json.Unmarshal(e.Payload, &d); err != nil {
				return nil, err
			}
			d.Seq = e.Seq
			d.Active = true
			if d.Supersedes != "" {
				for i := range st.Decisions {
					if st.Decisions[i].ID == d.Supersedes {
						st.Decisions[i].Active = false
					}
				}
			}
			st.Decisions = append(st.Decisions, d)
		case domain.EvtDecisionUndone:
			var d Decision
			if err := json.Unmarshal(e.Payload, &d); err != nil {
				return nil, err
			}
			d.Seq = e.Seq
			for i := range st.Decisions {
				if st.Decisions[i].ID == d.UndoOf {
					st.Decisions[i].Active = false
				}
			}
			// reactivate whatever the undone decision had superseded
			for i := range st.Decisions {
				if st.Decisions[i].ID == d.UndoOf && st.Decisions[i].Supersedes != "" {
					for j := range st.Decisions {
						if st.Decisions[j].ID == st.Decisions[i].Supersedes {
							st.Decisions[j].Active = true
						}
					}
				}
			}
		case domain.EvtConflictResolved:
			var c Conflict
			if err := json.Unmarshal(e.Payload, &c); err != nil {
				return nil, err
			}
			c.Seq = e.Seq
			st.Conflict = &c
		}
	}
	return st, nil
}

// CandidateDecision returns the active decision for a candidate analysis, if any.
func (st *State) CandidateDecision(analysisID, candidateID string) *Decision {
	for i := len(st.Decisions) - 1; i >= 0; i-- {
		d := st.Decisions[i]
		if d.Active && d.AnalysisID == analysisID && d.Candidate == candidateID {
			return &st.Decisions[i]
		}
	}
	return nil
}

// DecisionOnAnalysis returns any active accept on the same analysis (used to
// detect conflicting accepts between competing candidates).
func (st *State) ActiveAccepts(analysisID string) []Decision {
	var out []Decision
	for _, d := range st.Decisions {
		if d.Active && d.AnalysisID == analysisID && d.Action == "accept" {
			out = append(out, d)
		}
	}
	return out
}

// SortedDecisions is a convenience for views.
func (st *State) SortedDecisions() []Decision {
	out := make([]Decision, len(st.Decisions))
	copy(out, st.Decisions)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

var _ = pipeline.DefaultParams
