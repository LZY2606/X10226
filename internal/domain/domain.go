package domain

import "encoding/json"

const AlgorithmVersion = "tga-stage-v1"

type Value struct {
	Text            string   `json:"text"`
	Number          *float64 `json:"number,omitempty"`
	Unit            string   `json:"unit,omitempty"`
	CoordinateBasis string   `json:"coordinate_basis,omitempty"`
	Diagnostics     []string `json:"diagnostics,omitempty"`
}

type RawPoint struct {
	Elapsed     Value  `json:"elapsed"`
	Temperature Value  `json:"temperature"`
	Mass        Value  `json:"mass"`
	Quality     string `json:"quality,omitempty"`
}

type BlankPoint struct {
	Elapsed     Value `json:"elapsed"`
	Temperature Value `json:"temperature"`
	Mass        Value `json:"mass"`
}

type DriftCorrection struct {
	ID            string `json:"id"`
	InterceptMg   Value  `json:"intercept_mg"`
	SlopeMgPerSec Value  `json:"slope_mg_per_sec"`
	Basis         string `json:"basis"`
}

type FurnaceSegment struct {
	ID          string `json:"id"`
	StartSec    Value  `json:"start_sec"`
	EndSec      Value  `json:"end_sec"`
	StartTempC  Value  `json:"start_temp_c"`
	TargetTempC Value  `json:"target_temp_c"`
	RateCPerMin Value  `json:"rate_c_per_min"`
}

type GasValveEvent struct {
	ID                  string `json:"id"`
	ValveSec            Value  `json:"valve_sec"`
	FromGas             string `json:"from_gas"`
	ToGas               string `json:"to_gas"`
	DelaySec            Value  `json:"delay_sec"`
	DelayUncertaintySec Value  `json:"delay_uncertainty_sec"`
	TemperatureLagC     Value  `json:"temperature_lag_c"`
}

type InitialMass struct {
	ID     string `json:"id"`
	MassMg Value  `json:"mass_mg"`
	Method string `json:"method"`
}

type AnalysisParameters struct {
	ID                    string  `json:"id"`
	DtgThresholdMgPerMin  float64 `json:"dtg_threshold_mg_per_min"`
	NoiseThresholdMg      float64 `json:"noise_threshold_mg"`
	PeakPersistencePoints int     `json:"peak_persistence_points"`
	GapThresholdSec       float64 `json:"gap_threshold_sec"`
	OverlapValleyRatio    float64 `json:"overlap_valley_ratio"`
}

type SourceInput struct {
	ID             string            `json:"id"`
	ProjectID      string            `json:"project_id"`
	Layer          string            `json:"layer"`
	Name           string            `json:"name"`
	Version        int               `json:"version"`
	ParentID       string            `json:"parent_id,omitempty"`
	Status         string            `json:"status"`
	OriginalText   string            `json:"original_text"`
	Payload        json.RawMessage   `json:"payload"`
	Units          map[string]string `json:"units"`
	Diagnostics    []string          `json:"diagnostics,omitempty"`
	ContentHash    string            `json:"content_hash"`
	CreatedEventID string            `json:"created_event_id"`
	CreatedAt      string            `json:"created_at"`
}

type Node struct {
	ID                   string          `json:"id"`
	ProjectID            string          `json:"project_id"`
	RunID                string          `json:"run_id"`
	Kind                 string          `json:"kind"`
	StableKey            string          `json:"stable_key"`
	Version              int             `json:"version"`
	Status               string          `json:"status"`
	Title                string          `json:"title"`
	AlgorithmVersion     string          `json:"algorithm_version"`
	ParameterFingerprint string          `json:"parameter_fingerprint"`
	DirectSources        []string        `json:"direct_sources"`
	Evidence             []Evidence      `json:"evidence"`
	Payload              json.RawMessage `json:"payload"`
	ContentHash          string          `json:"content_hash"`
	CreatedEventID       string          `json:"created_event_id"`
	CreatedAt            string          `json:"created_at"`
}

type Evidence struct {
	Label         string `json:"label"`
	Value         Value  `json:"value"`
	SourceNodeID  string `json:"source_node_id,omitempty"`
	SourceInputID string `json:"source_input_id,omitempty"`
	Rule          string `json:"rule,omitempty"`
}

type Edge struct {
	ProjectID  string `json:"project_id"`
	RunID      string `json:"run_id"`
	FromNodeID string `json:"from_node_id"`
	ToNodeID   string `json:"to_node_id"`
	Relation   string `json:"relation"`
}

type Event struct {
	ID            string          `json:"id"`
	ProjectID     string          `json:"project_id"`
	Seq           int64           `json:"seq"`
	Type          string          `json:"type"`
	Actor         string          `json:"actor"`
	Summary       string          `json:"summary"`
	ParentEventID string          `json:"parent_event_id,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
	CreatedAt     string          `json:"created_at"`
}

type DecisionState struct {
	CandidateID       string `json:"candidate_id"`
	Action            string `json:"action"`
	Actor             string `json:"actor"`
	EventID           string `json:"event_id"`
	Rationale         string `json:"rationale"`
	At                string `json:"at"`
	Conflict          bool   `json:"conflict"`
	SupersededEventID string `json:"superseded_event_id,omitempty"`
}

type Run struct {
	ID                   string `json:"id"`
	ProjectID            string `json:"project_id"`
	Seq                  int    `json:"seq"`
	TriggerEventID       string `json:"trigger_event_id"`
	AlgorithmVersion     string `json:"algorithm_version"`
	ParameterFingerprint string `json:"parameter_fingerprint"`
	CreatedAt            string `json:"created_at"`
}

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}
