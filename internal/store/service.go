package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"tgaaudit/internal/domain"
	"tgaaudit/internal/pipeline"
)

// CreateProject appends a project_created event and projections.
func (s *Store) CreateProject(ctx context.Context, name string, seed int64, note string) (string, error) {
	id := deterministicID("project", name, fmt.Sprint(seed), s.now().Format(timeLayout))
	payload := map[string]any{"name": name, "seed": seed, "note": note}
	ev, err := s.appendEvent(ctx, id, domain.EvProjectCreated, payload)
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO projects(id,name,created_at,seed,note) VALUES(?,?,?,?,?)
		 ON CONFLICT(id) DO NOTHING`,
		id, name, ev.At, seed, note)
	return id, err
}

const timeLayout = "2006-01-02T15:04:05.000000000Z07:00"

func deterministicID(prefix string, parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%s|", p)
	}
	sum := h.Sum(nil)
	return prefix + "_" + hex.EncodeToString(sum[:14])
}

// appendEvent is the only writer to the events table. It enforces the
// per-project hash chain inside the store mutex / transaction.
func (s *Store) appendEvent(ctx context.Context, projectID, typ string, payload any) (domain.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback()
	var prevHash string
	var maxSeq int
	row := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq),0), COALESCE(hash,'') FROM events WHERE project_id=?`,
		projectID)
	if err := row.Scan(&maxSeq, &prevHash); err != nil {
		return domain.Event{}, err
	}
	pj, err := json.Marshal(payload)
	if err != nil {
		return domain.Event{}, err
	}
	at := s.now().Format(timeLayout)
	evID := deterministicID("ev", projectID, fmt.Sprint(maxSeq+1), typ, string(pj), at)
	sum := sha256.Sum256(append(append([]byte(prevHash), pj...), []byte(at)...))
	hash := hex.EncodeToString(sum[:])
	ev := domain.Event{ID: evID, Seq: maxSeq + 1, ProjectID: projectID, At: at,
		Type: typ, Payload: pj, PrevHash: prevHash, Hash: hash}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO events(seq,id,project_id,at,type,payload,prev_hash,hash)
		 VALUES(?,?,?,?,?,?,?,?)`,
		ev.Seq, ev.ID, ev.ProjectID, ev.At, ev.Type, string(ev.Payload),
		ev.PrevHash, ev.Hash)
	if err != nil {
		return domain.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

// SectionInput is a decoded section awaiting validation.
type SectionInput struct {
	Kind   string
	Table  *domain.Table
	ParseE string
}

// ImportResult reports the full import outcome including half failures.
type ImportResult struct {
	Attempt   domain.ImportAttempt `json:"attempt"`
	Committed []string             `json:"committed"`
	Current   *DerivationSnapshot  `json:"current,omitempty"`
}

// Import validates, records the attempt, commits good sections immutably and
// reruns the deterministic pipeline. Bad sections are recorded but not
// committed (half failure); the attempt always exists.
func (s *Store) Import(ctx context.Context, projectID, bundleChecksum string,
	inputs []SectionInput) (*ImportResult, error) {
	attempt := domain.ImportAttempt{
		ID:             deterministicID("att", projectID, bundleChecksum, s.now().Format(timeLayout)),
		At:             s.now().Format(timeLayout),
		BundleChecksum: bundleChecksum,
		Sections:       []domain.AttemptSection{},
	}
	anyOK, anyBad := false, false
	var committed []string
	for _, in := range inputs {
		rec := domain.AttemptSection{Kind: in.Kind}
		if in.ParseE != "" {
			rec.OK, rec.Fatal, anyBad = false, true, true
			rec.Diags = append(rec.Diags, in.ParseE)
			attempt.Sections = append(attempt.Sections, rec)
			continue
		}
		sec, accepted, rejected, diags := domain.Validate(in.Kind, in.Table)
		rec.RowsAccepted, rec.RowsRejected = accepted, rejected
		rec.Diags = append(rec.Diags, diags...)
		if sec == nil {
			rec.OK, rec.Fatal, anyBad = false, true, true
			attempt.Sections = append(attempt.Sections, rec)
			continue
		}
		rec.OK = true
		if rejected > 0 || len(diags) > 0 {
			rec.OK, anyBad = true, true // accepted but with per-row diagnostics
		}
		batchID, err := s.commitSection(ctx, projectID, sec)
		if err != nil {
			return nil, err
		}
		rec.CommittedAs = batchID
		committed = append(committed, batchID)
		anyOK = true
		attempt.Sections = append(attempt.Sections, rec)
	}
	sort.Slice(attempt.Sections, func(i, j int) bool {
		return kindOrder(attempt.Sections[i].Kind) < kindOrder(attempt.Sections[j].Kind)
	})
	attempt.OK = anyOK && !anyBad
	attempt.Partial = anyOK && anyBad
	attemptEv, err := s.appendEvent(ctx, projectID, domain.EvImportAttempted, attempt)
	if err != nil {
		return nil, err
	}
	pj, _ := json.Marshal(attempt)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO attempts(project_id,id,at,ok,partial,bundle_checksum,error,payload)
		 VALUES(?,?,?,?,?,?,?,?)`,
		projectID, attempt.ID, attempt.At, b(attempt.OK), b(attempt.Partial),
		attempt.BundleChecksum, attempt.Error, string(pj)); err != nil {
		return nil, err
	}
	_ = attemptEv
	snap, err := s.recompute(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &ImportResult{Attempt: attempt, Committed: committed, Current: snap}, nil
}

