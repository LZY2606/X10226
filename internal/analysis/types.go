// Package analysis implements the derived layer for TGA stage decomposition.
// Principles enforced here:
//
//   - valve switch time is not sample reaction time (atmosphere delay and
//     temperature lag produce an uncertain reaction window);
//   - stages may overlap and mass-loss percentages are not forced to total
//     100%;
//   - sampling gaps split the series: no DTG value, interpolation, or stage
//     boundary ever crosses a gap;
//   - overlapping peak clusters produce every legal decomposition as
//     first-class candidates with explicit reject reasons.
package analysis

import "tga-review/internal/domain"

// Input pins every direct source version used by a run.
type Input struct {
	ProjectID string
	RunID     string
	Params    domain.Params

	Points   []domain.RawPoint
	Drift    *domain.DriftSpec
	Blank    []domain.BlankPoint
	Furnace  []domain.FurnaceSegment
	Gas      []domain.GasEvent
	Initial  []domain.InitialMass

	SourceIDs map[string]string // layer -> sourceID (direct provenance)
	// ArtifactID maps (kind, local) to the stable artifact id assigned by
	// the service, so value refs stay valid after export/import.
	ArtifactID func(kind string) string
}

// SeriesPoint is one normalized, gap-aware sample.
type SeriesPoint struct {
	Seq       int     `json:"seq"`
	TimeS     float64 `json:"time_s"`
	TempC     float64 `json:"temp_c"`
	RawMassMg float64 `json:"raw_mass_mg"`
	Status    string  `json:"status"`
	NonMono   bool    `json:"non_monotonic"`
	GapAfter  bool    `json:"gap_after"`
}

// Segment is a contiguous run of samples. Gaps separate segments.
type Segment struct {
	ID     string        `json:"id"`
	Index  int           `json:"index"`
	Points []SeriesPoint `json:"points"`
}

type NormalizedPayload struct {
	Segments    []Segment           `json:"segments"`
	Orphans     []SeriesPoint       `json:"orphans"` // rows with unreadable time; kept, never interpolated
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
}

type CorrectedPoint struct {
	Seq         int     `json:"seq"`
	TimeS       float64 `json:"time_s"`
	TempC       float64 `json:"temp_c"`
	RawMassMg   float64 `json:"raw_mass_mg"`
	CorrMassMg  float64 `json:"corr_mass_mg"`
	CorrectionMg float64 `json:"correction_mg"`
	Status      string  `json:"status"`
	NonMono     bool    `json:"non_monotonic"`
	GapAfter    bool    `json:"gap_after"`
}

type CorrectedPayload struct {
	Method      string           `json:"method"`
	Segments    []CorrSegment    `json:"segments"`
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
}

type CorrSegment struct {
	ID     string           `json:"id"`
	Index  int              `json:"index"`
	Points []CorrectedPoint `json:"points"`
}

type DTGPoint struct {
	Seq     int     `json:"seq"` // index of left endpoint
	TimeS   float64 `json:"time_s"` // midpoint time
	StartS  float64 `json:"start_s"`
	EndS    float64 `json:"end_s"`
	TempC   float64 `json:"temp_c"`
	MGperMin float64 `json:"mg_per_min"` // negative during mass loss
}

type DTGPayload struct {
	Segments    []DTGSegment   `json:"segments"`
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
}
type DTGSegment struct {
	ID       string     `json:"id"`
	Index    int        `json:"index"`
	Points   []DTGPoint `json:"points"`
}

// Peak is one DTG mass-loss peak.
type Peak struct {
	LocalID    string  `json:"local_id"`
	SegmentID  string  `json:"segment_id"`
	TimeS      float64 `json:"time_s"`
	TempC      float64 `json:"temp_c"`
	MGperMin   float64 `json:"mg_per_min"`
	Prominence float64 `json:"prominence"`
	GroupID    string  `json:"group_id"` // overlapping-peak cluster
}

type PeakPayload struct {
	Peaks       []Peak             `json:"peaks"`
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
}

// ReactionWindow is the uncertain time interval at which a valve switch is
// seen by the sample: center = valve + delay, half-width = temperature lag.
type ReactionWindow struct {
	GasEventID string  `json:"gas_event_id"`
	Gas        string  `json:"gas"`
	ValveS     float64 `json:"valve_s"`
	CenterS    float64 `json:"center_s"`
	HalfWidthS float64 `json:"half_width_s"`
	LowS       float64 `json:"low_s"`
	HighS      float64 `json:"high_s"`
}

// GasSpan is one gas regime on the reaction timeline.
type GasSpan struct {
	Gas    string  `json:"gas"`
	StartS float64 `json:"start_s"`
	EndS   float64 `json:"end_s"`
}

type AtmosPayload struct {
	Windows     []ReactionWindow `json:"windows"`
	Spans       []GasSpan        `json:"spans"` // derived regimes incl. uncertainty bands
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
}

// StageProposal is one legal decomposition of a peak cluster.
type StageProposal struct {
	GroupID    string             `json:"group_id"`
	Kind       string             `json:"kind"` // unique | alt-merged | alt-separate
	Legal      bool               `json:"legal"`
	RejectNote string             `json:"reject_note,omitempty"`
	Candidates []domain.Candidate `json:"candidates"`
}

type StagesPayload struct {
	Proposals   []StageProposal       `json:"proposals"`
	Residue     domain.Residue        `json:"residue"`
	InitialID   string                `json:"initial_id"`
	Diagnostics []domain.Diagnostic   `json:"diagnostics"`
}
