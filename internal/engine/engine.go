// Package engine is the deterministic, replayable state machine for TGA review.
//
// The event log is the single source of truth. Every projection is obtained by
// replaying events from seq=1, so "go back to an old event" means replay up to
// that seq: the dependency versions and visible conclusions of that moment are
// reconstructed rather than overwritten by later computations.
package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"tga/internal/model"
)

const AlgorithmVersion = "tga-v1"

// Clock supplies event timestamps. Tests pass a virtual clock.
type Clock func() time.Time

// DefaultParams is algorithm tga-v1 parameter revision 0.
func DefaultParams() map[string]string {
	return map[string]string{
		"gas_delay_s":                 "60",
		"thermal_lag_c":               "8",
		"min_prominence_mg_per_s":     "0.0005",
		"gap_factor":                  "2.0",
		"drift_model":                 "linear",
		"reaction_window_halfwidth_s": "30",
	}
}

// State is one complete projection of (a prefix of) the event log.
type State struct {
	Project     *model.Project
	AtSeq       int64
	At          time.Time
	Events      []*model.Event
	Submissions map[model.Layer][]*model.Submission
	Params      []*model.Params
	Decisions   []*model.Decision
	Artifacts   map[string]*model.Artifact
	ArtifactIDs []string
	Judgments   []model.Judgment
}

// ActiveSubmission returns the newest non-rejected submission for a layer.
func (s *State) ActiveSubmission(l model.Layer) *model.Submission {
	rows := s.Submissions[l]
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Status != "rejected" {
			return rows[i]
		}
	}
	return nil
}

// ActiveParams returns the newest parameter set (defaults if none).
func (s *State) ActiveParams() *model.Params {
	if len(s.Params) == 0 {
		return &model.Params{Values: DefaultParams(), Fingerprint: ParamsFP(DefaultParams())}
	}
	return s.Params[len(s.Params)-1]
}

// ActiveDecisions returns non-undone decisions keyed by target artifact id.
func (s *State) ActiveDecisions() map[string]*model.Decision {
	out := map[string]*model.Decision{}
	for _, d := range s.Decisions {
		if d.Undone {
			delete(out, d.Target)
			continue
		}
		out[d.Target] = d
	}
	return out
}

// CurrentArtifacts lists current (non-stale) artifacts in creation order.
func (s *State) CurrentArtifacts() []*model.Artifact {
	var out []*model.Artifact
	for _, id := range s.ArtifactIDs {
		if a := s.Artifacts[id]; a.Status == "current" {
			out = append(out, a)
		}
	}
	return out
}

// Engine constructs events and replays logs.
type Engine struct {
	IDPrefix string
	Clock    Clock
}

func New(prefix string, clk Clock) *Engine {
	if clk == nil {
		clk = time.Now
	}
	if prefix == "" {
		prefix = "tga"
	}
	return &Engine{IDPrefix: prefix, Clock: clk}
}

