package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"tgareview/internal/domain"
	"tgareview/internal/store"
)

// DecisionInput carries one human action.
type DecisionInput struct {
	CandidateID   string         `json:"candidate_id"`
	CandidateKind string         `json:"candidate_kind"`
	Action        string         `json:"action"` // accept|reject|adjust
	Rationale     string         `json:"rationale"`
	Params        map[string]any `json:"params,omitempty"`
}

// MakeDecision appends a decision, records a conflict if it contradicts an
// earlier active decision, and also snapshots an adjusted candidate as a new
// proposed candidate tied to the decision.
func (a *App) MakeDecision(ctx context.Context, projectID, actor string, in DecisionInput) ([]domain.Event, error) {
	if in.Action != domain.CandAccepted && in.Action != domain.CandRejected && in.Action != domain.CandAdjusted {
		return nil, fmt.Errorf("unknown action %q", in.Action)
	}
	art, cand, err := a.findCandidate(ctx, projectID, in.CandidateID)
	if err != nil {
		return nil, err
	}
	tx, err := a.St.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	payload := map[string]any{
		"candidate_id":   cand.ID,
		"candidate_name": cand.Name,
		"candidate_kind": cand.Kind,
		"action":         in.Action,
		"rationale":      in.Rationale,
		"artifact":       art.ID,
		"params":         in.Params,
	}
	ev, err := a.St.AppendEventTx(ctx, tx, projectID, domain.EvDecision, actor,
		fmt.Sprintf("%s %s：%s", actionCN(in.Action), cand.Name, in.Rationale), payload)
	if err != nil {
		return nil, err
	}
	if err := a.St.SaveDecision(ctx, tx, ev, store.DecisionRec{
		CandidateID: cand.ID, CandidateKind: cand.Kind,
		Action: in.Action, Rationale: in.Rationale, Params: in.Params,
	}); err != nil {
		return nil, err
	}
	outEvents := []domain.Event{ev}

	// conflicting with a prior active decision?
	if prev, perr := a.activeDecisionBefore(ctx, tx, projectID, cand.ID, ev.Seq); perr == nil && contradicts(prev.Action, in.Action) {
		cev, err := a.St.AppendEventTx(ctx, tx, projectID, domain.EvConflict, "system",
			fmt.Sprintf("冲突：候选 %s 先前被%s，现被%s", cand.Name, actionCN(prev.Action), actionCN(in.Action)),
			map[string]any{
				"candidate_id": cand.ID, "prior_event": prev.EventID, "prior_action": prev.Action,
				"new_event": ev.ID, "new_action": in.Action,
				"state":      "open",
			})
		if err != nil {
			return nil, err
		}
		outEvents = append(outEvents, cev)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return outEvents, nil
}

// ResolveConflict records how a conflict was adjudicated.
func (a *App) ResolveConflict(ctx context.Context, projectID, actor, conflictEventID, resolution string) (domain.Event, error) {
	tx, err := a.St.BeginTx(ctx)
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback()
	ev, err := a.St.AppendEventTx(ctx, tx, projectID, domain.EvConflictResolved, actor,
		"冲突解决："+resolution, map[string]any{
			"conflict_event": conflictEventID, "resolution": resolution,
		})
	if err != nil {
		return domain.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

// RevokeDecision undoes a decision by marking it inactive and appending a
// decision_revoked event; nothing is ever deleted.
func (a *App) RevokeDecision(ctx context.Context, projectID, actor, candidateID, reason string) (domain.Event, error) {
	tx, err := a.St.BeginTx(ctx)
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback()
	ev, err := a.St.AppendEventTx(ctx, tx, projectID, domain.EvDecisionRevoked, actor,
		"撤销对候选 "+candidateID+" 的裁定："+reason,
		map[string]any{"candidate_id": candidateID, "reason": reason})
	if err != nil {
		return domain.Event{}, err
	}
	if _, err := a.St.RevokeDecision(ctx, tx, projectID, candidateID, ev.ID); err != nil {
		return domain.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

func contradicts(prev, now string) bool {
	return (prev == domain.CandAccepted && now == domain.CandRejected) ||
		(prev == domain.CandRejected && now == domain.CandAccepted)
}

func actionCN(a string) string {
	switch a {
	case domain.CandAccepted:
		return "接受"
	case domain.CandRejected:
		return "否定"
	case domain.CandAdjusted:
		return "参数调整"
	}
	return a
}

func (a *App) findCandidate(ctx context.Context, projectID, candidateID string) (*domain.Artifact, domain.Candidate, error) {
	arts, err := a.St.ListArtifacts(ctx, projectID, domain.ArtCandidates)
	if err != nil {
		return nil, domain.Candidate{}, err
	}
	for i := range arts {
		for _, c := range arts[i].Candidates {
			if c.ID == candidateID {
				return &arts[i], c, nil
			}
		}
	}
	return nil, domain.Candidate{}, errors.New("候选不存在: " + candidateID)
}

func (a *App) activeDecisionBefore(ctx context.Context, _ *store.Tx, projectID, candidateID string, beforeSeq int64) (*store.DecisionRec, error) {
	decs, err := a.St.ListDecisions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var active *store.DecisionRec
	for i := range decs {
		d := &decs[i]
		if d.CandidateID == candidateID && d.EventSeq < beforeSeq && d.Active {
			active = d
		}
	}
	if active == nil {
		return nil, store.ErrNotFound
	}
	return active, nil
}

// AddNote appends a free-form journal note.
func (a *App) AddNote(ctx context.Context, projectID, actor, text string) (domain.Event, error) {
	return a.St.AppendEvent(ctx, projectID, domain.EvNote, actor, text, map[string]any{"text": text})
}

// CandidateView is a candidate annotated with decision and staleness.
type CandidateView struct {
	domain.Candidate
	Current     bool                `json:"current"`
	StaleReasons []string           `json:"stale_reasons,omitempty"`
	Decision    *store.DecisionRec  `json:"decision,omitempty"`
	ArtifactID  string              `json:"artifact_id"`
	ArtifactSeq int64               `json:"artifact_seq"`
}

// buildViewsAt projects the full review state at an event position.
func (a *App) buildViewsAt(ctx context.Context, projectID string, seq int64, historical bool) (*ReviewView, error) {
	v := &ReviewView{ProjectID: projectID, AtSeq: seq, Historical: historical}
	events, err := a.St.ListEvents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, ev := range events {
		if ev.Seq <= seq {
			v.Events = append(v.Events, ev)
		}
	}
	p, err := paramsAtSeq(ctx, a.St, projectID, seq)
	if err != nil {
		return nil, err
	}
	v.Params = p

	for _, kind := range []string{domain.ArtCorrectedTG, domain.ArtDTG, domain.ArtAtmoTimeline, domain.ArtCandidates, domain.ArtResidue} {
		art, err := a.St.ArtifactAtSeq(ctx, projectID, kind, seq)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		// Artifact.Data remains generic JSON for the API; the SVG layer decodes
		// it into the typed derive structs when drawing.
		stale, reasons := false, []string(nil)
		if !historical {
			stale, reasons = a.IsStale(ctx, projectID, *art)
		}
		av := ArtifactView{Artifact: art, Stale: stale, StaleReasons: reasons}
		v.Artifacts = append(v.Artifacts, av)
	}

	// candidates from the candidates artifact at seq, with decision @seq
	if ca, err := a.St.ArtifactAtSeq(ctx, projectID, domain.ArtCandidates, seq); err == nil {
		for _, c := range ca.Candidates {
			cv := CandidateView{Candidate: c, ArtifactID: ca.ID, ArtifactSeq: ca.Seq}
			if !historical {
				if st, rs := a.IsStale(ctx, projectID, *ca); st {
					cv.Current = false
					cv.StaleReasons = rs
				} else {
					cv.Current = true
				}
				if dec, derr := decisionEffectiveAt(ctx, a.St, projectID, c.ID, seq); derr == nil {
					cv.Decision = dec
				}
			} else {
				if dec, derr := decisionEffectiveAt(ctx, projectID, a.St, projectID, c.ID, seq); derr == nil {
					cv.Decision = dec
				}
			}
			v.Candidates = append(v.Candidates, cv)
		}
	}

	// current sources snapshot
	for _, k := range domain.SourceKinds {
		if s, err := a.St.LatestSource(ctx, projectID, k); err == nil {
			if historical {
				// find the latest version committed by seq
				vers, _ := a.St.ListSourceVersions(ctx, projectID, k)
				picked := -1
				for _, sv := range vers {
					if seqOfCommit(events, sv.CommitEventID) <= seq {
						picked = sv.Version
					}
				}
				if picked >= 1 && picked < s.Version {
					if hs, herr := a.St.SourceAtVersion(ctx, projectID, k, picked); herr == nil {
						v.Sources = append(v.Sources, *hs)
						continue
					}
				}
				if picked < 1 {
					continue
				}
			}
			v.Sources = append(v.Sources, *s)
		}
	}

	decs, _ := a.St.ListDecisions(ctx, projectID)
	for _, d := range decs {
		if d.EventSeq <= seq {
			v.Decisions = append(v.Decisions, d)
		}
	}
	return v, nil
}

func seqOfCommit(events []domain.Event, eventID string) int64 {
	for _, ev := range events {
		if ev.ID == eventID {
			return ev.Seq
		}
	}
	return 1<<62
}

func decisionEffectiveAt(ctx context.Context, st *store.Store, projectID, candidateID string, seq int64) (*store.DecisionRec, error) {
	decs, err := st.ListDecisions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	var active *store.DecisionRec
	for i := range decs {
		d := &decs[i]
		if d.CandidateID == candidateID && d.EventSeq <= seq {
			if d.Active {
				dd := *d
				active = &dd
			} else {
				active = nil
			}
		}
	}
	if active == nil {
		return nil, store.ErrNotFound
	}
	return active, nil
}
