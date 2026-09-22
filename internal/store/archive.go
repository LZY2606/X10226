package store

import (
	"tgareview/internal/domain"
)

// Archive is the portable export format. Identity, ordering and source
// relationships are preserved verbatim on re-import.
type Archive struct {
	Format    string         `json:"format"` // "tga-review-archive-1"
	ProjectID string         `json:"project_id"`
	Name      string         `json:"name"`
	Origin    string         `json:"origin"`
	Seed      int64          `json:"seed"`
	Events    []domain.Event `json:"events"`
}

// ExportedProject bundles an archive with per-seq snapshots for re-import.
type ExportedProject struct {
	Archive
	Snapshots map[int64][]byte `json:"snapshots,omitempty"` // view JSON by seq
}
