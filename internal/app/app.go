// Package app is the transactional application service. Every user action is
// one event plus its immutable data, committed atomically.
package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"tga-review/internal/clock"
	"tga-review/internal/derive"
	"tga-review/internal/domain"
	"tga-review/internal/fold"
	"tga-review/internal/ingest"
	"tga-review/internal/store"
)

// App is the service facade used by HTTP handlers and tests.
type App struct {
	Store *store.Store
	Clock clock.Clock
}

func New(st *store.Store, c clock.Clock) *App {
	if c == nil {
		c = clock.Real{}
	}
	return &App{Store: st, Clock: c}
}

// CreateProject creates a project and its first event.
func (a *App) CreateProject(id, name, note, actor string) (domain.Event, error) {
	exists, err := a.Store.ProjectExists(id)
	if err != nil {
		return domain.Event{}, err
	}
	if exists {
		return domain.Event{}, fmt.Errorf("project %s already exists", id)
	}
	if err := a.Store.CreateProject(store.Project{ID: id, Name: name, Note: note, CreatedAt: a.Clock.Now()}); err != nil {
		return domain.Event{}, err
	}
	return a.Store.AppendEvent(id, domain.EvCreate, domain.CreatePayload{Name: name, Note: note}, actor, a.Clock.Now())
}

// View folds the project at head (atSeq<=0) or a historical event.
func (a *App) View(projID string, atSeq int) (*fold.View, error) {
	evs, err := a.Store.Events(projID)
	if err != nil {
		return nil, err
	}
	name := projID
	if ps, err := a.Store.ListProjects(); err == nil {
		for _, p := range ps {
			if p.ID == projID {
				name = p.Name
			}
		}
	}
	return fold.Fold(projID, name, evs, a.Store, atSeq)
}

// ImportResult reports what an import did.
type ImportResult struct {
	Batch   *domain.Batch      `json:"batch"`
	Event   *domain.Event      `json:"event"`
	Sources []*domain.RawSource `json:"sources"`
}

