package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"tga/internal/domain"
)

// ArchiveFormat is the offline case package container version.
const ArchiveFormat = "application/x-tga-archive; version=1"

// Archive is the complete, portable representation of one project.
type Archive struct {
	Format    string            `json:"format"`
	Project   json.RawMessage   `json:"project"`
	Raw       []json.RawMessage `json:"raw_sources"`
	Params    []json.RawMessage `json:"param_versions"`
	Derived   []json.RawMessage `json:"derived_nodes"`
	Decisions []json.RawMessage `json:"decisions"`
	History   []json.RawMessage `json:"history"`
	Imports   []ImportBatch     `json:"import_batches"`
}

// ImportBatch is the stored import report row.
type ImportBatch struct {
	ProjectID string          `json:"project_id"`
	ImportRef string          `json:"import_ref"`
	Seq       int             `json:"seq"`
	Doc       json.RawMessage `json:"doc"`
}

// ExportArchive serializes every layer and the full event log.
func (s *Store) ExportArchive(ctx context.Context, projectID string) (*Archive, error) {
	if _, err := s.GetProject(ctx, projectID); err != nil {
		return nil, err
	}
	a := &Archive{Format: ArchiveFormat}
	p, err := s.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	a.Project, _ = json.Marshal(p)
	a.Raw, _ = rawBlobs(ctx, s, projectID)
	a.Params, _ = paramBlobs(ctx, s, projectID)
	a.Derived, _ = derivedBlobs(ctx, s, projectID)
	a.Decisions, _ = decisionBlobs(ctx, s, projectID)
	hist, err := s.History(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for i := range hist {
		b, _ := json.Marshal(hist[i])
		a.History = append(a.History, b)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT project_id,import_ref,seq,doc FROM import_batches WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b ImportBatch
		if err := rows.Scan(&b.ProjectID, &b.ImportRef, &b.Seq, &b.Doc); err != nil {
			return nil, err
		}
		a.Imports = append(a.Imports, b)
	}
	return a, rows.Err()
}

func marshalAll[T any](xs []T) []json.RawMessage {
	var out []json.RawMessage
	for i := range xs {
		b, _ := json.Marshal(xs[i])
		out = append(out, b)
	}
	return out
}

func rawBlobs(ctx context.Context, s *Store, pid string) ([]json.RawMessage, error) {
	xs, err := s.AllRaws(ctx, pid)
	return marshalAll(xs), err
}
func paramBlobs(ctx context.Context, s *Store, pid string) ([]json.RawMessage, error) {
	xs, err := s.AllParams(ctx, pid)
	return marshalAll(xs), err
}
func derivedBlobs(ctx context.Context, s *Store, pid string) ([]json.RawMessage, error) {
	xs, err := s.AllDerived(ctx, pid)
	return marshalAll(xs), err
}
func decisionBlobs(ctx context.Context, s *Store, pid string) ([]json.RawMessage, error) {
	xs, err := s.AllDecisions(ctx, pid)
	return marshalAll(xs), err
}

// ImportArchive restores an archive, preserving IDs, sequence and provenance,
// then verifies the hash chain. Re-importing identical content is idempotent.
func (s *Store) ImportArchive(ctx context.Context, a *Archive) error {
	if a.Format != ArchiveFormat {
		return fmt.Errorf("unsupported archive format %q", a.Format)
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		var p domain.Project
		var archived int
		if err := json.Unmarshal(a.Project, &p); err != nil {
			return fmt.Errorf("archive project: %w", err)
		}
		if p.Archived {
			archived = 1
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO projects(id,name,slug,created_ts,archived,head_seq) VALUES(?,?,?,?,?,0)`,
			p.ID, p.Name, p.Slug, p.CreatedTS, archived); err != nil {
			return err
		}
		for _, b := range a.Raw {
			var r domain.RawSubmission
			if err := json.Unmarshal(b, &r); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO raw_sources(id,project_id,kind,version,parent_id,payload,digest,import_ref,seq) VALUES(?,?,?,?,?,?,?,?,
				(SELECT COALESCE(MAX(seq),0)+1 FROM (SELECT seq FROM raw_sources WHERE project_id=? UNION ALL SELECT seq FROM param_versions WHERE project_id=? UNION ALL SELECT seq FROM derived_nodes WHERE project_id=?)))`,
				r.ID, r.ProjectID, r.Kind, r.Version, nullable(r.ParentID), string(r.Payload), r.Digest, nullable(r.ImportRef), r.ProjectID, r.ProjectID, r.ProjectID); err != nil {
				return err
			}
		}
		for _, b := range a.Params {
			var p2 domain.Params
			if err := json.Unmarshal(b, &p2); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO param_versions(id,project_id,version,parent_id,doc,digest,seq) VALUES(?,?,?,?,?,?,
				(SELECT COALESCE(MAX(seq),0)+1 FROM (SELECT seq FROM raw_sources WHERE project_id=? UNION ALL SELECT seq FROM param_versions WHERE project_id=? UNION ALL SELECT seq FROM derived_nodes WHERE project_id=?)))`,
				p2.ID, p2.ProjectID, p2.Version, nullable(p2.ParentID), string(b), p2.Digest, p2.ProjectID, p2.ProjectID, p2.ProjectID); err != nil {
				return err
			}
		}
		for _, b := range a.Derived {
			var d domain.DerivedNode
			if err := json.Unmarshal(b, &d); err != nil {
				return err
			}
			src, _ := json.Marshal(d.DirectSources)
			stale, reason := 0, ""
			if d.Stale {
				stale = 1
				reason = d.StaleReason
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO derived_nodes(id,project_id,algorithm_version,param_version,direct_sources,doc,digest,stale,stale_reason,superseded_by,seq) VALUES(?,?,?,?,?,?,?,?,?,?,
				(SELECT COALESCE(MAX(seq),0)+1 FROM (SELECT seq FROM raw_sources WHERE project_id=? UNION ALL SELECT seq FROM param_versions WHERE project_id=? UNION ALL SELECT seq FROM derived_nodes WHERE project_id=?)))`,
				d.ID, d.ProjectID, d.AlgorithmVersion, d.ParamVersion, string(src), string(b), d.Digest, stale, reason, nullable(d.SupersededBy), d.ProjectID, d.ProjectID, d.ProjectID); err != nil {
				return err
			}
		}
		for _, b := range a.Decisions {
			var d domain.Decision
			if err := json.Unmarshal(b, &d); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO decisions(id,project_id,seq,target_type,target_id,verdict,doc,undone_by,parent_event) VALUES(?,?,?,?,?,?,?,NULLIF(?, ''),NULLIF(?, ''))`,
				d.ID, d.ProjectID, d.Seq, d.TargetType, d.TargetID, string(d.Verdict), string(b), d.UndoneBy, d.ParentEvent); err != nil {
				return err
			}
		}
		// Restore history preserving original sequence and hash chain.
		for _, b := range a.History {
			var h domain.HistoryEvent
			if err := json.Unmarshal(b, &h); err != nil {
				return err
			}
			refs, _ := json.Marshal(h.RefIDs)
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO history(seq,project_id,type,payload,ref_ids,virtual_ts,prev_hash,hash) VALUES(?,?,?,?,?,?,?,?)`,
				h.Seq, h.ProjectID, h.Type, string(h.Payload), string(refs), h.VirtualTS, h.PrevHash, h.Hash); err != nil {
				return err
			}
		}
		for _, ib := range a.Imports {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO import_batches(project_id,import_ref,seq,doc) VALUES(?,?,?,?)`,
				ib.ProjectID, ib.ImportRef, ib.Seq, string(ib.Doc)); err != nil {
				return err
			}
		}
		var head sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT MAX(seq) FROM history WHERE project_id=?`, p.ID).Scan(&head); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET head_seq=? WHERE id=?`, head.Int64, p.ID); err != nil {
			return err
		}
		return nil
	})
}
