package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tgareview/internal/domain"
)

// SaveArtifact persists one derivation output inside the derivation event tx.
func (s *Store) SaveArtifact(ctx context.Context, tx *sql.Tx, a *domain.Artifact, ev domain.Event) error {
	a.DerivEvent = ev.ID
	a.Seq = ev.Seq
	a.CreatedAt = ev.At
	if a.ID == "" {
		a.ID = domain.ContentID("art", domain.CanonicalJSON(map[string]any{
			"project": a.ProjectID, "kind": a.Kind, "algo": a.AlgoVersion,
			"pfp": a.ParamFP, "inputs": a.Inputs, "seq": a.Seq,
		}))
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO artifacts(id,project_id,kind,algo_version,param_fp,inputs,deriv_event,seq,data,candidates,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.ProjectID, a.Kind, a.AlgoVersion, a.ParamFP, string(jsonRaw(a.Inputs)),
		ev.ID, ev.Seq, string(jsonRaw(a.Data)), string(jsonRaw(a.Candidates)), ev.At.Format(timeFmt))
	return err
}

// BeginTx exposes a transaction for the app-level derivation/decision units.
func (s *Store) BeginTx(ctx context.Context) (*sql.Tx, error) { return s.db.BeginTx(ctx, nil) }

// AppendEventTx appends an event on an existing transaction.
func (s *Store) AppendEventTx(ctx context.Context, tx *sql.Tx, projectID, typ, actor, summary string, payload map[string]any) (domain.Event, error) {
	return s.appendEventTx(ctx, tx, projectID, typ, actor, summary, payload)
}

// LatestArtifact returns the newest artifact of a kind (may be stale).
func (s *Store) LatestArtifact(ctx context.Context, projectID, kind string) (*domain.Artifact, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,kind,algo_version,param_fp,inputs,deriv_event,seq,data,candidates,created_at
		 FROM artifacts WHERE project_id=? AND kind=? ORDER BY seq DESC LIMIT 1`, projectID, kind)
	return scanArtifact(row)
}

// ArtifactAtSeq returns the newest artifact of a kind produced at or before seq.
func (s *Store) ArtifactAtSeq(ctx context.Context, projectID, kind string, seq int64) (*domain.Artifact, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,kind,algo_version,param_fp,inputs,deriv_event,seq,data,candidates,created_at
		 FROM artifacts WHERE project_id=? AND kind=? AND seq<=? ORDER BY seq DESC LIMIT 1`, projectID, kind, seq)
	return scanArtifact(row)
}

// ListArtifacts returns all artifacts of a kind newest-first.
func (s *Store) ListArtifacts(ctx context.Context, projectID, kind string) ([]domain.Artifact, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,kind,algo_version,param_fp,inputs,deriv_event,seq,data,candidates,created_at
		 FROM artifacts WHERE project_id=? AND kind=? ORDER BY seq DESC`, projectID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		a.ProjectID = projectID
		out = append(out, *a)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanArtifact(row scanner) (*domain.Artifact, error) {
	var a domain.Artifact
	var inputs, data, cands, created string
	if err := row.Scan(&a.ID, &a.Kind, &a.AlgoVersion, &a.ParamFP, &inputs, &a.DerivEvent,
		&a.Seq, &data, &cands, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	a.CreatedAt = parseTime(created)
	_ = json.Unmarshal([]byte(inputs), &a.Inputs)
	_ = json.Unmarshal([]byte(data), &a.Data)
	_ = json.Unmarshal([]byte(cands), &a.Candidates)
	return &a, nil
}
