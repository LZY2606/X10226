package service_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"tgareview/internal/casepack"
	"tgareview/internal/domain"
	"tgareview/internal/service"
	"tgareview/internal/store"
)

// TestExportDeleteImport verifies identity, ordering and source relations
// survive a full archive round trip after the local database is deleted.
func TestExportDeleteImport(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	pid := h.baselineProject(ctx)
	v, _ := h.svc.HeadView(ctx, pid)
	if len(v.View.Stages) == 0 || len(v.Events) == 0 {
		t.Fatal("baseline not materialized")
	}
	var stageID string
	for _, s := range v.View.Stages {
		if s.Rejected == nil {
			stageID = s.ID
		}
	}
	if _, _, err := h.svc.Decide(ctx, pid, service.DecideRequest{
		Kind: domain.DecAcceptStage, Target: "stage", TargetID: stageID,
		Note: "persisted verdict", Actor: "reviewer",
	}); err != nil {
		t.Fatal(err)
	}
	before, err := h.svc.HeadView(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}

	arc, err := h.svc.ExportProject(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.DeleteProject(ctx, pid); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.HeadView(ctx, pid); err == nil {
		t.Fatal("project should be gone after delete")
	}
	if _, err := h.svc.ImportArchive(ctx, arc); err != nil {
		t.Fatalf("reimport: %v", err)
	}
	after, err := h.svc.HeadView(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}

	if len(after.Events) != len(before.Events) {
		t.Fatalf("event count %d want %d", len(after.Events), len(before.Events))
	}
	for i := range before.Events {
		if before.Events[i].EventID != after.Events[i].EventID ||
			before.Events[i].Seq != after.Events[i].Seq {
			t.Fatalf("event identity/order changed at %d", i)
		}
	}
	found := false
	for _, s := range after.View.Stages {
		if s.ID == stageID && s.Verdict == "accepted" {
			found = true
		}
	}
	if !found {
		t.Fatal("accepted stage verdict did not survive round trip")
	}

	// Time travel at seq 1 must show only project.created (old dependencies).
	old, err := h.svc.ViewAt(ctx, pid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(old.Events) != 1 || old.Events[0].Type != "project.created" {
		t.Fatalf("seq 1 view has %d events", len(old.Events))
	}
}

// TestFreshDBFile reimports into a brand-new database file, proving nothing
// depends on developer-machine state.
func TestFreshDBFile(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.db")
	st1, err := store.Open(p1)
	if err != nil {
		t.Fatal(err)
	}
	cur := mustTime(virtualStart)
	clk := func() time.Time { t2 := cur; cur = cur.Add(time.Minute); return t2 }
	svc1 := service.New(st1, clk)
	pid, err := casepack.NewRunner(svc1).CreateBaseline(context.Background(), "x")
	if err != nil {
		t.Fatal(err)
	}
	arc, err := svc1.ExportProject(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	if err := st1.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(p1, filepath.Join(dir, "a.db.old")); err != nil {
		t.Fatal(err)
	}
	st2, err := store.Open(p1)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	cur2 := mustTime(virtualStart)
	svc2 := service.New(st2, func() time.Time { t2 := cur2; cur2 = cur2.Add(time.Minute); return t2 })
	if _, err := svc2.ImportArchive(context.Background(), arc); err != nil {
		t.Fatal(err)
	}
	v, err := svc2.HeadView(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	if v.Meta.ID != pid || v.View == nil || len(v.View.Residues) == 0 {
		t.Fatal("reimport into fresh db failed")
	}
}
