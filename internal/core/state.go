// Package core projects the event log into reviewable state: source
// versions, derived results with staleness, decisions with conflicts and
// undo, and machine-readable judgments. State can be rebuilt as of any
// event sequence for time-travel review.
package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"tgaview/internal/store"
)

// Event kinds.
const (
	EvSourceSubmitted = "source.submitted"
	EvSourceRevised   = "source.revised"
	EvDerivedComputed = "derived.computed"
	EvDecisionMade    = "decision.made"
	EvDecisionUndone  = "decision.undone"
	EvJudgment        = "judgment.recorded"
	EvImportCompleted = "import.completed"
	EvImportPartial   = "import.partial_failed"
)

// Source kinds (the six layered raw inputs).
const (
	SrcSamplePoints    = "sample_points"
	SrcDriftCorrection = "drift_correction"
	SrcFurnaceProgram  = "furnace_program"
	SrcGasValveEvents  = "gas_valve_events"
	SrcBlankCurve      = "blank_curve"
	SrcInitialMass     = "initial_mass"
)

// SourcePayload is the payload of source.submitted / source.revised.
type SourcePayload struct {
	SourceID string `json:"source_id"`
	Version  int    `json:"version"`
	Parent   int    `json:"parent"` // 0 for v1
	Kind     string `json:"kind"`
	Raw      string `json:"raw"`    // original representation, immutable
	Format   string `json:"format"` // e.g. "csv+units"
	Note     string `json:"note,omitempty"`
}

// DerivedPayload is the payload of derived.computed.
type DerivedPayload struct {
	ID               string          `json:"id"`
	Kind             string          `json:"kind"` // slot, e.g. "tg.corrected"
	AlgoVersion      string          `json:"algo_version"`
	ParamFingerprint string          `json:"param_fingerprint"`
	Params           map[string]any  `json:"params,omitempty"`
	Inputs           []string        `json:"inputs"` // "src:<id>@v<n>" or "der:<id>"
	Body             json.RawMessage `json:"body"`
}

// DecisionPayload is the payload of decision.made.
type DecisionPayload struct {
	ID         string         `json:"id"`
	Target     string         `json:"target"` // candidate set id, derived id, ...
	Action     string         `json:"action"` // accept | reject | adjust
	Params     map[string]any `json:"params,omitempty"`
	Rationale  string         `json:"rationale"`
	Reviewer   string         `json:"reviewer"`
	Supersedes string         `json:"supersedes,omitempty"`
}

// UndoPayload is the payload of decision.undone.
type UndoPayload struct {
	DecisionID string `json:"decision_id"`
	Reason     string `json:"reason"`
}

// JudgmentPayload is a machine-readable domain judgment.
type JudgmentPayload struct {
	Rule      string          `json:"rule"`
	DerivedID string          `json:"derived_id,omitempty"`
	Data      json.RawMessage `json:"data"`
}

// ImportFailure describes one file that failed during import.
type ImportFailure struct {
	File    string `json:"file"`
	Message string `json:"message"`
}

// ImportPayload records an import run (completed or partial).
type ImportPayload struct {
	RunID    string          `json:"run_id"`
	Files    []string        `json:"files"`
	Failures []ImportFailure `json:"failures,omitempty"`
	Status   string          `json:"status"` // "ok" | "partial"
}

// SourceVersion is one immutable version of a raw input.
type SourceVersion struct {
	SourceID string
	Version  int
	Parent   int
	Kind     string
	Raw      string
	Format   string
	Note     string
	Seq      int64
}

// Source is a raw input with its version chain.
type Source struct {
	ID       string
	Kind     string
	Versions []*SourceVersion
}

// Latest returns the newest version.
func (s *Source) Latest() *SourceVersion { return s.Versions[len(s.Versions)-1] }

// Derived is a computed artifact with provenance and staleness.
type Derived struct {
	ID               string
	Kind             string
	AlgoVersion      string
	ParamFingerprint string
	Params           map[string]any
	Inputs           []string
	Body             json.RawMessage
	Seq              int64
	Stale            bool
	StaleReasons     []string
}

