package pipeline

import (
	"fmt"
	"hash/fnv"

	"tga/internal/domain"
)

// Analysis is the full, content-addressed result of running the pipeline over
// one committed source version with one parameter version.
type Analysis struct {
	AnalysisID       string              `json:"analysis_id"`
	SourceVersion    int                 `json:"source_version"`
	ParamsVersion    int                 `json:"params_version"`
	AlgoVersion      string              `json:"algo_version"`
	ParamFingerprint string              `json:"param_fingerprint"`
	InitialMassMG    *float64            `json:"initial_mass_mg"`
	InitialMassRef   string              `json:"initial_mass_ref"`
	Segments         []Segment           `json:"segments"`
	Gaps             []Gap               `json:"gaps"`
	Points           []AnalyzedPoint     `json:"points"`
	PeakGroups       []PeakGroup         `json:"peak_groups"`
	Candidates       []StageCandidate    `json:"candidates"`
	Residue          ResidueChain        `json:"residue"`
	Atmosphere       []AtmosphereState   `json:"atmosphere"`
	Findings         []domain.Finding    `json:"findings"`
}

// Segment is one contiguous run of samples; gaps split segments and are never bridged.
type Segment struct {
	ID        string  `json:"id"`
	Index     int     `json:"index"`
	StartS    float64 `json:"start_s"`
	EndS      float64 `json:"end_s"`
	PointFrom int     `json:"point_from"`
	PointTo   int     `json:"point_to"`
}

type Gap struct {
	ID      string  `json:"id"`
	StartS  float64 `json:"start_s"`
	EndS    float64 `json:"end_s"`
	WidthS  float64 `json:"width_s"`
}

// AnalyzedPoint is the corrected, derived view of one raw sample point.
type AnalyzedPoint struct {
	ID          string  `json:"id"`
	RawIndex    int     `json:"raw_index"`
	Segment     int     `json:"segment"`
	TimeS       float64 `json:"time_s"`
	TempC       float64 `json:"temp_c"`
	RawMassMG   float64 `json:"raw_mass_mg"`
	DriftMG     float64 `json:"drift_mg"`
	BlankMG     float64 `json:"blank_mg"`
	CorrMassMG  float64 `json:"corr_mass_mg"`
	DTGMGPM     float64 `json:"dtg_mgpm"`
	Uptick      bool    `json:"uptick"`
}

// PeakGroup groups one or several overlapping (shoulder) DTG minima.
type PeakGroup struct {
	ID             string       `json:"id"`
	Segment        int          `json:"segment"`
	MergedStartS   float64      `json:"merged_start_s"`
	MergedEndS     float64      `json:"merged_end_s"`
	MergedApexS    float64      `json:"merged_apex_s"`
	MergedApexDTG  float64      `json:"merged_apex_dtg"`
	SubPeaks       []SubPeak    `json:"sub_peaks"`
	TouchesGap     bool         `json:"touches_gap"`
}

type SubPeak struct {
	ID        string  `json:"id"`
	ApexS     float64 `json:"apex_s"`
	ApexDTG   float64 `json:"apex_dtg"`
	StartS    float64 `json:"start_s"`
	EndS      float64 `json:"end_s"`
	IsShoulder bool   `json:"is_shoulder"`
}

// StageCandidate is one legal decomposition of the run into stages. Overlapping
// peaks make the decomposition non-unique, so every legal candidate is retained.
type StageCandidate struct {
	ID             string  `json:"id"`
	Label          string  `json:"label"`
	Stages         []Stage `json:"stages"`
	TotalLossPct   float64 `json:"total_loss_pct"`
	TotalKnown     bool    `json:"total_known"`
	Status         string  `json:"status"`
	RejectReason   string  `json:"reject_reason,omitempty"`
}

type Stage struct {
	ID            string  `json:"id"`
	GroupID       string  `json:"group_id"`
	Label         string  `json:"label"`
	StartS        float64 `json:"start_s"`
	EndS          float64 `json:"end_s"`
	ApexS         float64 `json:"apex_s"`
	StartMassMG   float64 `json:"start_mass_mg"`
	EndMassMG     float64 `json:"end_mass_mg"`
	MassLossMG    float64 `json:"mass_loss_mg"`
	MassLossPct   float64 `json:"mass_loss_pct"`
	Known         bool    `json:"known"`
	Overlaps      bool    `json:"overlaps"`
	GasAtValve    string  `json:"gas_at_valve"`
	GasAtReaction string  `json:"gas_at_reaction"`
	BoundaryNote  string  `json:"boundary_note,omitempty"`
}

// ResidueChain is the expandable calculation chain behind the final residue %.
type ResidueChain struct {
	ID             string     `json:"id"`
	Known          bool       `json:"known"`
	FinalMassMG    float64    `json:"final_mass_mg"`
	FinalWindowFrom float64   `json:"final_window_from_s"`
	FinalWindowTo   float64   `json:"final_window_to_s"`
	InitialMassMG  float64    `json:"initial_mass_mg"`
	ResiduePct     float64    `json:"residue_pct"`
	Nodes          []CalcNode `json:"nodes"`
	UnknownReason  string     `json:"unknown_reason,omitempty"`
}

// CalcNode is one row in an expandable calculation chain. Every displayed value
// gets its own stable identity via NodeID.
type CalcNode struct {
	NodeID  string   `json:"node_id"`
	Label   string   `json:"label"`
	Value   *float64 `json:"value,omitempty"`
	Unit    string   `json:"unit,omitempty"`
	Formula string   `json:"formula,omitempty"`
	Sources []string `json:"sources,omitempty"`
	Diag    string   `json:"diag,omitempty"`
	Unknown bool     `json:"unknown,omitempty"`
}

type AtmosphereState struct {
	ValveTimeS   float64 `json:"valve_time_s"`
	ReactionTimeS float64 `json:"reaction_time_s"`
	FromGas      string  `json:"from_gas"`
	ToGas        string  `json:"to_gas"`
}

func hnum(prefix string, parts ...any) string {
	h := fnv.New64a()
	fmt.Fprint(h, parts...)
	return fmt.Sprintf("%s_%016x", prefix, h.Sum64())
}

// DefaultParams returns the v1 algorithm default parameters.
func DefaultParams() domain.AnalysisParams {
	return domain.AnalysisParams{
		GapThresholdS:      60,
		GasDelayS:          18,
		TempLagS:           12,
		DTGWindowS:         0, // 0 = nearest-neighbour central difference
		PeakProminenceMGPM: 0.05,
		NoiseThresholdMG:   0.02,
		ShoulderDepthMGPM:  0.02,
		BlankSubtract:      true,
	}
}
