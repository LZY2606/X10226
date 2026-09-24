package compute

import (
	"fmt"

	"github.com/thermal/tga/internal/domain"
)

type ArtifactKind string

const (
	KindSeries   ArtifactKind = "series"
	KindDTG      ArtifactKind = "dtg"
	KindCandidate ArtifactKind = "stage_candidate"
	KindResidue  ArtifactKind = "residue"
)

// Provenance is attached to every derived value.
type Provenance struct {
	ArtifactID   string   `json:"artifact_id"`
	Algorithm    string   `json:"algorithm_version"`
	ParamVersion int      `json:"params_version"`
	ParamFP      string   `json:"params_fingerprint"`
	SourceLayers []string `json:"source_layers"`
	SourceVers   map[string]int `json:"source_layer_versions"`
	InputHashes  map[string]string `json:"input_hashes"`
	DependsOn    []string `json:"depends_on"`
}

type ErrorState struct {
	Present bool     `json:"present"`
	Codes   []string `json:"codes,omitempty"`
	Message string   `json:"message,omitempty"`
}

type ArtifactBase struct {
	Kind      ArtifactKind `json:"kind"`
	ID        string       `json:"id"`
	Status    string       `json:"status"` // current | error
	Prov      Provenance   `json:"provenance"`
	Error     *ErrorState  `json:"error,omitempty"`
	Stale     bool         `json:"stale,omitempty"` // set by projection, not stored
}

// SeriesPoint carries every component of the correction chain explicitly.
type SeriesPoint struct {
	Index         int     `json:"index"`
	TimeS         float64 `json:"t_s"`
	TempC         float64 `json:"temp_c"`
	RawMG         float64 `json:"raw_mg"`
	DriftMG       float64 `json:"drift_mg"`
	BlankMG       float64 `json:"blank_mg"`
	CorrectedMG   float64 `json:"corrected_mg"`
	HasGapAfter   bool    `json:"has_gap_after"`
	NonMonotonic  bool    `json:"non_monotonic,omitempty"`
	UpwardBlip    bool    `json:"upward_blip,omitempty"`
	ExcludedDiag  string  `json:"excluded_diag,omitempty"`
}

type SeriesArtifact struct {
	ArtifactBase
	Points    []SeriesPoint `json:"points"`
	SplitAtGaps []int       `json:"split_at_gaps"` // point indices after which a gap starts
}

type DTGPoint struct {
	Index     int     `json:"index"`
	TimeS     float64 `json:"t_s"`
	DTGMGMin  float64 `json:"dtg_mg_min"`
	Smoothed  float64 `json:"smoothed_mg_min"`
}

type DTGArtifact struct {
	ArtifactBase
	Points     []DTGPoint `json:"points"`
	Segments   [][2]int   `json:"segments"` // contiguous [lo,hi] index ranges (never cross gaps)
}

// Candidate is a stage decomposition candidate. Candidates are first-class:
// legal alternatives and rejected possibilities are all retained.
type Candidate struct {
	ArtifactBase
	Key             string   `json:"key"`
	Label           string   `json:"label"`
	StartS          float64  `json:"start_s"`
	EndS            float64  `json:"end_s"`
	PeakS           float64  `json:"peak_s"`
	ValveTimeS      float64  `json:"valve_time_s"`
	ReactionStartS  float64  `json:"reaction_start_s"`
	GasAtReaction   string   `json:"gas_at_reaction"`
	StartTempC      float64  `json:"start_temp_c"`
	PeakTempC       float64  `json:"peak_temp_c"`
	EndTempC        float64  `json:"end_temp_c"`
	MassLossMG      float64  `json:"mass_loss_mg"`
	MassLossPct     *float64 `json:"mass_loss_pct"`
	OverlapsWith    []string `json:"overlaps_with"`
	Accepted        bool     `json:"accepted"`
	Rejected        bool     `json:"rejected"`
	RejectCode      string   `json:"reject_code,omitempty"`
	RejectReason    string   `json:"reject_reason,omitempty"`
	AlternativeFor  string   `json:"alternative_for,omitempty"`
	CrossesGap      bool     `json:"crosses_gap,omitempty"`
	ManualState     string   `json:"manual_state,omitempty"` // accepted|rejected|adjusted|""
	Assertions      []Assertion `json:"assertions"`
}

type ResidueChainNode struct {
	Step   string  `json:"step"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Detail string  `json:"detail"`
}

type ResidueArtifact struct {
	ArtifactBase
	FinalS          float64            `json:"final_s"`
	FinalTempC      float64            `json:"final_temp_c"`
	FinalMassMG     float64            `json:"final_mass_mg"`
	InitialMassMG   float64            `json:"initial_mass_mg"`
	ResiduePct      *float64           `json:"residue_pct"`
	Basis           string             `json:"basis"` // loaded
	Chain           []ResidueChainNode `json:"chain"`
	Assertions      []Assertion        `json:"assertions"`
}

// Assertion is a machine-readable statement of the judgement basis.
type Assertion struct {
	Code  string                 `json:"code"`
	Text  string                 `json:"text"`
	Attrs map[string]interface{} `json:"attrs,omitempty"`
}

// Set bundles the artifacts derived from one source/parameter configuration.
type Set struct {
	Series    *SeriesArtifact
	DTG       *DTGArtifact
	Candidates []*Candidate
	Residue   *ResidueArtifact
}

func (s Set) All() []Artifact {
	out := []Artifact{}
	if s.Series != nil {
		out = append(out, s.Series)
	}
	if s.DTG != nil {
		out = append(out, s.DTG)
	}
	for _, c := range s.Candidates {
		out = append(out, c)
	}
	if s.Residue != nil {
		out = append(out, s.Residue)
	}
	return out
}

type Artifact interface {
	base() *ArtifactBase
}

func (a *SeriesArtifact) base() *ArtifactBase  { return &a.ArtifactBase }
func (a *DTGArtifact) base() *ArtifactBase     { return &a.ArtifactBase }
func (a *Candidate) base() *ArtifactBase       { return &a.ArtifactBase }
func (a *ResidueArtifact) base() *ArtifactBase { return &a.ArtifactBase }

func errArtifact(kind ArtifactKind, id string, prov Provenance, codes []string, msg string) ArtifactBase {
	return ArtifactBase{Kind: kind, ID: id, Status: "error", Prov: prov,
		Error: &ErrorState{Present: true, Codes: codes, Message: msg}}
}

func pct(v float64) *float64 { return &v }

func stableID(kind ArtifactKind, prov Provenance, key string) string {
	id := fmt.Sprintf("%s:%s:p%d", kind, key, prov.ParamVersion)
	return id
}
