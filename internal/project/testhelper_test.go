package project

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"tgareview/internal/bundle"
	"tgareview/internal/cases"
	"tgareview/internal/clock"
	"tgareview/internal/domain"
	"tgareview/internal/engine"
	"tgareview/internal/store"
)

func virtualClock() *clock.Virtual {
	return clock.NewVirtual(time.Date(2026, 9, 22, 9, 0, 0, 0, time.UTC))
}

func newTestService(t *testing.T) (*Service, *store.Store, *clock.Virtual, context.Context) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	clk := virtualClock()
	return New(st, clk), st, clk, context.Background()
}

func baselineBundle(t *testing.T) *bundle.Bundle {
	t.Helper()
	raw, err := cases.ReadFile("tga_case_v1/baseline.json")
	if err != nil {
		t.Fatalf("read embedded baseline: %v", err)
	}
	b, err := bundle.Parse(raw)
	if err != nil {
		t.Fatalf("parse baseline: %v", err)
	}
	return b
}

func importBaseline(t *testing.T, svc *Service, ctx context.Context) (string, *ImportResult) {
	t.Helper()
	b := baselineBundle(t)
	pid, err := svc.CreateProject(ctx, b.ProjectID, b.Name, b.Description)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	res, err := svc.Import(ctx, pid, b)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return pid, res
}

func currentArtifact(t *testing.T, svc *Service, ctx context.Context, pid string) *engine.Artifact {
	t.Helper()
	st, err := svc.CurrentState(ctx, pid)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	id := st.CurrentArtifactID()
	if id == "" {
		t.Fatalf("no current artifact")
	}
	return st.Artifacts[id]
}

func findRule(art *engine.Artifact, code string) *engine.RuleJudgement {
	for i := range art.Judgements {
		if art.Judgements[i].Code == code {
			return &art.Judgements[i]
		}
	}
	return nil
}

func oneLayerBundle(name, kind string, data json.RawMessage, parent string) *bundle.Bundle {
	return &bundle.Bundle{
		FormatVersion: bundle.FormatVersion, Name: name,
		Layers: []bundle.LayerInput{{Kind: domain.LayerKind(kind), ParentRevision: parent, Data: data}},
	}
}
