package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"tgareview/internal/domain"
)

func (s *Store) InsertSource(src domain.Source) error {
	if src.SubmittedAt.IsZero() {
		src.SubmittedAt = time.Now()
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO sources
		 (id,project_id,kind,version,parent_id,origin,submitted_at,data_json,diag_json)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		src.ID, src.ProjectID, src.Kind, src.Version, src.ParentID, src.Origin,
		src.SubmittedAt.UTC().Format(time.RFC3339Nano),
		mustJSON(src.Data), mustJSON(src.Diagnostics))
	return err
}

func scanSource(row *sql.Row) (domain.Source, error) {
	var src domain.Source
	var ts, data, diags string
	err := row.Scan(&src.ID, &src.ProjectID, &src.Kind, &src.Version, &src.ParentID,
		&src.Origin, &ts, &data, &diags)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return src, ErrNotFound
		}
		return src, err
	}
	src.SubmittedAt, _ = time.Parse(time.RFC3339Nano, ts)
	_ = json.Unmarshal([]byte(data), &src.Data)
	_ = json.Unmarshal([]byte(diags), &src.Diagnostics)
	return src, nil
}

func (s *Store) GetSourceByID(id string) (domain.Source, error) {
	return scanSource(s.db.QueryRow(
		`SELECT id,project_id,kind,version,parent_id,origin,submitted_at,data_json,diag_json
		 FROM sources WHERE id=?`, id))
}

// LatestSourceVersion returns the highest version number for a kind (0 if none).
func (s *Store) LatestSourceVersion(projectID, kind string) (int, string, error) {
	var v int
	var parent string
	err := s.db.QueryRow(
		`SELECT COALESCE(MAX(version),0) FROM sources WHERE project_id=? AND kind=?`,
		projectID, kind).Scan(&v)
	if err != nil {
		return 0, "", err
	}
	if v > 0 {
		_ = s.db.QueryRow(
			`SELECT id FROM sources WHERE project_id=? AND kind=? AND version=?`,
			projectID, kind, v).Scan(&parent)
	}
	return v, parent, nil
}

func (s *Store) SourceAtVersion(projectID, kind string, version int) (domain.Source, error) {
	return scanSource(s.db.QueryRow(
		`SELECT id,project_id,kind,version,parent_id,origin,submitted_at,data_json,diag_json
		 FROM sources WHERE project_id=? AND kind=? AND version=?`, projectID, kind, version))
}

func (s *Store) AllSources(projectID string) ([]domain.Source, error) {
	rows, err := s.db.Query(
		`SELECT id,project_id,kind,version,parent_id,origin,submitted_at,data_json,diag_json
		 FROM sources WHERE project_id=? ORDER BY kind,version`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Source
	for rows.Next() {
		var src domain.Source
		var ts, data, diags string
		if err := rows.Scan(&src.ID, &src.ProjectID, &src.Kind, &src.Version, &src.ParentID,
			&src.Origin, &ts, &data, &diags); err != nil {
			return nil, err
		}
		src.SubmittedAt, _ = time.Parse(time.RFC3339Nano, ts)
		_ = json.Unmarshal([]byte(data), &src.Data)
		_ = json.Unmarshal([]byte(diags), &src.Diagnostics)
		out = append(out, src)
	}
	return out, nil
}

// --- fragments (rejected half-failure sections) -----------------------------

type FragmentRow struct {
	ID         string
	ProjectID  string
	BatchID    string
	KindHint   string
	Origin     string
	RawSnippet string
	Diagnostics []domain.Diagnostic
	OccurredAt time.Time
}

func (s *Store) InsertFragment(f FragmentRow) error {
	if f.OccurredAt.IsZero() {
		f.OccurredAt = time.Now()
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO fragments
		 (id,project_id,batch_id,kind_hint,origin,raw_snippet,diag_json,occurred_at)
		 VALUES(?,?,?,?,?,?,?,?)`,
		f.ID, f.ProjectID, f.BatchID, f.KindHint, f.Origin, f.RawSnippet,
		mustJSON(f.Diagnostics), f.OccurredAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) AllFragments(projectID string) ([]FragmentRow, error) {
	rows, err := s.db.Query(
		`SELECT id,project_id,batch_id,kind_hint,origin,raw_snippet,diag_json,occurred_at
		 FROM fragments WHERE project_id=? ORDER BY rowid`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FragmentRow
	for rows.Next() {
		var f FragmentRow
		var diags, ts string
		if err := rows.Scan(&f.ID, &f.ProjectID, &f.BatchID, &f.KindHint, &f.Origin,
			&f.RawSnippet, &diags, &ts); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(diags), &f.Diagnostics)
		f.OccurredAt, _ = time.Parse(time.RFC3339Nano, ts)
		out = append(out, f)
	}
	return out, nil
}
