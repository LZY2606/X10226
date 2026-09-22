package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tga-archive/internal/derive"
	"tga-archive/internal/domain"
	"tga-archive/internal/ingest"
	"tga-archive/internal/store"
)

// App is the application service: it validates commands, appends events and
// refreshes the derived-node cache. All times come from an injectable clock so
// tests use virtual time while the server uses UTC.
type App struct {
	Store *store.Store
	Clock func() string
}

func New(st *store.Store) *App {
	return &App{Store: st, Clock: func() string { return time.Now().UTC().Format(time.RFC3339Nano) }}
}

func (a *App) CreateProject(name, caseID string, seed int64, note string) (store.ProjectInfo, error) {
	at := a.Clock()
	id := domain.HashID("prj", name, caseID, at)
	p := domain.ProjectPayload{Name: name, CaseID: caseID, Seed: seed, Note: note}
	return a.Store.CreateProject(p, id, at)
}

// SubmitResult reports per-layer acceptance of a bundled import.
type SubmitResult struct {
	Events    []domain.Envelope        `json:"events"`
	Payloads  []domain.SourcePayload   `json:"payloads"`
	Accepted  int                      `json:"accepted"`
	Rejected  int                      `json:"rejected"`
}

// SubmitBundle imports a multi-layer document. Each section is one immutable
// event, so a half-failed section leaves the other sections unaffected.
func (a *App) SubmitBundle(projectID string, text string) (SubmitResult, error) {
	res := SubmitResult{}
	if _, err := a.Store.Project(projectID); err != nil {
		return res, err
	}
	payloads := ingest.ImportBundle(text)
	for _, p := range payloads {
		ev, err := a.Store.AppendEvent(projectID, domain.EvSourceSubmitted, a.lastEventID(projectID), p)
		if err != nil {
			return res, err
		}
		res.Events = append(res.Events, ev)
		res.Payloads = append(res.Payloads, p)
		if p.Accepted {
			res.Accepted++
		} else {
			res.Rejected++
		}
	}
	if err := a.Refresh(context.Background(), projectID); err != nil {
		return res, err
	}
	return res, nil
}

// SubmitLayer imports one layer fragment (used for revisions and error demos).
func (a *App) SubmitLayer(projectID, layer, label, rawText, parentSourceID string) (domain.SourcePayload, domain.Envelope, error) {
	if _, err := a.Store.Project(projectID); err != nil {
		return domain.SourcePayload{}, domain.Envelope{}, err
	}
	p := ingest.ParseLayer(layer, label, rawText, parentSourceID)
	ev, err := a.Store.AppendEvent(projectID, domain.EvSourceSubmitted, a.lastEventID(projectID), p)
	if err != nil {
		return p, ev, err
	}
	return p, ev, a.Refresh(context.Background(), projectID)
}

func (a *App) lastEventID(projectID string) string {
	evs, err := a.Store.Events(projectID)
	if err != nil || len(evs) == 0 {
		return ""
	}
	return evs[len(evs)-1].ID
}

// ReviseParams appends a new parameter version (parent points at prior).
func (a *App) ReviseParams(projectID string, p domain.Params, reason string) (domain.Envelope, error) {
	state, err := a.CurrentState(projectID)
	if err != nil {
		return domain.Envelope{}, err
	}
	pv := domain.HashID("params", domain.CanonicalJSON(p))
	pp := domain.ParamsPayload{ParamVersion: pv, Parent: state.ParamVersion, Params: p, Reason: reason}
	ev, err := a.Store.AppendEvent(projectID, domain.EvParamsRevised, a.lastEventID(projectID), pp)
	if err != nil {
		return ev, err
	}
	return ev, a.Refresh(context.Background(), projectID)
}

