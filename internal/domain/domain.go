// Package domain defines the immutable event log and the derived graph
// ("lineage") used by the thermogravimetric review system.
//
// Every recorded fact is an Event appended to a project. Events are never
// overwritten: corrections, revisions and conflicting decisions are expressed
// as new events that point at their parents. Derived results (Node values) are
// produced by a versioned algorithm from direct source events, and carry the
// parameter fingerprint used to compute them.
package domain

import (
	"encoding/json"
)

// FragmentKind identifies the six layered source inputs.
type FragmentKind string

const (
	KindSample  FragmentKind = "sample_points"
	KindBlank   FragmentKind = "blank_curve"
	KindFurnace FragmentKind = "furnace_program"
	KindGas     FragmentKind = "gas_valve_events"
	KindTare    FragmentKind = "initial_mass"
)

// AllFragmentKinds lists the layered source kinds in review order.
func AllFragmentKinds() []FragmentKind {
	return []FragmentKind{KindSample, KindBlank, KindFurnace, KindGas, KindTare}
}

// EventKind enumerates every append-only fact in the project log.
type EventKind string

const (
	EvProjectCreated EventKind = "project_created"
	EvFragmentAdded  EventKind = "fragment_added"
	EvParamSet       EventKind = "param_set"
	EvDerived        EventKind = "derived"
	EvDecision       EventKind = "decision"
	EvDecisionUndo  EventKind = "decision_undone"
	EvConflict      EventKind = "conflict_resolved"
	EvNote           EventKind = "note"
)

// Severity for diagnostics.
type Severity string

const (
	SevError Severity = "error"
	SevWarn  Severity = "warn"
	SevInfo  Severity = "info"
)

// Diagnostic records why a row/value could not be processed. The original
// representation is always kept; unconvertible values never block the rest of
// the import ("half-failed" imports) and never get dropped for plotting.
type Diagnostic struct {
	Code     string    `json:"code"`
	Severity Severity  `json:"severity"`
	Row      int       `json:"row,omitempty"`
	Field    string    `json:"field,omitempty"`
	Message  string    `json:"message"`
	Fragment string    `json:"fragment,omitempty"`
	Version  int       `json:"version,omitempty"`
}

// Cell is one original token: raw representation retained verbatim; Value is
// the normalized numeric value when ParseOK is true. Failed values stay in
// place with a diagnostic instead of being removed.
type Cell struct {
	Raw     string  `json:"raw"`
	Value   float64 `json:"value,omitempty"`
	Unit    string  `json:"unit,omitempty"`
	ParseOK bool    `json:"parse_ok"`
}

// Row is a single imported record.
type Row struct {
	Index       int             `json:"index"`
	Original    map[string]Cell `json:"original"`
	Diagnostics []Diagnostic    `json:"diagnostics,omitempty"`
}

// Fragment is a submitted version of one layered source. Versions are
// append-only; a re-submission creates a new fragment referencing the previous
// version and never mutates it.
type Fragment struct {
	Kind     FragmentKind `json:"kind"`
	Version  int          `json:"version"`
	Filename string       `json:"filename"`
	Format   string       `json:"format"`
	Rows     []Row        `json:"rows"`
	// Imported is true when the fragment was fully usable; false for a
	// half-failed import (some rows/values rejected with diagnostics kept).
	Imported bool `json:"imported"`
	// Accepted is false while the import is quarantined pending review; such
	// fragments never feed derivation until a decision accepts them.
	Accepted bool `json:"accepted"`
}

// Param is a single algorithm parameter carrying its own unit/coordinate basis.
type Param struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// ParamSet is a versioned, fingerprinted set of algorithm parameters.
type ParamSet struct {
	Version    int              `json:"version"`
	Parent     int              `json:"parent"`
	Params     map[string]Param `json:"params"`
	Fingerprint string           `json:"fingerprint"`
	Reason     string           `json:"reason,omitempty"`
}

