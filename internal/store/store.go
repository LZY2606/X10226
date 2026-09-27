package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"tga/internal/domain"
)

// Clock supplies virtual time. Tests and the bundled case engine pin it; the
// server defaults to a monotonic test-safe clock but tests inject fixed times.
type Clock func() time.Time

// Store is an event-sourced repository over SQLite. All writes append an
// event first; read models are projections rebuilt by replaying events.
type Store struct {
	db    *sql.DB
	clock Clock
}

func (s *Store) SetClock(c Clock) { s.clock = c }

func (s *Store) now() string {
	if s.clock != nil {
		return s.clock().UTC().Format(time.RFC3339)
	}
	return time.Now().UTC().Format(time.RFC3339)
}

// Project is the reconstructed state of one project at a given event seq.
type Project struct {
	ID        string
	Name      string
	CreatedAt string
	Sources   map[domain.Layer][]*domain.Source
	Current   map[domain.Layer]*domain.Source
	Analyses  []*domain.Analysis
	Decisions []*domain.Decision
	CurrentAnalysis *domain.Analysis
	Batches   []*ImportBatch
	Params    map[string]domain.Params
	Events    []*domain.Event
}

// ImportBatch records a multi-file import including partial failure detail.
type ImportBatch struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"project_id"`
	CommittedAt string       `json:"committed_at"`
	Status      string       `json:"status"` // complete | partial | failed
	Files       []FileResult `json:"files"`
}

// FileResult is the per-file outcome of an import batch.
type FileResult struct {
	Filename string        `json:"filename"`
	Layer    string        `json:"layer"`
	Status   string        `json:"status"` // committed | rejected
	SourceID string        `json:"source_id,omitempty"`
	Diags    []domain.Diag `json:"diags,omitempty"`
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// appendEvent is the only write primitive. It links a SHA-256 hash chain over
// (seq, project, type, time, payload, prevHash) so archive tampering is
// detectable on replay and identities stay stable through export/re-import.
func (s *Store) appendEventTx(tx *sql.Tx, projectID, typ, actor string, payload map[string]any) (*domain.Event, error) {
	var maxSeq sql.NullInt64
	if err := tx.QueryRow(
		`SELECT COALESCE(MAX(seq),0) FROM events WHERE project_id=?`, projectID).
		Scan(&maxSeq); err != nil {
		return nil, err
	}
	var prevHash sql.NullString
	_ = tx.QueryRow(
		`SELECT hash FROM events WHERE project_id=? ORDER BY seq DESC LIMIT 1`, projectID).
		Scan(&prevHash)
	ev := &domain.Event{
		Seq: int(maxSeq.Int64) + 1, ProjectID: projectID, Type: typ,
		At: s.now(), Actor: actor, PrevHash: prevHash.String, Payload: payload,
	}
	ev.ID = domain.ShortID("evt", map[string]any{
		"project": projectID, "seq": ev.Seq, "type": typ, "payload": domain.Hash(payload)})
	ev.Hash = domain.ShortID("h", map[string]any{
		"seq": ev.Seq, "project": projectID, "type": typ, "at": ev.At,
		"actor": actor, "prev": ev.PrevHash, "payload": domain.Hash(payload)})
	_, err := tx.Exec(`INSERT INTO events
	  (seq,id,project_id,type,at,actor,prev_hash,hash,payload)
	  VALUES (?,?,?,?,?,?,?,?,?)`,
		ev.Seq, ev.ID, projectID, typ, ev.At, actor, ev.PrevHash, ev.Hash,
		mustJSON(payload))
	if err != nil {
		return nil, err
	}
	return ev, nil
}

// CreateProject appends a project_created event. IDs are name-derived so a
// replayed archive recreates the same project id.
func (s *Store) CreateProject(name string) (string, error) {
	id := domain.ShortID("proj", map[string]any{"name": name})
	err := s.tx(func(tx *sql.Tx) error {
		var exists int
		_ = tx.QueryRow(`SELECT COUNT(1) FROM projects WHERE id=?`, id).Scan(&exists)
		if exists != 0 {
			return ErrExists
		}
		_, err := s.appendEventTx(tx, id, "project_created", "system",
			map[string]any{"name": name})
		return err
	})
	return id, err
}

func (s *Store) tx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ListProjects returns projected project summaries.
func (s *Store) ListProjects() ([]*Project, error) {
	rows, err := s.db.Query(`SELECT id,name,created_at FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	for rows.Next() {
		p := &Project{}
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// LoadProject replays the event stream into the full projection.
func (s *Store) LoadProject(id string) (*Project, error) {
	return s.loadAt(id, -1)
}

// LoadAt replays up to and inclusive of sequence atSeq (historical view).
func (s *Store) LoadAt(id, atSeq string) (*Project, error) {
	// atSeq parsing handled by caller-facing wrapper
	return s.loadAt(id, parseSeq(atSeq))
}

func parseSeq(x string) int {
	n := 0
	for _, c := range x {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func (s *Store) loadAt(id string, atSeq int) (*Project, error) {
	p := &Project{ID: id, Sources: map[domain.Layer][]*domain.Source{},
		Current: map[domain.Layer]*domain.Source{}, Params: map[string]domain.Params{}}
	rows, err := s.db.Query(
		`SELECT seq,id,type,at,actor,prev_hash,hash,payload FROM events
		 WHERE project_id=? ORDER BY seq`, id)
	if err != nil {
		return nil, err
	}
	var evs []*domain.Event
	for rows.Next() {
		ev := &domain.Event{}
		var payload string
		if err := rows.Scan(&ev.Seq, &ev.ID, &ev.Type, &ev.At, &ev.Actor,
			&ev.PrevHash, &ev.Hash, &payload); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &ev.Payload); err != nil {
			rows.Close()
			return nil, err
		}
		evs = append(evs, ev)
	}
	rows.Close()
	if len(evs) == 0 {
		return nil, ErrNotFound
	}
	for _, ev := range evs {
		if atSeq >= 0 && ev.Seq > atSeq {
			break
		}
		if err := s.reduce(p, ev); err != nil {
			return nil, fmt.Errorf("reduce seq %d: %w", ev.Seq, err)
		}
		p.Events = append(p.Events, ev)
	}
	s.finalize(p)
	return p, nil
}

func (s *Store) finalize(p *Project) {
	var keys []domain.Layer
	for k := range p.Sources {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		list := p.Sources[k]
		sort.Slice(list, func(i, j int) bool { return list[i].Version < list[j].Version })
		for i, src := range list {
			src.Superseded = i < len(list)-1
		}
		p.Current[k] = list[len(list)-1]
	}
	sort.Slice(p.Analyses, func(i, j int) bool { return p.Analyses[i].Seq < p.Analyses[j].Seq })
	// current = newest analysis whose inputs were the latest at its creation;
	// recomputed and flagged stale by the service when newer sources exist.
	if len(p.Analyses) > 0 {
		p.CurrentAnalysis = p.Analyses[len(p.Analyses)-1]
	}
}
