package smoke

import (
	"path/filepath"
	"testing"
	"time"

	"tga-review/internal/cases"
	"tga-review/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := base
	s, err := store.Open(filepath.Join(dir, "tga.db"), func() time.Time {
		now = now.Add(time.Second)
		return now
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestQuickVariantSignals(t *testing.T) {
	root := filepath.Join("..", "..", "testdata")
	for _, variant := range cases.AllMutations {
		t.Run(variant, func(t *testing.T) {
			s := openStore(t)
			pid, diags, err := cases.ImportVariant(s, root, variant, "x")
			if err != nil {
				t.Fatal(err)
			}
			v, err := s.HeadView(pid)
			if err != nil {
				t.Fatal(err)
			}
			if v.Latest == nil {
				t.Fatal("no derivation")
			}
			t.Logf("%s: series=%d stages=%d residues=%d gaps=%d diags(import)=%d derDiags=%d",
				variant, len(v.Latest.Series), len(v.Latest.Stages),
				len(v.Latest.Residues), len(v.Latest.Gaps), len(diags), len(v.Latest.Diags))
		})
	}
}
