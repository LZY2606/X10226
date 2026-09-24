// Package domain holds the immutable, layered TGA data model.
//
// Layering rules enforced across the system:
//   - raw inputs are immutable once submitted; revisions are new versions
//     linked to a parent version;
//   - every physical quantity keeps its original textual representation and
//     only the derived layer normalizes units;
//   - values that cannot be converted stay in place with diagnostics rather
//     than being dropped to make plotting possible;
//   - every derived value carries algorithm version, parameter fingerprint
//     and direct source references.
package domain

import "time"

// Kind constants for raw sources.
const (
	KindSample      = "sample"
	KindBlank       = "blank"
	KindDrift       = "drift"
	KindProgram     = "program"
	KindGas         = "gas"
	KindInitialMass = "initial_mass"
)

// AllKinds lists every accepted raw source kind in display order.
func AllKinds() []string {
	return []string{KindSample, KindBlank, KindDrift, KindProgram, KindGas, KindInitialMass}
}

// Q is a physical quantity that keeps the raw token it was parsed from.
type Q struct {
	Text  string  `json:"text"`  // original representation, e.g. "24.8 °C"
	OK    bool    `json:"ok"`    // false when normalization failed
	Value float64 `json:"value"` // normalized canonical value (0 when !OK)
	Unit  string  `json:"unit"`  // canonical unit: min | C | mg | C/min ...
	Diag  string  `json:"diag,omitempty"`
}

// Dim marks the dimensional axis of a quantity for conversion.
type Dim string

const (
	DimTime Dim = "time"
	DimTemp Dim = "temp"
	DimMass Dim = "mass"
	DimRate Dim = "rate"
)

// SamplePoint is one raw acquisition row.
type SamplePoint struct {
	Index    int    `json:"index"` // 1-based position in the raw block
	FurnaceT Q      `json:"furnace_t"`
	SampleT  Q      `json:"sample_t,omitempty"`
	T        Q      `json:"t"`
	M        Q      `json:"m"`
	Excluded bool   `json:"excluded,omitempty"`
	Note     string `json:"note,omitempty"`
}

type SampleSeries struct {
	Label    string        `json:"label"`
	TimeUnit string        `json:"time_unit"`
	TempUnit string        `json:"temp_unit"`
	MassUnit string        `json:"mass_unit"`
	Points   []SamplePoint `json:"points"`
	RawText  string        `json:"raw_text"`
}

type BlankPoint struct {
	Index    int  `json:"index"`
	FurnaceT Q    `json:"furnace_t"`
	T        Q    `json:"t"`
	M        Q    `json:"m"`
	Excluded bool `json:"excluded,omitempty"`
}

type BlankCurve struct {
	Label    string       `json:"label"`
	TimeUnit string       `json:"time_unit"`
	TempUnit string       `json:"temp_unit"`
	MassUnit string       `json:"mass_unit"`
	Points   []BlankPoint `json:"points"`
	RawText  string       `json:"raw_text"`
}

type DriftPoint struct {
	Index    int  `json:"index"`
	T        Q    `json:"t"`
	Offset   Q    `json:"offset"` // balance reading offset to subtract
	Excluded bool `json:"excluded,omitempty"`
}

type BalanceDrift struct {
	Label    string       `json:"label"`
	Method   string       `json:"method"`
	TimeUnit string       `json:"time_unit"`
	MassUnit string       `json:"mass_unit"`
	Rate     *Q           `json:"rate,omitempty"`
	Points   []DriftPoint `json:"points"`
	RawText  string       `json:"raw_text"`
}

// ProgSegment is one furnace program instruction.
type ProgSegment struct {
	Index     int    `json:"index"`
	Kind      string `json:"kind"` // hold | ramp
	StartT    Q      `json:"start_t"`
	EndT      Q      `json:"end_t"`
	StartTemp Q      `json:"start_temp"`
	EndTemp   Q      `json:"end_temp"`
	Rate      Q      `json:"rate,omitempty"`
	Gas       string `json:"gas,omitempty"`
}

type FurnaceProgram struct {
	Segments []ProgSegment `json:"segments"`
	RawText  string        `json:"raw_text"`
}

type GasEvent struct {
	Index int    `json:"index"`
	At    Q      `json:"at"`
	From  string `json:"from"`
	To    string `json:"to"`
	Flow  Q      `json:"flow,omitempty"`
	Note  string `json:"note,omitempty"`
}

type GasEvents struct {
	Label   string     `json:"label"`
	Events  []GasEvent `json:"events"`
	RawText string     `json:"raw_text"`
}

