package engine

import (
	"tga-review/internal/domain"
)

// snapshotOf materializes a replayable snapshot. Nodes are read from the
// store so the snapshot always embeds the exact visible derived versions.
func (e *Engine) snapshotOf(st *State, sources []domain.Source) *domain.Snapshot {
	nodes := append([]domain.Node(nil), st.Nodes...)
	decisions := map[string]domain.Decision{}
	if len(st.Events) > 0 {
		if last := st.Events[len(st.Events)-1].Snap; last != nil {
			for k, d := range last.Decisions {
				decisions[k] = d
			}
		}
	}
	residueID := ""
	if st.Residue != nil {
		residueID = st.Residue.ID
	}
	return &domain.Snapshot{
		Seq:        int64(len(st.Events)),
		Sources:    sources,
		Parameters: st.Params,
		Nodes:      nodes,
		StageOrder: st.StageOrder,
		ResidueID:  residueID,
		Decisions:  decisions,
		Flags:      map[string]string{},
	}
}

// decisionsAt replays decision-bearing events up to (and including) seq.
func decisionsAt(events []domain.Event, seq int64) map[string]domain.Decision {
	out := map[string]domain.Decision{}
	for _, ev := range events {
		if ev.Seq > seq {
			break
		}
		if ev.Snap != nil {
			for k, d := range ev.Snap.Decisions {
				out[k] = d
			}
		}
	}
	return out
}