func (e *Engine) hashID(parts ...string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|", e.IDPrefix)
	for _, p := range parts {
		fmt.Fprintf(h, "%s|", p)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ParamsFP is the canonical parameter fingerprint.
func ParamsFP(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s;", k, values[k])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:12]
}

func (e *Engine) eventID(ev model.Event) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%d\n%s\n%s\n%s", ev.ProjectID, ev.Seq, ev.Type, ev.PrevID, ev.Payload)
	return hex.EncodeToString(h.Sum(nil))[:20]
}

func (e *Engine) next(prev *model.Event, projectID, typ string, payload any) model.Event {
	raw := mustJSON(payload)
	ev := model.Event{ProjectID: projectID, Type: typ, Payload: raw, CreatedAt: e.Clock()}
	if prev == nil {
		ev.Seq = 1
	} else {
		ev.Seq = prev.Seq + 1
		ev.PrevID = prev.ID
	}
	ev.ID = e.eventID(ev)
	return ev
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ---- command payload types (stable JSON contract) ----

type projectPayload struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type importPayload struct {
	Submission model.Submission `json:"submission"`
}

type paramsPayload struct {
	ParentSeq int64             `json:"parent_seq"`
	Values    map[string]string `json:"values"`
}

type decisionPayload struct {
	Target     string            `json:"target"`
	Action     string            `json:"action"`
	Reason     string            `json:"reason"`
	Adjust     map[string]string `json:"adjust,omitempty"`
	Supersedes int64             `json:"supersedes"`
}

type undoPayload struct {
	TargetSeq int64  `json:"target_seq"`
	Reason    string `json:"reason"`
}

// CreateProject makes the genesis event.
func (e *Engine) CreateProject(name string) (model.Project, model.Event) {
	id := e.hashID("project", name)
	p := model.Project{ID: id, Name: name, CreatedAt: e.Clock()}
	ev := e.next(nil, id, "project_created", projectPayload{ID: id, Name: name})
	return p, ev
}

// HashSubmission computes the stable id of a raw submission.
func (e *Engine) HashSubmission(layer model.Layer, seq int64) string {
	return e.hashID("sub", string(layer), fmt.Sprint(seq))
}

// Import makes a raw_submission event. The caller has assigned id/kind/parent.
func (e *Engine) Import(prev *model.Event, sub model.Submission) model.Event {
	return e.next(prev, prev.ProjectID, "raw_submission", importPayload{Submission: sub})
}

// ReviseParams makes a params_revision event.
func (e *Engine) ReviseParams(prev *model.Event, parentSeq int64, values map[string]string) model.Event {
	return e.next(prev, prev.ProjectID, "params_revision", paramsPayload{ParentSeq: parentSeq, Values: values})
}

// Decide records a human accept/reject/adjust verdict.
func (e *Engine) Decide(prev *model.Event, target, action, reason string, adjust map[string]string, supersedes int64) model.Event {
	return e.next(prev, prev.ProjectID, "decision", decisionPayload{
		Target: target, Action: action, Reason: reason, Adjust: adjust, Supersedes: supersedes,
	})
}

// Undo marks a prior decision undone.
func (e *Engine) Undo(prev *model.Event, targetSeq int64, reason string) model.Event {
	return e.next(prev, prev.ProjectID, "undo", undoPayload{TargetSeq: targetSeq, Reason: reason})
}

// Replay projects events through the supplied prefix length (0 = all).
func (e *Engine) Replay(events []*model.Event, upToSeq int64) (*State, error) {
	st := &State{
		Submissions: map[model.Layer][]*model.Submission{},
		Artifacts:   map[string]*model.Artifact{},
	}
	for _, ev := range events {
		if upToSeq > 0 && ev.Seq > upToSeq {
			break
		}
		st.Events = append(st.Events, ev)
		st.AtSeq, st.At = ev.Seq, ev.CreatedAt
		if err := e.apply(st, ev); err != nil {
			return nil, fmt.Errorf("replay event %d (%s): %w", ev.Seq, ev.Type, err)
		}
	}
	if err := e.derive(st); err != nil {
		return nil, err
	}
	return st, nil
}

func (e *Engine) apply(st *State, ev *model.Event) error {
	switch ev.Type {
	case "project_created":
		var p projectPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return err
		}
		st.Project = &model.Project{ID: p.ID, Name: p.Name, CreatedAt: ev.CreatedAt}
	case "raw_submission":
		var p importPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return err
		}
		sub := p.Submission
		sub.EventSeq = ev.Seq
		sub.CreatedAt = ev.CreatedAt
		if sub.ID == "" {
			sub.ID = e.hashID("sub", string(sub.Layer), fmt.Sprint(ev.Seq))
		}
		st.Submissions[sub.Layer] = append(st.Submissions[sub.Layer], &sub)
	case "params_revision":
		var p paramsPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return err
		}
		st.Params = append(st.Params, &model.Params{
			EventSeq: ev.Seq, ParentSeq: p.ParentSeq, Values: p.Values,
			Fingerprint: ParamsFP(p.Values), CreatedAt: ev.CreatedAt,
		})
	case "decision":
		var p decisionPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return err
		}
		st.Decisions = append(st.Decisions, &model.Decision{
			EventSeq: ev.Seq, Target: p.Target, Action: p.Action, Reason: p.Reason,
			Adjust: p.Adjust, Supersedes: p.Supersedes, CreatedAt: ev.CreatedAt,
		})
	case "undo":
		var p undoPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return err
		}
		for _, d := range st.Decisions {
			if d.EventSeq == p.TargetSeq && !d.Undone {
				d.Undone = true
				d.UndoneBySeq = ev.Seq
			}
		}
	default:
		return fmt.Errorf("unknown event type %q", ev.Type)
	}
	return nil
}
