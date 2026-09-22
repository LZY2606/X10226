// Package app wires the engine, the importer and the store into transactional
// user-level commands. Every command appends immutable events; every read is a
// fresh deterministic replay.
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"tga/internal/engine"
	"tga/internal/model"
	"tga/internal/parse"
	"tga/internal/store"
)

// App is the application service.
type App struct {
	Store  *store.Store
	Engine *engine.Engine
}

// New builds an App with a virtual-able clock.
func New(st *store.Store, clk engine.Clock) *App {
	if clk == nil {
		clk = time.Now
	}
	return &App{Store: st, Engine: engine.New("tga", clk)}
}

// CreateProject creates and persists a project.
func (a *App) CreateProject(ctx context.Context, name string) (*model.Project, error) {
	p, ev := a.Engine.CreateProject(name)
	if err := a.Store.CreateProject(ctx, p); err != nil {
		return nil, err
	}
	if err := a.Store.AppendEvent(ctx, ev); err != nil {
		return nil, err
	}
	return &p, nil
}

// ImportInput is one raw layer import request.
type ImportInput struct {
	Layer  model.Layer
	Raw    string
	Source string
	Kind   string // initial | revision | partial (auto if empty)
}

// ImportLayer parses raw text for a layer and appends a raw_submission event.
// Raw text is stored verbatim; partial failures are kept, not rejected.
func (a *App) ImportLayer(ctx context.Context, projectID string, in ImportInput) (*model.Submission, *model.Event, error) {
	if !in.Layer.Valid() {
		return nil, nil, fmt.Errorf("invalid layer %q", in.Layer)
	}
	parsed, err := parse.Parse(in.Layer, in.Raw)
	if err != nil {
		return nil, nil, err
	}
	st, err := a.ReplayHead(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	prev, err := a.Store.LastEvent(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	if prev == nil {
		return nil, nil, fmt.Errorf("project %s has no genesis event", projectID)
	}
	parent := st.ActiveSubmission(in.Layer)
	kind := in.Kind
	if kind == "" {
		if parent == nil {
			kind = "initial"
		} else {
			kind = "revision"
		}
	}
	if parsed.Status == "partial" {
		kind = "partial"
	}

	sub := model.Submission{
		Layer: in.Layer, Kind: kind, Source: in.Source, Units: parsed.Units,
		Samples: parsed.Samples, Blank: parsed.Blank, Program: parsed.Program,
		Gas: parsed.Gas, M0: parsed.M0, ParseErrors: parsed.Diags,
		AcceptedRows: parsed.Accepted, TotalRows: parsed.Total,
		Status: parsed.Status, Raw: in.Raw,
	}
	if parent != nil {
		sub.ParentID = parent.ID
	}
	sub.ID = a.Engine.HashSubmission(in.Layer, prev.Seq+1)
	ev := a.Engine.Import(prev, sub)
	if err := a.Store.AppendEvent(ctx, ev); err != nil {
		return nil, nil, err
	}
	sub.EventSeq = ev.Seq
	sub.CreatedAt = ev.CreatedAt
	return &sub, &ev, nil
}

// ReviseParams appends a params_revision event.
func (a *App) ReviseParams(ctx context.Context, projectID string, values map[string]string) (*model.Event, error) {
	st, err := a.ReplayHead(ctx, projectID)
	if err != nil {
		return nil, err
	}
	prev, err := a.Store.LastEvent(ctx, projectID)
	if err != nil {
		return nil, err
	}
	merged := map[string]string{}
	for k, v := range st.ActiveParams().Values {
		merged[k] = v
	}
	for k, v := range values {
		merged[k] = v
	}
	parentSeq := int64(0)
	if len(st.Params) > 0 {
		parentSeq = st.Params[len(st.Params)-1].EventSeq
	}
	ev := a.Engine.ReviseParams(prev, parentSeq, merged)
	if err := a.Store.AppendEvent(ctx, ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

// DecideInput is a human verdict request.
type DecideInput struct {
	Target string
	Action string // accept | reject | adjust
	Reason string
	Adjust map[string]string
}

// Decide appends a decision event; an adjust on an already decided target
// supersedes that decision's seq.
func (a *App) Decide(ctx context.Context, projectID string, in DecideInput) (*model.Event, error) {
	if in.Action != "accept" && in.Action != "reject" && in.Action != "adjust" {
		return nil, fmt.Errorf("invalid action %q", in.Action)
	}
	st, err := a.ReplayHead(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if st.Artifacts[in.Target] == nil {
		return nil, fmt.Errorf("unknown target artifact %q", in.Target)
	}
	prev, err := a.Store.LastEvent(ctx, projectID)
	if err != nil {
		return nil, err
	}
	supersedes := int64(0)
	if d := st.ActiveDecisions()[in.Target]; d != nil {
		supersedes = d.EventSeq
	}
	ev := a.Engine.Decide(prev, in.Target, in.Action, in.Reason, in.Adjust, supersedes)
	if err := a.Store.AppendEvent(ctx, ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

// Undo marks a prior decision undone (append-only event).
func (a *App) Undo(ctx context.Context, projectID string, targetSeq int64, reason string) (*model.Event, error) {
	st, err := a.ReplayHead(ctx, projectID)
	if err != nil {
		return nil, err
	}
	found := false
	for _, d := range st.Decisions {
		if d.EventSeq == targetSeq && !d.Undone {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("no live decision at seq %d", targetSeq)
	}
	prev, err := a.Store.LastEvent(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ev := a.Engine.Undo(prev, targetSeq, reason)
	if err := a.Store.AppendEvent(ctx, ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

// ReplayHead replays the full log to the current head.
func (a *App) ReplayHead(ctx context.Context, projectID string) (*engine.State, error) {
	events, err := a.Store.Events(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return a.Engine.Replay(events, 0)
}

// ReplayAt replays up to a given seq (time travel: old dependency versions and
// the conclusions visible then are reconstructed).
func (a *App) ReplayAt(ctx context.Context, projectID string, seq int64) (*engine.State, error) {
	events, err := a.Store.Events(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return a.Engine.Replay(events, seq)
}

// ListProjects returns all projects.
func (a *App) ListProjects(ctx context.Context) ([]model.Project, error) {
	return a.Store.ListProjects(ctx)
}

// ParamsText parses key=value parameter text.
func ParseParamsText(text string) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range splitLines(text) {
		line = trim(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := splitKV(line)
		if kv[0] == "" {
			return nil, fmt.Errorf("bad parameter line: %q", line)
		}
		out[kv[0]] = kv[1]
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no parameters found")
	}
	return out, nil
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, ch := range s {
		if ch == '\n' {
			out = append(out, trim(cur))
			cur = ""
			continue
		}
		cur += string(ch)
	}
	return append(out, trim(cur))
}

func trim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func splitKV(line string) [2]string { return [2]string{} }

// mustJSON helper kept here for snapshots.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
