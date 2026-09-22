// Package derive holds pure domain calculations: corrected TG series, DTG,
// peak/ stage candidates, atmosphere reaction windows and residue chains.
// Everything here is deterministic: same algorithm version, parameters and
// direct sources always yield identical node identities.
package derive

import "tga-archive/internal/domain"

// AlgorithmVersion is bumped whenever a calculation in this package changes.
const AlgorithmVersion = "derive-v1.0.0"

// Inputs bundles the currently accepted sources of every layer.
type Inputs struct {
	Samples     *domain.SourcePayload
	Drift       *domain.SourcePayload
	Furnace     *domain.SourcePayload
	Valves      *domain.SourcePayload
	Blank       *domain.SourcePayload
	InitialMass *domain.SourcePayload
}

// Point of the corrected TG series. Intervals crossing a declared gap or an
// invalid source value get no interpolation: DTG stays unknown there.
type Point struct {
	Time      domain.Measure `json:"time"`
	Temp      domain.Measure `json:"temp"`
	MassRaw   domain.Measure `json:"massRaw"`
	MassCorr  domain.Measure `json:"massCorr"`
	DTG       domain.Measure `json:"dtg"`
	Flags     []string       `json:"flags,omitempty"`
	GapBefore bool           `json:"gapBefore,omitempty"`
}

type Peak struct {
	At      int             `json:"at"`
	Time    domain.Measure  `json:"time"`
	Temp    domain.Measure  `json:"temp"`
	DTG     domain.Measure  `json:"dtg"`
	Cluster int             `json:"cluster"`
}

// StageCandidate is one legal decomposition of a peak cluster. Overlapping
// stages are allowed and mass-loss percentages are not forced to sum to 100.
type StageCandidate struct {
	ID           string             `json:"id"`
	Key          string             `json:"key"`
	Cluster      int                `json:"cluster"`
	Kind         string             `json:"kind"` // single | merged
	MemberKeys   []string           `json:"memberKeys,omitempty"`
	StartTime    domain.Measure     `json:"startTime"`
	EndTime      domain.Measure     `json:"endTime"`
	StartTemp    domain.Measure     `json:"startTemp"`
	EndTemp      domain.Measure     `json:"endTemp"`
	PeakTime     domain.Measure     `json:"peakTime"`
	PeakTemp     domain.Measure     `json:"peakTemp"`
	PeakDTG      domain.Measure     `json:"peakDTG"`
	MassStart    domain.Measure     `json:"massStart"`
	MassEnd      domain.Measure     `json:"massEnd"`
	LossMg       domain.Measure     `json:"lossMg"`
	LossPct      domain.Measure     `json:"lossPct"`
	Overlaps     []string           `json:"overlaps,omitempty"`
	Exclusives   []string           `json:"exclusives,omitempty"`
	Unknown      bool               `json:"unknown,omitempty"`
	Status       string             `json:"status"` // primary | alternative | ruled_accepted | ruled_rejected
	RejectReason []string           `json:"rejectReason,omitempty"`
	Chain        []ChainStep        `json:"chain"`
	SourceIDs    []string           `json:"sourceIds"`
}

// ChainStep is one readable/programmable step of a calculation chain.
type ChainStep struct {
	Step   int                `json:"step"`
	Label  string             `json:"label"`
	Value  domain.Measure     `json:"value"`
	Formula string            `json:"formula,omitempty"`
	Inputs []string           `json:"inputs,omitempty"`
	Note   string             `json:"note,omitempty"`
}

// ReactionWindow maps a valve switch to the interval actually experienced by
// the sample after transport delay and measured thermal lag.
type ReactionWindow struct {
	ID          string         `json:"id"`
	Gas         string         `json:"gas"`
	ValveAt     domain.Measure `json:"valveAt"`
	StartAt     domain.Measure `json:"startAt"`
	EndAt       domain.Measure `json:"endAt"`
	ValveTemp   domain.Measure `json:"valveTemp"`
	StartTemp   domain.Measure `json:"startTemp"`
	FurnaceTempAtValve domain.Measure `json:"furnaceTempAtValve"`
	MeasuredLagK domain.Measure `json:"measuredLagK"`
	Delay       domain.Measure `json:"delay"`
	Note        string         `json:"note,omitempty"`
}

// Residue is a residual-mass level (per stage and final) with full chain.
type Residue struct {
	ID       string         `json:"id"`
	Label    string         `json:"label"`
	At       domain.Measure `json:"at"`
	Temp     domain.Measure `json:"temp"`
	MassCorr domain.Measure `json:"massCorr"`
	Percent  domain.Measure `json:"percent"`
	Unknown  bool           `json:"unknown,omitempty"`
	Chain    []ChainStep    `json:"chain"`
}

// Report is the full deterministic derivation result.
type Report struct {
	AlgorithmVersion string          `json:"algorithmVersion"`
	ParamVersion     string          `json:"paramVersion"`
	Params           domain.Params   `json:"params"`
	Points           []Point         `json:"points"`
	Gaps             []domain.Gap    `json:"gaps"`
	Peaks            []Peak          `json:"peaks"`
	Candidates       []StageCandidate `json:"candidates"`
	Windows          []ReactionWindow `json:"windows"`
	Residues         []Residue        `json:"residues"`
	Diagnostics      []domain.Diagnostic `json:"diagnostics,omitempty"`
	SourceFingerprints map[string]string `json:"sourceFingerprints"`
}

// Basis describes the initial mass used for percentages.
type Basis struct {
	Mass domain.Measure
	At   domain.Measure
	Note string
}
