package domain

import "time"

// RawKind identifies one of the six stratified raw input layers. The order is
// also the order reviewers verify them.
type RawKind string

const (
	KindSamples   RawKind = "samples"   // original sampled points (mass, T, furnace time)
	KindDrift     RawKind = "drift"     // balance drift correction
	KindProgram   RawKind = "program"   // furnace program segments
	KindGas       RawKind = "gas"       // gas valve events
	KindBlank     RawKind = "blank"     // blank run curve
	KindInitMass  RawKind = "initmass"  // sample initial mass, layer-revised
)

// AllKinds lists layers in review order.
var AllKinds = []RawKind{KindSamples, KindDrift, KindProgram, KindGas, KindBlank, KindInitMass}

func (k RawKind) Label() string {
	switch k {
	case KindSamples:
		return "原始采样点"
	case KindDrift:
		return "天平漂移校正"
	case KindProgram:
		return "炉程序"
	case KindGas:
		return "气体阀事件"
	case KindBlank:
		return "空白曲线"
	case KindInitMass:
		return "样品初重"
	}
	return string(k)
}

// SamplePoint is one original TG sample. Cells retain original tokens; only
// Canon values are used by derived layers.
type SamplePoint struct {
	Index   int    `json:"index"`
	Time    Cell   `json:"time"`
	FTemp   Cell   `json:"furnace_temp"`
	Mass    Cell   `json:"mass"`
	SampleT Cell   `json:"sample_temp,omitempty"`
	Note    string `json:"note,omitempty"`
}

// DriftPoint is one balance-baseline drift record (linear/anchor correction).
type DriftPoint struct {
	Index int    `json:"index"`
	Time  Cell   `json:"time"`
	Drift Cell   `json:"drift_mass"` // signed baseline offset in mass units
	Note  string `json:"note,omitempty"`
}

// ProgramStep is one furnace program segment. Mode is ramp|hold|cool.
type ProgramStep struct {
	Index  int    `json:"index"`
	Start  Cell   `json:"start_time"`
	End    Cell   `json:"end_time"`
	TStart Cell   `json:"temp_start"`
	TEnd   Cell   `json:"temp_end"`
	Mode   string `json:"mode"`
}

// GasEvent is one gas valve switch at valve time.
type GasEvent struct {
	Index int    `json:"index"`
	Time  Cell   `json:"time"`
	Gas   Cell   `json:"gas"`
	Note  string `json:"note,omitempty"`
}

// BlankPoint is one blank curve point.
type BlankPoint struct {
	Index int  `json:"index"`
	Time  Cell `json:"time"`
	Mass  Cell `json:"mass"`
}

// InitMass is a layer-saved initial sample mass revision.
type InitMass struct {
	Mass Cell   `json:"mass"`
	Note string `json:"note,omitempty"`
}

// Payload is the typed body of a raw source, selected by Kind.
type Payload struct {
	Samples  []SamplePoint `json:"samples,omitempty"`
	Drift    []DriftPoint  `json:"drift,omitempty"`
	Program  []ProgramStep `json:"program,omitempty"`
	Gas      []GasEvent    `json:"gas,omitempty"`
	Blank    []BlankPoint  `json:"blank,omitempty"`
	InitMass *InitMass     `json:"init_mass,omitempty"`
}

// RowDiag is a per-row diagnostic attached during import (kept with the row).
type RowDiag struct {
	Layer string `json:"layer"`
	Row   int    `json:"row"`
	Level string `json:"level"` // error | warning
	Msg   string `json:"msg"`
}

// RawSource is an immutable version of one input layer.
type RawSource struct {
	ID       string    `json:"id"`
	Project  string    `json:"project"`
	Kind     RawKind   `json:"kind"`
	Version  int       `json:"version"`
	ParentID string    `json:"parent_id,omitempty"`
	BatchID  string    `json:"batch_id"`
	Filename string    `json:"filename,omitempty"`
	Text     string    `json:"text"`
	Payload  Payload   `json:"payload"`
	Diags    []RowDiag `json:"diags,omitempty"`
	Revision string    `json:"revision,omitempty"` // revision reason, revisions only
	Created  time.Time `json:"created"`
}

// Batch is one import attempt. Half-failed imports are recorded with
// status=partial so reviewers see both accepted fragments and rejected shards.
type Batch struct {
	ID         string    `json:"id"`
	Project    string    `json:"project"`
	Label      string    `json:"label"`
	Status     string    `json:"status"` // ok | partial | rejected
	Accepted   []string  `json:"accepted_ids"`
	AcceptedK  []RawKind `json:"accepted_kinds"`
	Rejected   []Reject  `json:"rejected,omitempty"`
	DiagCount  int       `json:"diag_count"`
	Created    time.Time `json:"created"`
}

// Reject is an input fragment that failed structural validation and was never
// stored as a raw source.
type Reject struct {
	Fragment string `json:"fragment"`
	Header   string `json:"header"`
	Reason   string `json:"reason"`
}
