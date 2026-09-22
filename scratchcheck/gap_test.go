package scratchcheck

import (
	"context"
	"testing"

	"tga-review/internal/domain"
	"tga-review/internal/ingest"
	"tga-review/internal/store"
)

func buildProject(t *testing.T, samples, gas string) (*store.App, string, *store.Store) {
	dir := t.TempDir()
	st, err := store.Open(dir + "/t.db")
	if err != nil { t.Fatal(err) }
	app := store.NewApp(st, domain.NewVirtualClock(mustTime("2026-01-01T00:00:00Z")))
	ctx := context.Background()
	p, err := app.CreateProject(ctx, "g", "", nil)
	if err != nil { t.Fatal(err) }
	if gas == "" { gas = ingest.CaseGasV1 }
	ins := []store.LayerInput{
		{Layer: domain.LayerSamples, Text: samples},
		{Layer: domain.LayerDrift, Text: ingest.CaseDriftV1},
		{Layer: domain.LayerProgram, Text: ingest.CaseProgramV1},
		{Layer: domain.LayerGas, Text: gas},
		{Layer: domain.LayerBlank, Text: ingest.CaseBlankV1},
		{Layer: domain.LayerInitMass, Text: ingest.CaseInitMassV1},
	}
	if _, err := app.ImportMany(ctx, p.ID, ins); err != nil { t.Fatal(err) }
	return app, p.ID, st
}

func TestGap(t *testing.T) {
	app, pid, st := buildProject(t, ingest.CaseSamplesV2Gap, "")
	defer st.Close()
	ctx := context.Background()
	b, err := app.RunPipeline(ctx, pid, "gap")
	if err != nil { t.Fatal(err) }
	for _, n := range b.Nodes {
		switch pl := n.Payload.(type) {
		case domain.GapMapPayload:
			t.Logf("GAPS %d: %+v", len(pl.Gaps), pl.Gaps)
			if len(pl.Gaps) == 0 { t.Error("expected at least one gap") }
		case domain.DTGPayload:
			segs := map[string]bool{}
			flags := 0
			for _, d := range pl.Points { segs[d.SegmentID] = true; if d.NoiseFlag != "" { flags++ } }
			t.Logf("segments=%v flags=%d", segs, flags)
			if len(segs) < 2 { t.Error("DTG should split at gap") }
		case domain.StagesPayload:
			for _, sg := range pl.Stages {
				t.Logf("STAGE %.1f-%.1f %.2f%%", sg.StartTMin, sg.EndTMin, sg.MassLossPct)
				if sg.StartTMin < 7.6 && sg.EndTMin > 10.4 {
					t.Errorf("stage %s interpolates across gap", sg.ID)
				}
			}
		case domain.ResiduesPayload:
			t.Logf("RES %+v", pl.Residues[0].ResidualPct)
		}
	}
}
