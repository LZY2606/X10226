package smoke

import (
	"path/filepath"
	"testing"
	"time"

	"tga-review/internal/cases"
	"tga-review/internal/store"
)

func TestDbg(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := base
	s, _ := store.Open(filepath.Join(dir, "t.db"), func() time.Time { now = now.Add(time.Second); return now })
	defer s.Close()
	pid, _, err := cases.ImportVariant(s, filepath.Join("..", "..", "testdata"), "base", "x")
	t.Logf("pid=%v err=%v", pid, err)
	_, err = s.HeadView(pid)
	t.Logf("view err=%v", err)
}
