package app

import (
	"context"

	"tga-review/internal/domain"
	"tga-review/internal/ingest"
)

// AdjustParams stores a new parameter version (parent fingerprint retained),
// emits a decision and recomputes. Only downstream derived nodes expire.
func (s *Service) AdjustParams(ctx context.Context, projectID string, p domain.Params, rationale string) (string, error) {
	cur, curFP, err := s.st.CurrentParams(ctx, projectID)
	if err != nil {
		return "", err
	}
	p.Version = cur.Version + 1
	p.ParentFP = curFP
	fp := p.Fingerprint()
	now := s.now()
	if err := s.st.InsertParams(ctx, projectID, p.Version, fp, curFP, p, now); err != nil {
		return "", err
	}
	d, err := s.addDecision(ctx, projectID, domain.DecisionAdjustParams, "params",
		map[string]any{"fingerprint": fp, "version": p.Version, "params": p}, rationale)
	if err != nil {
		return "", err
	}
	if _, _, err := s.recompute(ctx, projectID, "params:"+d.ID); err != nil {
		return "", err
	}
	return fp, nil
}

// ReviseInitialMass commits a new initial_mass source revision (the old layer
// is preserved with a parent link) and recomputes. R6: only mass-ratio nodes
// (dtg normalization and residue) can change; corrected mass is unaffected.
func (s *Service) ReviseInitialMass(ctx context.Context, projectID, raw, format, rationale string) error {
	res := ingest.Parse(domain.LayerInitMass, format, raw)
	if res.Fatal {
		return errFromDiag(res.Diag)
	}
	now := s.now()
	prev, _ := s.st.CurrentSource(ctx, projectID, domain.LayerInitMass)
	version, parent := 1, ""
	if prev != nil {
		version, parent = prev.Version+1, prev.ID
	}
	src := domain.Source{
		ProjectID: projectID, Layer: domain.LayerInitMass, Version: version, ParentID: parent,
		Format: normalizeFormat(format), Raw: raw, Payload: res.Payload, Diag: res.Diag,
		Accepted: true, CreatedAt: now,
	}
	src.ID = domain.ContentID(domain.KindSource, map[string]any{
		"project": projectID, "layer": string(domain.LayerInitMass), "version": version,
		"parent": parent, "raw": raw, "payload_fingerprint": domain.Fingerprint(res.Payload),
	})
	if err := s.st.InsertSource(ctx, src); err != nil {
		return err
	}
	if _, err := s.addDecision(ctx, projectID, domain.DecisionReviseInitMass, src.ID,
		map[string]any{"source_id": src.ID, "version": version, "parent_id": parent}, rationale); err != nil {
		return err
	}
	if _, _, err := s.recompute(ctx, projectID, "initial_mass:"+src.ID); err != nil {
		return err
	}
	return nil
}

// ReviseLayer commits a new revision of any raw layer (used by the counter
// example archive to change exactly one direct source).
func (s *Service) ReviseLayer(ctx context.Context, projectID string, layer domain.SourceLayer, raw, format, rationale string) (*domain.Source, []domain.Diag, error) {
	res := ingest.Parse(layer, format, raw)
	if res.Fatal {
		return nil, res.Diag, errFromDiag(res.Diag)
	}
	now := s.now()
	prev, _ := s.st.CurrentSource(ctx, projectID, layer)
	version, parent := 1, ""
	if prev != nil {
		version, parent = prev.Version+1, prev.ID
	}
	src := domain.Source{
		ProjectID: projectID, Layer: layer, Version: version, ParentID: parent,
		Format: normalizeFormat(format), Raw: raw, Payload: res.Payload, Diag: res.Diag,
		Accepted: true, CreatedAt: now,
	}
	src.ID = domain.ContentID(domain.KindSource, map[string]any{
		"project": projectID, "layer": string(layer), "version": version,
		"parent": parent, "raw": raw, "payload_fingerprint": domain.Fingerprint(res.Payload),
	})
	if err := s.st.InsertSource(ctx, src); err != nil {
		return nil, res.Diag, err
	}
	if _, _, err := s.recompute(ctx, projectID, "revise:"+src.ID); err != nil {
		return nil, res.Diag, err
	}
	return &src, res.Diag, nil
}

func (s *Service) ConfirmResidue(ctx context.Context, projectID, rationale string) error {
	open, err := s.openConflicts(ctx, projectID)
	if err != nil {
		return err
	}
	if len(open) > 0 {
		return errFromDiag([]domain.Diag{{Code: "E_CONFLICT", Severity: "error", Message: "resolve open conflicts before completing review"}})
	}
	if _, err := s.addDecision(ctx, projectID, domain.DecisionConfirmResidue, "residue", nil, rationale); err != nil {
		return err
	}
	if _, _, err := s.recompute(ctx, projectID, "confirm_residue"); err != nil {
		return err
	}
	seq, err := s.nextEventSeq(ctx, projectID)
	if err != nil {
		return err
	}
	now := s.now()
	return s.st.AppendEvent(ctx, domain.Event{
		ProjectID: projectID, Seq: seq,
		ID: domain.ContentID(domain.KindEvent, map[string]any{"p": projectID, "seq": seq, "type": domain.EvtReviewComplete}),
		Type: domain.EvtReviewComplete, CreatedAt: now,
		Payload: map[string]any{"rationale": rationale},
	})
}

func (s *Service) openConflicts(ctx context.Context, projectID string) ([]domain.Conflict, error) {
	all, err := s.st.AllConflicts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var out []domain.Conflict
	for _, c := range all {
		if c.Status == "open" {
			out = append(out, c)
		}
	}
	return out, nil
}

type appErr struct{ msg string }

func (e *appErr) Error() string { return e.msg }

func errFromDiag(diags []domain.Diag) error {
	for _, d := range diags {
		if d.Severity == "error" {
			return &appErr{msg: d.Code + ": " + d.Message}
		}
	}
	return &appErr{msg: "invalid input"}
}
