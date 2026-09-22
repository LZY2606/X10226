package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tga/internal/domain"
)

// nextSeq returns the next per-project monotonically increasing layer sequence.
func nextSeq(tx *sql.Tx, projectID string) (int, error) {
	var n sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(seq) FROM (
		SELECT seq FROM raw_sources WHERE project_id=?
		UNION ALL SELECT seq FROM param_versions WHERE project_id=?
		UNION ALL SELECT seq FROM derived_nodes WHERE project_id=?
	)`, projectID, projectID, projectID).Scan(&n); err != nil {
		return 0, err
	}
	return int(n.Int64) + 1, nil
}

// CommitRaw inserts an immutable raw source version and appends history in one
// transaction. It is idempotent on (project, kind, digest).
func (s *Store) CommitRaw(ctx context.Context, sub domain.RawSubmission, now Clock) (domain.HistoryEvent, bool, error) {
	var hev domain.HistoryEvent
	var created bool
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		seq, err := nextSeq(tx, sub.ProjectID)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO raw_sources(id,project_id,kind,version,parent_id,payload,digest,import_ref,seq) VALUES(?,?,?,?,?,?,?,?,?)`,
			sub.ID, sub.ProjectID, sub.Kind, sub.Version, nullable(sub.ParentID), string(sub.Payload), sub.Digest, nullable(sub.ImportRef), seq)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil // already present; idempotent replay
		}
		created = true
		e, err := appendEvent(tx, sub.ProjectID, "raw_committed", map[string]any{
			"id": sub.ID, "kind": sub.Kind, "version": sub.Version,
			"parent_id": sub.ParentID, "digest": sub.Digest, "import_ref": sub.ImportRef,
		}, []string{sub.ID}, now())
		if err != nil {
			return err
		}
		hev = e
		return nil
	})
	return hev, created, err
}

// CommitParams stores a new parameter version and appends history.
func (s *Store) CommitParams(ctx context.Context, p domain.Params, now Clock) (domain.HistoryEvent, error) {
	var hev domain.HistoryEvent
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		seq, err := nextSeq(tx, p.ProjectID)
		if err != nil {
			return err
		}
		doc, _ := json.Marshal(p)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO param_versions(id,project_id,version,parent_id,doc,digest,seq) VALUES(?,?,?,?,?,?,?)`,
			p.ID, p.ProjectID, p.Version, nullable(p.ParentID), string(doc), p.Digest, seq); err != nil {
			return err
		}
		e, err := appendEvent(tx, p.ProjectID, "params_committed", map[string]any{
			"id": p.ID, "version": p.Version, "parent_id": p.ParentID,
			"algorithm_version": p.AlgorithmVersion, "digest": p.Digest, "reason": p.Reason,
		}, []string{p.ID}, now())
		if err != nil {
			return err
		}
		hev = e
		return nil
	})
	return hev, err
}

// CommitDerived stores a derived node (idempotent by ID).
func (s *Store) CommitDerived(ctx context.Context, d domain.DerivedNode, now Clock) (domain.HistoryEvent, bool, error) {
	var hev domain.HistoryEvent
	var created bool
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(1) FROM derived_nodes WHERE id=?`, d.ID).Scan(&exists); err != nil {
			return err
		}
		doc, _ := json.Marshal(d)
		src, _ := json.Marshal(d.DirectSources)
		if exists == 0 {
			seq, err := nextSeq(tx, d.ProjectID)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO derived_nodes(id,project_id,algorithm_version,param_version,direct_sources,doc,digest,stale,stale_reason,superseded_by,seq) VALUES(?,?,?,?,?,?,?,0,'','',?)`,
				d.ID, d.ProjectID, d.AlgorithmVersion, d.ParamVersion, string(src), string(doc), d.Digest, seq); err != nil {
				return err
			}
			created = true
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE derived_nodes SET doc=?,digest=?,stale=0,stale_reason='' WHERE id=?`, string(doc), d.Digest, d.ID); err != nil {
				return err
			}
		}
		e, err := appendEvent(tx, d.ProjectID, "derived", map[string]any{
			"id": d.ID, "param_version": d.ParamVersion, "algorithm_version": d.AlgorithmVersion,
			"digest": d.Digest, "stale": d.Stale,
		}, []string{d.ID}, now())
		if err != nil {
			return err
		}
		hev = e
		return nil
	})
	return hev, created, err
}

// MarkStale flags derived nodes that depend on a changed source as stale; the
// rows remain viewable but never masquerade as current.
func (s *Store) MarkStale(ctx context.Context, projectID, reason string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE derived_nodes SET stale=1,stale_reason=? WHERE id=? AND stale=0`, reason, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// MarkSuperseded links an older derived node to its replacement.
func (s *Store) MarkSuperseded(ctx context.Context, oldID, byID string) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE derived_nodes SET superseded_by=? WHERE id=? AND superseded_by=''`, byID, oldID)
		return err
	})
}

// CommitDecision appends a human decision (idempotent on project+seq).
func (s *Store) CommitDecision(ctx context.Context, d domain.Decision, now Clock) (domain.HistoryEvent, error) {
	var hev domain.HistoryEvent
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		doc, _ := json.Marshal(d)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO decisions(id,project_id,seq,target_type,target_id,verdict,doc,undone_by,parent_event) VALUES(?,?,?,?,?,?,?,NULLIF(?, ''),NULLIF(?, ''))`,
			d.ID, d.ProjectID, d.Seq, d.TargetType, d.TargetID, string(d.Verdict), string(doc), d.UndoneBy, d.ParentEvent); err != nil {
			return err
		}
		typ := "decision"
		if d.Verdict == domain.VerdictConflict {
			typ = "conflict"
		}
		e, err := appendEvent(tx, d.ProjectID, typ, json.RawMessage(doc), []string{d.ID, d.TargetID}, now())
		if err != nil {
			return err
		}
		hev = e
		return nil
	})
	return hev, err
}

// UndoDecision marks a decision undone and appends an undo event.
func (s *Store) UndoDecision(ctx context.Context, projectID, decisionID, undoID, note string, now Clock) (domain.HistoryEvent, error) {
	var hev domain.HistoryEvent
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE decisions SET undone_by=? WHERE id=?`, undoID, decisionID); err != nil {
			return err
		}
		e, err := appendEvent(tx, projectID, "undo", map[string]any{
			"undone_decision": decisionID, "undo_id": undoID, "note": note,
		}, []string{decisionID, undoID}, now())
		if err != nil {
			return err
		}
		hev = e
		return nil
	})
	return hev, err
}

// RecordImport stores the import batch report (idempotent).
func (s *Store) RecordImport(ctx context.Context, projectID string, rep domain.ImportReport, seq int, now Clock) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		doc, _ := json.Marshal(rep)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO import_batches(project_id,import_ref,seq,doc) VALUES(?,?,?,?)`,
			projectID, rep.ImportRef, seq, string(doc)); err != nil {
			return err
		}
		_, err := appendEvent(tx, projectID, map[bool]string{true: "import_partial", false: "import_ok"}[rep.Partial],
			json.RawMessage(doc), []string{rep.ImportRef}, now())
		return err
	})
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ErrConflict is a generic store conflict sentinel.
var ErrConflict = errors.New("conflict")
