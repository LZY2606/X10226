package app

import (
	"context"
	"encoding/json"

	"tga-review/internal/domain"
)

// BundleFormat is the portable archive. Ids, event sequence and parent links
// are preserved verbatim, so deleting the local database and importing this
// bundle reconstructs identity, order and provenance.
const BundleFormat = "tga-review-bundle"

type Bundle struct {
	Format       string        `json:"format"`
	Version      int           `json:"version"`
	ProjectID    string        `json:"project_id"`
	ProjectName  string        `json:"project_name"`
	ExportedAt   string        `json:"exported_at"`
	Events       []domain.Event `json:"events"`
	Batches      []BatchPayload `json:"batches"`
}

// BatchPayload carries the original submission bytes plus layer outcomes so a
// re-import reproduces the same stratified rows and batch ids.
type BatchPayload struct {
	Batch  domain.Batch `json:"batch"`
	Source string       `json:"source"` // original text/json as imported
}

// Export builds the portable archive.
func (s *Service) Export(ctx context.Context, projectID string) (*Bundle, error) {
	name, err := s.Store.ProjectName(ctx, projectID)
	if err != nil {
		return nil, err
	}
	events, err := s.Store.Events(ctx, projectID)
	if err != nil {
		return nil, err
	}
	batches, err := s.Store.Batches(ctx, projectID)
	if err != nil {
		return nil, err
	}
	bp := make([]BatchPayload, 0, len(batches))
	for _, b := range batches {
		bp = append(bp, BatchPayload{Batch: b, Source: string(b.Payload)})
	}
	bundle := &Bundle{
		Format: BundleFormat, Version: 1, ProjectID: projectID,
		ProjectName: name, ExportedAt: s.nowISO(), Events: events, Batches: bp,
	}
	if _, err := s.appendEvent(ctx, projectID, domain.EvtExported, "system",
		map[string]string{"at": bundle.ExportedAt}, "", ""); err != nil {
		return nil, err
	}
	// re-read to include the exported event itself
	if events2, err := s.Store.Events(ctx, projectID); err == nil {
		bundle.Events = events2
	}
	return bundle, nil
}

// ImportBundle reconstructs a project from an archive. It writes batches from
// their original bytes (identical ids) and replays event insertion in original
// sequence. Existing identical rows are ignored, making it idempotent.
func (s *Service) ImportBundle(ctx context.Context, raw []byte) (string, error) {
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return "", err
	}
	if b.Format != BundleFormat || b.Version != 1 {
		return "", errInvalid("unsupported bundle format/version")
	}
	if err := s.Store.CreateProject(ctx, b.ProjectID, b.ProjectName, s.nowISO()); err != nil {
		return "", err
	}
	for _, bp := range b.Batches {
		parsed, err := parseExisting(b.ProjectID, bp)
		if err != nil {
			return "", err
		}
		if err := s.Store.InsertBatch(ctx, b.ProjectID, bp.Batch, parsed); err != nil {
			return "", err
		}
	}
	for _, ev := range b.Events {
		// skip ledger bookkeeping that would be regenerated; keep all others
		if ev.Type == domain.EvtExported {
			continue
		}
		if err := s.Store.AppendEvent(ctx, ev); err != nil {
			// tolerate re-import of an identical event
			if !isUniqueErr(err) {
				return "", err
			}
		}
	}
	return b.ProjectID, nil
}

type errString string

func (e errString) Error() string { return string(e) }
func errInvalid(m string) error  { return errString(m) }
func isUniqueErr(err error) bool {
	if err == nil {
		return false
	}
	return contains(err.Error(), "UNIQUE constraint failed")
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
