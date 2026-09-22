// Package casepack embeds the offline domain case
// "失重谱系：TGA 阶段分解、气氛切换与残留物证据链" and replays its
// scripted steps into the event log: baseline import, initial compute,
// conflicting decisions, a parameter revision, a local source change,
// an undo, and a partial import failure.
package casepack

import (
	"embed"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"tgaview/internal/core"
	"tgaview/internal/importer"
	"tgaview/internal/store"
)

//go:embed data
var data embed.FS

// CaseMeta mirrors case.json.
type CaseMeta struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Narrative string            `json:"narrative"`
	Sources   map[string]string `json:"sources"`
	Steps     []Step            `json:"steps"`
}

// Step is one replay instruction.
type Step struct {
	Op         string         `json:"op"`
	Comment    string         `json:"comment,omitempty"`
	ID         string         `json:"id,omitempty"`
	Reviewer   string         `json:"reviewer,omitempty"`
	Action     string         `json:"action,omitempty"`
	Target     string         `json:"target,omitempty"`
	Params     map[string]any `json:"params,omitempty"`
	Rationale  string         `json:"rationale,omitempty"`
	Supersedes string         `json:"supersedes,omitempty"`
	Source     string         `json:"source,omitempty"`
	File       string         `json:"file,omitempty"`
	Note       string         `json:"note,omitempty"`
	DecisionID string         `json:"decision_id,omitempty"`
	Reason     string         `json:"reason,omitempty"`
}

// Meta loads the embedded case metadata.
func Meta() (*CaseMeta, error) {
	b, err := data.ReadFile("data/case.json")
	if err != nil {
		return nil, err
	}
	var m CaseMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// File returns the raw content of an embedded case file (relative to data/).
func File(rel string) (string, error) {
	b, err := data.ReadFile(filepath.Join("data", rel))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func bundleFromDir(dir string) (importer.Bundle, error) {
	entries, err := data.ReadDir(filepath.Join("data", dir))
	if err != nil {
		return importer.Bundle{}, err
	}
	b := importer.Bundle{Name: dir, Files: map[string]string{}}
	for _, e := range entries {
		content, err := File(filepath.Join(dir, e.Name()))
		if err != nil {
			return b, err
		}
		b.Files[e.Name()] = content
	}
	return b, nil
}

// resolveTarget maps symbolic targets ("stages:split", "stages:merged",
// "atmosphere", "residue") to concrete derived/candidate identities in the
// current state.
func resolveTarget(state *core.State, sym string) (string, error) {
	if strings.HasPrefix(sym, "stages:") {
		want := strings.TrimPrefix(sym, "stages:")
		for _, id := range state.DerivedOrder {
			d := state.Derived[id]
			if d.Kind != "stages" || d.Stale {
				continue
			}
			var cands struct {
				Sets []struct {
					ID   string `json:"id"`
					Kind string `json:"kind"`
				} `json:"sets"`
			}
			if err := json.Unmarshal(d.Body, &cands); err != nil {
				return "", err
			}
			for _, s := range cands.Sets {
				if s.Kind == want {
					return s.ID, nil
				}
			}
		}
		return "", fmt.Errorf("no current stages candidate set of kind %q", want)
	}
	for _, id := range state.DerivedOrder {
		d := state.Derived[id]
		if d.Kind == sym && !d.Stale {
			return d.ID, nil
		}
	}
	return "", fmt.Errorf("no current derived of kind %q", sym)
}

// Replay executes the scripted case steps into the store. It is the
// machine-checkable narrative of the case: every step appends events, so
// the full history (including conflicts and undo) is preserved.
func Replay(st *store.Store) error {
	meta, err := Meta()
	if err != nil {
		return err
	}
	for i, step := range meta.Steps {
		switch step.Op {
		case "import_baseline":
			b, err := bundleFromDir("baseline")
			if err != nil {
				return err
			}
			if _, err := importer.ImportBundle(st, b, fmt.Sprintf("run/%s/step%02d", meta.ID, i)); err != nil {
				return err
			}
		case "import_partial":
			b, err := bundleFromDir("partial")
			if err != nil {
				return err
			}
			if _, err := importer.ImportBundle(st, b, fmt.Sprintf("run/%s/step%02d-partial", meta.ID, i)); err != nil {
				return err
			}
		case "run_pipeline", "run_pipeline_adjusted":
			if err := core.RunPipelineCurrent(st); err != nil {
				return err
			}
		case "revise_source":
			raw, err := File(step.File)
			if err != nil {
				return err
			}
			state, err := core.RebuildState(st, 0)
			if err != nil {
				return err
			}
			src := state.Sources[step.Source]
			if src == nil {
				return fmt.Errorf("step %d: source %s not imported yet", i, step.Source)
			}
			parent := src.Latest().Version
			if _, err := st.Append(core.EvSourceRevised, step.Source, core.SourcePayload{
				SourceID: step.Source, Version: parent + 1, Parent: parent,
				Kind: step.Source, Raw: raw, Format: "csv+units", Note: step.Note,
			}); err != nil {
				return err
			}
		case "decision":
			state, err := core.RebuildState(st, 0)
			if err != nil {
				return err
			}
			target, err := resolveTarget(state, step.Target)
			if err != nil {
				return fmt.Errorf("step %d: %w", i, err)
			}
			if _, err := st.Append(core.EvDecisionMade, target, core.DecisionPayload{
				ID: step.ID, Target: target, Action: step.Action, Params: step.Params,
				Rationale: step.Rationale, Reviewer: step.Reviewer, Supersedes: step.Supersedes,
			}); err != nil {
				return err
			}
		case "undo":
			if _, err := st.Append(core.EvDecisionUndone, step.DecisionID, core.UndoPayload{
				DecisionID: step.DecisionID, Reason: step.Reason,
			}); err != nil {
				return err
			}
		default:
			return fmt.Errorf("step %d: unknown op %q", i, step.Op)
		}
	}
	return nil
}
