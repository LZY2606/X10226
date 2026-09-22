package scratchcheck

import (
	"context"
	"fmt"
	"testing"
	"time"

	"tga-review/internal/domain"
	"tga-review/internal/ingest"
	"tga-review/internal/store"
)

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil { panic(err) }
	return t
}

func TestSanity(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir + "/t.db")
	if err != nil { t.Fatal(err) }
	defer st.Close()
	app := store.NewApp(st, domain.NewVirtualClock(mustTime("2026-01-01T00:00:00Z")))
	ctx := context.Background()
	p, err := app.CreateProject(ctx, "demo", "", nil)
	if err != nil { t.Fatalf("create: %v", err) }
	fmt.Println("project", p.ID)
	ins := []store.LayerInput{
		{Layer: domain.LayerSamples, Text: ingest.CaseSamplesV1},
		{Layer: domain.LayerDrift, Text: ingest.CaseDriftV1},
		{Layer: domain.LayerProgram, Text: ingest.CaseProgramV1},
		{Layer: domain.LayerGas, Text: ingest.CaseGasV1},
		{Layer: domain.LayerBlank, Text: ingest.CaseBlankV1},
		{Layer: domain.LayerInitMass, Text: ingest.CaseInitMassV1},
	}
	res, err := app.ImportMany(ctx, p.ID, ins)
	if err != nil { t.Fatal(err) }
	t.Logf("import status=%s bad=%v diags=%d", res.Status, res.FormatErrors, len(res.Diagnostics))
	b, err := app.RunPipeline(ctx, p.ID, "baseline")
	if err != nil { t.Fatal(err) }
	for _, n := range b.Nodes {
		switch pl := n.Payload.(type) {
		case domain.PeaksPayload:
			for _, pk := range pl.Peaks {
				t.Logf("PEAK t=%.1f dtg=%.3f amb=%d", pk.TMin, pk.DTGMgMin, len(pk.AmbiguousWith))
			}
		case domain.StagesPayload:
			for _, sg := range pl.Stages {
				t.Logf("STAGE %.1f-%.1f loss=%.3fmg %.2f%% overlaps=%d %s note=%s", sg.StartTMin, sg.EndTMin, sg.MassLossMg, sg.MassLossPct, len(sg.Overlaps), sg.Status, sg.RejectReason)
			}
		case domain.ResiduesPayload:
			for _, r := range pl.Residues {
				t.Logf("RESIDUE %.3f%% unknown=%v reason=%s", r.ResidualPct, r.Unknown, r.UnknownReason)
			}
		case domain.ReactionWindowsPayload:
			for _, w := range pl.Windows {
				t.Logf("WINDOW gas=%s valve=%.2f reach=%.2f sampT=%.1fK", w.Gas, w.ValveTMin, w.ReachTMin, w.SampleTK)
			}
		case domain.GapMapPayload:
			t.Logf("GAPS %d", len(pl.Gaps))
		case domain.DTGPayload:
			bounce := 0
			for _, d := range pl.Points { if d.NoiseFlag != "" { bounce++ } }
			t.Logf("DTG flags=%d", bounce)
		}
	}
}
