package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"tga-review/internal/domain"
	"tga-review/internal/engine"
	"tga-review/internal/ingest"
)

type App struct {
	Store *Store
	Clock domain.Clock
}

func NewApp(st *Store, clock domain.Clock) *App {
	if clock == nil {
		clock = domain.SystemClock{}
	}
	return &App{Store: st, Clock: clock}
}

func randID(prefix string) string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

func shaText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (a *App) now() string { return a.Clock.Now().UTC().Format(time.RFC3339Nano) }

func (a *App) appendEvent(ctx context.Context, tx execTx, projectID, typ, actor string, payload any) (*domain.Event, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	parent, _, err := a.Store.LastEventID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ev := &domain.Event{ProjectID: projectID, Type: typ, At: a.now(), Actor: actor, ParentID: parent, Payload: body}
	if err := a.Store.AppendEvent(ctx, tx, ev); err != nil {
		return nil, err
	}
	return ev, nil
}

func (a *App) CreateProject(ctx context.Context, name, description string, labels map[string]string) (*domain.Project, error) {
	if name == "" {
		return nil, fmt.Errorf("project name required")
	}
	id := randID(domain.PrefixProject)
	p := &domain.Project{ID: id, Name: name, Description: description, Labels: labels, CreatedAt: a.Clock.Now().UTC()}
	err := a.Store.Tx(ctx, func(tx execTx) error {
		if e := a.Store.CreateProjectTx(ctx, tx, p); e != nil {
			return e
		}
		_, e := a.appendEvent(ctx, tx, id, domain.EvProjectCreated, "user", domain.ProjectCreated{Name: name, Description: description, Labels: labels})
		return e
	})
	if err != nil {
		return nil, err
	}
	params := domain.DefaultParams(id)
	params.Version = 1
	params.ID, _ = domain.ContentID(domain.PrefixVersion, "params", struct {
		Project string
		Body    string
	}{id, params.CanonicalJSON()})
	err = a.Store.Tx(ctx, func(tx execTx) error {
		if e := a.Store.SaveParams(ctx, tx, params); e != nil {
			return e
		}
		_, e := a.appendEvent(ctx, tx, id, domain.EvParamsRevised, "system", domain.ParamsRevised{
			ParamsID: params.ID, Name: params.Name,
			Fingerprint: domain.Fingerprint{Algorithm: "params", Version: domain.AlgoVersion, Params: params.CanonicalJSON()},
		})
		return e
	})
	if err != nil {
		return nil, err
	}
	return p, nil
}

// LayerInput is one layer submission in an import session.
type LayerInput struct {
	Layer     string
	MediaType string
	Text      string
}

// ImportResult reports the import state machine outcome.
type ImportResult struct {
	SessionID   string
	Status      domain.ImportStatus
	Committed   map[string]string // layer -> version id
	Diagnostics []domain.Diagnostic
	FormatErrors map[string]string // layer -> fatal format error
}

// BeginImport opens a session and appends import.began.
func (a *App) BeginImport(ctx context.Context, projectID string, layers []string) (string, error) {
	sessionID := randID(domain.PrefixImportSess)
	err := a.Store.Tx(ctx, func(tx execTx) error {
		_, e := a.appendEvent(ctx, tx, projectID, domain.EvImportBegan, "user", domain.ImportBegan{SessionID: sessionID, Layers: layers})
		return e
	})
	return sessionID, err
}

// CommitLayer parses and commits a single layer. A format error is returned and
// the layer is recorded as bad for the session, but other layers are unaffected
// (import half failure). Raw text is stored verbatim and never overwritten.
func (a *App) CommitLayer(ctx context.Context, projectID, sessionID string, in LayerInput) (*domain.SourceVersion, []domain.Diagnostic, error) {
	parsed, perr := ingest.Parse(sessionID, in.Layer, in.Text)
	var diags []domain.Diagnostic
	if parsed != nil {
		diags = parsed.Diagnostics
	}
	if perr != nil {
		return nil, diags, perr
	}
	mediaType := in.MediaType
	if mediaType == "" {
		mediaType = "text/csv"
	}
	existing, _ := a.Store.ListVersions(ctx, projectID, in.Layer)
	verNo := len(existing) + 1
	parent := ""
	if len(existing) > 0 {
		parent = existing[len(existing)-1].ID
	}
	vid := randID(domain.PrefixSource)
	sv := &domain.SourceVersion{
		ID: vid, ProjectID: projectID, Layer: in.Layer, Version: verNo,
		ParentVersion: parent, MediaType: mediaType, RawText: in.Text,
		SHA256: shaText(in.Text), CommittedAt: a.now(), Accepted: true,
		Samples: parsed.Samples, Drifts: parsed.Drifts, Program: parsed.Program,
		Gases: parsed.Gases, Blanks: parsed.Blanks, Masses: parsed.Masses,
	}
	err := a.Store.Tx(ctx, func(tx execTx) error {
		if e := a.Store.AddDiagnostics(ctx, tx, projectID, diags); e != nil {
			return e
		}
		ev, e := a.appendEvent(ctx, tx, projectID, domain.EvLayerCommitted, "user", domain.LayerCommitted{
			VersionID: vid, Layer: in.Layer, Version: verNo, Parent: parent,
			SHA: sv.SHA256, RowsOK: countRows(parsed), RowsBad: len(diags),
		})
		if e != nil {
			return e
		}
		sv.CommitEventID = ev.ID
		return a.Store.SaveVersion(ctx, tx, sv)
	})
	if err != nil {
		return nil, diags, err
	}
	return sv, diags, nil
}

