package domain

func Replay(events []Event) (*ProjectState, error) {
	st := NewState("", "", "")
	for _, ev := range events {
		if _, err := ApplyEvent(st, ev); err != nil {
			return nil, err
		}
	}
	applyDecisions(st)
	return st, nil
}

func applyDecisions(st *ProjectState) {
	active := map[string]CandidateDecision{}
	for _, d := range st.Decisions {
		if d.Undone {
			delete(active, d.TargetKind+":"+d.TargetID)
		} else {
			active[d.TargetKind+":"+d.TargetID] = d
		}
	}
	for ei := range st.Evaluations {
		ev := &st.Evaluations[ei]
		for si := range ev.Stages {
			stage := &ev.Stages[si]
			if d, ok := active["stage:"+stage.ID]; ok && targetVisibleAtDecision(st, d, *ev) {
				applyStageDecision(stage, d)
			}
		}
		for ri := range ev.Residues {
			res := &ev.Residues[ri]
			if d, ok := active["residue:"+res.ID]; ok && targetVisibleAtDecision(st, d, *ev) {
				res.ResiduePct.Status = d.Action
			}
		}
	}
}

func targetVisibleAtDecision(st *ProjectState, d CandidateDecision, ev Evaluation) bool {
	if len(st.Evaluations) == 0 {
		return true
	}
	return ev.At <= d.At || ev.ID == currentEvaluationID(st)
}

func currentEvaluationID(st *ProjectState) string {
	if len(st.Evaluations) == 0 {
		return ""
	}
	return st.Evaluations[len(st.Evaluations)-1].ID
}

func applyStageDecision(stage *StageCandidate, d CandidateDecision) {
	if d.Action == "adjusted" {
		stage.Status = "accepted_adjusted"
		if d.AdjustedStartS != nil {
			stage.StartS = *d.AdjustedStartS
		}
		if d.AdjustedEndS != nil {
			stage.EndS = *d.AdjustedEndS
		}
		return
	}
	stage.Status = d.Action
}

func MarkStale(ev *Evaluation, currentDeps map[string]string, reason string) {
	mark := func(deps map[string]string, setter func(string)) {
		for key, old := range deps {
			if cur, ok := currentDeps[key]; ok && cur != old {
				setter(reason)
				return
			}
		}
	}
	for i := range ev.Series {
		mark(ev.Series[i].Deps, func(r string) { ev.Series[i].StaleReason = r })
	}
	for i := range ev.Stages {
		mark(ev.Stages[i].Deps, func(r string) { ev.Stages[i].StaleReason = r })
	}
	for i := range ev.Residues {
		mark(ev.Residues[i].Deps, func(r string) { ev.Residues[i].StaleReason = r })
	}
	for i := range ev.Atmosphere {
		mark(ev.Atmosphere[i].Deps, func(r string) { ev.Atmosphere[i].StaleReason = r })
	}
	for i := range ev.Furnace {
		mark(ev.Furnace[i].Deps, func(r string) { ev.Furnace[i].StaleReason = r })
	}
}

func CurrentDeps(st *ProjectState) map[string]string {
	out := map[string]string{}
	// Evaluate latest state and copy its direct dependency fingerprints.
	if len(st.Evaluations) > 0 {
		latest := st.Evaluations[len(st.Evaluations)-1]
		for k, v := range latest.Deps {
			out[k] = v
		}
	}
	return out
}

func ProjectForView(st *ProjectState) *ProjectState {
	applyDecisions(st)
	if len(st.Evaluations) > 1 {
		latest := st.Evaluations[len(st.Evaluations)-1]
		cur := map[string]string{"sources": latest.SourceFingerprint, "parameters": latest.ParameterHash}
		for i := range st.Evaluations[:len(st.Evaluations)-1] {
			MarkStale(&st.Evaluations[i], cur, "a later direct source or parameter revision changed this dependency")
		}
	}
	return st
}
