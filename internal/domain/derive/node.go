// Package derive computes derived TGA results as immutable nodes. Every node
// carries its algorithm version, a parameter fingerprint and the stable ids of
// its direct sources, so any displayed value can be traced back and recomputed
// step by step. Candidates, rejection reasons, errors and unknown states are
// first-class data.
package derive

import (
	"encoding/json"

	"tgasys/internal/domain/identity"
	"tgasys/internal/domain/sources"
)

// Node kinds.
const (
	KindDriftCorrected = "drift_corrected"
	KindNormalized     = "normalized_tg"
	KindDTG            = "dtg"
	KindPeakScan       = "peak_scan"
	KindStageSplit     = "stage_split"
	KindResidue        = "residue"
	KindAtmosphere     = "atmosphere_events"
)

// Node status.
const (
	StatusOK      = "ok"
	StatusUnknown = "unknown"
	StatusError   = "error"
)

// Candidate lifecycle (human decisions move these).
const (
	CandProposed  = "proposed"
	CandAccepted  = "accepted"
	CandRejected  = "rejected"
	CandContested = "contested" // conflicting active decisions
)

// Params is the full algorithm parameter set. Fingerprinted into every node.
type Params struct {
	Drift  DriftParams  `json:"drift"`
	DTG    DTGParams    `json:"dtg"`
	Peak   PeakParams   `json:"peak"`
	Stage  StageParams  `json:"stage"`
	Atmos  AtmosParams  `json:"atmos"`
}

type DriftParams struct {
	Extrapolation string `json:"extrapolation"` // "clamp"
}

type DTGParams struct {
	SmoothWindow int `json:"smooth_window"` // odd, >=1
}

type PeakParams struct {
	MinHeightMgMin     float64 `json:"min_height_mg_min"`
	MinProminenceMgMin float64 `json:"min_prominence_mg_min"`
}

type StageParams struct {
	Boundary string `json:"boundary"` // "half_prominence"
}

type AtmosParams struct {
	TransportLagS float64 `json:"transport_lag_s"` // gas line + furnace volume delay
	TempLagS      float64 `json:"temp_lag_s"`      // sample temperature lags furnace
}

func DefaultParams() Params {
	return Params{
		Drift: DriftParams{Extrapolation: "clamp"},
		DTG:   DTGParams{SmoothWindow: 5},
		Peak:  PeakParams{MinHeightMgMin: 0.05, MinProminenceMgMin: 0.03},
		Stage: StageParams{Boundary: "half_prominence"},
		Atmos: AtmosParams{TransportLagS: 45, TempLagS: 20},
	}
}

// Fingerprint is the stable parameter hash recorded on every node.
func (p Params) Fingerprint() string { return identity.Short(p) }

// Node is one derived result. Deps are ordered stable ids of direct sources
// (submissions or other nodes).
type Node struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	AlgoVersion string         `json:"algo_version"`
	ParamsFP    string         `json:"params_fp"`
	Deps        []string       `json:"deps"`
	Status      string         `json:"status"`
	Diags       []sources.Diag `json:"diags,omitempty"`
	Payload     json.RawMessage `json:"payload"`
}

// NewNode builds a node with a deterministic id.
func NewNode(kind, algoVersion string, deps []string, p Params, payload any, status string, diags []sources.Diag) *Node {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return &Node{
		ID:          identity.NodeID(kind, algoVersion, deps, p.Fingerprint()),
		Kind:        kind,
		AlgoVersion: algoVersion,
		ParamsFP:    p.Fingerprint(),
		Deps:        deps,
		Status:      status,
		Diags:       diags,
		Payload:     raw,
	}
}

// Decode unmarshals the node payload into v.
func (n *Node) Decode(v any) error { return json.Unmarshal(n.Payload, v) }

// ---- payloads ----

// CPoint is a drift-corrected sample point (canonical units: s, mg, K).
type CPoint struct {
	T        float64 `json:"t_s"`
	Mass     float64 `json:"mass_mg"`
	Temp     float64 `json:"temp_k"`
	Usable   bool    `json:"usable"`
	GapAfter bool    `json:"gap_after,omitempty"`
}

