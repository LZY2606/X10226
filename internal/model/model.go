// Package model defines the domain types of the TGA review system:
// layered raw inputs (samples, drift correction, furnace program, gas
// valve events, blank curve, initial mass), the append-only event log,
// human decisions and derived analysis results.
//
// All physical quantities are carried as QValue: the original textual
// representation is preserved at import time; normalisation to internal
// units happens only in the derivation layer.
package model

// Unit is a physical unit or coordinate basis tag.
type Unit string

const (
	UnitMG          Unit = "mg"
	UnitG           Unit = "g"
	UnitC           Unit = "degC"
	UnitK           Unit = "K"
	UnitMin         Unit = "min"
	UnitSec         Unit = "s"
	UnitPercent     Unit = "%"
	UnitMGPerMin    Unit = "mg/min"
	UnitPctPerMin   Unit = "%/min"
	UnitKPerMin     Unit = "K/min"
	UnitMLPerMin    Unit = "mL/min"
	UnitNone        Unit = "1"
	UnitUnknown     Unit = "?"
)

// LayerKind identifies one of the six separately stored input layers.
type LayerKind string

const (
	LayerSamples     LayerKind = "samples"
	LayerDrift       LayerKind = "drift"
	LayerFurnace     LayerKind = "furnace"
	LayerGas         LayerKind = "gas"
	LayerBlank       LayerKind = "blank"
	LayerInitialMass LayerKind = "initial_mass"
)

// QValue is a quantity with unit, coordinate basis and the exact text
// seen at import. Unparseable values keep Valid=false and stay in place
// with a diagnostic; they are never dropped to make plotting possible.
type QValue struct {
	Value       float64 `json:"value,omitempty"`
	Unit        Unit    `json:"unit"`
	Basis       string  `json:"basis,omitempty"`
	Original    string  `json:"original"`
	Valid       bool    `json:"valid"`
	Diagnostic  string  `json:"diagnostic,omitempty"`
}

// RejectedRow is an input row that could not be accepted; the raw text is
// retained so partial imports remain auditable.
type RejectedRow struct {
	LineNo int    `json:"line_no"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

// SamplePoint is one raw sampling row of the thermobalance export.
type SamplePoint struct {
	Index        int    `json:"index"`
	LineNo       int    `json:"line_no"`
	T            QValue `json:"t"`
	Mass         QValue `json:"mass"`
	Temp         QValue `json:"temp"`
	Rejected     bool   `json:"rejected,omitempty"`
	RejectReason string `json:"reject_reason,omitempty"`
}

// BlankPoint is one point of the empty-pan baseline curve.
type BlankPoint struct {
	Index int    `json:"index"`
	LineNo int   `json:"line_no"`
	T     QValue `json:"t"`
	Mass  QValue `json:"mass"`
	Rejected bool `json:"rejected,omitempty"`
	RejectReason string `json:"reject_reason,omitempty"`
}

// FurnaceStep is one segment of the temperature program, expressed on
// the furnace clock.
type FurnaceStep struct {
	Index  int    `json:"index"`
	Label  string `json:"label"`
	TStart QValue `json:"t_start"`
	TEnd   QValue `json:"t_end"`
	Rate   QValue `json:"rate"`
	Target QValue `json:"target"`
}

// GasEvent is one valve switch. TValve is the instrument-clock switch
// time; Delay is the transport/reaction lag that moves the effective
// reaction time away from the valve time. Delay may be unknown.
type GasEvent struct {
	Index   int    `json:"index"`
	TValve  QValue `json:"t_valve"`
	FromGas string `json:"from_gas"`
	ToGas   string `json:"to_gas"`
	Flow    QValue `json:"flow"`
	Delay   QValue `json:"delay"`
}

// DriftCorrection records how balance baseline drift is corrected.
type DriftCorrection struct {
	Model              string `json:"model"` // "linear-from-blank" | "manual"
	Slope              QValue `json:"slope"`
	Intercept          QValue `json:"intercept"`
	BasisSubmissionID  string `json:"basis_submission_id,omitempty"`
	AlgorithmVersion   string `json:"algorithm_version"`
	ParamFingerprint   string `json:"param_fingerprint"`
	AnchoredAtInitial  bool   `json:"anchored_at_initial"`
}

// InitialMassLayer is the separately stored starting mass, including the
// plateau window from which it was read.
type InitialMassLayer struct {
	Mass    QValue `json:"mass"`
	Method  string `json:"method"`
	TStart  QValue `json:"t_start"`
	TEnd    QValue `json:"t_end"`
	Revision int   `json:"revision"`
}

// Submission is one immutable, frozen version of one input layer.
type Submission struct {
	ID           string          `json:"id"`
	Kind         LayerKind       `json:"kind"`
	Seq          int             `json:"seq"`
	ParentID     string          `json:"parent_id,omitempty"`
	RevisionOf   string          `json:"revision_of,omitempty"`
	ImportedAt   string          `json:"imported_at"`
	SourceName   string          `json:"source_name"`
	InputSHA256  string          `json:"input_sha"`
	Note         string          `json:"note,omitempty"`
	Points       []SamplePoint   `json:"points,omitempty"`
	Blank        []BlankPoint    `json:"blank,omitempty"`
	Drift        *DriftCorrection `json:"drift,omitempty"`
	Steps        []FurnaceStep   `json:"steps,omitempty"`
	Events       []GasEvent      `json:"events,omitempty"`
	InitialMass  *InitialMassLayer `json:"initial_mass,omitempty"`
	AcceptedRows int            `json:"accepted_rows"`
	RejectedRows []RejectedRow  `json:"rejected_rows,omitempty"`
	Partial      bool           `json:"partial,omitempty"`
}
