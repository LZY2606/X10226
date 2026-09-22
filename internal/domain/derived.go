package domain

// Derived kinds.
type DerivedKind string

const (
	DerivedCorrected DerivedKind = "corrected_series"
	DerivedDTG       DerivedKind = "dtg_series"
	DerivedStages    DerivedKind = "stage_candidates"
	DerivedResidue   DerivedKind = "residue"
)

// DerivedRecord wraps every derived artifact with full provenance. Stale is
// projected by the engine when a newer layer/params version exists.
type DerivedRecord struct {
	ID        string      `json:"id"`
	Kind      DerivedKind `json:"kind"`
	Algo      string      `json:"algo_version"`
	ParamsID  string      `json:"params_id"`
	Sources   []string    `json:"source_ids"` // direct sources only
	Stale     bool        `json:"stale"`
	SupersededBy string   `json:"superseded_by,omitempty"`
	Payload   any         `json:"payload"`
	Findings  []Finding   `json:"findings,omitempty"`
}

// SeriesPoint is a typed point in a derived series. GapSegment marks a run of
// points; a break between two runs is a sampling gap that is never bridged.
type SeriesPoint struct {
	ID         string   `json:"id"`
	SourceID   string   `json:"source_point_id"`
	Min        float64  `json:"min"`
	C          float64  `json:"c"`
	Mg         *float64 `json:"mg,omitempty"`           // corrected mass, nil if unknown
	RawMg      *float64 `json:"raw_mg,omitempty"`       // raw typed mass (spikes kept)
	GapSegment int      `json:"gap_segment"`            // 1-based run index
	Unknown    bool     `json:"unknown,omitempty"`      // unconvertible original value
}

// CorrectedSeries is the blank/drift corrected sample curve.
type CorrectedSeries struct {
	SeriesID    string        `json:"series_id"`
	InitialMG   float64       `json:"initial_mg"`
	InitialMassID string     `json:"initial_mass_id,omitempty"`
	DriftID     string        `json:"drift_id,omitempty"`
	BlankID     string        `json:"blank_id,omitempty"`
	Points      []SeriesPoint `json:"points"`
	// Gaps[i] = time interval (minutes) where points are missing; no
	// interpolation crosses these.
	Gaps        [][2]float64   `json:"gaps"`
}

// DTGPoint is one DTG sample (mg/min, negative for mass loss).
type DTGPoint struct {
	ID       string  `json:"id"`
	Min      float64 `json:"min"`
	C        float64 `json:"c"`
	MGPM     float64 `json:"mg_per_min"`
	GapSegment int    `json:"gap_segment"`
}

// DTGSeries holds raw-numerical and smoothed DTG plus detected extrema.
type DTGSeries struct {
	SeriesID     string     `json:"series_id"`
	Points       []DTGPoint `json:"points"`
	Peaks        []DTGPeak  `json:"peaks"`
	GapSegments  int        `json:"gap_segments"`
}

// DTGPeak is a local mass-loss extremum within one gap segment.
type DTGPeak struct {
	ID         string  `json:"id"`
	AtMin      float64 `json:"at_min"`
	AtC        float64 `json:"at_c"`
	MGPM       float64 `json:"mg_per_min"`
	GapSegment int     `json:"gap_segment"`
	// EffectiveAtMin applies gas delay + thermal lag: the estimated time at
	// which the valve/atmosphere change actually affects the sample.
	EffectiveAtMin float64 `json:"effective_at_min,omitempty"`
}

// StageStatus marks whether a candidate is fully determined.
type StageStatus string

const (
	StageOK      StageStatus = "ok"
	StageUnknown StageStatus = "unknown" // crosses a sampling gap
)

// StageCandidate is one decomposition stage. Candidates from overlapping
// peaks may overlap in time; losses are not renormalized to sum to 100%.
type StageCandidate struct {
	ID         string      `json:"id"`
	Label      string      `json:"label"`
	StartMin   float64     `json:"start_min"`
	EndMin     float64     `json:"end_min"`
	StartC     float64     `json:"start_c"`
	EndC       float64     `json:"end_c"`
	PeakMin    float64     `json:"peak_min"`
	PeakC      float64     `json:"peak_c"`
	MassLossMG float64     `json:"mass_loss_mg"`
	MassLossPct *float64   `json:"mass_loss_percent,omitempty"` // nil if initial mass unknown
	Status     StageStatus `json:"status"`
	GapIDs     [][2]float64 `json:"gaps_crossed,omitempty"`
	// PeakIDs gives the DTG peaks supporting this candidate; a merged
	// candidate carries several, making overlaps explicit.
	PeakIDs    []string    `json:"peak_ids"`
	// CandidateSet groups mutually exclusive legal decompositions. All
	// members of a set survive when decomposition is not unique.
	CandidateSet string    `json:"candidate_set,omitempty"`
	// RejectedReason is filled on members an automated rule discards as
	// implausible (still retained as first-class data).
	RejectedReason string   `json:"rejected_reason,omitempty"`
	Atmosphere   string    `json:"atmosphere,omitempty"` // effective atmosphere
}

// StageCandidates is one derived stage decomposition.
type StageCandidates struct {
	Stages []StageCandidate `json:"stages"`
}

// ChainStep is one node of an expandable residue calculation chain.
type ChainStep struct {
	ID       string   `json:"id"`
	Op       string   `json:"op"`
	Detail   string   `json:"detail"`
	Value    *float64 `json:"value,omitempty"`
	Unit     string   `json:"unit,omitempty"`
	Refs     []string `json:"refs,omitempty"`
}

// Residue is the final residual-rate result, never just a number.
type Residue struct {
	ID            string      `json:"id"`
	Percent       *float64    `json:"percent"`
	Status        StageStatus `json:"status"`
	FinalMG       *float64    `json:"final_mg,omitempty"`
	InitialMG     *float64    `json:"initial_mg,omitempty"`
	InitialMassID string      `json:"initial_mass_id,omitempty"`
	WindowStartMin float64    `json:"window_start_min"`
	WindowEndMin   float64    `json:"window_end_min"`
	N             int         `json:"n"`
	Reason        string      `json:"reason,omitempty"`
	Chain         []ChainStep `json:"chain"`
}
