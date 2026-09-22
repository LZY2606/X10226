package service

import (
	"context"
	"encoding/json"

	"tga/internal/derive"
	"tga/internal/domain"
	"tga/internal/store"
)

// DefaultParams returns the v1 parameter set pinned to the newest input layers.
func DefaultParams() domain.Params {
	return domain.Params{
		AlgorithmVersion: derive.AlgorithmVersion,
		WindowSize:       5,
		Prominence:       0.02,
		NoiseThreshold:   0.03,
		GapThreshold:     120,
		AtmosDelay:       45,
		TempLag:          60,
		OverlapSplit:     true,
		ForceSum100:      false,
	}
}

// ReviseParams stores a new parameter version with a parent relationship.
// Only derived results depending on the parameter version go stale.
func (s *Service) ReviseParams(ctx context.Context, projectID string, mutate func(domain.Params) domain.Params, reason string) (*domain.Params, error) {
	base := DefaultParams()
	if prev, err := s.st.LatestParams(ctx, projectID); err == nil && prev != nil {
		base = *prev
	}
	base = mutate(base)
	all, _ := s.st.AllParams(ctx, projectID)
	base.ProjectID = projectID
	base.Version = len(all) + 1
	base.Reason = reason
	if len(all) > 0 {
		base.ParentID = all[len(all)-1].ID
	}
	if im, _ := s.st.LatestRaw(ctx, projectID, domain.KindInitialMass); im != nil {
		base.InitialMassVersion = im.ID
	}
	if d, _ := s.st.LatestRaw(ctx, projectID, domain.KindDrift); d != nil {
		base.DriftVersion = d.ID
	}
	if b, _ := s.st.LatestRaw(ctx, projectID, domain.KindBlank); b != nil {
		base.BlankVersion = b.ID
	}
	base.Digest = domain.DigestOf(base)
	base.ID = domain.ParamID(projectID, base.Version, base.Digest)
	if _, err := s.st.CommitParams(ctx, base, s.clock()); err != nil {
		return nil, err
	}
	if err := s.invalidateDerived(ctx, projectID); err != nil {
		return nil, err
	}
	return &base, nil
}

// RunDerivation computes the current derived node from the newest raw layers
// and parameters. Identical inputs reproduce the same node ID (no duplicate);
// when inputs change a new node is created and the old one marked superseded.
func (s *Service) RunDerivation(ctx context.Context, projectID string) (*domain.DerivedNode, error) {
	pdef := DefaultParams()
	params := &pdef
	if p, err := s.st.LatestParams(ctx, projectID); err == nil && p != nil {
		params = p
	}
	raws := map[domain.SourceKind]domain.RawSubmission{}
	digests := map[domain.SourceKind]string{}
	for _, k := range []domain.SourceKind{domain.KindSamples, domain.KindDrift, domain.KindProgram, domain.KindGasEvent, domain.KindBlank, domain.KindInitialMass} {
		r, err := s.st.LatestRaw(ctx, projectID, k)
		if err != nil || r == nil {
			continue
		}
		raws[k] = *r
		digests[k] = r.Digest
	}
	if _, ok := raws[domain.KindSamples]; !ok {
		return nil, ErrNoSamples
	}
	in, err := derive.ParseInputs(raws)
	if err != nil {
		return nil, err
	}
	node := derive.Compute(projectID, params.ID, *params, in, digests)
	prev, _ := s.st.CurrentDerived(ctx, projectID)
	if _, _, err := s.st.CommitDerived(ctx, node, s.clock()); err != nil {
		return nil, err
	}
	if prev != nil && prev.ID != node.ID {
		_ = s.st.MarkSuperseded(ctx, prev.ID, node.ID)
	}
	return &node, nil
}

// invalidateDerived marks every currently-current derived node stale when a
// direct dependency changes. Stale nodes remain viewable but are not current.
func (s *Service) invalidateDerived(ctx context.Context, projectID string) error {
	nodes, err := s.st.AllDerived(ctx, projectID)
	if err != nil {
		return err
	}
	var ids []string
	latestKinds := map[domain.SourceKind]int{}
	for _, k := range []domain.SourceKind{domain.KindSamples, domain.KindDrift, domain.KindProgram, domain.KindGasEvent, domain.KindBlank, domain.KindInitialMass} {
		if r, err := s.st.LatestRaw(ctx, projectID, k); err == nil && r != nil {
			latestKinds[k] = r.Version
		}
	}
	latestParam, _ := s.st.LatestParams(ctx, projectID)
	for _, n := range nodes {
		if n.Stale || n.SupersededBy != "" {
			continue
		}
		stale := false
		for k, v := range n.DirectSources {
			if lv, ok := latestKinds[domain.SourceKind(k)]; ok && lv != v {
				stale = true
				break
			}
		}
		if latestParam != nil && n.ParamVersion != "" && n.ParamVersion != latestParam.ID {
			stale = true
		}
		if stale {
			ids = append(ids, n.ID)
		}
	}
	return s.st.MarkStale(ctx, projectID, "direct dependency changed; recompute for current result", ids)
}

// ExportProject returns the portable archive.
func (s *Service) ExportProject(ctx context.Context, projectID string) (json.RawMessage, error) {
	a, err := s.st.ExportArchive(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if _, err := s.st.Event(ctx, projectID, "export", map[string]any{"format": store.ArchiveFormat}, nil, s.clock()); err != nil {
		return nil, err
	}
	return json.Marshal(a)
}

// ImportProject restores an archive and verifies its chain.
func (s *Service) ImportProject(ctx context.Context, b []byte) (string, error) {
	var a store.Archive
	if err := json.Unmarshal(b, &a); err != nil {
		return "", err
	}
	if err := s.st.ImportArchive(ctx, &a); err != nil {
		return "", err
	}
	var p domain.Project
	if err := json.Unmarshal(a.Project, &p); err != nil {
		return "", err
	}
	if err := s.st.VerifyChain(ctx, p.ID); err != nil {
		return "", err
	}
	return p.ID, nil
}
