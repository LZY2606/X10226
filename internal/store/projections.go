package store

import (
	"database/sql"
	"encoding/json"
)

// projectEvent maintains raw_docs / import_batches projections from events.
// All other state is derived by the application layer during replay.
func projectEvent(tx *sql.Tx, ev *StoredEvent) error {
	switch ev.Kind {
	case "doc_committed":
		return applyDocCommitted(tx, ev)
	case "batch_opened":
		return execOne(tx,
			`INSERT OR IGNORE INTO import_batches(id,project,opened_seq) VALUES(?,?,?)`,
			str(ev.Payload, "batch_id"), ev.Project, ev.Seq)
	case "batch_closed":
		return execOne(tx,
			`UPDATE import_batches SET closed_seq=?, status=?, detail=? WHERE id=? AND project=?`,
			ev.Seq, str(ev.Payload, "status"), str(ev.Payload, "detail"),
			str(ev.Payload, "batch_id"), ev.Project)
	}
	return nil
}

func applyDocCommitted(tx *sql.Tx, ev *StoredEvent) error {
	id := str(ev.Payload, "id")
	project := ev.Project
	kind := str(ev.Payload, "kind")
	layer := str(ev.Payload, "layer")
	version := intNum(ev.Payload, "version")
	parent := str(ev.Payload, "parent_doc")
	origin := str(ev.Payload, "origin")
	rawText := str(ev.Payload, "raw_text")
	content, err := json.Marshal(ev.Payload["content"])
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE raw_docs SET superseded=1 WHERE project=? AND layer=? AND id<>?`,
		project, layer, id); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO raw_docs(id,project,kind,layer,version,parent_doc,origin,committed,raw_text,content,superseded)
		VALUES(?,?,?,?,?,?,?,?,?,?,0)`, id, project, kind, layer, version, parent, origin, ev.Seq, rawText, string(content))
	return err
}

func execOne(tx *sql.Tx, q string, args ...interface{}) error {
	_, err := tx.Exec(q, args...)
	return err
}

func str(m map[string]interface{}, k string) string {
	if v, ok := m[k]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func intNum(m map[string]interface{}, k string) int {
	if v, ok := m[k]; ok {
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		case int64:
			return int(t)
		case json.Number:
			n, _ := t.Int64()
			return int(n)
		}
	}
	return 0
}

// DocRow is one projected source document.
type DocRow struct {
	ID         string                 `json:"id"`
	Kind       string                 `json:"kind"`
	Layer      string                 `json:"layer"`
	Version    int                    `json:"version"`
	ParentDoc  string                 `json:"parent_doc"`
	Origin     string                 `json:"origin"`
	Committed  int64                  `json:"committed_seq"`
	RawText    string                 `json:"raw_text,omitempty"`
	Content    map[string]interface{} `json:"content"`
	Superseded bool                   `json:"superseded"`
}

func docFromRow(row *sql.Row) (*DocRow, error) {
	var d DocRow
	var content string
	if err := row.Scan(&d.ID, &d.Kind, &d.Layer, &d.Version, &d.ParentDoc, &d.Origin,
		&d.Committed, &d.RawText, &content, &d.Superseded); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(content), &d.Content); err != nil {
		return nil, err
	}
	return &d, nil
}

// CurrentDoc returns the newest committed document of a layer.
func (s *Store) CurrentDoc(project, layer string) (*DocRow, error) {
	row := s.db.QueryRow(`SELECT id,kind,layer,version,parent_doc,origin,committed,raw_text,content,superseded
		FROM raw_docs WHERE project=? AND layer=? AND superseded=0 ORDER BY version DESC,committed DESC LIMIT 1`,
		project, layer)
	d, err := docFromRow(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return d, err
}

// DocByID fetches a specific document version.
func (s *Store) DocByID(project, id string) (*DocRow, error) {
	row := s.db.QueryRow(`SELECT id,kind,layer,version,parent_doc,origin,committed,raw_text,content,superseded
		FROM raw_docs WHERE project=? AND id=?`, project, id)
	d, err := docFromRow(row)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return d, err
}

// DocsByLayer returns every committed version, newest first.
func (s *Store) DocsByLayer(project, layer string) ([]*DocRow, error) {
	rows, err := s.db.Query(`SELECT id,kind,layer,version,parent_doc,origin,committed,raw_text,content,superseded
		FROM raw_docs WHERE project=? AND layer=? ORDER BY version DESC,committed DESC`, project, layer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DocRow
	for rows.Next() {
		var d DocRow
		var content string
		if err := rows.Scan(&d.ID, &d.Kind, &d.Layer, &d.Version, &d.ParentDoc, &d.Origin,
			&d.Committed, &d.RawText, &content, &d.Superseded); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(content), &d.Content); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// BatchRow is one projected import batch.
type BatchRow struct {
	ID        string `json:"id"`
	OpenedSeq int64  `json:"opened_seq"`
	ClosedSeq int64  `json:"closed_seq"`
	Status    string `json:"status"`
	Detail    string `json:"detail"`
}

func (s *Store) Batches(project string) ([]*BatchRow, error) {
	rows, err := s.db.Query(`SELECT id,opened_seq,closed_seq,status,detail FROM import_batches
		WHERE project=? ORDER BY opened_seq`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*BatchRow
	for rows.Next() {
		var b BatchRow
		if err := rows.Scan(&b.ID, &b.OpenedSeq, &b.ClosedSeq, &b.Status, &b.Detail); err != nil {
			return nil, err
		}
		out = append(out, &b)
	}
	return out, rows.Err()
}

// DeleteProject removes every row of a project (used by the demo reset).
func (s *Store) DeleteProject(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`DELETE FROM events WHERE project=?`,
		`DELETE FROM raw_docs WHERE project=?`,
		`DELETE FROM import_batches WHERE project=?`,
		`DELETE FROM projects WHERE id=?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
