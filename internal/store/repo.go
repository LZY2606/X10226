package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"tga-review/internal/domain"
)

// ErrExists is returned when an immutable row would be overwritten.
var ErrExists = errors.New("immutable row already exists")

// Project is the projects table projection.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) CreateProject(p Project) error {
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.Exec(
		`INSERT INTO projects(id,name,note,created_at) VALUES(?,?,?,?)`,
		p.ID, p.Name, p.Note, p.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT id,name,note,created_at FROM projects ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		var ts string
		if err := rows.Scan(&p.ID, &p.Name, &p.Note, &ts); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, p)
	}
	return out, nil
}

func (s *Store) ProjectExists(id string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

// AppendEvent stores an event with the next per-project sequence. It does NOT
// derive anything; callers persist derived nodes inside the same transaction
// flow before commit.
func (s *Store) AppendEvent(projID, typ string, payload any, actor string, at time.Time) (domain.Event, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback()
	ev, err := appendEventTx(tx, projID, typ, payload, actor, at)
	if err != nil {
		return domain.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

func appendEventTx(tx *sql.Tx, projID, typ string, payload any, actor string, at time.Time) (domain.Event, error) {
	var maxSeq int
	row := tx.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM events WHERE project_id=?`, projID)
	if err := row.Scan(&maxSeq); err != nil {
		return domain.Event{}, err
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return domain.Event{}, err
	}
	seq := maxSeq + 1
	id := domain.HashID("evt-", map[string]any{
		"project": projID, "seq": seq, "type": typ,
		"payload": json.RawMessage(rawPayload), "at": at.Format(time.RFC3339Nano),
	})
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if _, err := tx.Exec(
		`INSERT INTO events(seq,id,project_id,type,payload,actor,created_at) VALUES(?,?,?,?,?,?,?)`,
		seq, id, projID, typ, string(rawPayload), actor, at.Format(time.RFC3339Nano)); err != nil {
		return domain.Event{}, err
	}
	return domain.Event{Seq: seq, ID: id, Project: projID, Type: typ, Payload: rawPayload, Actor: actor, Created: at}, nil
}

// Events returns all events of a project in order.
func (s *Store) Events(projID string) ([]domain.Event, error) {
	rows, err := s.db.Query(
		`SELECT seq,id,type,payload,actor,created_at FROM events WHERE project_id=? ORDER BY seq`, projID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var ev domain.Event
		var pld, ts string
		if err := rows.Scan(&ev.Seq, &ev.ID, &ev.Type, &pld, &ev.Actor, &ts); err != nil {
			return nil, err
		}
		ev.Project = projID
		ev.Payload = json.RawMessage(pld)
		ev.Created, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, ev)
	}
	return out, nil
}

// SaveRaw inserts an immutable raw source. Same content id twice is a no-op
// conflict reported as ErrExists (raw input can never be overwritten).
func (s *Store) SaveRaw(r *domain.RawSource) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := saveRawTx(tx, r); err != nil {
		return err
	}
	return tx.Commit()
}

func saveRawTx(tx *sql.Tx, r *domain.RawSource) error {
	payload, err := jget(r.Payload)
	if err != nil {
		return err
	}
	diags, err := jget(r.Diags)
	if err != nil {
		return err
	}
	res, err := tx.Exec(
		`INSERT OR IGNORE INTO raw_sources(id,project_id,kind,version,parent_id,batch_id,filename,text,payload,diags,revision,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Project, string(r.Kind), r.Version, r.ParentID, r.BatchID, r.Filename, r.Text,
		payload, diags, r.Revision, r.Created.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrExists
	}
	return nil
}

func (s *Store) SaveBatch(b *domain.Batch) error {
	acc, _ := jget(b.Accepted)
	accK, _ := jget(b.AcceptedK)
	rej, _ := jget(b.Rejected)
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO batches(id,project_id,label,status,accepted,accepted_kinds,rejected,diag_count,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		b.ID, b.Project, b.Label, b.Status, acc, accK, rej, b.DiagCount, b.Created.Format(time.RFC3339Nano))
	return err
}

func scanRaw(scan func(...any) error) (*domain.RawSource, error) {
	var r domain.RawSource
	var kind, payload, diags, ts string
	if err := scan(&r.ID, &r.Project, &kind, &r.Version, &r.ParentID, &r.BatchID,
		&r.Filename, &r.Text, &payload, &diags, &r.Revision, &ts); err != nil {
		return nil, err
	}
	r.Kind = domain.RawKind(kind)
	if err := json.Unmarshal([]byte(payload), &r.Payload); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(diags), &r.Diags)
	r.Created, _ = time.Parse(time.RFC3339Nano, ts)
	return &r, nil
}

const rawCols = `id,project_id,kind,version,parent_id,batch_id,filename,text,payload,diags,revision,created_at`

// RawByID fetches one raw source.
func (s *Store) RawByID(projID, id string) (*domain.RawSource, error) {
	row := s.db.QueryRow(`SELECT `+rawCols+` FROM raw_sources WHERE project_id=? AND id=?`, projID, id)
	return scanRaw(row.Scan)
}

// RawHead returns the highest-version source of each kind.
func (s *Store) RawHead(projID string) (map[domain.RawKind]*domain.RawSource, error) {
	rows, err := s.db.Query(
		`SELECT `+rawCols+` FROM raw_sources r
		 WHERE version=(SELECT MAX(version) FROM raw_sources WHERE project_id=r.project_id AND kind=r.kind)
		 AND project_id=? ORDER BY kind, version`, projID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[domain.RawKind]*domain.RawSource{}
	for rows.Next() {
		r, err := scanRaw(rows.Scan)
		if err != nil {
			return nil, err
		}
		out[r.Kind] = r
	}
	return out, nil
}

// RawAll returns every version of every layer (history), newest last.
func (s *Store) RawAll(projID string) ([]*domain.RawSource, error) {
	rows, err := s.db.Query(`SELECT `+rawCols+` FROM raw_sources WHERE project_id=? ORDER BY kind,version`, projID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.RawSource
	for rows.Next() {
		r, err := scanRaw(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// NextRawVersion returns the next version number for a layer.
func (s *Store) NextRawVersion(projID string, kind domain.RawKind) (int, string, error) {
	var v int
	var parent sql.NullString
	err := s.db.QueryRow(
		`SELECT version,id FROM raw_sources WHERE project_id=? AND kind=? ORDER BY version DESC LIMIT 1`,
		projID, string(kind)).Scan(&v, &parent)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, "", nil
	}
	if err != nil {
		return 0, "", err
	}
	return v + 1, parent.String, nil
}

// Batches lists import attempts.
func (s *Store) Batches(projID string) ([]*domain.Batch, error) {
	rows, err := s.db.Query(
		`SELECT id,project_id,label,status,accepted,accepted_kinds,rejected,diag_count,created_at
		 FROM batches WHERE project_id=? ORDER BY created_at,id`, projID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Batch
	for rows.Next() {
		var b domain.Batch
		var acc, accK, rej, ts string
		if err := rows.Scan(&b.ID, &b.Project, &b.Label, &b.Status, &acc, &accK, &rej, &b.DiagCount, &ts); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(acc), &b.Accepted)
		_ = json.Unmarshal([]byte(accK), &b.AcceptedK)
		_ = json.Unmarshal([]byte(rej), &b.Rejected)
		b.Created, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, &b)
	}
	return out, nil
}

// SaveDerived inserts a content-addressed node; identical id is a no-op.
func (s *Store) SaveDerived(d *domain.Derived) error {
	b, err := jget(d)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT OR IGNORE INTO derived_nodes(id,project_id,slot,kind,params_id,json,created_at)
		 VALUES(?,?,?,?,?,?,?)`,
		d.ID, d.Project, d.Slot, d.Kind, d.ParamsID, string(b), d.Created.Format(time.RFC3339Nano))
	return err
}

func (s *Store) DerivedByID(projID, id string) (*domain.Derived, error) {
	var js string
	err := s.db.QueryRow(`SELECT json FROM derived_nodes WHERE project_id=? AND id=?`, projID, id).Scan(&js)
	if err != nil {
		return nil, err
	}
	var d domain.Derived
	if err := json.Unmarshal([]byte(js), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// AllDerived returns every persisted node version.
func (s *Store) AllDerived(projID string) ([]*domain.Derived, error) {
	rows, err := s.db.Query(`SELECT json FROM derived_nodes WHERE project_id=? ORDER BY created_at,slot,id`, projID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Derived
	for rows.Next() {
		var js string
		if err := rows.Scan(&js); err != nil {
			return nil, err
		}
		var d domain.Derived
		if err := json.Unmarshal([]byte(js), &d); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, nil
}

// SetKV / GetKV hold small bookkeeping values (e.g. head params id).
func (s *Store) SetKV(projID, key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO keyvals(project_id,key,value) VALUES(?,?,?)
		 ON CONFLICT(project_id,key) DO UPDATE SET value=excluded.value`,
		projID, key, value)
	return err
}

func (s *Store) GetKV(projID, key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM keyvals WHERE project_id=? AND key=?`, projID, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}
