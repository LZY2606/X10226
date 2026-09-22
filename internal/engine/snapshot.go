// Package engine turns the immutable event log + raw source records into
// layered derived artifacts. Artifacts are content addressed by logical
// family and parameter fingerprint, so the same inputs always get the same
// stable ids, and local source changes only invalidate dependent results.
package engine

import "tga-review/internal/domain"

// Snapshot is the current layered raw baseline: one winning record per layer
// index for each source kind.
type Snapshot struct {
	Records map[string][]domain.SourceRecord
	Params  domain.Params
}

func emptySnapshot() *Snapshot {
	return &Snapshot{Records: map[string][]domain.SourceRecord{}}
}

// BuildSnapshot replays records up to and including the accepted import that
// introduced event seq upToSeq. Layers present in a later accepted import
// supersede earlier layers; absent layers are inherited (amend semantics).
func BuildSnapshot(records []domain.SourceRecord, upToSeq int64, params domain.Params) *Snapshot {
	snap := emptySnapshot()
	// Find latest accepted import seq per kind.
	latest := map[string]int64{}
	for _, r := range records {
		if r.ImportSeq > upToSeq {
			continue
		}
		if r.Status == "rejected" {
			continue
		}
		if r.ImportSeq >= latest[r.Kind] {
			latest[r.Kind] = r.ImportSeq
		}
	}
	added := map[string]map[int]bool{}
	for k := range latest {
		added[k] = map[int]bool{}
	}
	// stable order: iterate records sorted by import seq then layer index
	for _, r := range sortedAccepted(records, upToSeq) {
		if r.ImportSeq != latest[r.Kind] {
			continue
		}
		if added[r.Kind][r.LayerIndex] {
			continue
		}
		added[r.Kind][r.LayerIndex] = true
		snap.Records[r.Kind] = append(snap.Records[r.Kind], r)
	}
	snap.Params = params
	return snap
}

func sortedAccepted(records []domain.SourceRecord, upToSeq int64) []domain.SourceRecord {
	out := make([]domain.SourceRecord, 0, len(records))
	for _, r := range records {
		if r.ImportSeq <= upToSeq && r.Status != "rejected" {
			out = append(out, r)
		}
	}
	// insertion sort by (import seq, kind, layer)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && lessRec(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func lessRec(a, b domain.SourceRecord) bool {
	if a.ImportSeq != b.ImportSeq {
		return a.ImportSeq < b.ImportSeq
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.LayerIndex < b.LayerIndex
}
