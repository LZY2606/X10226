package engine

import "tga/internal/model"

// FirstKind returns the newest current artifact of a kind.
func (s *State) FirstKind(kind string) *model.Artifact {
	var out *model.Artifact
	for _, id := range s.ArtifactIDs {
		a := s.Artifacts[id]
		if a.Kind == kind && a.Status == "current" {
			out = a
		}
	}
	return out
}

// AllKind returns all current artifacts of a kind in order.
func (s *State) AllKind(kind string) []*model.Artifact {
	var out []*model.Artifact
	for _, id := range s.ArtifactIDs {
		a := s.Artifacts[id]
		if a.Kind == kind && a.Status == "current" {
			out = append(out, a)
		}
	}
	return out
}

// CorrectedRows extracts the corrected series rows.
func (s *State) CorrectedRows() []CorrectedPoint {
	a := s.FirstKind("corrected_series")
	if a == nil {
		return nil
	}
	if rows, ok := a.Data["rows"].([]CorrectedPoint); ok {
		return rows
	}
	return nil
}

// DTGPoints extracts the DTG point slice.
func (s *State) DTGPoints() []DTGPoint {
	a := s.FirstKind("dtg_series")
	if a == nil {
		return nil
	}
	if pp, ok := a.Data["points"].([]DTGPoint); ok {
		return pp
	}
	return nil
}