// Rule appends an accept/reject decision. Candidate identity is verified
// against the current derivation; conflicts still append (never overwrite).
func (a *App) Rule(projectID, candidateKey, decision, rationale string, adjusted map[string]float64) (domain.Envelope, error) {
	if decision != "accept" && decision != "reject" {
		return domain.Envelope{}, fmt.Errorf("decision must be accept or reject")
	}
	state, err := a.CurrentState(projectID)
	if err != nil {
		return domain.Envelope{}, err
	}
	var cid string
	for _, c := range state.Report.Candidates {
		if c.Key == candidateKey {
			cid = c.ID
		}
	}
	if cid == "" {
		return domain.Envelope{}, fmt.Errorf("candidate %s not found in current derivation", candidateKey)
	}
	rp := domain.RulingPayload{
		RulingID:    domain.HashID("ruling", candidateKey, decision, rationale, domain.CanonicalJSON(adjusted)),
		CandidateID: cid,
		Decision:    decision,
		Rationale:   rationale,
		Adjusted:    adjusted,
	}
	ev, err := a.Store.AppendEvent(projectID, domain.EvRuling, a.lastEventID(projectID), rp)
	if err != nil {
		return ev, err
	}
	return ev, a.Refresh(context.Background(), projectID)
}

// ResolveConflict explicitly keeps one ruling for a candidate; the others are
// deactivated but remain in history.
func (a *App) ResolveConflict(projectID, candidateKey, keptRulingID, note string) (domain.Envelope, error) {
	state, err := a.CurrentState(projectID)
	if err != nil {
		return domain.Envelope{}, err
	}
	var cid string
	for _, c := range state.Report.Candidates {
		if c.Key == candidateKey {
			cid = c.ID
		}
	}
	if cid == "" {
		return domain.Envelope{}, fmt.Errorf("candidate %s not found", candidateKey)
	}
	cp := domain.ConflictPayload{CandidateID: cid, KeptRuling: keptRulingID, Note: note}
	ev, err := a.Store.AppendEvent(projectID, domain.EvConflictResolved, a.lastEventID(projectID), cp)
	if err != nil {
		return ev, err
	}
	return ev, a.Refresh(context.Background(), projectID)
}

// UndoRuling deactivates a ruling through an undo event (never deletes it).
func (a *App) UndoRuling(projectID, rulingID, reason string) (domain.Envelope, error) {
	up := domain.UndoPayload{RulingID: rulingID, Reason: reason}
	ev, err := a.Store.AppendEvent(projectID, domain.EvUndo, a.lastEventID(projectID), up)
	if err != nil {
		return ev, err
	}
	return ev, a.Refresh(context.Background(), projectID)
}

// Refresh recomputes derived nodes and upserts them; nodes that disappear
// become stale but stay readable.
func (a *App) Refresh(ctx context.Context, projectID string) error {
	state, err := a.CurrentState(projectID)
	if err != nil {
		return err
	}
	nodes := map[string]json.RawMessage{}
	put := func(id string, v any) {
		raw, err := json.Marshal(v)
		if err != nil {
			return
		}
		nodes[id] = raw
	}
	for i := range state.Report.Candidates {
		c := state.Report.Candidates[i]
		put(c.ID, c)
	}
	for i := range state.Report.Windows {
		w := state.Report.Windows[i]
		put(w.ID, w)
	}
	for i := range state.Report.Residues {
		r := state.Report.Residues[i]
		put(r.ID, r)
	}
	put("report:latest", state.Report)
	seq, err := a.Store.LastSeq(projectID)
	if err != nil {
		return err
	}
	return a.Store.UpsertNodes(ctx, projectID, seq, nodes)
}

// CurrentState folds the whole log; StateAt folds a prefix (time travel).
func (a *App) CurrentState(projectID string) (*State, error) {
	return a.StateAt(projectID, -1)
}

func (a *App) StateAt(projectID string, seq int64) (*State, error) {
	info, err := a.Store.Project(projectID)
	if err != nil {
		return nil, err
	}
	var evs []domain.Envelope
	if seq < 0 {
		evs, err = a.Store.Events(projectID)
	} else {
		evs, err = a.Store.EventsUntil(projectID, seq)
	}
	if err != nil {
		return nil, err
	}
	return Fold(info, evs), nil
}

// CandidateAt returns the candidate view as it existed at seq (time travel).
func (a *App) CandidateAt(projectID, candidateKey string, seq int64) (derive.StageCandidate, bool, error) {
	st, err := a.StateAt(projectID, seq)
	if err != nil {
		return derive.StageCandidate{}, false, err
	}
	for _, c := range st.Report.Candidates {
		if c.Key == candidateKey {
			return c, true, nil
		}
	}
	return derive.StageCandidate{}, false, nil
}