// Import parses a copyable sectioned submission, stores accepted fragments as
// immutable raw sources, records structural rejects (half-failure), derives
// dependent nodes and appends one IMPORT_COMMITTED event with slot binds.
func (a *App) Import(projID, label, filename, text, actor string) (*ImportResult, error) {
	frags := ingest.Split(text)
	if len(frags) == 0 {
		return nil, errors.New("empty submission: no sections found")
	}

	head, err := a.Store.RawHead(projID)
	if err != nil {
		return nil, err
	}
	now := a.Clock.Now()
	batchID := domain.HashID("bat-", map[string]any{"project": projID, "text": text, "at": now.Format(time.RFC3339Nano), "label": label})

	batch := &domain.Batch{ID: batchID, Project: projID, Label: label, Created: now}
	var sources []*domain.RawSource
	newByKind := map[domain.RawKind]*domain.RawSource{}

	for _, f := range frags {
		parsed, perr := ingest.ParseFragment(f)
		if perr != nil {
			batch.Rejected = append(batch.Rejected, domain.Reject{
				Fragment: f.Text, Header: f.Header, Reason: perr.Error(),
			})
			continue
		}
		kind := parsed.Kind
		ver, parent, err := a.Store.NextRawVersion(projID, kind)
		if err != nil {
			return nil, err
		}
		rs := &domain.RawSource{
			ID:       domain.HashID("raw-", map[string]any{"project": projID, "kind": string(kind), "ver": ver, "text": f.Text}),
			Project:  projID,
			Kind:     kind,
			Version:  ver,
			ParentID: parent,
			BatchID:  batchID,
			Filename: filename,
			Text:     f.Text,
			Payload:  parsed.Payload,
			Diags:    parsed.Diags,
			Created:  now,
		}
		if ver > 1 {
			rs.Revision = "layer revision v" + fmt.Sprint(ver)
		}
		batch.Accepted = append(batch.Accepted, rs.ID)
		batch.AcceptedK = append(batch.AcceptedK, kind)
		batch.DiagCount += len(parsed.Diags)
		sources = append(sources, rs)
		newByKind[kind] = rs
	}

	switch {
	case len(sources) == 0:
		batch.Status = "rejected"
	case len(batch.Rejected) > 0:
		batch.Status = "partial"
	default:
		batch.Status = "ok"
	}

	newHead := map[domain.RawKind]*domain.RawSource{}
	for k, v := range head {
		newHead[k] = v
	}
	for k, v := range newByKind {
		newHead[k] = v
	}

	params := domain.DefaultParams()
	paramsID, _ := a.Store.GetKV(projID, "head_params_id")
	if pjson, _ := a.Store.GetKV(projID, "head_params"); pjson != "" {
		_ = json.Unmarshal([]byte(pjson), &params)
	}

	eng := derive.NewEngine(params, newHead, a.Clock.Now())
	res := eng.Run(projID)

	tx, err := a.Store.DB().Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for _, rs := range sources {
		payload, _ := json.Marshal(rs.Payload)
		diags, _ := json.Marshal(rs.Diags)
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO raw_sources(id,project_id,kind,version,parent_id,batch_id,filename,text,payload,diags,revision,created_at)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			rs.ID, rs.Project, string(rs.Kind), rs.Version, rs.ParentID, rs.BatchID,
			rs.Filename, rs.Text, string(payload), string(diags), rs.Revision, rs.Created.Format(time.RFC3339Nano)); err != nil {
			return nil, err
		}
	}
	acc, _ := json.Marshal(batch.Accepted)
	accK, _ := json.Marshal(batch.AcceptedK)
	rej, _ := json.Marshal(batch.Rejected)
	if _, err := tx.Exec(
		`INSERT OR REPLACE INTO batches(id,project_id,label,status,accepted,accepted_kinds,rejected,diag_count,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		batch.ID, batch.Project, batch.Label, batch.Status, string(acc), string(accK), string(rej),
		batch.DiagCount, batch.Created.Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	for _, d := range []*domain.Derived{res.Correct, res.DTG, res.Stages, res.Residue} {
		if d == nil {
			continue
		}
		if err := saveDerivedTx(tx, d); err != nil {
			return nil, err
		}
	}

	added := make([]domain.Ref, 0, len(sources))
	seenK := map[domain.RawKind]bool{}
	changedSet := map[string]bool{}
	for k := range newByKind {
		for _, slot := range derive.AffectedSlots(k) {
			changedSet[slot] = true
		}
	}
	bindFor := map[string]*domain.Derived{
		domain.SlotCorrect: res.Correct, domain.SlotDTG: res.DTG,
		domain.SlotStages: res.Stages, domain.SlotResidue: res.Residue,
	}
	for _, rs := range sources {
		if !seenK[rs.Kind] {
			added = append(added, domain.Ref{Kind: "raw", ID: rs.ID, Ver: rs.Version, Label: rs.Kind.Label()})
			seenK[rs.Kind] = true
		}
	}
	var binds []domain.Bind
	slotOrder := []string{domain.SlotCorrect, domain.SlotDTG, domain.SlotStages, domain.SlotResidue}
	for _, slot := range slotOrder {
		d := bindFor[slot]
		if d == nil {
			continue
		}
		binds = append(binds, domain.Bind{Slot: slot, NodeID: d.ID, Reason: bindReason(slot, changedSet[slot], len(head) == 0)})
	}
	pl := domain.ImportPayload{
		BatchID: batchID, Status: batch.Status, Added: added,
		Rejected: batch.Rejected, Binds: binds, Label: label,
	}
	if _, err := appendEventRaw(tx, projID, domain.EvImport, pl, actor, a.Clock.Now()); err != nil {
		return nil, err
	}
	if paramsID == "" {
		paramsID = derive.ParamsID(params)
	}
	if _, err := tx.Exec(`INSERT INTO keyvals(project_id,key,value) VALUES(?,?,?)
		ON CONFLICT(project_id,key) DO UPDATE SET value=excluded.value`,
		projID, "head_params_id", paramsID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	evs, _ := a.Store.Events(projID)
	var last *domain.Event
	if len(evs) > 0 {
		last = &evs[len(evs)-1]
	}
	return &ImportResult{Batch: batch, Event: last, Sources: sources}, nil
}

func bindReason(slot string, changed bool, first bool) string {
	if first {
		return "initial derivation"
	}
	if changed {
		return "recomputed: direct input changed"
	}
	return "unchanged: content-addressed identity stable"
}

func saveDerivedTx(tx *sql.Tx, d *domain.Derived) error {
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`INSERT OR IGNORE INTO derived_nodes(id,project_id,slot,kind,params_id,json,created_at)
		 VALUES(?,?,?,?,?,?,?)`,
		d.ID, d.Project, d.Slot, d.Kind, d.ParamsID, string(b), d.Created.Format(time.RFC3339Nano))
	return err
}

func appendEventRaw(tx *sql.Tx, projID, typ string, payload any, actor string, at time.Time) (domain.Event, error) {
	var maxSeq int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM events WHERE project_id=?`, projID).Scan(&maxSeq); err != nil {
		return domain.Event{}, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Event{}, err
	}
	seq := maxSeq + 1
	id := domain.HashID("evt-", map[string]any{
		"project": projID, "seq": seq, "type": typ, "payload": json.RawMessage(raw), "at": at.Format(time.RFC3339Nano),
	})
	if _, err := tx.Exec(`INSERT INTO events(seq,id,project_id,type,payload,actor,created_at) VALUES(?,?,?,?,?,?,?)`,
		seq, id, projID, typ, string(raw), actor, at.Format(time.RFC3339Nano)); err != nil {
		return domain.Event{}, err
	}
	return domain.Event{Seq: seq, ID: id, Project: projID, Type: typ, Payload: raw, Actor: actor, Created: at}, nil
}
