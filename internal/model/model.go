// Package model defines the immutable domain model for the TGA review system.
//
// Layering rule: raw imports keep their original representation and units;
// normalized SI-ish working values (seconds, Celsius, milligrams) live only in
// the derived layer. Values that cannot be converted stay in place with a
// diagnostic instead of being dropped.
package model

import "time"

// Layer is one of the separately preserved raw input strata.
type Layer string

const (
	LayerSamples Layer = "mass_samples"     // 原始采样点 (time, temperature, mass)
	LayerBlank   Layer = "blank_curve"      // 空白曲线 / 天平基线
	LayerProgram Layer = "furnace_program"  // 炉程序
	LayerGas     Layer = "gas_valve_events" // 气体阀事件
	LayerM0      Layer = "initial_mass"     // 样品初重
)

// AllLayers lists raw strata in review order.
func AllLayers() []Layer {
	return []Layer{LayerSamples, LayerBlank, LayerProgram, LayerGas, LayerM0}
}

func (l Layer) Valid() bool {
	for _, x := range AllLayers() {
		if x == l {
			return true
		}
	}
	return false
}

// Severity levels for diagnostics.
const (
	SevInfo  = "info"
	SevWarn  = "warn"
	SevError = "error"
)

// Diag is a machine readable diagnostic attached at the exact position where
// the problem occurred.
type Diag struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Severity string `json:"severity,omitempty"`
}

// Point is a raw sample row. Raw textual representations are preserved
// alongside normalized values; OK flags guard every conversion.
type Point struct {
	Seq     int     `json:"seq"`
	TimeRaw string  `json:"time_raw"`
	TimeSec float64 `json:"time_sec"`
	TimeOK  bool    `json:"time_ok"`
	TempRaw string  `json:"temp_raw"`
	TempC   float64 `json:"temp_c"`
	TempOK  bool    `json:"temp_ok"`
	MassRaw string  `json:"mass_raw"`
	MassMg  float64 `json:"mass_mg"`
	MassOK  bool    `json:"mass_ok"`
	Diags   []Diag  `json:"diags,omitempty"`
}

// ProgramStep is one furnace program segment (ramp or hold).
type ProgramStep struct {
	Seq      int     `json:"seq"`
	Name     string  `json:"name"`
	T0Sec    float64 `json:"t0_sec"`
	T1Sec    float64 `json:"t1_sec"`
	StartC   float64 `json:"start_c"`
	EndC     float64 `json:"end_c"`
	RateCMin float64 `json:"rate_c_per_min"`
}

// GasEvent is a valve switch with the gases involved.
type GasEvent struct {
	Seq     int     `json:"seq"`
	TimeRaw string  `json:"time_raw"`
	TimeSec float64 `json:"time_sec"`
	TimeOK  bool    `json:"time_ok"`
	FromGas string  `json:"from"`
	ToGas   string  `json:"to"`
	Note    string  `json:"note,omitempty"`
}

// M0 is the weighed initial sample mass (its own raw stratum).
type M0 struct {
	ValueRaw  string  `json:"value_raw"`
	ValueMg   float64 `json:"value_mg"`
	ValueOK   bool    `json:"value_ok"`
	Unit      string  `json:"unit"`
	Method    string  `json:"method,omitempty"`
	WeighedAt string  `json:"weighed_at,omitempty"`
	Note      string  `json:"note,omitempty"`
}

// Submission is an immutable, append-only raw import (one stratum per event).
type Submission struct {
	ID           string            `json:"id"`
	EventSeq     int64             `json:"event_seq"`
	Layer        Layer             `json:"layer"`
	Kind         string            `json:"kind"` // initial | revision | partial
	ParentID     string            `json:"parent_id,omitempty"`
	Source       string            `json:"source"`
	Units        map[string]string `json:"units"`
	Samples      []Point           `json:"samples,omitempty"`
	Blank        []Point           `json:"blank,omitempty"`
	Program      []ProgramStep     `json:"program,omitempty"`
	Gas          []GasEvent        `json:"gas,omitempty"`
	M0           *M0               `json:"m0,omitempty"`
	ParseErrors  []Diag            `json:"parse_errors,omitempty"`
	AcceptedRows int               `json:"accepted_rows"`
	TotalRows    int               `json:"total_rows"`
	Status       string            `json:"status"` // accepted | partial | rejected
	CreatedAt    time.Time         `json:"created_at"`
	Raw          string            `json:"raw"`
}

