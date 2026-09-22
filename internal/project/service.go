package project

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"tgareview/internal/bundle"
	"tgareview/internal/clock"
	"tgareview/internal/domain"
	"tgareview/internal/engine"
	"tgareview/internal/ids"
	"tgareview/internal/store"
)

// Service is the application service over a Store.
type Service struct {
	st  *store.Store
	clk clock.Clock
}

func New(st *store.Store, clk clock.Clock) *Service {
	if clk == nil {
		clk = clock.System{}
	}
	return &Service{st: st, clk: clk}
}

var ErrExists = errors.New("project already exists")

// CreateProject appends project_created. id may be supplied (re-import) or empty.
func (s *Service) CreateProject(ctx context.Context, id, name, desc string) (string, error) {
	if id == "" {
		id = ids.Of("proj", "project", name+"|"+s.clk.Now().Format(time.RFC3339Nano))
	}
	var existing string
	err := s.st.DB.QueryRowContext(ctx, `SELECT id FROM projects WHERE id=?`, id).Scan(&existing)
	if err == nil {
		return "", fmt.Errorf("%w: %s", ErrExists, id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	now := s.clk.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(id,name,description,created_at) VALUES(?,?,?,?)`,
		id, name, desc, now); err != nil {
		return "", err
	}
	if err := s.appendEvent(ctx, tx, id, 1, EvProjectCreated, now, marshalPayload(CreatedPayload{Name: name, Description: desc})); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

// ImportResult reports what an import did.
type ImportResult struct {
	ProjectID   string
	ImportID    string
	Status      string
	Accepted    []LayerCommitInfo
	Diagnostics []bundle.FragmentDiagnostic
	ArtifactID  string
	Sequence    int64
}

// Import validates and commits a bundle. Fatal envelope errors are returned to
// the caller with nothing written; structural layer failures are retained as a
// partial import (half-failure) and other layers still commit.
func (s *Service) Import(ctx context.Context, projectID string, b *bundle.Bundle) (*ImportResult, error) {
	res := bundle.Validate(b)
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := s.clk.Now().UTC().Format(time.RFC3339Nano)
	seqStart, err := s.nextSeq(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	importID := ids.Of("imp", projectID, fmt.Sprintf("%d|%s|%d", seqStart, b.Name, len(res.Accepted)))

	var committed []LayerCommitInfo
	for _, in := range res.Accepted {
		rev, err := s.commitLayer(ctx, tx, projectID, in, now)
		if err != nil {
			return nil, err
		}
		committed = append(committed, rev)
	}
	for _, d := range res.Diagnostics {
		var row sql.NullInt64
		if d.Row != nil {
			row = sql.NullInt64{Int64: int64(*d.Row), Valid: true}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO fragments(project_id,seq,kind,row_idx,field,message,raw_value) VALUES(?,?,?,?,?,?,?)`,
			projectID, seqStart, string(d.LayerKind), row, d.Field, d.Message, d.Raw); err != nil {
			return nil, err
		}
	}

	status := "committed"
	if len(committed) == 0 {
		status = "rejected"
	} else if len(res.RejectedKinds) > 0 || len(res.Diagnostics) > 0 {
		status = "partial"
	}
	seq, err := s.nextSeq(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}
	impPayload := ImportPayload{ImportID: importID, Status: status, Accepted: committed, Diagnostics: res.Diagnostics}
	if err := s.appendEvent(ctx, tx, projectID, seq, EvImportSubmitted, now, marshalPayload(impPayload)); err != nil {
		return nil, err
	}

	var artID string
	if len(committed) > 0 {
		artID, err = s.recompute(ctx, tx, projectID, seq)
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &ImportResult{ProjectID: projectID, ImportID: importID, Status: status,
		Accepted: committed, Diagnostics: res.Diagnostics, ArtifactID: artID, Sequence: seq}, nil
}

func (s *Service) commitLayer(ctx context.Context, tx *sql.Tx, projectID string, in bundle.LayerInput, now string) (LayerCommitInfo, error) {
	seq, err := s.nextSeq(ctx, tx, projectID)
	if err != nil {
		return LayerCommitInfo{}, err
	}
	canonical := string(domain.CanonicalJSON(in.Data))
	rev := ids.Of("rev", projectID, string(in.Kind)+"|"+in.ParentRevision+"|"+canonical)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO layer_revisions(project_id,rev_id,kind,parent_rev,seq,source_file,raw_canonical,status)
		 VALUES(?,?,?,?,?,?,?, 'accepted')
		 ON CONFLICT(project_id,rev_id) DO NOTHING`,
		projectID, rev, string(in.Kind), in.ParentRevision, seq, in.SourceFile, canonical)
	if err != nil {
		return LayerCommitInfo{}, err
	}
	if err := s.appendEvent(ctx, tx, projectID, seq, EvLayerCommitted, now, marshalPayload(
		LayerCommitInfo{Kind: in.Kind, RevID: rev, ParentRev: in.ParentRevision, SourceFile: in.SourceFile})); err != nil {
		return LayerCommitInfo{}, err
	}
	return LayerCommitInfo{Kind: in.Kind, RevID: rev, ParentRev: in.ParentRevision, SourceFile: in.SourceFile}, nil
}

// ReviseParams creates a new parameter version with a parent relation and
// recomputes. Only derived artifacts depend on parameters; raw layers are
// untouched.
func (s *Service) ReviseParams(ctx context.Context, projectID string, p engine.Params, reason string) (string, string, error) {
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	now := s.clk.Now().UTC().Format(time.RFC3339Nano)
	seq, err := s.nextSeq(ctx, tx, projectID)
	if err != nil {
		return "", "", err
	}
	var parent string
	// parent params rev id: read latest from prior params events (simple scan)
	parent = s.latestParamsRev(ctx, tx, projectID)
	pb, _ := json.Marshal(p)
	rev := ids.Of("pre", projectID, string(domain.CanonicalJSON(pb))+"|"+reason)
	if err := s.appendEvent(ctx, tx, projectID, seq, EvParamsRevised, now,
		marshalPayload(ParamsPayload{ParamsRevID: rev, ParentRevID: parent, Params: p, Reason: reason})); err != nil {
		return "", "", err
	}
	artID, err := s.recompute(ctx, tx, projectID, seq)
	if err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	return rev, artID, nil
}

func (s *Service) latestParamsRev(ctx context.Context, tx *sql.Tx, projectID string) string {
	rows, err := tx.QueryContext(ctx, `SELECT payload FROM events WHERE project_id=? AND type=? ORDER BY seq DESC LIMIT 1`,
		projectID, EvParamsRevised)
	if err != nil {
		return ""
	}
	defer rows.Close()
	if rows.Next() {
		var raw string
		_ = rows.Scan(&raw)
		var pl ParamsPayload
		if json.Unmarshal([]byte(raw), &pl) == nil {
			return pl.ParamsRevID
		}
	}
	return ""
}

// Decide records a human accept/reject/parameter decision. A decision that
// conflicts with the current live decision supersedes it; nothing is deleted.
func (s *Service) Decide(ctx context.Context, projectID, artifactID, targetID, kind, value, rationale string) (*Decision, error) {
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := s.clk.Now().UTC().Format(time.RFC3339Nano)
	seq, err := s.nextSeq(ctx, tx, projectID)
	if err != nil {
		return nil, err
	}

	state, err := s.replayAt(ctx, tx, projectID, seq-1)
	if err != nil {
		return nil, err
	}
	var supersedes, conflictOf string
	if prior := state.Decisions[targetID]; prior != nil && !prior.Undone {
		supersedes = prior.ID
	}
	if kind == "accept" {
		if cand := findCandidate(state, artifactID, targetID); cand != nil {
			for _, other := range cand.OverlapsWith {
				if d := state.Decisions[other]; d != nil && !d.Undone && d.Kind == "accept" {
					conflictOf = d.ID
				}
			}
		}
	}
	id := ids.Of("dec", projectID, fmt.Sprintf("%d|%s|%s|%s|%s", seq, targetID, kind, value, rationale))
	pl := DecisionPayload{DecisionID: id, ArtifactID: artifactID, TargetID: targetID,
		Kind: kind, Value: value, Rationale: rationale, Supersedes: supersedes, ConflictOf: conflictOf}
	if err := s.appendEvent(ctx, tx, projectID, seq, EvDecisionRecorded, now, marshalPayload(pl)); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO decisions(project_id,id,seq,artifact_id,target_id,kind,value,rationale,supersedes,undone)
		 VALUES(?,?,?,?,?,?,?,?,?,0)`,
		projectID, id, seq, artifactID, targetID, kind, value, rationale, supersedes); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Decision{ID: id, Seq: seq, ArtifactID: artifactID, TargetID: targetID, Kind: kind,
		Value: value, Rationale: rationale, Supersedes: supersedes, ConflictOf: conflictOf}, nil
}

