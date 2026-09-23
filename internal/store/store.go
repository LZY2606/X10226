// Package store 提供基于 SQLite 的持久化：源版本、派生值、事件日志与全局序号。
// 所有写入追加式；原始输入一经提交不可覆盖。
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"tgagsb/internal/domain"
)

// Store 包装 SQLite 连接。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）数据库并建表。path 为 ":memory:" 时仅内存。
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT)`,
		`CREATE TABLE IF NOT EXISTS sources (
			id TEXT PRIMARY KEY, kind TEXT NOT NULL, parent_id TEXT,
			seq INTEGER NOT NULL UNIQUE, submitted_at TEXT NOT NULL,
			raw TEXT NOT NULL, diagnostics TEXT NOT NULL DEFAULT '[]')`,
		`CREATE TABLE IF NOT EXISTS derived (
			id TEXT PRIMARY KEY, kind TEXT NOT NULL, algo_version TEXT NOT NULL,
			param_fp TEXT NOT NULL, source_ids TEXT NOT NULL,
			seq INTEGER NOT NULL UNIQUE, status TEXT NOT NULL, payload TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY, seq INTEGER NOT NULL UNIQUE, at TEXT NOT NULL,
			actor TEXT NOT NULL, action TEXT NOT NULL, target_id TEXT,
			payload TEXT, undone_by TEXT)`,
		`CREATE TABLE IF NOT EXISTS params (
			id TEXT PRIMARY KEY, seq INTEGER NOT NULL UNIQUE, payload TEXT NOT NULL)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// NextSeq 返回全局单调序号（虚拟时间顺序的基础）。
func (s *Store) NextSeq() (int64, error) {
	var cur int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM (
		SELECT seq FROM sources UNION ALL SELECT seq FROM derived
		UNION ALL SELECT seq FROM events UNION ALL SELECT seq FROM params)`).Scan(&cur)
	if err != nil {
		return 0, err
	}
	return cur + 1, nil
}

// CountID 用于生成确定性的稳定身份：kind-序号。
func (s *Store) CountID(prefix string) (string, error) {
	var n int64
	var err error
	switch prefix {
	case "src":
		err = s.db.QueryRow(`SELECT COUNT(*) FROM sources`).Scan(&n)
	case "drv":
		err = s.db.QueryRow(`SELECT COUNT(*) FROM derived`).Scan(&n)
	case "evt":
		err = s.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n)
	case "par":
		err = s.db.QueryRow(`SELECT COUNT(*) FROM params`).Scan(&n)
	default:
		return "", fmt.Errorf("unknown id prefix %q", prefix)
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%04d", prefix, n+1), nil
}

// InsertSource 追加一份源版本（不可覆盖）。
func (s *Store) InsertSource(v *domain.SourceVersion) error {
	diag, _ := json.Marshal(v.Diagnostics)
	_, err := s.db.Exec(`INSERT INTO sources(id,kind,parent_id,seq,submitted_at,raw,diagnostics)
		VALUES(?,?,?,?,?,?,?)`, v.ID, string(v.Kind), nullStr(v.ParentID), v.Seq, v.SubmittedAt, v.Raw, string(diag))
	return err
}

// Sources 返回某类别按 seq 升序的全部版本。
func (s *Store) Sources(kind domain.SourceKind) ([]domain.SourceVersion, error) {
	rows, err := s.db.Query(`SELECT id,kind,COALESCE(parent_id,''),seq,submitted_at,raw,diagnostics
		FROM sources WHERE kind=? ORDER BY seq`, string(kind))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSources(rows)
}

// AllSources 返回全部源版本（按 seq 升序）。
func (s *Store) AllSources() ([]domain.SourceVersion, error) {
	rows, err := s.db.Query(`SELECT id,kind,COALESCE(parent_id,''),seq,submitted_at,raw,diagnostics
		FROM sources ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSources(rows)
}

func scanSources(rows *sql.Rows) ([]domain.SourceVersion, error) {
	var out []domain.SourceVersion
	for rows.Next() {
		var v domain.SourceVersion
		var diag string
		var kind string
		if err := rows.Scan(&v.ID, &kind, &v.ParentID, &v.Seq, &v.SubmittedAt, &v.Raw, &diag); err != nil {
			return nil, err
		}
		v.Kind = domain.SourceKind(kind)
		_ = json.Unmarshal([]byte(diag), &v.Diagnostics)
		out = append(out, v)
	}
	return out, rows.Err()
}

// InsertDerived 追加一条派生值。
func (s *Store) InsertDerived(d *domain.Derived) error {
	srcs, _ := json.Marshal(d.SourceIDs)
	_, err := s.db.Exec(`INSERT INTO derived(id,kind,algo_version,param_fp,source_ids,seq,status,payload)
		VALUES(?,?,?,?,?,?,?,?)`, d.ID, string(d.Kind), d.AlgoVersion, d.ParamFP, string(srcs), d.Seq, string(d.Status), d.Payload)
	return err
}

