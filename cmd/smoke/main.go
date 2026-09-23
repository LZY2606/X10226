package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"tga-review/internal/app"
	"tga-review/internal/cases"
	"tga-review/internal/store"
)

func main() {
	dir, _ := os.MkdirTemp("", "tga-smoke")
	defer os.RemoveAll(dir)
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(dir, "t.db"))
	if err != nil { panic(err) }
	defer st.Close()
	clk := &app.StepClock{T: time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC), Step: time.Second}
	svc := app.New(st, clk)
	p, err := svc.CreateProject(ctx, "Smoke", "smoke")
	if err != nil { panic(err) }
	res, err := cases.Baseline(ctx, svc, p.ID)
	if err != nil { panic(err) }
	fmt.Println("import:", res.Status)
	for _, f := range res.Fragments {
		fmt.Println("  frag", f.Layer, f.Accepted, len(f.Diag))
	}
	vm, _ := svc.View(ctx, p.ID)
	for _, n := range vm.Nodes {
		fmt.Println("node", n.Kind, n.Status, "cur=", n.Current)
	}
	for _, s := range vm.Stages {
		fmt.Printf("stage %s cluster=%d peak=%.0fs temp=%.1fC area%%=%.2f atmo=%s %s bounded=%v\n", s.StageKey, s.Cluster, s.PeakS, s.PeakTempC, s.AreaPct, s.Atmosphere, s.Status, s.BoundedByGap)
	}
	if vm.Residue != nil {
		fmt.Printf("residue=%.2f%%\n", vm.Residue.ResiduePct)
	}
	for _, key := range []string{"atmo_delay","balance_drift","overlap_peaks","nonmonotonic","sample_gap","initial_mass_revision","half_import"} {
		e, err := cases.RunSpec(ctx, svc, p.ID, key)
		if err != nil { fmt.Println("SCENARIO ERR", key, err); continue }
		fmt.Println("scenario", key, "->", e.Status, "scope:", e.Scope)
	}
	_ = json.NewEncoder
}