// Decision is a human decision event in history.
type Decision struct {
	ID         string
	Target     string
	Action     string
	Params     map[string]any
	Rationale  string
	Reviewer   string
	Supersedes string
	Seq        int64
	Undone     bool
}

// Judgment is a recorded machine-readable domain judgment.
type Judgment struct {
	Seq       int64
	Rule      string
	DerivedID string
	Data      json.RawMessage
}

// ImportRecord is a projected import run.
type ImportRecord struct {
	Seq      int64
	RunID    string
	Status   string
	Files    []string
	Failures []ImportFailure
}

// State is the projected review state as of some event sequence.
type State struct {
	Sources      map[string]*Source
	SourceOrder  []string
	Derived      map[string]*Derived
	DerivedOrder []string
	Decisions    map[string][]*Decision // target -> history
	Judgments    []Judgment
	Imports      []ImportRecord
	Head         int64
}

// Build projects events (already filtered to the desired seq window).
func Build(events []store.Event) *State {
	st := &State{
		Sources:   map[string]*Source{},
		Derived:   map[string]*Derived{},
		Decisions: map[string][]*Decision{},
	}
	for _, e := range events {
		st.Head = e.Seq
		switch e.Kind {
		case EvSourceSubmitted, EvSourceRevised:
			var p SourcePayload
			if json.Unmarshal(e.Payload, &p) != nil {
				continue
			}
			src := st.Sources[p.SourceID]
			if src == nil {
				src = &Source{ID: p.SourceID, Kind: p.Kind}
				st.Sources[p.SourceID] = src
				st.SourceOrder = append(st.SourceOrder, p.SourceID)
			}
			src.Versions = append(src.Versions, &SourceVersion{
				SourceID: p.SourceID, Version: p.Version, Parent: p.Parent,
				Kind: p.Kind, Raw: p.Raw, Format: p.Format, Note: p.Note, Seq: e.Seq,
			})
		case EvDerivedComputed:
			var p DerivedPayload
			if json.Unmarshal(e.Payload, &p) != nil {
				continue
			}
			if _, dup := st.Derived[p.ID]; dup {
				continue // idempotent recompute
			}
			st.Derived[p.ID] = &Derived{
				ID: p.ID, Kind: p.Kind, AlgoVersion: p.AlgoVersion,
				ParamFingerprint: p.ParamFingerprint, Params: p.Params,
				Inputs: p.Inputs, Body: p.Body, Seq: e.Seq,
			}
			st.DerivedOrder = append(st.DerivedOrder, p.ID)
		case EvDecisionMade:
			var p DecisionPayload
			if json.Unmarshal(e.Payload, &p) != nil {
				continue
			}
			st.Decisions[p.Target] = append(st.Decisions[p.Target], &Decision{
				ID: p.ID, Target: p.Target, Action: p.Action, Params: p.Params,
				Rationale: p.Rationale, Reviewer: p.Reviewer, Supersedes: p.Supersedes,
				Seq: e.Seq,
			})
		case EvDecisionUndone:
			var p UndoPayload
			if json.Unmarshal(e.Payload, &p) != nil {
				continue
			}
			for _, ds := range st.Decisions {
				for _, d := range ds {
					if d.ID == p.DecisionID {
						d.Undone = true
					}
				}
			}
		case EvJudgment:
			var p JudgmentPayload
			if json.Unmarshal(e.Payload, &p) != nil {
				continue
			}
			st.Judgments = append(st.Judgments, Judgment{Seq: e.Seq, Rule: p.Rule, DerivedID: p.DerivedID, Data: p.Data})
		case EvImportCompleted, EvImportPartial:
			var p ImportPayload
			if json.Unmarshal(e.Payload, &p) != nil {
				continue
			}
			st.Imports = append(st.Imports, ImportRecord{
				Seq: e.Seq, RunID: p.RunID, Status: p.Status, Files: p.Files, Failures: p.Failures,
			})
		}
	}
	st.computeStaleness()
	return st
}

