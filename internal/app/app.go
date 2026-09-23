// Package app is the transactional service over the event store. Every
// mutation replays the log, applies one action, and appends atomically, so
// restart/reopen always continues from the same deterministic history.
package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"tga-archive/internal/domain"
	"tga-archive/internal/reduce"
	"tga-archive/internal/store"
)

// App is the application service.
type App struct {
	st     *store.Store
	mu     sync.Mutex
	start  time.Time
}

// New wraps an opened store.
func New(st *store.Store) *App {
	return &App{st: st, start: time.Now().UTC()}
}

// Store exposes the underlying store (for tests).
func (a *App) Store() *store.Store { return a.st }

func (a *App) clockFor(projectID, seed string) *Clock {
	// Clock is deterministic per project seed, stable across restarts.
	if seed == "" {
		seed = projectID
	}
	return NewClock(seed)
}

// ProjectDetail is the full replayed view used by the review page.
type ProjectDetail struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	State    *reduce.State          `json:"state"`
	Events   []domain.Event         `json:"events"`
	Seed     string                 `json:"seed"`
}

// CreateProject appends project.created.
func (a *App) CreateProject(ctx context.Context, name, seed string) (*ProjectDetail, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if seed == "" {
		seed = fmt.Sprintf("seed-%d", time.Now().UnixNano())
	}
	pid, ev := reduce.CreateProject(name, seed, "", a.clockFor("", seed))
	exists, err := a.st.ProjectExists(ctx, pid)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("project with that name/seed already exists")
	}
	if err := a.st.AppendProject(ctx, pid, name, ev); err != nil {
		return nil, err
	}
	return a.load(ctx, pid)
}

// Get loads and replays one project.
func (a *App) Get(ctx context.Context, pid string) (*ProjectDetail, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.load(ctx, pid)
}

// ListProjects returns project rows.
func (a *App) ListProjects(ctx context.Context) ([]store.ProjectRow, error) {
	return a.st.ListProjects(ctx)
}

func (a *App) load(ctx context.Context, pid string) (*ProjectDetail, error) {
	evs, err := a.st.Events(ctx, pid)
	if err != nil {
		return nil, err
	}
	if len(evs) == 0 {
		return nil, fmt.Errorf("project not found: %s", pid)
	}
	var info domain.ProjectInfo
	_ = json.Unmarshal(evs[0].Payload, &info)
	st, err := reduce.Replay(evs)
	if err != nil {
		return nil, err
	}
	if err := a.st.ReplaceArtifactProjection(ctx, pid, artMap(st)); err != nil {
		return nil, err
	}
	return &ProjectDetail{ID: pid, Name: info.Name, State: st, Events: evs, Seed: info.ClockSeed}, nil
}

func artMap(st *reduce.State) map[string]store.ProjArt {
	out := map[string]store.ProjArt{}
	for id, ar := range st.Artifacts {
		out[id] = store.ProjArt{Kind: ar.Kind, FirstSeq: ar.FirstSeq, LatestSeq: ar.LatestSeq}
	}
	return out
}

// ImportBundle applies an import action.
func (a *App) ImportBundle(ctx context.Context, pid string, b domain.Bundle) (*ProjectDetail, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	d, err := a.load(ctx, pid)
	if err != nil {
		return nil, err
	}
	clk := a.clockFor(pid, d.Seed)
	clk.ForwardTo(len(d.Events))
	out, err := d.State.ApplyBundle(b, clk)
	if err != nil {
		return nil, err
	}
	if err := a.st.AppendEvents(ctx, pid, out.Events); err != nil {
		return nil, err
	}
	return a.load(ctx, pid)
}

// ReviseParams applies a parameter revision.
func (a *App) ReviseParams(ctx context.Context, pid string, p domain.Params, reason string) (*ProjectDetail, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	d, err := a.load(ctx, pid)
	if err != nil {
		return nil, err
	}
	clk := a.clockFor(pid, d.Seed)
	clk.ForwardTo(len(d.Events))
	out, err := d.State.ReviseParams(p, reason, clk)
	if err != nil {
		return nil, err
	}
	if err := a.st.AppendEvents(ctx, pid, out.Events); err != nil {
		return nil, err
	}
	return a.load(ctx, pid)
}

// Decide records a decision/conflict/undo.
func (a *App) Decide(ctx context.Context, pid string, kind string, raw json.RawMessage) (*ProjectDetail, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	d, err := a.load(ctx, pid)
	if err != nil {
		return nil, err
	}
	clk := a.clockFor(pid, d.Seed)
	clk.ForwardTo(len(d.Events))
	var ev domain.Event
	switch kind {
	case "decision":
		var dec domain.Decision
		if err := json.Unmarshal(raw, &dec); err != nil {
			return nil, err
		}
		ev, err = d.State.RecordDecision(dec, clk)
	case "resolve":
		var c domain.ConflictResolution
		if err := json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		ev, err = d.State.ResolveConflict(c, clk)
	case "undo":
		var u domain.Undo
		if err := json.Unmarshal(raw, &u); err != nil {
			return nil, err
		}
		ev, err = d.State.UndoDecision(u, clk)
	default:
		return nil, fmt.Errorf("unknown decision kind %q", kind)
	}
	if err != nil {
		return nil, err
	}
	if err := a.st.AppendEvents(ctx, pid, []domain.Event{ev}); err != nil {
		return nil, err
	}
	return a.load(ctx, pid)
}