// Node is one derived (or sourced) result in the dependency graph. IDs are
// content-addressed, so the same logical value keeps a stable identity across
// export/delete/reimport.
type Node struct {
	ID     string    `json:"id"`
	Kind   string    `json:"kind"`
	Status string    `json:"status"`
	Label  string    `json:"label"`
	Value  *float64  `json:"value,omitempty"`
	Unit   string    `json:"unit,omitempty"`
	// Basis documents the coordinate reference frame, e.g. "t_since_run_start".
	Basis string   `json:"basis,omitempty"`
	Times []float64 `json:"times,omitempty"`
	Vals  []float64 `json:"vals,omitempty"`
	// Extra carries typed structured detail (stage windows, peaks, ...).
	Extra  map[string]any `json:"extra,omitempty"`
	Direct []NodeRef      `json:"direct,omitempty"`
	Rejects []Rejection    `json:"rejects,omitempty"`
	// SourceIDs are fragment/param identity markers used for staleness
	// comparison: f:<kind>:<version>, p:<version>.
	SourceIDs []string `json:"source_ids,omitempty"`
	// Provenance
	Algorithm    string `json:"algorithm"`
	ParamFP      string `json:"param_fp"`
	DerivedEvent int    `json:"derived_event,omitempty"`
	// Group ties mutually-exclusive stage alternatives together.
	Group string `json:"group,omitempty"`
}

// NodeRef is a directed edge in the dependency graph.
type NodeRef struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Role  string `json:"role"`
}

// Rejection explains why a candidate was not adopted.
type Rejection struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Series point for SVG rendering.
type Pt struct {
	T float64 `json:"t"`
	V float64 `json:"v"`
}

// Decision records an accept / reject / parameter adjustment made by a
// reviewer on a node (typically a stage candidate or residue rate).
type Decision struct {
	ID            int      `json:"id"`
	NodeID        string   `json:"node_id"`
	Action        string   `json:"action"`
	Reason        string   `json:"reason"`
	Retracted     bool     `json:"retracted"`
	RetractReason string   `json:"retract_reason,omitempty"`
	// Adjustment parameters (manual stage window edits), expressed in the
	// node's coordinate basis.
	Adjust *Adjustment `json:"adjust,omitempty"`
}

// Adjustment is a reviewer parameter/window edit attached to a decision.
type Adjustment struct {
	StartT float64  `json:"start_t,omitempty"`
	EndT   float64  `json:"end_t,omitempty"`
	Params map[string]Param `json:"params,omitempty"`
}

// Conflict is recorded when two active decisions contradict one another
// (e.g. two mutually exclusive stage alternatives both accepted).
type Conflict struct {
	ID        int    `json:"id"`
	NodeA     string `json:"node_a"`
	NodeB     string `json:"node_b"`
	Winner    string `json:"winner"`
	Reason    string `json:"reason"`
}

// ChainItem is one step in an expandable calculation chain.
type ChainItem struct {
	Node   *Node    `json:"node"`
	Role   string   `json:"role"`
	Formula string  `json:"formula,omitempty"`
	Inputs []Pt    `json:"inputs,omitempty"`
	Output *Pt     `json:"output,omitempty"`
	Note   string  `json:"note,omitempty"`
}

// EventPayloads is a tagged payload union. Exactly one field is populated.
type Payloads struct {
	Name       string      `json:"name,omitempty"`
	Fragment   *Fragment   `json:"fragment,omitempty"`
	ParamSet   *ParamSet   `json:"param_set,omitempty"`
	Run        *RunRecord  `json:"run,omitempty"`
	Decision   *Decision   `json:"decision,omitempty"`
	Undo       *UndoRecord `json:"undo,omitempty"`
	Conflict   *Conflict   `json:"conflict,omitempty"`
	Text       string      `json:"text,omitempty"`
}

// RunRecord snapshots one derivation run: all nodes produced, and which
// previously-current nodes became stale as a consequence.
type RunRecord struct {
	Number    int      `json:"number"`
	EventID   int      `json:"event_id"`
	Algorithm string   `json:"algorithm"`
	ParamFP   string   `json:"param_fp"`
	Nodes     []Node   `json:"nodes"`
	StaleIDs  []string `json:"stale_ids"`
}

// UndoRecord retracts a decision (retracted decisions stay in history).
type UndoRecord struct {
	DecisionID int    `json:"decision_id"`
	Reason     string `json:"reason"`
}

// Event is the single append-only log record.
type Event struct {
	ID        int             `json:"id"`
	ProjectID string          `json:"project_id"`
	Kind      EventKind       `json:"kind"`
	At        string          `json:"at"`
	Payload   json.RawMessage `json:"payload"`
}
