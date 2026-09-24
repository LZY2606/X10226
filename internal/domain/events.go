package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type EventType string

const (
	EvProjectCreated EventType = "project_created"
	EvLayersCommitted EventType = "layers_committed" // initial or half-failed multi-layer import
	EvLayerRevised    EventType = "layer_revised"     // one direct source changed (new version)
	EvParamsRevised   EventType = "params_revised"    // parameter revision
	EvDecision        EventType = "decision"          // accept / reject / param adjust on a target
	EvConflictResolved EventType = "conflict_resolved"
	EvUndo            EventType = "undo"              // logical rollback to an event id
	EvProjectNote     EventType = "project_note"
)

// Event is the only thing persisted. Everything else is derived.
type Event struct {
	Seq      int             `json:"seq"`
	ID       string          `json:"id"`
	ParentID string          `json:"parent_id"`
	Type     EventType       `json:"type"`
	At       string          `json:"at"` // virtual time, RFC3339 UTC
	Actor    string          `json:"actor"`
	Payload  json.RawMessage `json:"payload"`
	Hash     string          `json:"hash"` // sha256 over canonical(prev hash + fields+payload)
}

// HashEvent computes the tamper-evident chain hash.
func HashEvent(prevHash string, seq int, id, parentID string, t EventType, at, actor string, payload []byte) string {
	canon, _ := json.Marshal(struct {
		Prev     string          `json:"prev"`
		Seq      int             `json:"seq"`
		ID       string          `json:"id"`
		ParentID string          `json:"parent_id"`
		Type     EventType       `json:"type"`
		At       string          `json:"at"`
		Actor    string          `json:"actor"`
		Payload  json.RawMessage `json:"payload"`
	}{prevHash, seq, id, parentID, t, at, actor, payload})
	sum := sha256.Sum256(canon)
	return hex.EncodeToString(sum[:])
}

// ----- payloads -----

type LayersCommitted struct {
	BatchID      string        `json:"batch_id"`
	Layers       []*RawLayer   `json:"layers"`        // committed (accepted) layers
	FailedLayers []FailedLayer `json:"failed_layers"` // layers rejected in this batch (half failure)
	Diagnostics  []Diagnostic  `json:"diagnostics"`
	Source       string        `json:"source,omitempty"`
}

type FailedLayer struct {
	Layer Layer  `json:"layer"`
	Code  string `json:"code"`
	Error string `json:"error"`
	Raw   string `json:"raw,omitempty"` // the rejected fragment is preserved
}

type LayerRevised struct {
	BatchID     string       `json:"batch_id"`
	Layer       *RawLayer    `json:"layer"` // version carries parent link
	Diagnostics []Diagnostic `json:"diagnostics"`
	Reason      string       `json:"reason,omitempty"`
}

type ParamsRevised struct {
	ParamsV2 Params `json:"params"` // compute package defines Params; mirrored struct
	Parent   int    `json:"parent_params_version"`
	Reason   string `json:"reason,omitempty"`
}

// Params is declared here to avoid an import cycle (compute imports domain).
type Params struct {
	Version        int     `json:"version"`
	AtmDelayS      float64 `json:"atm_delay_s"`       // atmosphere delay, seconds
	TempLagS       float64 `json:"temp_lag_s"`        // thermal lag, seconds
	SmoothWindowS  float64 `json:"smooth_window_s"`   // DTG smoothing window, seconds
	PeakPromMGMin  float64 `json:"peak_prom_mg_min"`  // minimum DTG prominence
	MinStageLossMG float64 `json:"min_stage_loss_mg"` // minimum stage mass loss
	GapThresholdS  float64 `json:"gap_threshold_s"`   // >= this spacing is a sampling gap
	DriftEnabled   bool    `json:"drift_enabled"`
	BlankEnabled   bool    `json:"blank_enabled"`
}

type DecisionPayload struct {
	TargetType  string  `json:"target_type"` // candidate | residue
	TargetID    string  `json:"target_id"`
	TargetKey   string  `json:"target_key"`
	Action      string  `json:"action"` // accept | reject | adjust
	Reason      string  `json:"reason"`
	ConflictsWith string `json:"conflicts_with,omitempty"`
	Adjust      *Params `json:"adjust,omitempty"` // when action=adjust
	ResolutionOf string `json:"resolution_of,omitempty"`
}

type ConflictResolvedPayload struct {
	ConflictID string `json:"conflict_id"`
	Winner     string `json:"winner_decision_id"`
	Loser      string `json:"loser_decision_id"`
	Reason     string `json:"reason"`
}

type UndoPayload struct {
	RollbackToSeq int    `json:"rollback_to_seq"`
	RollbackToID  string `json:"rollback_to_id"`
	Reason        string `json:"reason"`
}

type ProjectCreated struct {
	Name     string `json:"name"`
	Material string `json:"material"`
	Basis    string `json:"basis"`
}
