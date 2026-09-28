package domain

import "encoding/json"

type Quantity struct {
	Raw              string   `json:"raw"`
	Value            *float64 `json:"value,omitempty"`
	Unit             string   `json:"unit,omitempty"`
	CoordinateBasis  string   `json:"coordinate_basis,omitempty"`
	Diagnostics      []string `json:"diagnostics,omitempty"`
	Unconvertible    bool     `json:"unconvertible,omitempty"`
	Original         json.RawMessage `json:"original,omitempty"`
}

type RawMassPoint struct {
	Seq         int      `json:"seq"`
	Time        Quantity `json:"time"`
	FurnaceTime Quantity `json:"furnace_time"`
	Temperature Quantity `json:"temperature"`
	Mass        Quantity `json:"mass"`
}

type BlankPoint struct {
	Seq         int      `json:"seq"`
	Time        Quantity `json:"time"`
	FurnaceTime Quantity `json:"furnace_time"`
	BlankMass   Quantity `json:"blank_mass"`
}

type BalanceDrift struct {
	Slope         Quantity `json:"slope"`
	ReferenceTime Quantity `json:"reference_time"`
	Method        string   `json:"method"`
}

type FurnaceSegment struct {
	Index     int      `json:"index"`
	Name      string   `json:"name"`
	StartTime Quantity `json:"start_time"`
	EndTime   Quantity `json:"end_time"`
	StartTemp Quantity `json:"start_temp"`
	EndTemp   Quantity `json:"end_temp"`
	Rate      Quantity `json:"rate"`
}

type GasEvent struct {
	Index           int      `json:"index"`
	ValveAt         Quantity `json:"valve_at"`
	ReactionAt      Quantity `json:"reaction_at,omitempty"`
	FromGas         string   `json:"from_gas"`
	ToGas           string   `json:"to_gas"`
	AtmosphereDelay Quantity `json:"atmosphere_delay"`
	ThermalLag      Quantity `json:"thermal_lag"`
	Note            string   `json:"note,omitempty"`
}

type InitialMass struct {
	Mass           Quantity `json:"mass"`
	Method         string   `json:"method"`
	RevisionReason string   `json:"revision_reason,omitempty"`
}

type SourceBundle struct {
	Schema        string                     `json:"schema"`
	CasepackID    string                     `json:"casepack_id"`
	CasepackName  string                     `json:"casepack_name"`
	DirectOrigin  string                     `json:"direct_origin"`
	ProjectID     string                     `json:"project_id,omitempty"`
	ProjectName   string                     `json:"project_name,omitempty"`
	Sources       []SourcePayload            `json:"sources"`
	MachineEvidence map[string]any            `json:"machine_evidence,omitempty"`
}

type SourcePayload struct {
	Kind       string          `json:"kind"`
	Label      string          `json:"label"`
	ParentKind string          `json:"parent_kind,omitempty"`
	Revision   int             `json:"revision"`
	Payload    json.RawMessage `json:"payload"`
}

const (
	KindMassPoints    = "mass_points"
	KindBalanceDrift  = "balance_drift"
	KindFurnace       = "furnace_program"
	KindGasEvents     = "gas_events"
	KindBlankCurve    = "blank_curve"
	KindInitialMass   = "initial_mass"
)

type Fragment struct {
	ID        string          `json:"id"`
	SourceID  string          `json:"source_id"`
	Kind      string          `json:"kind"`
	Position  int             `json:"position"`
	State     string          `json:"state"`
	Raw       json.RawMessage `json:"raw"`
	Error     string          `json:"error,omitempty"`
	Diagnostics []string      `json:"diagnostics,omitempty"`
}

type Source struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"project_id"`
	Kind         string          `json:"kind"`
	Label        string          `json:"label"`
	Revision     int             `json:"revision"`
	ParentID     string          `json:"parent_id,omitempty"`
	State        string          `json:"state"`
	DirectOrigin string          `json:"direct_origin"`
	CasepackID   string          `json:"casepack_id,omitempty"`
	RawJSON      json.RawMessage `json:"raw_json"`
	Fingerprint  string          `json:"fingerprint"`
	Diagnostics  []string        `json:"diagnostics,omitempty"`
	CreatedAt    string          `json:"created_at"`
	Fragments    []Fragment      `json:"fragments,omitempty"`
}

type Event struct {
	ID        string                 `json:"id"`
	ProjectID string                 `json:"project_id"`
	Seq       int                    `json:"seq"`
	Type      string                 `json:"type"`
	Actor     string                 `json:"actor"`
	Summary   string                 `json:"summary"`
	At        string                 `json:"at"`
	SourceID  string                 `json:"source_id,omitempty"`
	RunID     string                 `json:"run_id,omitempty"`
	Payload   map[string]any         `json:"payload,omitempty"`
}

type Parameters struct {
	AlgorithmVersion string  `json:"algorithm_version"`
	AtmosphereDelay  float64 `json:"atmosphere_delay_min"`
	ThermalLag       float64 `json:"thermal_lag_min"`
	DTGThreshold     float64 `json:"dtg_threshold_mg_per_min"`
	PeakSeparation   float64 `json:"peak_separation_min"`
	GapLimitMin      float64 `json:"gap_limit_min"`
	Revision         int     `json:"revision"`
}

func DefaultParameters() Parameters {
	return Parameters{
		AlgorithmVersion: "tga-stage-decomposition@2026-09-28",
		AtmosphereDelay:  1.5,
		ThermalLag:       2.0,
		DTGThreshold:     0.05,
		PeakSeparation:   2.5,
		GapLimitMin:      1.5,
	}
}

type Decision struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"project_id"`
	CandidateID  string          `json:"candidate_id"`
	Action       string          `json:"action"`
	Actor        string          `json:"actor"`
	Rationale    string          `json:"rationale"`
	Adjustment   json.RawMessage `json:"adjustment,omitempty"`
	SupersedesID string          `json:"supersedes_id,omitempty"`
	CreatedAt    string          `json:"created_at"`
	UndoneAt     string          `json:"undone_at,omitempty"`
	ConflictWith string          `json:"conflict_with,omitempty"`
	State        string          `json:"state"`
}

type ConflictResolution struct {
	ID               string `json:"id"`
	ProjectID        string `json:"project_id"`
	WinningDecisionID string `json:"winning_decision_id"`
	LosingDecisionID string `json:"losing_decision_id"`
	Actor            string `json:"actor"`
	Reason           string `json:"reason"`
	CreatedAt        string `json:"created_at"`
	UndoneAt         string `json:"undone_at,omitempty"`
}
