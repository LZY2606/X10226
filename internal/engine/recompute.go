package engine

import (
	"context"

	"tga-review/internal/derive"
	"tga-review/internal/domain"
)

// Recompute projects the log, computes fresh derived artifacts and appends a
// derived_computed event listing exactly which previously current derived
// ids were invalidated by the newest source change. Local input changes only
// invalidate artifacts that depend on them.
func (e *Engine) Recompute(ctx context.Context, projectID string) (*derive.Derived, error) {
	events, err := e.Store.Events(ctx, projectID, 0)
	if err != nil {
		return nil, err
	}
	st, err := derive.Replay(events)
	if err != nil {
		return nil, err
	}

	var prev *derive.Derived
	if len(st.History) >= 1 {
		// Recompute the state at the previous tip to know the prior artifacts.
		prevTip := st.TipSeq - 1
		if prev, err = derivedAt(events, prevTip); err != nil {
			return nil, err
		}
	}

	d := derive.Compute(st, st.TipSeq+1)

	// Overlay live decisions onto the freshly computed candidates and rebuild
	// the residue evidence chain against them.
	applied := st.AppliedCandidatesWith(d)
	d.Candidates = applied
	if d.Series != nil && d.Residue != nil {
		d.Residue = rebuildResidue(d, st)
	}

	invalidated := diffArtifacts(prev, d)

	// Cache artifacts with stable ids for as-of history reads.
	if d.Series != nil {
		if body, err := jsonMarshalIndent(d.Series); err == nil {
			_ = e.Store.SaveArtifact(ctx, projectID, "series", d.Series.ID, st.TipSeq+1, body)
		}
	}

	payload := map[string]any{
		"series_id":      seriesID(d),
		"candidate_ids":  candidateIDs(d),
		"residue_id":     residueID(d),
		"invalidated":    toAny(invalidated),
		"derived":        anyOf(d),
	}
	seq, err := e.Store.NextSeq(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := e.append(ctx, &domain.Event{
		ID:        eventID(domain.EvDerivedComputed, seq),
		ProjectID: projectID,
		Type:      domain.EvDerivedComputed,
		AtVirtual: e.Clock.Now(),
		Payload:   payload,
	}); err != nil {
		return nil, err
	}
	return d, nil
}

func derivedAt(events []domain.Event, seq int64) (*derive.Derived, error) {
	st, err := derive.ViewAt(events, seq)
	if err != nil {
		return nil, err
	}
	if st.LatestDerived == nil {
		return nil, nil
}
	// Recompute a view artifact the same way, purely for comparison.
	d := derive.Compute(st, seq)
	d.Candidates = st.AppliedCandidatesWith(d)
	return d, nil
}

// diffArtifacts returns ids that changed or were invalidated.
func diffArtifacts(prev, next *derive.Derived) []string {
	if prev == nil || prev.Series == nil {
		if next != nil && next.Series != nil {
			return append([]string{next.Series.ID}, candidateIDs(next)...)
		}
		return nil
	}
	var changed []string
	if prev.Series == nil || next.Series == nil || prev.Series.FP != next.Series.FP ||
		len(prev.Series.Points) != len(next.Series.Points) ||
		!samePoints(prev.Series.Points, next.Series.Points) ||
		len(prev.Series.Gaps) != len(next.Series.Gaps) {
		changed = append(changed, next.Series.ID)
	}
	prevCand := map[string]bool{}
	for _, c := range prev.Candidates {
		prevCand[c.ID] = true
		if n := findCand(next.Candidates, c.ID); n == nil || candCoreChanged(c, *n) {
			changed = append(changed, c.ID)
		}
	}
	for _, c := range next.Candidates {
		if !prevCand[c.ID] {
			changed = append(changed, c.ID)
		}
	}
	if prev.Residue == nil || next.Residue == nil || prev.Residue.ResiduePct != next.Residue.ResiduePct {
		changed = append(changed, residueID(next))
	}
	return dedupe(changed)
}

func findCand(cs []derive.Candidate, id string) *derive.Candidate {
	for i := range cs {
		if cs[i].ID == id {
			return &cs[i]
		}
	}
	return nil
}

func candCoreChanged(a, b derive.Candidate) bool {
	return a.OnsetS != b.OnsetS || a.OffsetS != b.OffsetS || a.PeakS != b.PeakS ||
		a.Status != b.Status || a.LossPct != b.LossPct
}

func samePoints(a, b []derive.SPoint) bool {
	for i := range a {
		if a[i].TS != b[i].TS || a[i].MassMg != b[i].MassMg ||
			a[i].DTGPctPerS != b[i].DTGPctPerS || a[i].SegmentID != b[i].SegmentID {
			return false
		}
	}
	return true
}

func seriesID(d *derive.Derived) string {
	if d.Series != nil {
		return d.Series.ID
	}
	return ""
}

func residueID(d *derive.Derived) string {
	if d.Residue != nil {
		return d.Residue.ID
	}
	return ""
}

func candidateIDs(d *derive.Derived) []string {
	if d == nil {
		return nil
	}
	out := make([]string, 0, len(d.Candidates))
	for _, c := range d.Candidates {
		out = append(out, c.ID)
	}
	return out
}

func rebuildResidue(d *derive.Derived, st *derive.State) *derive.ResidueReport {
	return derive.BuildResidueExternal(d.Series, st, d.Candidates, d.AtSeq)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
