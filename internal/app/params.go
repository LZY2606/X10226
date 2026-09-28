package app

import (
	"context"
	"encoding/json"

	"tgareview/internal/domain"
	"tgareview/internal/store"
)

// ReviseParams validates, versions and records a parameter revision. Only
// artifacts whose parameter fingerprint changes become stale.
func (a *App) ReviseParams(ctx context.Context, projectID, actor, rationale string, next domain.Params) (domain.Event, error) {
	cur, err := a.CurrentParams(ctx, projectID)
	if err != nil {
		return domain.Event{}, err
	}
	next.Version = cur.Version + 1
	tx, err := a.St.BeginTx(ctx)
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback()
	ev, err := a.St.AppendEventTx(ctx, tx, projectID, domain.EvParamsRevised, actor,
		"参数修订 v"+itoa64(int64(next.Version))+"："+rationale, map[string]any{
			"version":        next.Version,
			"parent_version": cur.Version,
			"params":         next,
			"parent_fp":      cur.Fingerprint(),
			"new_fp":         next.Fingerprint(),
			"rationale":      rationale,
			"algo":           domain.AlgoVersion,
		})
	if err != nil {
		return domain.Event{}, err
	}
	b, _ := json.Marshal(next)
	if err := a.St.SetKV(ctx, tx, projectID, kvParams, string(b), ev.Seq); err != nil {
		return domain.Event{}, err
	}
	return ev, tx.Commit()
}

// paramsAtSeq reconstructs the parameter set effective at a journal position
// by replaying params_revised events.
func paramsAtSeq(ctx context.Context, st *store.Store, projectID string, seq int64) (domain.Params, error) {
	events, err := st.ListEvents(ctx, projectID)
	if err != nil {
		return domain.Params{}, err
	}
	p := domain.DefaultParams()
	found := false
	for _, ev := range events {
		if ev.Seq > seq {
			break
		}
		if ev.Type == domain.EvParamsRevised {
			raw, _ := json.Marshal(ev.Payload["params"])
			var np domain.Params
			if err := json.Unmarshal(raw, &np); err == nil {
				p = np
				found = true
			}
		}
	}
	_ = found
	return p, nil
}

func itoa64(i int64) string {
	return strconvFormatInt(i)
}
