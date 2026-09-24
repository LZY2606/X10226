package core

import "encoding/json"

const (
	EventProjectCreated = "project.created"
	EventImportReceived = "source.import_received"
	EventDecision       = "review.decision"
	EventUndo           = "review.undo"
)

func Replay(events []Event) State {
	state := State{ProjectID: "local-project", Parameters: DefaultParameters(), Current: true}
	previousImportSeq := 0
	for _, event := range events {
		state.Events = append(state.Events, event)
		state.Version = event.Seq
		state.ProjectID = event.ProjectID
		switch event.Type {
		case EventProjectCreated:
			var payload struct {
				ProjectID string `json:"project_id"`
			}
			_ = json.Unmarshal(event.Payload, &payload)
			state.ProjectID = payload.ProjectID
		case EventImportReceived:
			var payload importPayload
			_ = json.Unmarshal(event.Payload, &payload)
			state.Imports = append(state.Imports, payload.RawImport)
			state.Sources = append(state.Sources, payload.Records...)
			for _, rec := range payload.Records {
				applyRecord(&state, rec)
			}
			state.Diagnostics = append(state.Diagnostics, payload.RawImport.Diagnostics...)
			if payload.Parameters != nil {
				state.Parameters = *payload.Parameters
			}
			state.Plot, state.ValveViews = BuildSeries(state)
			previousImportSeq = event.Seq
			state.Candidates = RefreshCandidates(state.Candidates, DetectCandidates(state, previousImportSeq))
		case EventDecision:
			var decision Decision
			_ = json.Unmarshal(event.Payload, &decision)
			if decision.Supersedes != "" {
				for i := range state.Decisions {
					if state.Decisions[i].ID == decision.Supersedes {
						state.Decisions[i].Undone = true
					}
				}
			}
			state.Decisions = append(state.Decisions, decision)
		case EventUndo:
			var payload struct {
				DecisionID string `json:"decision_id"`
				Reason     string `json:"reason"`
			}
			_ = json.Unmarshal(event.Payload, &payload)
			for i := range state.Decisions {
				if state.Decisions[i].ID == payload.DecisionID {
					state.Decisions[i].Undone = true
					state.Decisions[i].UndoneReason = payload.Reason
				}
			}
		}
		state.Decisions = ApplyDecisionViews(state.Decisions, state.Candidates)
	}
	return state
}

type importPayload struct {
	RawImport
	Records    []SourceRecord `json:"records"`
	Parameters *Parameters    `json:"parameters"`
}

func applyRecord(state *State, rec SourceRecord) {
	switch rec.Kind {
	case KindSamples:
		var values []SamplePoint
		_ = json.Unmarshal(rec.Data, &values)
		state.Samples = values
	case KindDrift:
		var values []DriftCorrection
		_ = json.Unmarshal(rec.Data, &values)
		state.Drifts = appendVersion(state.Drifts, values)
	case KindFurnace:
		var values []FurnacePoint
		_ = json.Unmarshal(rec.Data, &values)
		state.Furnace = values
	case KindValves:
		var values []GasValveEvent
		_ = json.Unmarshal(rec.Data, &values)
		state.Valves = append(state.Valves, values...)
	case KindBlank:
		var values []BlankPoint
		_ = json.Unmarshal(rec.Data, &values)
		state.Blank = values
	case KindInitialMass:
		var values []InitialMass
		_ = json.Unmarshal(rec.Data, &values)
		state.InitialMasses = appendVersion(state.InitialMasses, values)
	}
}

func appendVersion[T any](old []T, add []T) []T {
	return append(old, add...)
}

func RefreshCandidates(previous, next []StageCandidate) []StageCandidate {
	nextByLogical := map[string]StageCandidate{}
	for _, candidate := range next {
		nextByLogical[candidate.ID] = candidate
	}
	for i := range previous {
		if _, alive := nextByLogical[previous[i].ID]; alive {
			previous[i].Current = false
			if previous[i].Status == "current" || previous[i].Status == "" {
				previous[i].Status = "stale"
			}
			if len(previous[i].RejectionCodes) == 0 {
				previous[i].RejectionCodes = []string{"SUPERSEDED_BY_DIRECT_SOURCE_REVISION"}
			}
		}
	}
	seen := map[string]bool{}
	for _, candidate := range previous {
		seen[candidate.RevisionID] = true
	}
	for _, candidate := range next {
		if !seen[candidate.RevisionID] {
			previous = append(previous, candidate)
		}
	}
	return previous
}

func ApplyDecisionViews(decisions []Decision, candidates []StageCandidate) []Decision {
	candidateSet := map[string]bool{}
	for _, candidate := range candidates {
		candidateSet[candidate.ID] = true
	}
	for i := range decisions {
		decisions[i].ConflictWith = nil
		if !candidateSet[decisions[i].CandidateID] {
			decisions[i].ConflictWith = []string{"CANDIDATE_NOT_PRESENT_AT_VERSION"}
		}
	}
	return decisions
}