func b(x bool) int {
	if x {
		return 1
	}
	return 0
}

func kindOrder(k string) int {
	for i, kk := range domain.SectionKinds {
		if kk == k {
			return i
		}
	}
	return 99
}

func (s *Store) commitSection(ctx context.Context, projectID string, sec *domain.RawSection) (string, error) {
	version := 1
	var parentID string
	row := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version),0) FROM batches WHERE project_id=? AND kind=?`,
		projectID, sec.Kind)
	if err := row.Scan(&version); err != nil {
		return "", err
	}
	if version > 0 {
		// version currently holds max
		var pid string
		if err := s.db.QueryRowContext(ctx,
			`SELECT id FROM batches WHERE project_id=? AND kind=? AND version=?`,
			projectID, sec.Kind, version).Scan(&pid); err == nil {
			parentID = pid
		}
		version++
	}
	sec.Checksum = checksumSection(sec)
	batch := domain.Batch{Kind: sec.Kind, Version: version, ParentID: parentID,
		CommittedAt: s.now().Format(timeLayout), Section: *sec}
	batch.ID = deterministicID("batch", projectID, sec.Kind, fmt.Sprint(version), sec.Checksum)
	ev, err := s.appendEvent(ctx, projectID, domain.EvCommitted, batch)
	if err != nil {
		return "", err
	}
	batch.EventID = ev.ID
	pj, _ := json.Marshal(batch)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO batches(project_id,id,kind,version,parent_id,committed_at,event_seq,payload)
		 VALUES(?,?,?,?,?,?,?,?)`,
		projectID, batch.ID, batch.Kind, batch.Version, batch.ParentID,
		batch.CommittedAt, ev.Seq, string(pj)); err != nil {
		return "", err
	}
	return batch.ID, nil
}

func checksumSection(sec *domain.RawSection) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|", sec.Kind, sec.SourceName)
	for _, c := range sec.Columns {
		fmt.Fprintf(h, "%s[%s:%s]|", c.Name, c.Unit, c.Basis)
	}
	for _, row := range sec.Rows {
		for _, c := range row {
			if c.Value != nil {
				fmt.Fprintf(h, "n:%g|", *c.Value)
			} else {
				fmt.Fprintf(h, "r:%q:%s|", c.Raw, c.Diag)
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ReviseParams appends a new parameter version and recomputes.
func (s *Store) ReviseParams(ctx context.Context, projectID string,
	p domain.Params, reason string) (*DerivationSnapshot, error) {
	if p.AlgorithmVersion == "" {
		p.AlgorithmVersion = domain.CurrentParamsVersion
	}
	fp := pipeline.ParamFingerprint(p)
	ev, err := s.appendEvent(ctx, projectID, domain.EvParamRevised,
		map[string]any{"params": p, "fingerprint": fp, "reason": reason})
	if err != nil {
		return nil, err
	}
	pj, _ := json.Marshal(p)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO params_versions(project_id,fingerprint,version_no,created_event_seq,payload)
		 VALUES(?,?,
		   COALESCE((SELECT MAX(version_no)+1 FROM params_versions WHERE project_id=?),1),
		   ?,?)`,
		projectID, fp, projectID, ev.Seq, string(pj))
	if err != nil {
		return nil, err
	}
	return s.recompute(ctx, projectID)
}

var _ = sql.ErrNoRows

// ProjectMeta is a project list entry.
type ProjectMeta struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Seed      int64  `json:"seed"`
	CreatedAt string `json:"created_at"`
	Events    int    `json:"events"`
	Note      string `json:"note,omitempty"`
}

// ListProjects returns all projects with event counts.
func (s *Store) ListProjects(ctx context.Context) ([]ProjectMeta, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.id,p.name,p.seed,p.created_at,p.note,
		        (SELECT COUNT(1) FROM events e WHERE e.project_id=p.id)
		 FROM projects p ORDER BY p.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectMeta{}
	for rows.Next() {
		var m ProjectMeta
		if err := rows.Scan(&m.ID, &m.Name, &m.Seed, &m.CreatedAt, &m.Note,
			&m.Events); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Node returns one node payload by id (works for superseded nodes too).
func (s *Store) Node(ctx context.Context, projectID, nodeID string) (*domain.Node, error) {
	var pj string
	err := s.db.QueryRowContext(ctx,
		`SELECT payload FROM nodes WHERE project_id=? AND id=?`,
		projectID, nodeID).Scan(&pj)
	if err != nil {
		return nil, err
	}
	var n domain.Node
	if err := json.Unmarshal([]byte(pj), &n); err != nil {
		return nil, err
	}
	return &n, nil
}
