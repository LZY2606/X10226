package casepack

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"tga-review/internal/clock"
	"tga-review/internal/domain"
	"tga-review/internal/engine"
	"tga-review/internal/ingest"
	"tga-review/internal/store"
)

// ReplayResult captures the observable state after a scenario replay.
type ReplayResult struct {
	ProjectID string
	Engine    *engine.Engine
	Store     *store.Store
	Reports   []*engine.ImportReport
	State     *engine.State
	Manifest  Manifest
}

// ProjectIDFor gives deterministic project ids per scenario.
func ProjectIDFor(m Manifest) string {
	return "proj-" + m.ID
}

// LoadScenarioMaterial reads the embedded files for a trigger into import
// inputs. A single file list may contain just one revised stratum.
func LoadScenarioMaterial(relFiles []string) ([]ingest.FileInput, error) {
	var out []ingest.FileInput
	for _, rel := range relFiles {
		data, err := fs.ReadFile(FS, FilesRoot+"/"+rel)
		if err != nil {
			return nil, fmt.Errorf("case file %s: %w", rel, err)
		}
		name := rel
		if i := strings.LastIndex(rel, "/"); i >= 0 {
			name = rel[i+1:]
		}
		out = append(out, ingest.FileInput{Filename: name, Content: string(data)})
	}
	return out, nil
}

func paramsFromSpec(s ParamsSpec) domain.Parameters {
	p := domain.Parameters{
		GasDelaySeconds: s.GasDelaySeconds, TempLagSeconds: s.TempLagSeconds,
		PeakWindowSeconds: s.PeakWindowSeconds, OverlapTolSeconds: s.OverlapTolSeconds,
		GapMaxSeconds: s.GapMaxSeconds, MassUnit: "mg", TimeUnit: "s", TempUnit: "C",
	}
	return p
}

// Replay materializes a scenario into st using fixed-seed virtual time and
// executes its triggers in order. Every judgement trigger becomes a
// machine-readable case.judgement event.
func Replay(ctx context.Context, st *store.Store, m Manifest) (*ReplayResult, error) {
	clk := clock.NewVirtual(m.Seed)
	eng := engine.New(st, clk)
	eng.SetActor("casepack:" + m.ID)
	pid := ProjectIDFor(m)
	if err := eng.CreateProject(ctx, pid, m.Title, m.Seed); err != nil {
		return nil, err
	}
	// apply scenario baseline params as revision v2
	base := paramsFromSpec(m.Params)
	if _, err := eng.ReviseParameters(ctx, pid, base, "casepack baseline parameters"); err != nil {
		return nil, err
	}
	res := &ReplayResult{ProjectID: pid, Engine: eng, Store: st, Manifest: m}
	for _, tr := range m.Triggers {
		switch tr.Kind {
		case "import":
			files, err := LoadScenarioMaterial(tr.Files)
			if err != nil {
				return nil, err
			}
			rep, err := eng.Import(ctx, pid, files)
			if err != nil {
				return nil, err
			}
			res.Reports = append(res.Reports, rep)
		case "revise_params":
			spec := defaultParams()
			if tr.Params != nil {
				spec = *tr.Params
			}
			if _, err := eng.ReviseParameters(ctx, pid, paramsFromSpec(spec), tr.Reason); err != nil {
				return nil, err
			}
		case "judgement":
			payload := map[string]any{}
			for k, v := range tr.Payload {
				payload[k] = v
			}
			payload["scenario"] = m.ID
			if err := eng.Judgement(ctx, pid, payload); err != nil {
				return nil, err
			}
		case "decide":
			_, err := eng.Decide(ctx, pid, engine.DecisionRequest{
				TargetID: tr.Target, State: tr.State, Reason: tr.Reason})
			if err != nil {
				return nil, err
			}
		case "resolve":
			if err := eng.ResolveConflict(ctx, pid, tr.Group, tr.Winner, tr.Reason); err != nil {
				return nil, err
			}
		case "undo":
			if err := eng.Undo(ctx, pid, tr.Target); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unknown trigger kind %q", tr.Kind)
		}
	}
	state, err := eng.CurrentView(ctx, pid)
	if err != nil {
		return nil, err
	}
	res.State = state
	return res, nil
}

// ManifestJSON returns the embedded manifest bytes.
func ManifestJSON() ([]byte, error) {
	var ms []Manifest
	if err := json.Unmarshal(manifestJSON, &ms); err != nil {
		return nil, err
	}
	return json.MarshalIndent(ms, "", "  ")
}
