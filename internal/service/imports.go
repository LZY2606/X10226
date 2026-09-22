package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"tgareview/internal/domain"
	"tgareview/internal/parse"
)

// ImportResult summarizes a fragment import, including half-failure.
type ImportResult struct {
	BatchID      string
	Status       string
	AcceptedRows int
	RejectedRows int
	Diag         string
	HeadSeq      int64
}

// ImportFragment parses and commits a tga-1 fragment. Accepted rows become
// immutable source.row events inside one batch; rejected rows remain preserved
// in the batch diagnostics and as failed source rows. Partial import still
// commits (import half-failure is first-class, not an all-or-nothing error).
func (s *Service) ImportFragment(ctx context.Context, pid, fragment, kind, parentBatch, origin, actor string) (*ImportResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadState(ctx, pid)
	if err != nil {
		return nil, err
	}
	parsed, err := parse.Parse(fragment, kind, parentBatch, origin, nil)
	if err != nil {
		return nil, err
	}
	batchID := domain.ShortID("batch", pid, fmt.Sprint(state.Seq+1),
		domain.Fingerprint(fragment))
	parsed.Batch.BatchID = batchID
	batch := parsed.Batch
	batchEv, err := s.commit(ctx, state, domain.EvBatchImported, actor, "", batch)
	if err != nil {
		return nil, err
	}
	for _, row := range parsed.Rows {
		row.BatchID = batchID
		if _, err := s.commit(ctx, state, domain.EvSourceRow, actor, batchEv.EventID, row); err != nil {
			return nil, err
		}
	}
	// Parameter rows inside the fragment become a new params revision.
	if p, ok := paramsFromRows(parsed.Rows); ok {
		parent := state.ParamsParent
		pp := domain.ParamsSetPayload{
			ParentParams: parent,
			Reason:       "import:" + batchID[:8],
			Params:       p,
		}
		if _, err := s.commit(ctx, state, domain.EvParamsSet, actor, batchEv.EventID, pp); err != nil {
			return nil, err
		}
	}
	return &ImportResult{
		BatchID: batchID, Status: batch.Status,
		AcceptedRows: batch.AcceptedRows, RejectedRows: batch.RejectedRows,
		Diag: batch.Diag, HeadSeq: state.Seq,
	}, nil
}

// paramsFromRows folds [params] rows into a new parameter revision.
func paramsFromRows(rows []domain.SourceRowPayload) (domain.Params, bool) {
	p := domain.DefaultParams()
	found := false
	set := func(name string, v float64) {
		found = true
		switch name {
		case "atmosphere_delay_s":
			p.AtmosphereDelayS = v
		case "temperature_lag_c":
			p.TemperatureLagC = v
		case "drift_rate_mg_per_min":
			p.DriftRateMgPerMin = v
		case "peak_prominence_mg_s":
			p.PeakProminenceMgS = v
		case "peak_merge_window_s":
			p.PeakMergeWindowS = v
		case "noise_tol_mg":
			p.NoiseTolMg = v
		case "gap_threshold_s":
			p.GapThresholdS = v
		case "blank_scale":
			p.BlankScale = v
		}
	}
	for _, r := range rows {
		if r.Layer != domain.LayerParams || r.Status != domain.RowOK {
			continue
		}
		for _, f := range r.Fields {
			if v, err := strconv.ParseFloat(f.Raw, 64); err == nil {
				set(f.Name, v)
			}
		}
	}
	p.AlgorithmVersion = domain.AlgorithmVersion
	return p, found
}

// SetParams commits a parameter revision (parameter revision scenario).
func (s *Service) SetParams(ctx context.Context, pid string, p domain.Params, reason, actor string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadState(ctx, pid)
	if err != nil {
		return 0, err
	}
	p.AlgorithmVersion = domain.AlgorithmVersion
	pp := domain.ParamsSetPayload{ParentParams: state.ParamsParent, Reason: reason, Params: p}
	if _, err := s.commit(ctx, state, domain.EvParamsSet, actor, "", pp); err != nil {
		return 0, err
	}
	return state.Seq, nil
}

// AddNote commits a machine-readable justification event.
func (s *Service) AddNote(ctx context.Context, pid, code, message string, refs []string, actor string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, err := s.loadState(ctx, pid)
	if err != nil {
		return 0, err
	}
	ev, err := s.commit(ctx, state, domain.EvNote, actor, "",
		domain.NotePayload{Code: code, Message: message, Refs: refs})
	if err != nil {
		return 0, err
	}
	return ev.Seq, nil
}

// MarshalView is a small helper used by the web layer.
func MarshalView(v any) ([]byte, error) { return json.Marshal(v) }
