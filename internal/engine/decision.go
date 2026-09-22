package engine

import (
	"tgareview/internal/domain"
)

// applyDecision records verdicts, detects conflicts between incompatible human
// decisions and marks adjusted candidates. Nothing is overwritten.
func applyDecision(s *State, d domain.DecisionPayload, seq int64) error {
	if existing, ok := s.Decisions[d.TargetID]; ok && conflictsWith(existing, d) {
		cid := domain.ShortID("conflict", d.TargetID)
		c := domain.Conflict{
			ID: cid, Kind: d.TargetKind,
			TargetIDs: []string{existing.EventID, d.EventID},
			Open:      true, OpenedSeq: seq,
		}
		s.Conflicts[cid] = c
	}
	s.Decisions[d.TargetID] = d
	return nil
}

func conflictsWith(a, b domain.DecisionPayload) bool {
	switch {
	case a.Kind == domain.DecAcceptStage && b.Kind == domain.DecRejectStage:
		return true
	case a.Kind == domain.DecRejectStage && b.Kind == domain.DecAcceptStage:
		return true
	case a.Kind == domain.DecAcceptResidue && b.Kind == domain.DecRejectResidue:
		return true
	case a.Kind == domain.DecRejectResidue && b.Kind == domain.DecAcceptResidue:
		return true
	}
	return false
}
