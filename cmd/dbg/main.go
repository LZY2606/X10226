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
	"tga-review/internal/domain"
	"tga-review/internal/store"
)

func main() {
	dir, _ := os.MkdirTemp("", "dbgx")
	defer os.RemoveAll(dir)
	ctx := context.Background()
	st, _ := store.Open(ctx, filepath.Join(dir, "t.db"))
	defer st.Close()
	svc := app.New(st, &app.StepClock{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Step: time.Second})
	p, _ := svc.CreateProject(ctx, "x", "x")
	cases.Baseline(ctx, svc, p.ID)
	src, _ := st.CurrentSource(ctx, p.ID, domain.LayerInitMass)
	b, _ := json.MarshalIndent(src.Payload["initial_mass"], "", " ")
	fmt.Println("initial_mass payload:", string(b))
	v, _ := svc.View(ctx, p.ID)
	if v.Residue != nil {
		fmt.Println("residue pct:", v.Residue.ResiduePct, "status:", v.Residue.Status)
	}
	for _, st := range v.Stages {
		fmt.Printf("stage %s peak=%.0f atmo=%s area%%=%.2f bounded=%v\n", st.StageKey, st.PeakS, st.Atmosphere, st.AreaPct, st.BoundedByGap)
	}
}
