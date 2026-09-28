package store

import (
	"context"
	"database/sql"
	"errors"
)

// SetKV stores/updates a project key (e.g. current parameters) inside tx.
func (s *Store) SetKV(ctx context.Context, tx *sql.Tx, projectID, k, v string, seq int64) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO kv(project_id,k,v,seq) VALUES(?,?,?,?)
		 ON CONFLICT(project_id,k) DO UPDATE SET v=excluded.v, seq=excluded.seq`,
		projectID, k, v, seq)
	return err
}

// GetKV reads a project key.
func (s *Store) GetKV(ctx context.Context, projectID, k string) (string, int64, error) {
	var v string
	var seq int64
	err := s.db.QueryRowContext(ctx, `SELECT v,seq FROM kv WHERE project_id=? AND k=?`, projectID, k).Scan(&v, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	return v, seq, err
}

// KVAtSeq returns the kv value effective at a given event seq.
func (s *Store) KVAtSeq(ctx context.Context, projectID, k string, seq int64) (string, error) {
	// current kv is keyed once; historical parameter versions are derived from
	// params_revised events in the app layer.
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT v FROM kv WHERE project_id=? AND k=? AND seq<=?`, projectID, k, seq).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}
