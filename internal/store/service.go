package store

import (
	"database/sql"
	"errors"

	"tga/internal/compute"
	"tga/internal/domain"
	"tga/internal/parse"
)

var (
	// ErrExists means a project with that content identity already exists.
	ErrExists = errors.New("project already exists")
	// ErrNotFound means the requested project/object is absent.
	ErrNotFound = errors.New("not found")
	// ErrRejected means an input file had fatal format errors.
	ErrRejected = errors.New("input rejected")
)

// CommitResult is what one layered import returns.
type CommitResult struct {
	Source *domain.Source       `json:"source,omitempty"`
	Diags  []domain.Diag        `json:"diags,omitempty"`
}

// CommitLayer parses and commits one input file as a new immutable source
// version. Fatal parse errors are recorded inside an import batch (so the
// half-failed import leaves input fragments + status) and reject the file.
func (s *Store) CommitLayer(projectID, filename, mediaType, text string,
	layer domain.Layer, actor string) (*CommitResult, error) {
	parsed, parseDiags := parse.ParseInput(mediaType, text, layer)
	batchID := domain.ShortID("batch", map[string]any{
		"project": projectID, "file": filename, "at": s.now()})
	batch := &ImportBatch{ID: batchID, ProjectID: projectID, Status: "complete"}
	res := &CommitResult{Diags: parseDiags}

	if parsed == nil || parsed.HasError() {
		batch.Status = "failed"
		if parsed != nil {
			batch.Status = "partial"
		}
		batch.Files = []FileResult{{Filename: filename, Layer: string(layer),
			Status: "rejected", Diags: parseDiags}}
		err := s.tx(func(tx *sql.Tx) error {
			_, e := s.appendEventTx(tx, projectID, "import_batch_recorded", actor,
				map[string]any{"batch": jsonMap(batch)})
			return e
		})
		if err != nil {
			return nil, err
		}
		return res, ErrRejected
	}

	err := s.tx(func(tx *sql.Tx) error {
		ver := 1
		var parentID string
		_ = tx.QueryRow(`SELECT COALESCE(MAX(version),0) FROM sources
		 WHERE project_id=? AND layer=?`, projectID, string(layer)).Scan(&ver)
		if ver > 0 {
			_ = tx.QueryRow(`SELECT id FROM sources WHERE project_id=? AND layer=?
			 AND version=?`, projectID, string(layer), ver).Scan(&parentID)
		}
		src := domain.Source{
			ProjectID: projectID, Layer: layer, Version: ver, ParentID: parentID,
			Filename: filename, MediaType: mediaType, RawText: text,
			Parsed: *parsed, ImportBatch: batchID,
		}
		src.ID = domain.ShortID("src", map[string]any{
			"project": projectID, "layer": string(layer), "version": ver,
			"raw_text": text, "filename": filename})
		batch.Status = "complete"
		batch.Files = []FileResult{{Filename: filename, Layer: string(layer),
			Status: "committed", SourceID: src.ID, Diags: nonErrors(parseDiags)}}
		if _, err := s.appendEventTx(tx, projectID, "source_committed", actor,
			map[string]any{"source": jsonMap(src)}); err != nil {
			return err
		}
		if _, err := s.appendEventTx(tx, projectID, "import_batch_recorded", actor,
			map[string]any{"batch": jsonMap(batch)}); err != nil {
			return err
		}
		res.Source = &src
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func nonErrors(ds []domain.Diag) []domain.Diag {
	var out []domain.Diag
	for _, d := range ds {
		if d.Severity != "error" {
			out = append(out, d)
		}
	}
	return out
}

// CommitBatch commits several files together; valid layers commit while
// invalid ones are rejected and recorded — the "import half-failure" path.
// The batch status is partial unless all files commit or all fail.
func (s *Store) CommitBatch(projectID, actor string, files []FileInput) (*ImportBatch, error) {
	batchID := domain.ShortID("batch", map[string]any{
		"project": projectID, "n": len(files), "at": s.now()})
	batch := &ImportBatch{ID: batchID, ProjectID: projectID}
	committed, rejected := 0, 0
	err := s.tx(func(tx *sql.Tx) error {
		for _, f := range files {
			parsed, diags := parse.ParseInput(f.MediaType, f.Text, f.Layer)
			fr := FileResult{Filename: f.Filename, Layer: string(f.Layer), Diags: diags}
			if parsed == nil || parsed.HasError() {
				fr.Status = "rejected"
				batch.Files = append(batch.Files, fr)
				rejected++
				continue
			}
			ver := 1
			var parentID string
			_ = tx.QueryRow(`SELECT COALESCE(MAX(version),0) FROM sources
			 WHERE project_id=? AND layer=?`, projectID, string(f.Layer)).Scan(&ver)
			if ver > 0 {
				_ = tx.QueryRow(`SELECT id FROM sources WHERE project_id=? AND layer=?
				 AND version=?`, projectID, string(f.Layer), ver).Scan(&parentID)
			}
			src := domain.Source{
				ProjectID: projectID, Layer: f.Layer, Version: ver, ParentID: parentID,
				Filename: f.Filename, MediaType: f.MediaType, RawText: f.Text,
				Parsed: *parsed, ImportBatch: batchID,
			}
			src.ID = domain.ShortID("src", map[string]any{
				"project": projectID, "layer": string(f.Layer), "version": ver,
				"raw_text": f.Text, "filename": f.Filename})
			if _, err := s.appendEventTx(tx, projectID, "source_committed", actor,
				map[string]any{"source": jsonMap(src)}); err != nil {
				return err
			}
			fr.Status = "committed"
			fr.SourceID = src.ID
			fr.Diags = nonErrors(diags)
			batch.Files = append(batch.Files, fr)
			committed++
		}
		switch {
		case rejected == 0:
			batch.Status = "complete"
		case committed == 0:
			batch.Status = "failed"
		default:
			batch.Status = "partial"
		}
		_, err := s.appendEventTx(tx, projectID, "import_batch_recorded", actor,
			map[string]any{"batch": jsonMap(batch)})
		return err
	})
	if err != nil {
		return nil, err
	}
	return batch, nil
}

// FileInput is one file of a multi-file import batch.
type FileInput struct {
	Filename  string
	MediaType string
	Layer     domain.Layer
	Text      string
}

// EnsureParams records a parameter set exactly once and returns it with the
// fingerprint filled.
func (s *Store) EnsureParams(projectID string, p domain.Params, actor string) (domain.Params, error) {
	p.Fingerprint = domain.ShortID("params", fingerprintBody(p))
	err := s.tx(func(tx *sql.Tx) error {
		var exists int
		_ = tx.QueryRow(`SELECT COUNT(1) FROM params WHERE fingerprint=?`, p.Fingerprint).
			Scan(&exists)
		if exists != 0 {
			return nil
		}
		if _, err := tx.Exec(`INSERT INTO params(fingerprint,body,first_seen_at)
		 VALUES (?,?,?)`, p.Fingerprint, mustJSON(p), s.now()); err != nil {
			return err
		}
		_, err := s.appendEventTx(tx, projectID, "params_added", actor,
			map[string]any{"params": jsonMap(p)})
		return err
	})
	return p, err
}