// MarkDerivedStale 把引用指定源版本的派生值标记为过期（局部失效）。
func (s *Store) MarkDerivedStale(sourceID string) (int, error) {
	rows, err := s.db.Query(`SELECT id, source_ids FROM derived WHERE status=?`, string(domain.StatusCurrent))
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id, srcs string
		if err := rows.Scan(&id, &srcs); err != nil {
			rows.Close()
			return 0, err
		}
		var list []string
		_ = json.Unmarshal([]byte(srcs), &list)
		for _, sid := range list {
			if sid == sourceID {
				ids = append(ids, id)
				break
			}
		}
	}
	rows.Close()
	for _, id := range ids {
		if _, err := s.db.Exec(`UPDATE derived SET status=? WHERE id=?`, string(domain.StatusStale), id); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// Derived 按类别返回派生值（按 seq 升序）。
func (s *Store) Derived(kind domain.DerivedKind) ([]domain.Derived, error) {
	rows, err := s.db.Query(`SELECT id,kind,algo_version,param_fp,source_ids,seq,status,payload
		FROM derived WHERE kind=? ORDER BY seq`, string(kind))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDerived(rows)
}

// AllDerived 返回全部派生值。
func (s *Store) AllDerived() ([]domain.Derived, error) {
	rows, err := s.db.Query(`SELECT id,kind,algo_version,param_fp,source_ids,seq,status,payload
		FROM derived ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDerived(rows)
}

func scanDerived(rows *sql.Rows) ([]domain.Derived, error) {
	var out []domain.Derived
	for rows.Next() {
		var d domain.Derived
		var kind, status, srcs string
		if err := rows.Scan(&d.ID, &kind, &d.AlgoVersion, &d.ParamFP, &srcs, &d.Seq, &status, &d.Payload); err != nil {
			return nil, err
		}
		d.Kind = domain.DerivedKind(kind)
		d.Status = domain.DerivedStatus(status)
		_ = json.Unmarshal([]byte(srcs), &d.SourceIDs)
		out = append(out, d)
	}
	return out, rows.Err()
}

// InsertEvent 追加事件。
func (s *Store) InsertEvent(e *domain.Event) error {
	_, err := s.db.Exec(`INSERT INTO events(id,seq,at,actor,action,target_id,payload,undone_by)
		VALUES(?,?,?,?,?,?,?,?)`, e.ID, e.Seq, e.At, e.Actor, string(e.Action), nullStr(e.TargetID), nullStr(e.Payload), nullStr(e.UndoneBy))
	return err
}

// Events 返回全部事件（按 seq 升序）。
func (s *Store) Events() ([]domain.Event, error) {
	rows, err := s.db.Query(`SELECT id,seq,at,actor,action,COALESCE(target_id,''),COALESCE(payload,''),COALESCE(undone_by,'')
		FROM events ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var action string
		if err := rows.Scan(&e.ID, &e.Seq, &e.At, &e.Actor, &action, &e.TargetID, &e.Payload, &e.UndoneBy); err != nil {
			return nil, err
		}
		e.Action = domain.DecisionAction(action)
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkEventUndone 记录某事件被 undoID 撤销（保留原事件）。
func (s *Store) MarkEventUndone(eventID, undoID string) error {
	_, err := s.db.Exec(`UPDATE events SET undone_by=? WHERE id=?`, undoID, eventID)
	return err
}

// SaveParams 追加一个参数版本。
func (s *Store) SaveParams(id string, seq int64, p domain.Params) error {
	b, _ := json.Marshal(p)
	_, err := s.db.Exec(`INSERT INTO params(id,seq,payload) VALUES(?,?,?)`, id, seq, string(b))
	return err
}

// ParamsVersions 返回全部参数版本（按 seq 升序）。
func (s *Store) ParamsVersions() ([]struct {
	ID  string
	Seq int64
	P   domain.Params
}, error,
) {
	rows, err := s.db.Query(`SELECT id,seq,payload FROM params ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		ID  string
		Seq int64
		P   domain.Params
	}
	for rows.Next() {
		var id, payload string
		var seq int64
		if err := rows.Scan(&id, &seq, &payload); err != nil {
			return nil, err
		}
		var p domain.Params
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return nil, err
		}
		out = append(out, struct {
			ID  string
			Seq int64
			P   domain.Params
		}{id, seq, p})
	}
	return out, rows.Err()
}

// LatestParams 返回最新参数；无则返回默认。
func (s *Store) LatestParams() (domain.Params, error) {
	vs, err := s.ParamsVersions()
	if err != nil || len(vs) == 0 {
		return domain.DefaultParams(), err
	}
	return vs[len(vs)-1].P, nil
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
