package service

import (
	"context"
	"encoding/json"

	"tga/internal/domain"
)

// DecideRequest is a human verdict issued from the review page.
type DecideRequest struct {
	TargetType  string // "phase" | "residue"
	TargetID    string
	Verdict     domain.DecisionVerdict
	Note        string
	Adjustments map[string]any
}

// Decide records a human decision pinned to the exact derived node version
// currently displayed. A second, opposite verdict on the same target becomes a
// first-class conflict; nothing is silently overwritten. Undo is itself an
// audited event.
func (s *Service) Decide(ctx context.Context, projectID string, req DecideRequest) (*domain.Decision, error) {
	cur, err := s.st.CurrentDerived(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, ErrNoSamples
	}
	if !targetExists(*cur, req.TargetType, req.TargetID) {
		return nil, ErrTargetNotFound
	}
	decs, err := s.st.AllDecisions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	seq := len(decs) + 1
	conflictWith := ""
	for _, d := range decs {
		if d.UndoneBy != "" {
			continue
		}
		if d.TargetID == req.TargetID && d.TargetType == req.TargetType && opposite(d.Verdict, req.Verdict) {
			conflictWith = d.ID
			break
		}
	}
	id := domain.DecisionID(projectID, seq, req.TargetID)
	verdict := req.Verdict
	if conflictWith != "" && req.Verdict != domain.VerdictAdjust {
		verdict = domain.VerdictConflict
	}
	d := domain.Decision{
		ID: id, ProjectID: projectID, Seq: seq,
		TargetType: req.TargetType, TargetID: req.TargetID, Verdict: verdict,
		Note: req.Note, Adjustments: req.Adjustments, ConflictWith: conflictWith,
		DerivedID: cur.ID,
	}
	if _, err := s.st.CommitDecision(ctx, d, s.clock()); err != nil {
		return nil, err
	}
	// reflect status on the candidate in a new derived snapshot only when a
	// non-conflict accept/reject is made; conflicts leave candidates proposed.
	if verdict == domain.VerdictAccept || verdict == domain.VerdictReject {
		if err := s.applyCandidateStatus(ctx, projectID, req.TargetType, req.TargetID, verdict, id); err != nil {
			return nil, err
		}
	}
	return &d, nil
}

// Undo marks a prior decision undone and records an undo history event.
func (s *Service) Undo(ctx context.Context, projectID, decisionID, note string) error {
	decs, err := s.st.AllDecisions(ctx, projectID)
	if err != nil {
		return err
	}
	var target *domain.Decision
	for i := range decs {
		if decs[i].ID == decisionID {
			target = &decs[i]
		}
	}
	if target == nil {
		return ErrTargetNotFound
	}
	undoID := domain.DecisionID(projectID, len(decs)+1, decisionID+":undo")
	if _, err := s.st.UndoDecision(ctx, projectID, decisionID, undoID, note, s.clock()); err != nil {
		return err
	}
	return nil
}

// applyCandidateStatus creates a new derived snapshot carrying the human
// status, preserving the prior snapshot immutably.
func (s *Service) applyCandidateStatus(ctx context.Context, projectID, ttype, tid string, v domain.DecisionVerdict, decID string) error {
	cur, err := s.st.CurrentDerived(ctx, projectID)
	if err != nil {
		return err
	}
	if cur == nil {
		return nil
	}
	updated := *cur
	status := domain.CandAccepted
	if v == domain.VerdictReject {
		status = domain.CandRejected
	}
	switch ttype {
	case "phase":
		for i := range updated.Phases {
			if updated.Phases[i].ID == tid {
				updated.Phases[i].Status = status
				updated.Phases[i].DecisionIDs = append(updated.Phases[i].DecisionIDs, decID)
				if v == domain.VerdictReject {
					updated.Phases[i].RejectedReason = "rejected by reviewer"
				}
			}
		}
	case "residue":
		for i := range updated.Residues {
			if updated.Residues[i].ID == tid {
				updated.Residues[i].Status = status
				updated.Residues[i].DecisionIDs = append(updated.Residues[i].DecisionIDs, decID)
			}
		}
	}
	updated.ID = cur.ID + ":h"
	for {
		exists := false
		all, _ := s.st.AllDerived(ctx, projectID)
		for _, n := range all {
			if n.ID == updated.ID {
				exists = true
			}
		}
		if !exists {
			break
		}
		updated.ID += "h"
	}
	updated.Digest = domain.DigestOf(struct {
		Base   domain.DerivedNode
		Status string
	}{*cur, string(status) + ":" + decID})
	if _, _, err := s.st.CommitDerived(ctx, updated, s.clock()); err != nil {
		return err
	}
	return s.st.MarkSuperseded(ctx, cur.ID, updated.ID)
}

func targetExists(n domain.DerivedNode, ttype, id string) bool {
	switch ttype {
	case "phase":
		for _, p := range n.Phases {
			if p.ID == id {
				return true
			}
		}
	case "residue":
		for _, r := range n.Residues {
			if r.ID == id {
				return true
			}
		}
	}
	return false
}

func opposite(a, b domain.DecisionVerdict) bool {
	return (a == domain.VerdictAccept && b == domain.VerdictReject) ||
		(a == domain.VerdictReject && b == domain.VerdictAccept)
}

var _ = json.Marshal
