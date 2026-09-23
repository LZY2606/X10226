// Package app is the TGA state machine. It is event sourced: every command
// appends an immutable event, raw documents are never overwritten, and derived
// results are deterministic functions of (documents, parameters, event seq).
package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"gotga/internal/domain"
	"gotga/internal/ingest"
	"gotga/internal/store"
)

type App struct {
	Store *store.Store
	Clock domain.Clock
}

func New(st *store.Store, c domain.Clock) *App {
	if c == nil {
		c = domain.WallClock{}
	}
	return &App{Store: st, Clock: c}
}

func (a *App) ts() string { return a.Clock.Now().Format("2006-01-02T15:04:05Z07:00") }

var ErrConflict = errors.New("conflicting decisions recorded; resolution required")

// Project is the lightweight project descriptor.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedTS string `json:"created_ts"`
}

func (a *App) CreateProject(name string) (*Project, error) {
	id := domain.HashID("proj", name, a.ts())
	if err := a.Store.CreateProject(id, name, a.ts(), "{}"); err != nil {
		return nil, err
	}
	p, _ := json.Marshal(map[string]string{"name": name})
	_, err := a.appendEvent(id, domain.EvProjectCreated, string(p), "", "init", "project created")
	if err != nil {
		return nil, err
	}
	return &Project{ID: id, Name: name, CreatedTS: a.ts()}, nil
}

func (a *App) appendEvent(projectID string, kind domain.EventType, payload, revokes, actor, note string) (*domain.Event, error) {
	e := &domain.Event{
		ID: domain.HashID("evt", projectID, kind, payload, a.ts(), note, revokes, randless(kind, projectID)),
		ProjectID: projectID, Kind: kind, Payload: payload, Revokes: revokes,
		Actor: actor, Note: note, TS: a.ts(),
	}
	seq, err := a.Store.AppendEvent(e)
	if err != nil {
		return nil, err
	}
	e.Seq = seq
	return e, nil
}

// ImportResult reports accepted/rejected layers, the event and affected ids.
type ImportResult struct {
	BatchID  string   `json:"batch_id"`
	EventSeq int64    `json:"event_seq"`
	Partial  bool     `json:"partial"`
	Accepted []string `json:"accepted"`
	Rejected []string `json:"rejected"`
	Diags    []string `json:"diags"`
}

// Import commits the accepted layers of a batch. A malformed layer rejects only
// that layer (import half-failure): accepted layers still become immutable
// documents and the rejection is recorded on the batch and event.
func (a *App) Import(projectID, text, actor, note, scenario string) (*ImportResult, error) {
	batchID := domain.HashID("batch", projectID, text, a.ts())
	br := ingest.ParseBatch(projectID, batchID, text)
	res := &ImportResult{BatchID: batchID, Partial: len(br.Rejected) > 0}
	var docIDs, accepted, rejected []string
	var diags []string
	for _, lr := range br.Accepted {
		lr.Doc.IngestTS = a.ts()
		if prev, _ := a.Store.LatestDoc(projectID, lr.Kind); prev != nil {
			lr.Doc.ParentID = prev.ID
		}
		docIDs = append(docIDs, lr.Doc.ID)
		accepted = append(accepted, string(lr.Kind))
		diags = append(diags, lr.Doc.Diags...)
	}
	for _, lr := range br.Rejected {
		var tag string
		if lr.Kind != "" {
			tag = string(lr.Kind) + ": "
		}
		rejected = append(rejected, tag+lr.Err.Error())
	}
	pl := domain.ImportCommitPayload{
		BatchID: batchID, DocIDs: docIDs, Partial: res.Partial,
		Accepted: accepted, Rejected: rejected, Scenario: scenario,
	}
	pb, _ := json.Marshal(pl)
	e, err := a.appendEvent(projectID, domain.EvImportCommitted, string(pb), "", actor, note)
	if err != nil {
		return nil, err
	}
	for _, lr := range br.Accepted {
		if err := a.Store.InsertDoc(lr.Doc, e.Seq); err != nil {
			return nil, err
		}
	}
	res.EventSeq = e.Seq
	res.Accepted, res.Rejected, res.Diags = accepted, rejected, diags
	_ = a.Store.SaveBatchFor(projectID, store.BatchRow{
		ID: batchID, EventSeq: e.Seq, Partial: res.Partial,
		Accepted: string(mustJSON(accepted)), Rejected: string(mustJSON(rejected)), RawText: text, TS: a.ts(),
	})
	if err := a.recompute(projectID, e.Seq); err != nil {
		return nil, err
	}
	return res, nil
}

// ReviseParams stores a new parameter version with a parent relationship.
func (a *App) ReviseParams(projectID string, p domain.Params, actor, note string) (int64, error) {
	cur, err := a.CurrentParams(projectID)
	if err != nil {
		return 0, err
	}
	pl := domain.ParamRevisePayload{ParentID: domain.ParamFingerprint(cur), Params: p}
	pb, _ := json.Marshal(pl)
	e, err := a.appendEvent(projectID, domain.EvParamRevised, string(pb), "", actor, note)
	if err != nil {
		return 0, err
	}
	if err := a.Store.SetKV(projectID, "params", string(pb)); err != nil {
		return 0, err
	}
	if err := a.recompute(projectID, e.Seq); err != nil {
		return 0, err
	}
	return e.Seq, nil
}

// ReviseSource replaces a single direct source layer. Only derived objects
// whose provenance lists that document may expire; content-addressed objects
// that do not change keep their identity and birth event.
func (a *App) ReviseSource(projectID string, kind domain.DocKind, text, actor, note string) (*ImportResult, error) {
	wrapped := fmt.Sprintf("## layer=%s label=revision source=revision\n%s\n", kind, text)
	res, err := a.Import(projectID, wrapped, actor, note, "source_revision:"+string(kind))
	if err != nil {
		return nil, err
	}
	if len(res.Accepted) != 1 {
		return res, fmt.Errorf("source revision must provide exactly one valid %s layer", kind)
	}
	pl := domain.SourceRevisePayload{Kind: kind}
	if newDoc, _ := a.Store.LatestDoc(projectID, kind); newDoc != nil {
		pl.NewDocID = newDoc.ID
		if newDoc.ParentID != "" {
			pl.OldDocID = newDoc.ParentID
		}
	}
	pb, _ := json.Marshal(pl)
	_, err = a.appendEvent(projectID, domain.EvSourceRevised, string(pb), "", actor, note)
	return res, err
}

func (a *App) CurrentParams(projectID string) (domain.Params, error) {
	v, err := a.Store.GetKV(projectID, "params")
	if errors.Is(err, sql.ErrNoRows) {
		return domain.DefaultParams(), nil
	}
	if err != nil {
		return domain.Params{}, err
	}
	var pl domain.ParamRevisePayload
	if err := json.Unmarshal([]byte(v), &pl); err != nil {
		return domain.Params{}, err
	}
	return pl.Params, nil
}
