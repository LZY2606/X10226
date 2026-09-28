package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tgareview/internal/domain"
)

// ErrNotFound is returned for missing rows.
var ErrNotFound = errors.New("not found")

// CommitSource persists an immutable source version and emits one event inside
// the same transaction. Rows rejected by the importer are preserved verbatim.
func (s *Store) CommitSource(ctx context.Context, src *domain.Source) (domain.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback()

	ver, err := s.nextVersionTx(ctx, tx, src.ProjectID, src.Kind)
	if err != nil {
		return domain.Event{}, err
	}
	src.Version = ver
	src.ParentVersion = ver - 1

	body := map[string]any{
		"project_id": src.ProjectID, "kind": src.Kind, "version": ver,
		"parent_version": src.ParentVersion, "filename": src.Filename,
		"header": src.Header, "unit_decl": src.UnitDecl,
		"accepted": src.Accepted, "rejected": src.Rejected,
	}
	// content-addressed id computed before insert (event references it)
	src.ID = domain.ContentID("src", domain.CanonicalJSON(body))

	ev, err := s.appendEventTx(ctx, tx, src.ProjectID, domain.EvSourceCommitted, "importer",
		domain.SourceLabels[src.Kind]+" v"+itoa(ver)+" 提交："+src.Filename+
			"（接受 "+itoa(src.Accepted)+" 行，拒绝 "+itoa(src.Rejected)+" 行）",
		map[string]any{
			"source_id": src.ID, "kind": src.Kind, "version": ver,
			"parent_version": src.ParentVersion, "filename": src.Filename,
			"accepted": src.Accepted, "rejected": src.Rejected,
		})
	if err != nil {
		return domain.Event{}, err
	}
	src.CommitEventID = ev.ID
	src.CreatedAt = ev.At

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sources(id,project_id,kind,version,parent_version,filename,header,unit_decl,
		   accepted,rejected,commit_event_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		src.ID, src.ProjectID, src.Kind, ver, src.ParentVersion, src.Filename,
		string(jsonRaw(src.Header)), string(jsonRaw(src.UnitDecl)),
		src.Accepted, src.Rejected, ev.ID, ev.At.Format(timeFmt)); err != nil {
		return domain.Event{}, err
	}
	for _, r := range src.Rows {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO source_rows(source_id,line,raw,cells,accepted,diagnostic) VALUES(?,?,?,?,?,?)`,
			src.ID, r.Line, string(jsonRaw(r.Raw)), string(jsonRaw(r.Cells)), r.Accepted, r.Diagnostic); err != nil {
			return domain.Event{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

func (s *Store) nextVersionTx(ctx context.Context, tx *sql.Tx, projectID, kind string) (int, error) {
	var v int
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version),0)+1 FROM sources WHERE project_id=? AND kind=?`,
		projectID, kind).Scan(&v)
	return v, err
}

// LatestSource returns the newest committed version of a kind.
func (s *Store) LatestSource(ctx context.Context, projectID, kind string) (*domain.Source, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,version,parent_version,filename,header,unit_decl,accepted,rejected,commit_event_id,created_at
		 FROM sources WHERE project_id=? AND kind=? ORDER BY version DESC LIMIT 1`, projectID, kind)
	src := &domain.Source{ProjectID: projectID, Kind: kind}
	var header, unitDecl, created string
	if err := row.Scan(&src.ID, &src.Version, &src.ParentVersion, &src.Filename,
		&header, &unitDecl, &src.Accepted, &src.Rejected, &src.CommitEventID, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	src.CreatedAt = parseTime(created)
	_ = json.Unmarshal([]byte(header), &src.Header)
	_ = json.Unmarshal([]byte(unitDecl), &src.UnitDecl)
	rows, err := s.rowsOf(ctx, s.db, src.ID)
	if err != nil {
		return nil, err
	}
	src.Rows = rows
	return src, nil
}

// SourceAtVersion loads a specific version (used when replaying history).
func (s *Store) SourceAtVersion(ctx context.Context, projectID, kind string, version int) (*domain.Source, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,version,parent_version,filename,header,unit_decl,accepted,rejected,commit_event_id,created_at
		 FROM sources WHERE project_id=? AND kind=? AND version=?`, projectID, kind, version)
	src := &domain.Source{ProjectID: projectID, Kind: kind}
	var header, unitDecl, created string
	if err := row.Scan(&src.ID, &src.Version, &src.ParentVersion, &src.Filename,
		&header, &unitDecl, &src.Accepted, &src.Rejected, &src.CommitEventID, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	src.CreatedAt = parseTime(created)
	_ = json.Unmarshal([]byte(header), &src.Header)
	_ = json.Unmarshal([]byte(unitDecl), &src.UnitDecl)
	rows, err := s.rowsOf(ctx, s.db, src.ID)
	if err != nil {
		return nil, err
	}
	src.Rows = rows
	return src, nil
}

// ListSourceVersions returns version metadata without rows.
func (s *Store) ListSourceVersions(ctx context.Context, projectID, kind string) ([]domain.Source, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,version,parent_version,filename,accepted,rejected,created_at
		 FROM sources WHERE project_id=? AND kind=? ORDER BY version`, projectID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Source
	for rows.Next() {
		var s2 domain.Source
		var c string
		if err := rows.Scan(&s2.ID, &s2.Version, &s2.ParentVersion, &s2.Filename, &s2.Accepted, &s2.Rejected, &c); err != nil {
			return nil, err
		}
		s2.Kind, s2.ProjectID, s2.CreatedAt = kind, projectID, parseTime(c)
		out = append(out, s2)
	}
	return out, rows.Err()
}

func (s *Store) rowsOf(ctx context.Context, q queryer, sourceID string) ([]domain.Row, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT line,raw,cells,accepted,diagnostic FROM source_rows WHERE source_id=? ORDER BY line`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Row
	for rows.Next() {
		var r domain.Row
		var raw, cells string
		if err := rows.Scan(&r.Line, &raw, &cells, &r.Accepted, &r.Diagnostic); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &r.Raw)
		_ = json.Unmarshal([]byte(cells), &r.Cells)
		out = append(out, r)
	}
	return out, rows.Err()
}

func itoa(i int) string {
	return intToStr(int64(i))
}