// computeStaleness marks derived results stale when:
//   - a direct source input has a newer version, or
//   - a newer derived of the same kind (slot) exists, or
//   - any derived input is itself stale (transitive).
//
// Stale results remain fully viewable but can never pose as current.
func (st *State) computeStaleness() {
	latestOfKind := map[string]int64{}
	for _, id := range st.DerivedOrder {
		d := st.Derived[id]
		if d.Seq > latestOfKind[d.Kind] {
			latestOfKind[d.Kind] = d.Seq
		}
	}
	mark := func(d *Derived, why string) {
		if !contains(d.StaleReasons, why) {
			d.StaleReasons = append(d.StaleReasons, why)
		}
		d.Stale = true
	}
	for _, id := range st.DerivedOrder {
		d := st.Derived[id]
		for _, in := range d.Inputs {
			if sid, v, ok := parseSrcRef(in); ok {
				if src := st.Sources[sid]; src != nil && src.Latest().Version > v {
					mark(d, fmt.Sprintf("来源 %s 已修订到 v%d（计算时用的是 v%d）", sid, src.Latest().Version, v))
				}
			}
		}
		if d.Seq < latestOfKind[d.Kind] {
			mark(d, fmt.Sprintf("存在更新的 %s 计算结果", d.Kind))
		}
	}
	// transitive: derived depending on stale derived
	changed := true
	for changed {
		changed = false
		for _, id := range st.DerivedOrder {
			d := st.Derived[id]
			if d.Stale {
				continue
			}
			for _, in := range d.Inputs {
				if did, ok := parseDerRef(in); ok {
					if dep := st.Derived[did]; dep != nil && dep.Stale {
						mark(d, fmt.Sprintf("依赖 %s 已过期", did))
						changed = true
					}
				}
			}
		}
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// SrcRef formats a source input reference.
func SrcRef(sourceID string, version int) string { return fmt.Sprintf("src:%s@v%d", sourceID, version) }

// DerRef formats a derived input reference.
func DerRef(derivedID string) string { return "der:" + derivedID }

func parseSrcRef(ref string) (string, int, bool) {
	if !strings.HasPrefix(ref, "src:") {
		return "", 0, false
	}
	var sid string
	var v int
	if _, err := fmt.Sscanf(strings.TrimPrefix(ref, "src:"), "%[^@]@v%d", &sid, &v); err != nil {
		return "", 0, false
	}
	return sid, v, true
}

func parseDerRef(ref string) (string, bool) {
	if !strings.HasPrefix(ref, "der:") {
		return "", false
	}
	return strings.TrimPrefix(ref, "der:"), true
}

// CurrentDecision returns the latest non-undone decision for target.
func (st *State) CurrentDecision(target string) *Decision {
	ds := st.Decisions[target]
	for i := len(ds) - 1; i >= 0; i-- {
		if !ds[i].Undone {
			return ds[i]
		}
	}
	return nil
}

// Conflict reports whether the active (non-undone) decisions on target
// disagree, and returns the active decisions.
func (st *State) Conflict(target string) (bool, []*Decision) {
	var active []*Decision
	for _, d := range st.Decisions[target] {
		if !d.Undone {
			active = append(active, d)
		}
	}
	if len(active) < 2 {
		return false, active
	}
	last := active[len(active)-1]
	// a decision that explicitly supersedes another resolves the chain
	if last.Supersedes != "" {
		return false, active
	}
	for _, d := range active[:len(active)-1] {
		if d.Action != last.Action {
			return true, active
		}
	}
	return false, active
}

// DecisionByID finds a decision anywhere in history.
func (st *State) DecisionByID(id string) *Decision {
	for _, ds := range st.Decisions {
		for _, d := range ds {
			if d.ID == id {
				return d
			}
		}
	}
	return nil
}

// SortedDerived returns derived ids ordered by kind then seq for display.
func (st *State) SortedDerived() []*Derived {
	out := make([]*Derived, 0, len(st.DerivedOrder))
	for _, id := range st.DerivedOrder {
		out = append(out, st.Derived[id])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Seq < out[j].Seq
	})
	return out
}