type InitialMass struct {
	Label   string `json:"label"`
	Mass    Q      `json:"mass"`
	At      Q      `json:"at"`
	Method  string `json:"method"`
	Note    string `json:"note,omitempty"`
	RawText string `json:"raw_text"`
}

// Diagnostic is attached to the raw batch that produced it; the offending
// raw content is never deleted.
type Diagnostic struct {
	Severity string `json:"severity"` // error | warning | info
	Code     string `json:"code"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
	Ref      string `json:"ref,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

// RawBatch is one import attempt.
type RawBatch struct {
	ID          string       `json:"id"`
	ProjectID   string       `json:"project_id"`
	Seq         int          `json:"seq"`
	FileName    string       `json:"file_name"`
	SubmittedAt time.Time    `json:"submitted_at"`
	Partial     bool         `json:"partial"`
	RawText     string       `json:"raw_text"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Source is an immutable, versioned raw source.
type Source struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Kind        string    `json:"kind"`
	Label       string    `json:"label"`
	Version     int       `json:"version"`
	ParentID    string    `json:"parent_id,omitempty"`
	BatchID     string    `json:"batch_id"`
	AddedSeq    int       `json:"added_seq"`
	SubmittedAt time.Time `json:"submitted_at"`
	Payload     []byte    `json:"payload"`
	RawText     string    `json:"raw_text"`
}

// Project is a review workspace.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Event is one append-only journal entry.
type Event struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Seq       int       `json:"seq"`
	Type      string    `json:"type"`
	Actor     string    `json:"actor"`
	At        time.Time `json:"at"`
	Payload   []byte    `json:"payload"`
}

// Event types.
const (
	EvProjectCreated   = "project_created"
	EvBatchImported    = "batch_imported"
	EvParamsUpdated    = "params_updated"
	EvSourceRevised    = "source_revised"
	EvDecisionRecorded = "decision_recorded"
	EvConflictResolved = "conflict_resolved"
	EvDecisionReverted = "decision_reverted"
	EvProjectImported  = "project_imported"
)

// Decision verdicts.
const (
	VerdictCandidate = "candidate"
	VerdictAccepted  = "accepted"
	VerdictRejected  = "rejected"
	VerdictAdjusted  = "adjusted"
)

// Decision is a human adjudication attached to a derived result.
type Decision struct {
	ID            string             `json:"id"`
	ProjectID     string             `json:"project_id"`
	EventSeq      int                `json:"event_seq"`
	TargetID      string             `json:"target_id"`
	Verdict       string             `json:"verdict"`
	Reason        string             `json:"reason"`
	Adjustments   map[string]float64 `json:"adjustments,omitempty"`
	Reverted      bool               `json:"reverted"`
	Conflict      bool               `json:"conflict"`
	Superseded    bool               `json:"superseded"`
	ConflictsWith []string           `json:"conflicts_with,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	Actor         string             `json:"actor"`
}

// Typed event payloads.
type ProjectCreatedPayload struct {
	Name string `json:"name"`
}

type BatchImportedPayload struct {
	BatchID   string   `json:"batch_id"`
	SourceIDs []string `json:"source_ids"`
	Kinds     []string `json:"kinds"`
	Partial   bool     `json:"partial"`
	DiagCount int      `json:"diag_count"`
	FileName  string   `json:"file_name"`
}

type ParamsUpdatedPayload struct {
	FromFP string `json:"from_fp"`
	ToFP   string `json:"to_fp"`
	Reason string `json:"reason"`
}

type SourceRevisedPayload struct {
	SourceID string `json:"source_id"`
	ParentID string `json:"parent_id"`
	Kind     string `json:"kind"`
	Version  int    `json:"version"`
	Note     string `json:"note"`
}

type DecisionRecordedPayload struct {
	DecisionID    string             `json:"decision_id"`
	TargetID      string             `json:"target_id"`
	Verdict       string             `json:"verdict"`
	Reason        string             `json:"reason"`
	Adjustments   map[string]float64 `json:"adjustments,omitempty"`
	Conflict      bool               `json:"conflict"`
	ConflictsWith []string           `json:"conflicts_with,omitempty"`
}

type ConflictResolvedPayload struct {
	WinningID string `json:"winning_id"`
	LosingID  string `json:"losing_id"`
	TargetID  string `json:"target_id"`
	Reason    string `json:"reason"`
}

type DecisionRevertedPayload struct {
	DecisionID string `json:"decision_id"`
	Reason     string `json:"reason"`
}

type ProjectImportedPayload struct {
	BundleFormat string `json:"bundle_format"`
	OriginalID   string `json:"original_id"`
	EventCount   int    `json:"event_count"`
}
