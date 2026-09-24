// Package derive is the pure, deterministic domain computation. It projects a
// sequence of immutable events into the current review state and computes TG /
// DTG series, stage candidates (including unknown / overlapping / gap-cut
// ones) and the final residue with an expandable calculation chain.
package derive

import "tga-review/internal/domain"

const AlgoVersion = "tgav1"

// Parameters are versioned analysis settings; their fingerprint travels with
// every derived value.
type Parameters struct {
	VersionID       string  `json:"version_id"`
	ParentVersionID string  `json:"parent_version_id,omitempty"`
	GapMultiple     float64 `json:"gap_multiple"`    // gap if dt > k * median(dt)
	SmoothWindowS   float64 `json:"smooth_window_s"` // centered MA window
	ProminencePct   float64 `json:"prominence_pct"`  // min DTG prominence, %/s
	TempLagS        float64 `json:"temp_lag_s"`      // earliest reaction offset
	GasDelayS       float64 `json:"gas_delay_s"`     // latest reaction offset
	DriftModel      string  `json:"drift_model"`     // none | blank_zeroed
}

func DefaultParameters(versionID string) Parameters {
	return Parameters{
		VersionID:     versionID,
		GapMultiple:   3.0,
		SmoothWindowS: 24.0,
		ProminencePct: 0.04,
		TempLagS:      8.0,
		GasDelayS:     45.0,
		DriftModel:    "blank_zeroed",
	}
}

type LayerVersion struct {
	StableID string `json:"stable_id"`
	Version  int    `json:"version"`
	ParentID string `json:"parent_id,omitempty"`
	Reason   string `json:"reason,omitempty"`
	AtSeq    int64  `json:"at_seq"`
	SourceID string `json:"source_id,omitempty"`
}

type InitialMass struct {
	LayerVersion
	Raw    domain.RawValue `json:"raw"`
	MassMg float64         `json:"mass_mg"`
	Known  bool            `json:"known"`
}

type DriftSpec struct {
	LayerVersion
	Model string `json:"model"`
	Note  string `json:"note,omitempty"`
}

type SPoint struct {
	Index       int     `json:"index"`
	TS          float64 `json:"t_s"`
	TempC       float64 `json:"temp_c"`
	MassMg      float64 `json:"mass_mg"`
	SmoothMg    float64 `json:"smooth_mg"`
	DTGPctPerS  float64 `json:"dtg_pct_per_s"`
	DTGValid    bool    `json:"dtg_valid"`
	SegmentID   int     `json:"segment_id"`
	NonMonotone bool    `json:"non_monotone"`
}

type GasWindow struct {
	ValveID       string  `json:"valve_id"`
	ValveAtS      float64 `json:"valve_at_s"`
	ReactionStart float64 `json:"reaction_start_s"`
	ReactionEnd   float64 `json:"reaction_end_s"`
	LagS          float64 `json:"lag_s"`
	DelayS        float64 `json:"delay_s"`
	Note          string  `json:"note"`
}

type Ambiguity struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"` // overlap_split | gap_boundary | nonmonotone
	OptionIDs []string `json:"option_ids"`
	Reason    string   `json:"reason"`
}

const (
	CandStatusProposed = "proposed"
	CandStatusUnknown  = "unknown"
	CandStatusAccepted = "accepted"
	CandStatusRejected = "rejected"
	CandStatusAdjusted = "adjusted"
)

type Candidate struct {
	ID          string     `json:"id"`
	PeakS       float64    `json:"peak_s"`
	PeakC       float64    `json:"peak_c"`
	OnsetS      float64    `json:"onset_s"`
	OffsetS     float64    `json:"offset_s"`
	OnsetC      float64    `json:"onset_c"`
	OffsetC     float64    `json:"offset_c"`
	BoundsKnown bool       `json:"bounds_known"`
	LossMg      float64    `json:"loss_mg"`
	LossPct     float64    `json:"loss_pct"`
	Status      string     `json:"status"`
	Overlaps    bool       `json:"overlaps"`
	GasWindow   *GasWindow `json:"gas_window,omitempty"`
	Reasons     []string   `json:"reasons"`
	Sources     []string   `json:"sources"`
	ComputedSeq int64      `json:"computed_seq"`
	DecisionIDs []string   `json:"decision_ids,omitempty"`
	Adjusted    bool       `json:"adjusted"`
}