// AsOf replays through seq and returns the historical snapshot and state.
func (a *App) AsOf(ctx context.Context, pid string, seq int) (*ProjectDetail, *reduce.Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	evs, err := a.st.Events(ctx, pid)
	if err != nil {
		return nil, nil, err
	}
	if seq > len(evs) {
		seq = len(evs)
	}
	st, err := reduce.Replay(evs[:seq])
	if err != nil {
		return nil, nil, err
	}
	snap := st.CurrentSnapshot()
	var info domain.ProjectInfo
	_ = json.Unmarshal(evs[0].Payload, &info)
	return &ProjectDetail{ID: pid, Name: info.Name, State: st, Events: evs[:seq], Seed: info.ClockSeed}, snap, nil
}

// ---- archive export / import ----

// Archive is the portable package. Ids are content-addressed so importing the
// archive after deleting the local database restores identity, order and
// provenance exactly.
type Archive struct {
	Format  string         `json:"format"`
	Version string         `json:"version"`
	Project ArchivedProject `json:"project"`
	SHA256  string         `json:"sha256"`
}

type ArchivedProject struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Events []domain.Event `json:"events"`
}

// Export builds the archive and self-hashes everything except the hash field.
func (a *App) Export(ctx context.Context, pid string) (*Archive, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	evs, err := a.st.Events(ctx, pid)
	if err != nil {
		return nil, err
	}
	if len(evs) == 0 {
		return nil, fmt.Errorf("project not found")
	}
	var info domain.ProjectInfo
	_ = json.Unmarshal(evs[0].Payload, &info)
	ar := &Archive{
		Format:  "tga-archive",
		Version: "1",
		Project: ArchivedProject{ID: pid, Name: info.Name, Events: evs},
	}
	ar.SHA256 = hashArchive(*ar)
	return ar, nil
}

func hashArchive(ar Archive) string {
	clone := ar
	clone.SHA256 = ""
	b, _ := json.MarshalIndent(clone, "", "  ")
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Verify checks the self-hash of an archive.
func Verify(ar *Archive) error {
	want := hashArchive(*ar)
	if want != ar.SHA256 {
		return fmt.Errorf("archive hash mismatch: want %s got %s", want, ar.SHA256)
	}
	return nil
}

// ImportArchive verifies, replays for validity and appends the whole log.
// Conflicting identical project ids are refused unless replace=true, in which
// case the project rows are deleted first (the exported events are the truth).
func (a *App) ImportArchive(ctx context.Context, ar *Archive, replace bool) (string, error) {
	if err := Verify(ar); err != nil {
		return "", err
	}
	if ar.Format != "tga-archive" {
		return "", fmt.Errorf("unsupported format %q", ar.Format)
	}
	if _, err := reduce.Replay(ar.Project.Events); err != nil {
		return "", fmt.Errorf("archive replay failed: %w", err)
	}
	// event ids must match content-addressed scheme
	for _, ev := range ar.Project.Events {
		want := domain.ContentID("ev", ar.Project.ID, ev.Seq, ev.Type, string(ev.Payload), ev.At)
		if want != ev.ID {
			return "", fmt.Errorf("event identity mismatch at seq %d", ev.Seq)
		}
	}
	if err := a.importLocked(ctx, ar, replace); err != nil {
		return "", err
	}
	return ar.Project.ID, nil
}

func (a *App) importLocked(ctx context.Context, ar *Archive, replace bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	exists, err := a.st.ProjectExists(ctx, ar.Project.ID)
	if err != nil {
		return err
	}
	if exists && !replace {
		return fmt.Errorf("project already exists (use replace to overwrite)")
	}
	if exists {
		if _, err := a.st.Exec(ctx, `DELETE FROM events WHERE project_id=?`, ar.Project.ID); err != nil {
			return err
		}
		if _, err := a.st.Exec(ctx, `DELETE FROM projects WHERE id=?`, ar.Project.ID); err != nil {
			return err
		}
		if _, err := a.st.Exec(ctx, `DELETE FROM artifacts WHERE project_id=?`, ar.Project.ID); err != nil {
			return err
		}
	}
	first := ar.Project.Events[0]
	if err := a.st.AppendProject(ctx, ar.Project.ID, ar.Project.Name, first); err != nil {
		return err
	}
	sorted := append([]domain.Event{}, ar.Project.Events[1:]...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })
	return a.st.AppendEvents(ctx, ar.Project.ID, sorted)
}

var _ = sort.Strings
