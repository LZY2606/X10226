// Package casepack defines the offline "失重谱系" case pack: domain-specific
// TGA materials plus machine-readable expected judgements. Scenario files are
// committed under casepack/files and embedded so the demo needs no network.
package casepack

import (
	"embed"
)

//go:embed files/*
var FS embed.FS

// FilesRoot is the embedded root of committed scenario materials.
const FilesRoot = "files"

// Manifest describes one counterexample / narrative scenario.
type Manifest struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Narrative   bool           `json:"narrative"`
	Description string         `json:"description"`
	Seed        int64          `json:"seed"`
	Params      ParamsSpec     `json:"params"`
	Files       []string       `json:"files"`
	Triggers    []Trigger      `json:"triggers"`
	Judgements  []Judgement    `json:"judgements"`
	Expect      Expectation    `json:"expect"`
}

// ParamsSpec sets the timing/decomposition rule parameters.
type ParamsSpec struct {
	GasDelaySeconds   float64 `json:"gas_delay_seconds"`
	TempLagSeconds    float64 `json:"temp_lag_seconds"`
	PeakWindowSeconds float64 `json:"peak_window_seconds"`
	OverlapTolSeconds float64 `json:"overlap_tol_seconds"`
	GapMaxSeconds     float64 `json:"gap_max_seconds"`
}

// Trigger is one replayable action over the scenario.
type Trigger struct {
	Kind    string            `json:"kind"` // import | revise_params | decide | resolve | undo | judgement
	Files   []string          `json:"files,omitempty"`
	Params  *ParamsSpec       `json:"params,omitempty"`
	Reason  string            `json:"reason,omitempty"`
	Target  string            `json:"target,omitempty"`
	State   string            `json:"state,omitempty"`
	Group   string            `json:"group,omitempty"`
	Winner  string            `json:"winner,omitempty"`
	Payload map[string]any    `json:"payload,omitempty"`
}

// Judgement is the machine-readable statement of the domain rule a scenario
// verifies, including the expected containment of impact.
type Judgement struct {
	Code    string   `json:"code"`
	Rule    string   `json:"rule"`
	AffectedKinds []string `json:"affected_node_kinds"`
	SparedKinds   []string `json:"spared_node_kinds"`
	Adopted bool     `json:"adopted"`
	Detail  string   `json:"detail"`
}

// Expectation asserts observable outcomes of the replay.
type Expectation struct {
	UnknownStages   int  `json:"unknown_stages"`
	Overlapping     bool `json:"overlapping"`
	CrossesGap      bool `json:"crosses_gap"`
	HalfFailure     bool `json:"half_failure"`
	InitRevised     bool `json:"init_revised"`
	NonMonotone     bool `json:"non_monotone"`
	StagesSumFree   bool `json:"stages_sum_free"`
	ReactionAfterValve bool `json:"reaction_after_valve"`
}