type ResidueReport struct {
	ID          string             `json:"id"`
	FinalS      float64            `json:"final_s"`
	FinalTempC  float64            `json:"final_temp_c"`
	FinalMassMg float64            `json:"final_mg_mg"`
	ResiduePct  float64            `json:"residue_pct"`
	Unknown     bool               `json:"unknown"`
	Chain       []domain.ChainLink `json:"chain"`
	Sources     []string           `json:"sources"`
	ComputedSeq int64              `json:"computed_seq"`
	Reasons     []string           `json:"reasons"`
}

type Series struct {
	ID          string       `json:"id"`
	Points      []SPoint     `json:"points"`
	Gaps        []domain.Gap `json:"gaps"`
	Sources     []string     `json:"sources"`
	FP          string       `json:"param_fingerprint"`
	ComputedSeq int64        `json:"computed_seq"`
	Notes       []string     `json:"notes"`
}

type Decision struct {
	ID            string   `json:"id"`
	CandidateID   string   `json:"candidate_id"`
	CandSeq       int64    `json:"candidate_seq"`
	Kind          string   `json:"kind"` // accept | reject | adjust
	Reason        string   `json:"reason"`
	AtSeq         int64    `json:"at_seq"`
	AdjustOnsetS  *float64 `json:"adjust_onset_s,omitempty"`
	AdjustOffsetS *float64 `json:"adjust_offset_s,omitempty"`
	Retracted     bool     `json:"retracted"`
	RetractsID    string   `json:"retracts_id,omitempty"`
	ConflictIDs   []string `json:"conflict_ids,omitempty"`
}

type Conflict struct {
	ID          string `json:"id"`
	CandidateID string `json:"candidate_id"`
	DecisionA   string `json:"decision_a"`
	DecisionB   string `json:"decision_b"`
	Pending     bool   `json:"pending"`
	Resolution  string `json:"resolution,omitempty"`
	ResolvedBy  string `json:"resolved_by,omitempty"`
	AtSeq       int64  `json:"at_seq"`
}

type MissingSource struct {
	Layer  domain.Layer `json:"layer"`
	Reason string       `json:"reason"`
}

// Derived is the computed world at one point in time.
type Derived struct {
	Series      *Series         `json:"series"`
	Candidates  []Candidate     `json:"candidates"`
	Ambiguities []Ambiguity     `json:"ambiguities"`
	Residue     *ResidueReport  `json:"residue"`
	Missing     []MissingSource `json:"missing_sources"`
	Params      Parameters      `json:"params"`
	AtSeq       int64           `json:"at_seq"`
}

type ImportRecordState struct {
	ImportID string           `json:"import_id"`
	Record   domain.RawRecord `json:"record"`
}

// State is the full projection of the event log.
type State struct {
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Seed        int64  `json:"seed"`
	ReviewStage string `json:"review_stage"`

	RawVersions   map[domain.Layer]LayerVersion
	RawPoints     []domain.RawPoint
	BlankPoints   []domain.RawPoint
	Furnace       []domain.FurnaceSegment
	Valves        []domain.ValveEvent
	InitialMass   *InitialMass
	Drift         *DriftSpec
	Params        Parameters
	ParamsHistory []Parameters

	Imports     map[string][]ImportRecordState
	ImportOrder []string

	LatestDerived *Derived
	History       []DerivedSnapshotRef
	Decisions     []Decision
	Conflicts     []Conflict
	Rules         map[string]map[string]any

	Diagnostics []string
	TipSeq      int64
}

// DerivedSnapshotRef records a derived_computed event and which previously
// current results it invalidated (local change propagation).
type DerivedSnapshotRef struct {
	AtSeq        int64    `json:"at_seq"`
	Invalidated  []string `json:"invalidated"`
	SeriesID     string   `json:"series_id"`
	CandidateIDs []string `json:"candidate_ids"`
	ResidueID    string   `json:"residue_id"`
}