// ConcludeImport closes the session as success, partial or failed.
func (a *App) ConcludeImport(ctx context.Context, projectID, sessionID string, layersOK, layersBad []string, status domain.ImportStatus, summary string) error {
	return a.Store.Tx(ctx, func(tx execTx) error {
		_, e := a.appendEvent(ctx, tx, projectID, domain.EvImportConcluded, "system", domain.ImportConcluded{
			SessionID: sessionID, Status: status, LayersOK: layersOK, LayersBad: layersBad, Summary: summary,
		})
		return e
	})
}

// ImportMany runs the full import state machine over a batch of layers.
func (a *App) ImportMany(ctx context.Context, projectID string, inputs []LayerInput) (*ImportResult, error) {
	var layers []string
	for _, in := range inputs {
		layers = append(layers, in.Layer)
	}
	sessionID, err := a.BeginImport(ctx, projectID, layers)
	if err != nil {
		return nil, err
	}
	res := &ImportResult{SessionID: sessionID, Committed: map[string]string{}, FormatErrors: map[string]string{}}
	var okLayers, badLayers []string
	for _, in := range inputs {
		sv, diags, ferr := a.CommitLayer(ctx, projectID, sessionID, in)
		res.Diagnostics = append(res.Diagnostics, diags...)
		if ferr != nil {
			badLayers = append(badLayers, in.Layer)
			res.FormatErrors[in.Layer] = ferr.Error()
			continue
		}
		okLayers = append(okLayers, in.Layer)
		res.Committed[in.Layer] = sv.ID
	}
	status := domain.ImportSuccess
	if len(badLayers) == len(inputs) {
		status = domain.ImportFailed
	} else if len(badLayers) > 0 || len(res.Diagnostics) > 0 {
		status = domain.ImportPartial
	}
	summary := fmt.Sprintf("%d layers committed, %d rejected, %d row diagnostics", len(okLayers), len(badLayers), len(res.Diagnostics))
	if e := a.ConcludeImport(ctx, projectID, sessionID, okLayers, badLayers, status, summary); e != nil {
		return nil, e
	}
	res.Status = status
	return res, nil
}

func countRows(r *ingest.Result) int {
	return len(r.Samples) + len(r.Drifts) + len(r.Program) + len(r.Gases) + len(r.Blanks) + len(r.Masses)
}

// CurrentParams returns the newest parameter set for a project.
func (a *App) CurrentParams(ctx context.Context, projectID string) (*domain.Params, error) {
	events, err := a.Store.LoadEvents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	pid := ""
	for _, e := range events {
		if e.Type == domain.EvParamsRevised {
			var pr domain.ParamsRevised
			if json.Unmarshal(e.Payload, &pr) == nil {
				pid = pr.ParamsID
			}
		}
	}
	if pid == "" {
		return nil, fmt.Errorf("no parameters")
	}
	return a.Store.LoadParams(ctx, pid)
}

// ReviseParams stores a new parameter version linked to its parent and appends
// params.revised. It does not overwrite the old version.
func (a *App) ReviseParams(ctx context.Context, projectID string, mutate func(domain.Params) domain.Params, reason string) (*domain.Params, error) {
	cur, err := a.CurrentParams(ctx, projectID)
	if err != nil {
		return nil, err
	}
	np := mutate(*cur)
	np.ProjectID = projectID
	np.Version = cur.Version + 1
	np.ParentID = cur.ID
	np.ID, _ = domain.ContentID(domain.PrefixVersion, "params", struct {
		Project string
		Body    string
	}{projectID, np.CanonicalJSON()})
	err = a.Store.Tx(ctx, func(tx execTx) error {
		if e := a.Store.SaveParams(ctx, tx, np); e != nil {
			return e
		}
		_, e := a.appendEvent(ctx, tx, projectID, domain.EvParamsRevised, "user", domain.ParamsRevised{
			ParamsID: np.ID, Parent: cur.ID, Name: np.Name, Reason: reason,
			Fingerprint: domain.Fingerprint{Algorithm: "params", Version: domain.AlgoVersion, Params: np.CanonicalJSON()},
		})
		return e
	})
	if err != nil {
		return nil, err
	}
	return &np, nil
}

