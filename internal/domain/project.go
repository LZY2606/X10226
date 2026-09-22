package domain

import "time"

type Project struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Labels      map[string]string `json:"labels,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// DecisionRecord is a human verdict on a candidate/stage/residue.
type DecisionRecord struct {
	ID         string `json:"id"`
	EventID    string `json:"event_id"`
	Seq        int64  `json:"seq"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Action     string `json:"action"` // accept | reject | adjust
	ParamsID   string `json:"params_id,omitempty"`
	Reason     string `json:"reason"`
	At         string `json:"at"`
	Actor      string `json:"actor"`
	Undone     bool   `json:"undone"`
}

// ConflictRecord ties contradictory decisions together until resolved.
type ConflictRecord struct {
	ID             string           `json:"id"`
	EventID        string           `json:"event_id"`
	TargetID       string           `json:"target_id"`
	DecisionIDs    []string         `json:"decision_ids"`
	Reason         string           `json:"reason"`
	Resolved       bool             `json:"resolved"`
	WinnerDecision string           `json:"winner_decision_id,omitempty"`
	ResolutionNote string           `json:"resolution_note,omitempty"`
}

// ImportSession tracks the import state machine.
type ImportSession struct {
	ID        string       `json:"id"`
	Status    ImportStatus `json:"status"`
	Layers    []string     `json:"layers"`
	LayersOK  []string     `json:"layers_ok"`
	LayersBad []string     `json:"layers_bad"`
	BeganAt   string       `json:"began_at"`
	EndedAt   string       `json:"ended_at,omitempty"`
	Summary   string       `json:"summary,omitempty"`
}
