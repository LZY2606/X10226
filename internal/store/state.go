package store

import (
	"context"
	"encoding/json"

	"tga-review/internal/domain"
)

// DecisionView is one human decision with the object it applies to.
type DecisionView struct {
	EventID   string                       `json:"event_id"`
	Seq       int64                        `json:"seq"`
	At        string                       `json:"at"`
	Reviewer  string                       `json:"reviewer"`
	Undone    bool                         `json:"undone"`
	Conflict  *domain.ConflictResolvedPayload `json:"conflict,omitempty"`
	Payload   domain.DecisionPayload       `json:"payload"`
}

// State is the replay projection at one point in the ledger.
type State struct {
	ProjectID      string                  `json:"project_id"`
	ProjectName    string                  `json:"project_name"`
	Params         domain.Params           `json:"params"`
	ParamHistory   []ParamsVersion         `json:"param_history"`
	Runs           []domain.DerivedSet     `json:"runs"`
	CurrentRun     *domain.DerivedSet      `json:"current_run"`
	StaleRuns      []string                `json:"stale_runs"`
	Decisions      []DecisionView          `json:"decisions"`
	Events         []domain.Event          `json:"events"`
	ActiveEvents   []string                `json:"active_event_ids"`
}

// ParamsVersion ties a parameter set to the event that introduced it.
type ParamsVersion struct {
	EventID     string        `json:"event_id"`
	Seq         int64         `json:"seq"`
	Fingerprint string        `json:"fingerprint"`
	Params      domain.Params `json:"params"`
	Reason      string        `json:"reason"`
}

// Events reads the ledger in sequence order.
func (s *Store) Events(ctx context.Context, projectID string) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,seq,type,occurred_at,reviewer,undoes,conflict_of,payload
		 FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var ev domain.Event
		if err := rows.Scan(&ev.ID, &ev.Seq, &ev.Type, &ev.OccurredAt, &ev.Reviewer, &ev.Undoes, &ev.ConflictOf, &ev.Payload); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// Replay rebuilds state through the event at seq<=asOfSeq (0 = all).
func (s *Store) Replay(ctx context.Context, projectID string, asOfSeq int64) (*State, error) {
	name, err := s.ProjectName(ctx, projectID)
	if err != nil {
		return nil, err
	}
	events, err := s.Events(ctx, projectID)
	if err != nil {
		return nil, err
	}
	st := &State{ProjectID: projectID, ProjectName: name, Params: domain.DefaultParams()}
	undone := map[string]bool{}
	// first pass: collect undo relationships
	for _, ev := range events {
		if asOfSeq > 0 && ev.Seq > asOfSeq {
			break
		}
		if ev.Type == domain.EvtUndo {
			var p domain.UndoPayload
			_ = json.Unmarshal(ev.Payload, &p)
			undone[p.ReversedEventID] = true
		}
	}
	var latestRunID string
	// params version sequence index for parent linking
	for _, ev := range events {
		if asOfSeq > 0 && ev.Seq > asOfSeq {
			break
		}
		if undone[ev.ID] {
			continue
		}
		st.ActiveEvents = append(st.ActiveEvents, ev.ID)
		switch ev.Type {
		case domain.EvtParamsRevised:
			var p domain.ParamsRevisedPayload
			if err := json.Unmarshal(ev.Payload, &p); err == nil {
				st.Params = p.Params
				st.ParamHistory = append(st.ParamHistory, ParamsVersion{
					EventID: ev.ID, Seq: ev.Seq, Fingerprint: p.NewFingerprint,
					Params: p.Params, Reason: p.Reason,
				})
			}
		case domain.EvtRunProduced:
			var p domain.RunProducedPayload
			if err := json.Unmarshal(ev.Payload, &p); err == nil {
				st.Runs = append(st.Runs, p.Derived)
				latestRunID = p.Derived.RunID
			}
		case domain.EvtDecision:
			var p domain.DecisionPayload
			_ = json.Unmarshal(ev.Payload, &p)
			st.Decisions = append(st.Decisions, DecisionView{
				EventID: ev.ID, Seq: ev.Seq, At: ev.OccurredAt, Reviewer: ev.Reviewer, Payload: p,
			})
		case domain.EvtConflict:
			var c domain.ConflictResolvedPayload
			_ = json.Unmarshal(ev.Payload, &c)
			for i := range st.Decisions {
				if st.Decisions[i].EventID == c.SecondDecision {
					cp := c
					st.Decisions[i].Conflict = &cp
				}
			}
		}
	}
	// undo flags on decisions + current run selection
	undoSet := map[string]bool{}
	for _, ev := range events {
		if asOfSeq > 0 && ev.Seq > asOfSeq {
			break
		}
		if ev.Type == domain.EvtUndo {
			var p domain.UndoPayload
			_ = json.Unmarshal(ev.Payload, &p)
			undoSet[p.ReversedEventID] = true
		}
	}
	for i := range st.Decisions {
		if undoSet[st.Decisions[i].EventID] {
			st.Decisions[i].Undone = true
		}
	}
	for i := range st.Runs {
		if st.Runs[i].RunID == latestRunID {
			run := st.Runs[i]
			st.CurrentRun = &run
			break
		}
	}
	// mark stale: a run is stale if a params revision follows it or if an
	// imported batch postdates it; old runs remain viewable, never current.
	st.StaleRuns = markStale(st, events, asOfSeq, undone)
	applyDecisions(st)
	st.Events = events
	return st, nil
}