func (a *App) acceptedVersions(ctx context.Context, projectID string) (map[string]*domain.SourceVersion, error) {
	out := map[string]*domain.SourceVersion{}
	for _, layer := range domain.LayerOrder {
		vers, err := a.Store.ListVersions(ctx, projectID, layer)
		if err != nil {
			return nil, err
		}
		for i := len(vers) - 1; i >= 0; i-- {
			if vers[i].Accepted {
				out[layer] = vers[i]
				break
			}
		}
	}
	return out, nil
}

// RunPipeline executes the deterministic derivation chain from the currently
// accepted source versions + current params, persisting a new batch. Only
// derived nodes in the new batch are "current"; earlier batches remain stored
// and viewable (stale, never overwritten).
func (a *App) RunPipeline(ctx context.Context, projectID, note string) (*engine.Batch, error) {
	p, err := a.CurrentParams(ctx, projectID)
	if err != nil {
		return nil, err
	}
	versions, err := a.acceptedVersions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if versions[domain.LayerSamples] == nil {
		return nil, fmt.Errorf("cannot run pipeline: samples layer not imported")
	}
	if versions[domain.LayerInitMass] == nil {
		return nil, fmt.Errorf("cannot run pipeline: init_mass layer not imported")
	}
	batchID := randID(domain.PrefixBatch)
	batch, err := engine.Run(projectID, batchID, *p, versions)
	if err != nil {
		return nil, err
	}
	var nodeIDs []string
	for _, n := range batch.Nodes {
		nodeIDs = append(nodeIDs, n.ID)
	}
	err = a.Store.Tx(ctx, func(tx execTx) error {
		for _, n := range batch.Nodes {
			if e := a.Store.SaveDerived(ctx, tx, n, ""); e != nil {
				return e
			}
		}
		_, e := a.appendEvent(ctx, tx, projectID, domain.EvDerivedProduced, "system", domain.DerivedProduced{
			BatchID: batchID, Algorithm: "pipeline", Version: domain.AlgoVersion,
			Fingerprint: domain.Fingerprint{Algorithm: "pipeline", Version: domain.AlgoVersion, Params: p.CanonicalJSON()},
			NodeIDs: nodeIDs, Note: note,
		})
		return e
	})
	if err != nil {
		return nil, err
	}
	return batch, nil
}

// Decide appends a review decision (accept/reject/adjust). A contradictory
// decision on the same target is recorded too and a conflict is raised; the
// conflict must be resolved explicitly rather than silently overwriting.
func (a *App) Decide(ctx context.Context, projectID, targetType, targetID, action, reason, paramsID string) (*domain.DecisionRecord, error) {
	if action != "accept" && action != "reject" && action != "adjust" {
		return nil, fmt.Errorf("action must be accept|reject|adjust")
	}
	var rec *domain.DecisionRecord
	err := a.Store.Tx(ctx, func(tx execTx) error {
		ev, e := a.appendEvent(ctx, tx, projectID, domain.EvDecision, "reviewer", domain.Decision{
			TargetType: targetType, TargetID: targetID, Action: action, ParamsID: paramsID, Reason: reason,
		})
		if e != nil {
			return e
		}
		rec = &domain.DecisionRecord{ID: ev.ID, EventID: ev.ID, Seq: ev.Seq, TargetType: targetType, TargetID: targetID, Action: action, ParamsID: paramsID, Reason: reason, At: ev.At, Actor: ev.Actor}
		// detect conflict against the prior fold
		events, e2 := a.Store.LoadEvents(ctx, projectID)
		if e2 != nil {
			return e2
		}
		st, e2 := LoadStateAt(events, ev.Seq-1)
		if e2 != nil {
			return e2
		}
		if prior := st.DecisionFor(targetID); prior != nil && contradicts(prior.Action, action) {
			_, e2 = a.appendEvent(ctx, tx, projectID, domain.EvConflictRaised, "system", domain.ConflictRaised{
				TargetID: targetID, DecisionIDs: []string{prior.ID, ev.ID},
				Reason: fmt.Sprintf("%s then %s on same target", prior.Action, action),
			})
			if e2 != nil {
				return e2
			}
		}
		return nil
	})
	return rec, err
}

