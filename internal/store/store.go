// Package store 提供基于 SQLite 的事件日志持久化。
// 事件是唯一事实来源；投影由重放事件构建。
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"tga-review/internal/core"
)

// Store 包装一个 SQLite 数据库。
type Store struct {
	db *sql.DB
}

// Open 打开（必要时创建）数据库并建表。
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// OpenMemory 打开内存数据库（测试用）。
func OpenMemory() (*Store, error) {
	db, err := sql.Open("sqlite", "file::memory:?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  type TEXT NOT NULL,
  payload TEXT NOT NULL,
  actor TEXT NOT NULL,
  at TEXT NOT NULL,
  prev_id TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_events_id ON events(id);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, seq);
`)
	return err
}

// Close 关闭数据库。
func (s *Store) Close() error { return s.db.Close() }

// SetMeta 写入元数据。
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta(key,value) VALUES(?,?)
  ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// GetMeta 读取元数据；不存在返回空串。
func (s *Store) GetMeta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// LastEvent 返回项目中最后一条事件（无则返回 nil）。
func (s *Store) LastEvent(projectID string) (*core.Event, error) {
	row := s.db.QueryRow(`SELECT seq,id,project_id,type,payload,actor,at,prev_id
  FROM events WHERE project_id=? ORDER BY seq DESC LIMIT 1`, projectID)
	ev, err := scanEvent(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ev, nil
}

// Append 追加事件；seq 由数据库分配。返回带 seq 的事件。
func (s *Store) Append(ev core.Event) (*core.Event, error) {
	res, err := s.db.Exec(`INSERT INTO events(id,project_id,type,payload,actor,at,prev_id)
  VALUES(?,?,?,?,?,?,?)`, ev.ID, ev.ProjectID, ev.Type, string(ev.Payload), ev.Actor, ev.At, ev.PrevID)
	if err != nil {
		return nil, err
	}
	seq, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	ev.Seq = seq
	return &ev, nil
}

// AppendWithSeq 以指定 seq 插入事件（导出再导入时保持身份与顺序）。
func (s *Store) AppendWithSeq(ev core.Event) error {
	_, err := s.db.Exec(`INSERT INTO events(seq,id,project_id,type,payload,actor,at,prev_id)
  VALUES(?,?,?,?,?,?,?,?)`, ev.Seq, ev.ID, ev.ProjectID, ev.Type, string(ev.Payload), ev.Actor, ev.At, ev.PrevID)
	return err
}

// HasEvent 判断事件 ID 是否已存在（幂等导入）。
func (s *Store) HasEvent(id string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM events WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

// Events 按 seq 升序列出项目事件；uptoSeq>0 时只取到该位置（时间回溯）。
func (s *Store) Events(projectID string, uptoSeq int64) ([]core.Event, error) {
	q := `SELECT seq,id,project_id,type,payload,actor,at,prev_id FROM events WHERE project_id=?`
	args := []any{projectID}
	if uptoSeq > 0 {
		q += ` AND seq<=?`
		args = append(args, uptoSeq)
	}
	q += ` ORDER BY seq ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []core.Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ev)
	}
	return out, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(row scanner) (*core.Event, error) {
	var ev core.Event
	var payload string
	if err := row.Scan(&ev.Seq, &ev.ID, &ev.ProjectID, &ev.Type, &payload, &ev.Actor, &ev.At, &ev.PrevID); err != nil {
		return nil, err
	}
	ev.Payload = json.RawMessage(payload)
	return &ev, nil
}