type DriftCorrected struct {
	Points []CPoint `json:"points"`
}

// NPoint is a normalized TG point: fraction of initial mass.
type NPoint struct {
	T        float64 `json:"t_s"`
	Frac     float64 `json:"frac"`
	Temp     float64 `json:"temp_k"`
	Usable   bool    `json:"usable"`
	GapAfter bool    `json:"gap_after,omitempty"`
}

type Normalized struct {
	M0Mg   float64  `json:"m0_mg"`
	Points []NPoint `json:"points"`
}

// DPoint is one DTG sample (mg/min, negative = mass loss). Valid=false at gap
// edges and non-monotonic time steps: no interpolation across gaps.
type DPoint struct {
	T     float64 `json:"t_s"`
	Rate  float64 `json:"rate_mg_min"`
	Valid bool    `json:"valid"`
}

type DTG struct {
	Points []DPoint `json:"points"`
}

// PeakCandidate is one DTG peak. Overlapping peaks share an OverlapGroup.
type PeakCandidate struct {
	ID           string  `json:"id"`
	Rank         int     `json:"rank"`
	CenterT      float64 `json:"center_t_s"`
	CenterTempK  float64 `json:"center_temp_k"`
	HeightMgMin  float64 `json:"height_mg_min"`
	LeftT        float64 `json:"left_t_s"`
	RightT       float64 `json:"right_t_s"`
	OverlapGroup int     `json:"overlap_group"`
	Status       string  `json:"status"`
	RejectReason string  `json:"reject_reason,omitempty"`
}

type PeakScan struct {
	Peaks []PeakCandidate `json:"peaks"`
}

// StageCandidate is one proposed mass-loss stage. Stages may overlap and their
// mass losses are NOT constrained to sum to 100%. When boundaries cannot be
// uniquely determined, all legal alternatives are kept and linked.
type StageCandidate struct {
	ID            string   `json:"id"`
	Rank          int      `json:"rank"`
	StartT        float64  `json:"start_t_s"`
	EndT          float64  `json:"end_t_s"`
	MassLossFrac  float64  `json:"mass_loss_frac"`
	StartTempK    float64  `json:"start_temp_k"`
	EndTempK      float64  `json:"end_temp_k"`
	Basis         string   `json:"basis"` // evidence note
	Alternatives  []string `json:"alternatives,omitempty"`
	OverlapGroup  int      `json:"overlap_group"`
	Ambiguous     bool     `json:"ambiguous"` // boundaries not uniquely decidable
	Status        string   `json:"status"`
	RejectReason  string   `json:"reject_reason,omitempty"`
}

type StageSplit struct {
	Stages []StageCandidate `json:"stages"`
	Note   string           `json:"note"`
}

// Residue is the final residue fraction with its full computation chain.
type Residue struct {
	Frac          float64  `json:"frac"`
	FinalMassMg   float64  `json:"final_mass_mg"`
	InitialMassMg float64  `json:"initial_mass_mg"`
	Basis         string   `json:"basis"`
	Chain         []string `json:"chain"` // node ids, in order
	Unknown       bool     `json:"unknown"`
	UnknownReason string   `json:"unknown_reason,omitempty"`
}

// AtmosEvent maps a valve switch to the estimated sample-side event. Valve
// switch time != sample reaction time (transport + temperature lag).
type AtmosEvent struct {
	Index        int     `json:"index"`
	ValveT       float64 `json:"valve_t_s"`
	EffectiveT   float64 `json:"effective_t_s"`
	LagS         float64 `json:"lag_s"`
	FromGas      string  `json:"from_gas"`
	ToGas        string  `json:"to_gas"`
	FurnaceTempK float64 `json:"furnace_temp_k"`
	SampleTempK  float64 `json:"sample_temp_k"`
	Note         string  `json:"note"`
}

type Atmosphere struct {
	Events []AtmosEvent `json:"events"`
	Note   string       `json:"note"`
}