// markStale returns run ids superseded by later inputs/params.
func markStale(st *State, events []domain.Event, asOf int64, globallyUndone map[string]bool) []string {
	// gather (seq, kind) of params revisions and runs only
	var runSeq map[string]int64 = map[string]int64{}
	var lastChange int64
	for _, ev := range events {
		if asOf > 0 && ev.Seq > asOf {
			break
		}
		if globallyUndone[ev.ID] {
			continue
		}
		switch ev.Type {
		case domain.EvtRunProduced:
			var p domain.RunProducedPayload
			_ = json.Unmarshal(ev.Payload, &p)
			runSeq[p.Derived.RunID] = ev.Seq
		case domain.EvtParamsRevised:
			if ev.Seq > lastChange {
				lastChange = ev.Seq
			}
		case domain.EvtBatchImported:
			if ev.Seq > lastChange {
				lastChange = ev.Seq
			}
		}
	}
	var stale []string
	for id, seq := range runSeq {
		if seq < lastChange {
			stale = append(stale, id)
		}
	}
	return stale
}

// applyDecisions overlays active (latest, not undone) decisions onto the
// current run's stages/residue. Old decisions stay visible in history but the
// current run is never silently overwritten: adjusted stages are new objects.
func applyDecisions(st *State) {
	if st.CurrentRun == nil {
		return
	}
	byTarget := map[string]DecisionView{}
	for _, d := range st.Decisions {
		if d.Undone {
			continue
		}
		byTarget[d.Payload.TargetID] = d
	}
	run := st.CurrentRun
	run.Residue.Status = "CURRENT"
	for _, id := range st.StaleRuns {
		if id == run.RunID {
			run.Residue.Status = "STALE"
		}
	}
	for i := range run.Stages {
		sg := &run.Stages[i]
		if d, ok := byTarget[sg.ID]; ok {
			sg.DecisionID = d.EventID
			switch d.Payload.Action {
			case domain.DecisionAccept:
				sg.Status = "ACCEPTED"
			case domain.DecisionReject:
				sg.Status = "REJECTED"
				sg.RejectReason = d.Payload.Reason
			case domain.DecisionAdjust:
				sg.Status = "ADJUSTED"
				if d.Payload.NewOnsetT != nil {
					sg.TOnset = *d.Payload.NewOnsetT
				}
				if d.Payload.NewEndT != nil {
					sg.TEnd = *d.Payload.NewEndT
				}
			case domain.DecisionUncertain:
				sg.Status = "PROPOSED"
				sg.RejectReason = d.Payload.Reason
			}
		}
	}
	st.CurrentRun = run
}