// ResolveConflict records the winning decision for a conflicted target.
func (a *App) ResolveConflict(ctx context.Context, projectID, targetID, winnerDecisionID, note string) error {
	events, err := a.Store.LoadEvents(ctx, projectID)
	if err != nil {
		return err
	}
	st, err := LoadStateAt(events, 1<<62)
	if err != nil {
		return err
	}
	c := st.ConflictByTarget[targetID]
	if c == nil {
		return fmt.Errorf("no open conflict for target %s", targetID)
	}
	return a.Store.Tx(ctx, func(tx execTx) error {
		_, e := a.appendEvent(ctx, tx, projectID, domain.EvConflictResolved, "reviewer", domain.ConflictResolved{
			ConflictEventID: c.EventID, TargetID: targetID, WinnerDecision: winnerDecisionID, Note: note,
		})
		return e
	})
}

// Undo marks an event undone via a new history.undo event. The original event
// stays in the log; replay simply treats it as retracted.
func (a *App) Undo(ctx context.Context, projectID, eventID, reason string) error {
	return a.Store.Tx(ctx, func(tx execTx) error {
		_, e := a.appendEvent(ctx, tx, projectID, domain.EvUndo, "reviewer", domain.UndoPayload{UndoneEventID: eventID, Reason: reason})
		return e
	})
}

// HydratedState folds the log and loads all referenced bodies.
func (a *App) HydratedState(ctx context.Context, projectID string, atSeq int64) (*State, error) {
	events, err := a.Store.LoadEvents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var st *State
	if atSeq <= 0 || atSeq >= int64(len(events)) {
		st = NewState()
		for _, e := range events {
			if err := st.Fold(e); err != nil {
				return nil, err
			}
		}
	} else {
		st, err = LoadStateAt(events, atSeq)
		if err != nil {
			return nil, err
		}
	}
	if p, err := a.Store.GetProject(ctx, projectID); err == nil && p != nil {
		st.Project = p
	}
	// versions
	for _, layer := range domain.LayerOrder {
		vers, err := a.Store.ListVersions(ctx, projectID, layer)
		if err != nil {
			return nil, err
		}
		for _, v := range vers {
			if st.Sources[layer] == nil {
				st.Sources[layer] = map[string]*domain.SourceVersion{}
			}
			// At as-of views, hide versions committed after the cutoff.
			if atSeq > 0 {
				ev, err := a.eventByID(ctx, v.CommitEventID)
				if err != nil {
					return nil, err
				}
				if ev != nil && ev.Seq > atSeq {
					continue
				}
			}
			st.Sources[layer][v.ID] = v
		}
	}
	// params
	for _, e := range events {
		if e.Type != domain.EvParamsRevised {
			continue
		}
		var pr domain.ParamsRevised
		if json.Unmarshal(e.Payload, &pr) != nil {
			continue
		}
		if atSeq > 0 && e.Seq > atSeq {
			continue
		}
		if p, err := a.Store.LoadParams(ctx, pr.ParamsID); err == nil && p != nil {
			st.Params[p.ID] = *p
		}
	}
	// derived nodes for all batches visible at the cutoff
	for _, bm := range st.Batches {
		nodes, err := a.Store.LoadDerivedByBatch(ctx, projectID, bm.ID)
		if err != nil {
			return nil, err
		}
		for _, n := range nodes {
			st.Derived[n.ID] = n
		}
	}
	// diagnostics
	if ds, err := a.Store.ListDiagnostics(ctx, projectID); err == nil {
		st.Diagnostics = ds
	}
	return st, nil
}

func (a *App) eventByID(ctx context.Context, id string) (*domain.Event, error) {
	if id == "" {
		return nil, nil
	}
	// small lookup by scanning the project log; id embeds no project
	projects, err := a.Store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		events, err := a.Store.LoadEvents(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		for i := range events {
			if events[i].ID == id {
				return &events[i], nil
			}
		}
	}
	return nil, nil
}

// CurrentBatchNodes returns the derived nodes of the newest batch.
func (a *App) CurrentBatchNodes(ctx context.Context, projectID string) (string, []*domain.DerivedNode, error) {
	st, err := a.HydratedState(ctx, projectID, 0)
	if err != nil {
		return "", nil, err
	}
	if st.CurrentBatch == "" {
		return "", nil, nil
	}
	nodes, err := a.Store.LoadDerivedByBatch(ctx, projectID, st.CurrentBatch)
	return st.CurrentBatch, nodes, err
}

// BatchNodes returns nodes of any historical batch (stale included).
func (a *App) BatchNodes(ctx context.Context, projectID, batchID string) ([]*domain.DerivedNode, error) {
	return a.Store.LoadDerivedByBatch(ctx, projectID, batchID)
}
