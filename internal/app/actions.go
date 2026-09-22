package app

import (
	"encoding/json"
	"errors"
	"fmt"

	"tga-review/internal/derive"
	"tga-review/internal/domain"
)

// ReviseParams appends a PARAMS_REVISED event, recomputes the whole derived
// graph under the new fingerprint and rebinds every slot. Old nodes stay
// stored and remain reachable through history.
func (a *App) ReviseParams(projID string, p domain.Params, reason, actor string) (domain.Event, error) {
	head, err := a.Store.RawHead(projID)
	if err != nil {
		return domain.Event{}, err
	}
	oldID, _ := a.Store.GetKV(projID, "head_params_id")
	newID := derive.ParamsID(p)
	if oldID == newID {
		return domain.Event{}, errors.New("parameter revision is identical to current version")
	}
	eng := derive.NewEngine(p, head, a.Clock.Now())
	res := eng.Run(projID)

	tx, err := a.Store.DB().Begin()
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback()
	for _, d := range []*domain.Derived{res.Correct, res.DTG, res.Stages, res.Residue} {
		if d != nil {
			if err := saveDerivedTx(tx, d); err != nil {
				return domain.Event{}, err
			}
		}
	}
	payload, _ := json.Marshal(p)
	if _, err := tx.Exec(`INSERT INTO keyvals(project_id,key,value) VALUES(?,?,?)
		ON CONFLICT(project_id,key) DO UPDATE SET value=excluded.value`, projID, "head_params", string(payload)); err != nil {
		return domain.Event{}, err
	}
	if _, err := tx.Exec(`INSERT INTO keyvals(project_id,key,value) VALUES(?,?,?)
		ON CONFLICT(project_id,key) DO UPDATE SET value=excluded.value`, projID, "head_params_id", newID); err != nil {
		return domain.Event{}, err
	}
	binds := []domain.Bind{}
	for _, d := range []*domain.Derived{res.Correct, res.DTG, res.Stages, res.Residue} {
		if d != nil {
			binds = append(binds, domain.Bind{Slot: d.Slot, NodeID: d.ID, Reason: "recomputed under revised params"})
		}
	}
	pl := domain.ParamsPayload{Params: p, ParamsID: newID, ParentID: oldID, Reason: reason, Binds: binds}
	ev, err := appendEventRaw(tx, projID, domain.EvParams, pl, actor, a.Clock.Now())
	if err != nil {
		return domain.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

// Decide records an accept/reject/adjust on a candidate or residue, pinned to
// the exact derived node the reviewer saw.
func (a *App) Decide(projID string, pl domain.DecisionPayload) (domain.Event, error) {
	v, err := a.View(projID, 0)
	if err != nil {
		return domain.Event{}, err
	}
	switch pl.TargetKind {
	case "stage":
		found := false
		if v.Stages != nil {
			for _, c := range v.Stages.Stages.Candidates {
				if c.CID == pl.CID {
					found = true
					if pl.Snapshot == "" {
						pl.Snapshot = fmt.Sprintf("peak=%.2fs loss=%.4fmg status=%s", c.PeakTimeS, c.MassLossMg, c.Status)
					}
				}
			}
		}
		if !found {
			return domain.Event{}, fmt.Errorf("candidate %s not found in current stages node", pl.CID)
		}
		if v.Stages != nil {
			pl.NodeID = v.Stages.ID
		}
	case "residue":
		found := false
		if v.Residue != nil {
			for _, r := range v.Residue.Residue.Residues {
				if r.RID == pl.CID {
					found = true
					if pl.Snapshot == "" {
						pct := 0.0
						if r.ResiduePct != nil {
							pct = *r.ResiduePct
						}
						pl.Snapshot = fmt.Sprintf("final=%.4fmg residue=%.4f%%", r.FinalMg, pct)
					}
				}
			}
		}
		if !found {
			return domain.Event{}, fmt.Errorf("residue %s not found", pl.CID)
		}
		if v.Residue != nil {
			pl.NodeID = v.Residue.ID
		}
	default:
		return domain.Event{}, errors.New("target_kind must be stage or residue")
	}
	if pl.Action != "accept" && pl.Action != "reject" && pl.Action != "adjust" {
		return domain.Event{}, errors.New("action must be accept, reject or adjust")
	}
	if pl.Reviewer == "" {
		pl.Reviewer = "reviewer"
	}
	return a.Store.AppendEvent(projID, domain.EvDecision, pl, pl.Reviewer, a.Clock.Now())
}

// ResolveConflict records a winner between mutually exclusive human decisions
// (e.g. accepting both merged and resolved overlap alternatives).
func (a *App) ResolveConflict(projID, actor string, pl domain.ConflictPayload) (domain.Event, error) {
	if pl.WonCID == "" || pl.LostCID == "" {
		return domain.Event{}, errors.New("won_cid and lost_cid are required")
	}
	return a.Store.AppendEvent(projID, domain.EvConflict, pl, actor, a.Clock.Now())
}

// UndoDecision inverts a prior decision or conflict resolution; the old event
// stays in history and the inverse is itself recorded.
func (a *App) UndoDecision(projID, actor string, undoneSeq int, reason string) (domain.Event, error) {
	evs, err := a.Store.Events(projID)
	if err != nil {
		return domain.Event{}, err
	}
	var target *domain.Event
	for i := range evs {
		if evs[i].Seq == undoneSeq {
			target = &evs[i]
		}
	}
	if target == nil {
		return domain.Event{}, fmt.Errorf("event %d not found", undoneSeq)
	}
	if target.Type != domain.EvDecision && target.Type != domain.EvConflict {
		return domain.Event{}, errors.New("only decisions and conflict resolutions can be undone")
	}
	pl := domain.UndoPayload{UndoneEventSeq: undoneSeq, UndoneEventID: target.ID, Reason: reason}
	return a.Store.AppendEvent(projID, domain.EvUndo, pl, actor, a.Clock.Now())
}

// AddRule stores a machine-readable judgment basis event.
func (a *App) AddRule(projID, actor string, pl domain.RulePayload) (domain.Event, error) {
	if pl.Code == "" {
		return domain.Event{}, errors.New("rule code required")
	}
	return a.Store.AppendEvent(projID, domain.EvRule, pl, actor, a.Clock.Now())
}
