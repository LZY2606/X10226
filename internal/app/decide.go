package app

import (
	"encoding/json"
	"fmt"

	"gotga/internal/domain"
)

// Decide applies an accept/reject (or parameter adjustment) to a candidate or
// residue. Conflicting verdicts on the same subject are both retained and the
// situation is marked conflicting until an explicit resolution.
func (a *App) Decide(projectID, subjectType, subjectID, verdict, reason string, adj *domain.Params, actor string) (*domain.Decision, int64, error) {
	if verdict != "accepted" && verdict != "rejected" {
		return nil, 0, fmt.Errorf("verdict must be accepted or rejected")
	}
	prior, _ := a.Store.Decisions(projectID)
	var conflict []string
	for _, d := range prior {
		if d.SubjectType == subjectType && d.SubjectID == subjectID && !d.Revoked && d.Verdict != verdict {
			conflict = append(conflict, d.ID)
		}
	}
	d := &domain.Decision{
		ID:           domain.HashID("dec", projectID, subjectType, subjectID, verdict, reason, a.ts()),
		EventSeq:     0,
		SubjectType:  subjectType,
		SubjectID:    subjectID,
		Verdict:      verdict,
		Reason:       reason,
		ConflictWith: conflict,
		Adjustment:   adj,
		TS:           a.ts(),
	}
	pl := domain.DecisionPayload{SubjectType: subjectType, SubjectID: subjectID, Verdict: verdict, Adjustment: adj, Reason: reason}
	pb, _ := json.Marshal(pl)
	note := reason
	if len(conflict) > 0 {
		note = "conflicting decision: " + reason
	}
	e, err := a.appendEvent(projectID, domain.EvDecision, string(pb), "", actor, note)
	if err != nil {
		return nil, 0, err
	}
	d.EventSeq = e.Seq
	if len(conflict) == 0 {
		d.Winner = true
	}
	if err := a.Store.PutDecision(projectID, d); err != nil {
		return nil, 0, err
	}
	return d, e.Seq, nil
}

// ResolveConflict declares one winning decision; the losers stay recorded and
// are marked revoked=false but losing (history keeps both).
func (a *App) ResolveConflict(projectID, winnerID string, againstIDs []string, reason, actor string) (int64, error) {
	pl := domain.ConflictResolvePayload{AgainstIDs: againstIDs, WinnerID: winnerID, Reason: reason}
	pb, _ := json.Marshal(pl)
	e, err := a.appendEvent(projectID, domain.EvConflictResolved, string(pb), "", actor, reason)
	if err != nil {
		return 0, err
	}
	for _, id := range againstIDs {
		if err := a.Store.RevokeDecision(projectID, id, e.ID); err != nil {
			return 0, err
		}
	}
	// Mark the winner.
	all, _ := a.Store.Decisions(projectID)
	for i := range all {
		if all[i].ID == winnerID {
			_, _ = a.Store.DB.Exec(`UPDATE decisions SET winner=1,resolved_by=? WHERE id=?`, e.ID, winnerID)
		}
	}
	return e.Seq, nil
}

// Revoke undoes a prior event (decision or conflict). It appends a reversal
// event rather than deleting history; replaying to before the target restores
// the then-current view.
func (a *App) Revoke(projectID string, targetSeq int64, reason, actor string) (int64, error) {
	events, err := a.Store.Events(projectID)
	if err != nil {
		return 0, err
	}
	var target *domain.Event
	for i := range events {
		if events[i].Seq == targetSeq {
			target = &events[i]
			break
		}
	}
	if target == nil {
		return 0, fmt.Errorf("event seq %d not found", targetSeq)
	}
	pl := domain.RevokedPayload{TargetSeq: targetSeq, TargetID: target.ID, Reason: reason}
	pb, _ := json.Marshal(pl)
	e, err := a.appendEvent(projectID, domain.EvRevoked, string(pb), target.ID, actor, reason)
	if err != nil {
		return 0, err
	}
	if target.Kind == domain.EvDecision {
		var dp domain.DecisionPayload
		_ = json.Unmarshal([]byte(target.Payload), &dp)
		// revoke the decision row created at that seq
		_, _ = a.Store.DB.Exec(`UPDATE decisions SET revoked=1,resolved_by=? WHERE project_id=? AND event_seq=?`, e.ID, projectID, targetSeq)
	}
	return e.Seq, nil
}
