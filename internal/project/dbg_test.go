package project

import "testing"

func TestDbgCands(t *testing.T) {
	svc, _, _, ctx := newTestService(t)
	pid, _ := importBaseline(t, svc, ctx)
	art := currentArtifact(t, svc, ctx, pid)
	t.Logf("gaps=%d intervals=%d cands=%d", len(art.Gaps), len(art.Intervals), len(art.Candidates))
	for _, c := range art.Candidates {
		t.Logf("opt=%s start=%.0f end=%.0f peak=%.0f ov=%d", c.Option, c.StartS, c.EndS, c.PeakS, len(c.OverlapsWith))
	}
}
