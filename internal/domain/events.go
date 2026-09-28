package domain

import "encoding/json"

const (
	EventProjectCreated         = "project.created"
	EventSourceCommitted        = "source.committed"
	EventInitialMassRevised     = "initial_mass.revised"
	EventParameterRevised       = "parameter.revised"
	EventResultGenerated        = "result.generated"
	EventResultMarkedStale      = "result.stale"
	EventDecisionRecorded       = "decision.recorded"
	EventDecisionReverted       = "decision.reverted"
	EventConflictDetected       = "conflict.detected"
	EventConflictResolved       = "conflict.resolved"
	EventCounterexampleRecorded = "counterexample.recorded"
)

type ProjectCreatedPayload struct {
	Name string `json:"name"`
}

type SourceCommittedPayload struct {
	Batch        ImportBatch         `json:"batch"`
	Kind         string              `json:"kind"`
	RawFragment  string              `json:"raw_fragment"`
	Measurements []MeasurementSample `json:"measurements,omitempty"`
	Drift        []DriftCorrection   `json:"drift,omitempty"`
	Furnace      []FurnaceSegment    `json:"furnace,omitempty"`
	Valves       []GasValveEvent     `json:"valves,omitempty"`
	Blank        []BlankPoint        `json:"blank,omitempty"`
}

type InitialMassRevisedPayload struct {
	InitialMass InitialMass `json:"initial_mass"`
}

type ParameterRevisedPayload struct {
	Parameters ParameterSet `json:"parameters"`
}

type ResultGeneratedPayload struct {
	Result AnalysisResult `json:"result"`
}

type ResultStalePayload struct {
	ResultID    string      `json:"result_id"`
	ChangedRefs []SourceRef `json:"changed_refs"`
	AtSeq       int64       `json:"at_seq"`
}

type DecisionRecordedPayload struct {
	Decision Decision `json:"decision"`
}

type DecisionRevertedPayload struct {
	DecisionID string `json:"decision_id"`
	ByEventID  string `json:"by_event_id"`
	Reason     string `json:"reason"`
}

type ConflictPayload struct {
	Conflict Conflict `json:"conflict"`
}

type ConflictResolvedPayload struct {
	ConflictID        string `json:"conflict_id"`
	Resolution        string `json:"resolution"`
	WinningDecisionID string `json:"winning_decision_id"`
	Reason            string `json:"reason"`
}

type CounterexamplePayload struct {
	Counterexample Counterexample `json:"counterexample"`
}

func DecodePayload[T any](event Event) (T, error) {
	var payload T
	err := json.Unmarshal(event.Payload, &payload)
	return payload, err
}
