package domain

import (
	"encoding/json"
	"fmt"
)

func NewState(id, title, caseID string) *ProjectState {
	return &ProjectState{ID: id, Title: title, CaseID: caseID, Batches: map[string]RawBatch{}, Parameters: DefaultParameters()}
}

func DecodePayload(typ string, raw string) (any, error) {
	var p any
	switch typ {
	case "project.created":
		p = &ProjectCreated{}
	case "source.submitted":
		p = &SourceSubmitted{}
	case "parameter.revision.recorded":
		p = &ParameterRevisionRecorded{}
	case "evaluation.requested":
		p = &EvaluationRequested{}
	case "candidate.decision.recorded":
		p = &CandidateDecision{}
	case "candidate.decision.undone":
		p = &CandidateDecision{}
	case "conflict.recorded":
		p = &ConflictRecord{}
	case "conflict.resolved":
		p = &ConflictResolved{}
	case "principle.recorded":
		p = &PrincipleRecorded{}
	default:
		return nil, fmt.Errorf("unknown event type %q", typ)
	}
	if err := json.Unmarshal([]byte(raw), p); err != nil {
		return nil, err
	}
	return p, nil
}

func ApplyEvent(st *ProjectState, ev Event) (*Evaluation, error) {
	var produced *Evaluation
	switch p := ev.Payload.(type) {
	case ProjectCreated:
		st.ID = p.ID
		st.Title = p.Title
		st.CaseID = p.CaseID
	case *ProjectCreated:
		st.ID = p.ID
		st.Title = p.Title
		st.CaseID = p.CaseID
	case SourceSubmitted:
		b := p.Batch
		if b.Rows == nil {
			b.Rows = []map[string]string{}
		}
		st.Batches[b.ID] = b
		st.BatchOrder = append(st.BatchOrder, b.ID)
	case *SourceSubmitted:
		b := p.Batch
		if b.Rows == nil {
			b.Rows = []map[string]string{}
		}
		st.Batches[b.ID] = b
		st.BatchOrder = append(st.BatchOrder, b.ID)
	case ParameterRevisionRecorded:
		st.Parameters = p.Revision.Parameters
		st.ParameterHistory = append(st.ParameterHistory, p.Revision)
	case *ParameterRevisionRecorded:
		st.Parameters = p.Revision.Parameters
		st.ParameterHistory = append(st.ParameterHistory, p.Revision)
	case PrincipleRecorded:
		st.Principles = append(st.Principles, p.Principle)
	case *PrincipleRecorded:
		st.Principles = append(st.Principles, p.Principle)
	case EvaluationRequested:
		ev, _ := Evaluate(st, p, len(st.Evaluations)+1)
		st.Evaluations = append(st.Evaluations, ev)
		st.EvaluationRequests = append(st.EvaluationRequests, p)
		produced = &ev
	case *EvaluationRequested:
		out, _ := Evaluate(st, *p, len(st.Evaluations)+1)
		st.Evaluations = append(st.Evaluations, out)
		st.EvaluationRequests = append(st.EvaluationRequests, *p)
		produced = &out
	case CandidateDecision:
		st.Decisions = append(st.Decisions, p)
	case *CandidateDecision:
		st.Decisions = append(st.Decisions, *p)
	case ConflictRecord:
		st.Conflicts = append(st.Conflicts, p)
	case *ConflictRecord:
		st.Conflicts = append(st.Conflicts, *p)
	case ConflictResolved:
		applyConflictResolved(st, p.Conflict)
	case *ConflictResolved:
		applyConflictResolved(st, p.Conflict)
	}
	_ = produced
	return produced, nil
}

func applyConflictResolved(st *ProjectState, c ConflictRecord) {
	for i := range st.Conflicts {
		if st.Conflicts[i].ID == c.ID {
			st.Conflicts[i] = c
		}
	}
}
