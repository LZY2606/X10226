package store

import (
	"context"
	"encoding/json"
	"fmt"

	"tga/internal/domain"
)

// RawsByKind returns all versions of one source kind, oldest first.
func (s *Store) RawsByKind(ctx context.Context, projectID string, kind domain.SourceKind) ([]domain.RawSubmission, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,kind,version,COALESCE(parent_id,''),payload,digest,COALESCE(import_ref,'') FROM raw_sources WHERE project_id=? AND kind=? ORDER BY version`, projectID, string(kind))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.RawSubmission
	for rows.Next() {
		var r domain.RawSubmission
		var payload string
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Kind, &r.Version, &r.ParentID, &payload, &r.Digest, &r.ImportRef); err != nil {
			return nil, err
		}
		r.Payload = json.RawMessage(payload)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LatestRaw returns the highest version of a kind (nil rows if none).
func (s *Store) LatestRaw(ctx context.Context, projectID string, kind domain.SourceKind) (*domain.RawSubmission, error) {
	all, err := s.RawsByKind(ctx, projectID, kind)
	if err != nil || len(all) == 0 {
		return nil, err
	}
	r := all[len(all)-1]
	return &r, nil
}

// AllRaws returns every raw source grouped order by kind then version.
func (s *Store) AllRaws(ctx context.Context, projectID string) ([]domain.RawSubmission, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,kind,version,COALESCE(parent_id,''),payload,digest,COALESCE(import_ref,'') FROM raw_sources WHERE project_id=? ORDER BY kind,version`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.RawSubmission
	for rows.Next() {
		var r domain.RawSubmission
		var payload string
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Kind, &r.Version, &r.ParentID, &payload, &r.Digest, &r.ImportRef); err != nil {
			return nil, err
		}
		r.Payload = json.RawMessage(payload)
		out = append(out, r)
	}
	return out, rows.Err()
}

// AllParams returns all parameter versions oldest first.
func (s *Store) AllParams(ctx context.Context, projectID string) ([]domain.Params, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT doc FROM param_versions WHERE project_id=? ORDER BY version`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Params
	for rows.Next() {
		var doc string
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		var p domain.Params
		if err := json.Unmarshal([]byte(doc), &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// LatestParams returns the newest parameter version.
func (s *Store) LatestParams(ctx context.Context, projectID string) (*domain.Params, error) {
	all, err := s.AllParams(ctx, projectID)
	if err != nil || len(all) == 0 {
		return nil, err
	}
	p := all[len(all)-1]
	return &p, nil
}

// AllDerived returns derived nodes ordered by sequence.
func (s *Store) AllDerived(ctx context.Context, projectID string) ([]domain.DerivedNode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT doc,stale,COALESCE(stale_reason,''),COALESCE(superseded_by,'') FROM derived_nodes WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DerivedNode
	for rows.Next() {
		var doc string
		var stale int
		var reason, sup string
		if err := rows.Scan(&doc, &stale, &reason, &sup); err != nil {
			return nil, err
		}
		var d domain.DerivedNode
		if err := json.Unmarshal([]byte(doc), &d); err != nil {
			return nil, err
		}
		d.Stale = stale == 1
		d.StaleReason = reason
		d.SupersededBy = sup
		out = append(out, d)
	}
	return out, rows.Err()
}

// CurrentDerived returns the newest non-stale, non-superseded node, if any.
func (s *Store) CurrentDerived(ctx context.Context, projectID string) (*domain.DerivedNode, error) {
	all, err := s.AllDerived(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for i := len(all) - 1; i >= 0; i-- {
		if !all[i].Stale && all[i].SupersededBy == "" {
			return &all[i], nil
		}
	}
	return nil, nil
}

// AllDecisions returns decisions in sequence order.
func (s *Store) AllDecisions(ctx context.Context, projectID string) ([]domain.Decision, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT doc FROM decisions WHERE project_id=? ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Decision
	for rows.Next() {
		var doc string
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		var d domain.Decision
		if err := json.Unmarshal([]byte(doc), &d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// VerifyChain recomputes the history hash chain and reports the first break.
func (s *Store) VerifyChain(ctx context.Context, projectID string) error {
	hist, err := s.History(ctx, projectID)
	if err != nil {
		return err
	}
	prev := ""
	for _, h := range hist {
		want := domain.HistoryHash(h.Seq, h.ProjectID, h.Type, []byte(h.Payload), prev)
		if want != h.Hash {
			return fmt.Errorf("history chain broken at seq %d", h.Seq)
		}
		if h.PrevHash != prev {
			return fmt.Errorf("history prev mismatch at seq %d", h.Seq)
		}
		prev = h.Hash
	}
	return nil
}
