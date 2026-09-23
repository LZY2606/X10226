package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"tgareview/internal/domain"
)

var ErrNotFound = errors.New("not found")

type ProjectRow struct {
	ID        string
	Name      string
	Nonce     string
	CreatedAt time.Time
	Seq       int64
}

func (s *Store) CreateProject(p domain.Project) error {
	_, err := s.db.Exec(
		`INSERT INTO projects(id,name,nonce,created_at,seq) VALUES(?,?,?,?,0)`,
		p.ID, p.Name, p.Nonce, p.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) GetProject(id string) (*ProjectRow, error) {
	row := s.db.QueryRow(`SELECT id,name,nonce,created_at,seq FROM projects WHERE id=?`, id)
	var pr ProjectRow
	var ts string
	if err := row.Scan(&pr.ID, &pr.Name, &pr.Nonce, &ts, &pr.Seq); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	pr.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
	return &pr, nil
}

func (s *Store) ListProjects() ([]ProjectRow, error) {
	rows, err := s.db.Query(`SELECT id,name,nonce,created_at,seq FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectRow
	for rows.Next() {
		var pr ProjectRow
		var ts string
		if err := rows.Scan(&pr.ID, &pr.Name, &pr.Nonce, &ts, &pr.Seq); err != nil {
			return nil, err
		}
		pr.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, pr)
	}
	return out, nil
}

// NextSeq returns and persists the next event sequence inside a transaction.
func (s *Store) NextSeq(projectID string) (int64, error) {
	res, err := s.db.Exec(`UPDATE projects SET seq=seq+1 WHERE id=?`, projectID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return 0, ErrNotFound
	}
	var seq int64
	err = s.db.QueryRow(`SELECT seq FROM projects WHERE id=?`, projectID).Scan(&seq)
	return seq, err
}

func (s *Store) AppendEvent(e domain.Event) error {
	refs, _ := json.Marshal(e.Refs)
	_, err := s.db.Exec(
		`INSERT INTO events(project_id,seq,id,kind,occurred_at,actor,payload,refs_json)
		 VALUES(?,?,?,?,?,?,?,?)`,
		e.ProjectID, e.Seq, e.ID, e.Kind,
		e.OccurredAt.UTC().Format(time.RFC3339Nano), e.Actor, e.Payload, string(refs))
	return err
}

func scanEvent(row interface {
	Scan(dest ...any) error
}) (domain.Event, error) {
	var e domain.Event
	var ts, refs string
	if err := row.Scan(&e.Seq, &e.ID, &e.Kind, &ts, &e.Actor, &e.Payload, &refs); err != nil {
		return e, err
	}
	e.OccurredAt, _ = time.Parse(time.RFC3339Nano, ts)
	_ = json.Unmarshal([]byte(refs), &e.Refs)
	return e, nil
}

func (s *Store) Events(projectID string, upToSeq int64) ([]domain.Event, error) {
	q := `SELECT seq,id,kind,occurred_at,actor,payload,refs_json FROM events
	      WHERE project_id=? AND seq<=? ORDER BY seq`
	if upToSeq <= 0 {
		q = `SELECT seq,id,kind,occurred_at,actor,payload,refs_json FROM events
		    WHERE project_id=? ORDER BY seq`
	}
	var (
		rows *sql.Rows
		err  error
	)
	if upToSeq <= 0 {
		rows, err = s.db.Query(q, projectID)
	} else {
		rows, err = s.db.Query(q, projectID, upToSeq)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		e.ProjectID = projectID
		out = append(out, e)
	}
	return out, nil
}