// Params is a versioned analysis parameter set.
type Params struct {
	EventSeq    int64             `json:"event_seq"`
	ParentSeq   int64             `json:"parent_seq"`
	Fingerprint string            `json:"fingerprint"`
	Values      map[string]string `json:"values"`
	CreatedAt   time.Time         `json:"created_at"`
}

// Decision is a human verdict produced from the review path.
type Decision struct {
	EventSeq    int64             `json:"event_seq"`
	Target      string            `json:"target"`
	Action      string            `json:"action"` // accept | reject | adjust
	Reason      string            `json:"reason"`
	Adjust      map[string]string `json:"adjust,omitempty"`
	Supersedes  int64             `json:"supersedes"`
	Undone      bool              `json:"undone"`
	UndoneBySeq int64             `json:"undone_by_seq,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
}

// SourceRef points at the exact direct source of a derived artifact.
type SourceRef struct {
	EventSeq     int64  `json:"event_seq"`
	SubmissionID string `json:"submission_id"`
	Layer        Layer  `json:"layer"`
	Role         string `json:"role,omitempty"`
}

// Step is one readable link in an expandable calculation chain.
type Step struct {
	Label   string      `json:"label"`
	Formula string      `json:"formula,omitempty"`
	Inputs  []StepInput `json:"inputs,omitempty"`
	Result  string      `json:"result"`
}

// StepInput references either another artifact or a raw constant.
type StepInput struct {
	Label      string `json:"label"`
	ArtifactID string `json:"artifact_id,omitempty"`
	Display    string `json:"display"`
}

// Quantity is a single displayed domain value with unit and trust state.
type Quantity struct {
	Name    string      `json:"name"`
	Label   string      `json:"label"`
	Value   float64     `json:"value"`
	Unit    string      `json:"unit"`
	State   string      `json:"state"` // ok | unknown | error
	Display string      `json:"display"`
	Range   *[2]float64 `json:"range,omitempty"`
}

// Artifact is any versioned derived result with a stable identity and chain.
type Artifact struct {
	ID           string         `json:"id"`
	Kind         string         `json:"kind"`
	Title        string         `json:"title"`
	Algorithm    string         `json:"algorithm"`
	ParamFP      string         `json:"param_fp"`
	Sources      []SourceRef    `json:"sources"`
	Status       string         `json:"status"` // current | stale | partial
	CreatedSeq   int64          `json:"created_seq"`
	CreatedAt    time.Time      `json:"created_at"`
	SupersededBy string         `json:"superseded_by,omitempty"`
	Quantities   []Quantity     `json:"quantities,omitempty"`
	Steps        []Step         `json:"steps,omitempty"`
	Diags        []Diag         `json:"diags,omitempty"`
	Data         map[string]any `json:"data,omitempty"`
}

// Judgment is a machine readable domain finding (delay, overlap, gap, ...).
type Judgment struct {
	Code     string         `json:"code"`
	Severity string         `json:"severity"`
	Message  string         `json:"message"`
	AtSeq    int64          `json:"at_seq"`
	Refs     []string       `json:"refs,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

// Event is the append-only history record. Payload is canonical JSON.
type Event struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Seq       int64     `json:"seq"`
	Type      string    `json:"type"`
	PrevID    string    `json:"prev_id"`
	Payload   []byte    `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

// Project metadata.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Undo payload.
type UndoPayload struct {
	TargetSeq int64  `json:"target_seq"`
	Reason    string `json:"reason"`
}
