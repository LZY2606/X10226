package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"tga-review/internal/domain"
)

func appendEvent(tx *sql.Tx, projectID string, seq int, typ string, at time.Time, payload map[string]any) (string, error) {
	id := domain.EventStableID(projectID, seq)
	b, _ := json.Marshal(payload)
	_, err := tx.Exec(`INSERT INTO events(id, project_id, seq, type, at, payload) VALUES(?,?,?,?,?,?)`,
		id, projectID, seq, typ, at.Format(time.RFC3339Nano), string(b))
	if err != nil {
		return "", err
	}
	return id, nil
}

func nextSeqLocked(tx *sql.Tx, table, pid string) (int, error) {
	var n int
	if err := tx.QueryRow(
		fmt.Sprintf(`SELECT COALESCE(MAX(seq),0)+1 FROM %s WHERE project_id=?`, table), pid).
		Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func nextEventSeqLocked(tx *sql.Tx, pid string) (int, error) {
	var n int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE project_id=?`, pid).
		Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) getProjectLocked(tx *sql.Tx, pid string) (*domain.Project, error) {
	var p domain.Project
	var note, ct string
	err := tx.QueryRow(`SELECT id, name, material, note, created_at FROM projects WHERE id=?`, pid).
		Scan(&p.ID, &p.Name, &p.Material, &note, &ct)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.Note = note
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, ct)
	return &p, nil
}

func latestSubmissionLocked(tx *sql.Tx, pid string, layer domain.Layer) (*domain.Submission, error) {
	var data string
	err := tx.QueryRow(
		`SELECT data FROM submissions WHERE project_id=? AND layer=? ORDER BY seq DESC LIMIT 1`,
		pid, string(layer)).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var sub domain.Submission
	if err := json.Unmarshal([]byte(data), &sub); err != nil {
		return nil, err
	}
	return &sub, nil
}