// UndoDecision appends an undo event; the decision stays in history but is no
// longer live.
func (s *Service) UndoDecision(ctx context.Context, projectID, decisionID string) (int64, error) {
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := s.clk.Now().UTC().Format(time.RFC3339Nano)
	seq, err := s.nextSeq(ctx, tx, projectID)
	if err != nil {
		return 0, err
	}
	if err := s.appendEvent(ctx, tx, projectID, seq, EvDecisionUndone, now, marshalPayload(UndoPayload{DecisionID: decisionID})); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE decisions SET undone=1 WHERE project_id=? AND id=?`, projectID, decisionID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return seq, nil
}

func findCandidate(st *State, artifactID, targetID string) *engine.StageCandidate {
	art := st.Artifacts[artifactID]
	if art == nil {
		return nil
	}
	for i := range art.Candidates {
		if art.Candidates[i].ID == targetID {
			return &art.Candidates[i]
		}
	}
	return nil
}

func (s *Service) appendEvent(ctx context.Context, tx *sql.Tx, projectID string, seq int64, t EventType, at, payload string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO events(project_id,seq,type,at,payload) VALUES(?,?,?,?,?)`,
		projectID, seq, string(t), at, payload)
	return err
}

func (s *Service) nextSeq(ctx context.Context, tx *sql.Tx, projectID string) (int64, error) {
	var n int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE project_id=?`, projectID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// ProjectInfo is a list row.
type ProjectInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	MaxSeq      int64  `json:"max_seq"`
}

// ListProjects returns all projects ordered by creation time then id.
func (s *Service) ListProjects(ctx context.Context) ([]ProjectInfo, error) {
	rows, err := s.st.DB.QueryContext(ctx,
		`SELECT p.id,p.name,p.description,p.created_at,COALESCE(MAX(e.seq),0)
		 FROM projects p LEFT JOIN events e ON e.project_id=p.id
		 GROUP BY p.id ORDER BY p.created_at ASC, p.id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectInfo
	for rows.Next() {
		var p ProjectInfo
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.MaxSeq); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// GetProject reads one project header.
func (s *Service) GetProject(ctx context.Context, id string) (*ProjectInfo, error) {
	var p ProjectInfo
	err := s.st.DB.QueryRowContext(ctx,
		`SELECT p.id,p.name,p.description,p.created_at,COALESCE(MAX(e.seq),0)
		 FROM projects p LEFT JOIN events e ON e.project_id=p.id WHERE p.id=?`, id).
		Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.MaxSeq)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// DeleteProject removes all local rows for a project (used by the
// export/delete/reimport acceptance path).
func (s *Service) DeleteProject(ctx context.Context, id string) error {
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, tbl := range []string{"events", "layer_revisions", "fragments", "artifacts", "decisions", "projects"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+tbl+` WHERE project_id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MaxSeq returns the latest event sequence.
func (s *Service) MaxSeq(ctx context.Context, id string) (int64, error) {
	var n int64
	err := s.st.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM events WHERE project_id=?`, id).Scan(&n)
	return n, err
}

// EventInfo is one row of the audit timeline.
type EventInfo struct {
	Seq     int64  `json:"seq"`
	Type    string `json:"type"`
	At      string `json:"at"`
	Payload string `json:"payload"`
}

// EventList returns the full append-only event log in order.
func (s *Service) EventList(ctx context.Context, id string) ([]EventInfo, error) {
	rows, err := s.st.DB.QueryContext(ctx,
		`SELECT seq,type,at,payload FROM events WHERE project_id=? ORDER BY seq ASC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventInfo
	for rows.Next() {
		var e EventInfo
		if err := rows.Scan(&e.Seq, &e.Type, &e.At, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
