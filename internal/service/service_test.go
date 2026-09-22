package service_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"tgareview/internal/casepack"
	"tgareview/internal/service"
	"tgareview/internal/store"
)

const virtualStart = "2026-01-01T00:00:00Z"

// harness builds a service on a temp SQLite DB with a virtual clock.
type harness struct {
	t   *testing.T
	svc *service.Service
	st  *store.Store
	clk *virtualClock
}

type virtualClock struct{ cur time.Time }

func (c *virtualClock) now() time.Time {
	cur := c.cur
	c.cur = c.cur.Add(time.Minute)
	return cur
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	db := filepath.Join(t.TempDir(), "tga.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	clk := &virtualClock{cur: mustTime(virtualStart)}
	return &harness{t: t, st: st, clk: clk, svc: service.New(st, clk.now)}
}

func mustTime(s string) time.Time {
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return tt
}

func (h *harness) baselineProject(ctx context.Context) string {
	h.t.Helper()
	r := casepack.NewRunner(h.svc)
	pid, err := r.CreateBaseline(ctx, "test-baseline")
	if err != nil {
		h.t.Fatalf("create baseline: %v", err)
	}
	return pid
}

func ctx() context.Context { return context.Background() }
