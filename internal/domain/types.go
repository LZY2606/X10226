// Package domain defines the immutable core model of the TGA review system:
// raw imports, derived artifacts, decisions and archive principles.
package domain

import "time"

// Source kinds kept in separate layers of the raw-input baseline.
const (
	SrcPoints  = "points" // raw sampled TG points
	SrcInitial = "initial_mass"
	SrcDrift   = "drift"   // balance baseline drift correction
	SrcFurnace = "furnace" // furnace temperature program
	SrcGas     = "gas"     // gas valve events
	SrcBlank   = "blank"   // blank (empty-pan) curve
	SrcProgram = "program" // full .tga bundle (import half-failure layer)
	SrcParams  = "params"  // analysis parameters (delay/lag defaults)
)

// SourceRecord is one immutable raw line/object as submitted. Raw text is
// never overwritten; revisions arrive as new records with a parent id.
type SourceRecord struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	Kind       string    `json:"kind"`
	LayerIndex int       `json:"layer_index"`
	LineNo     int       `json:"line_no"`
	RawText    string    `json:"raw_text"`
	Fields     []string  `json:"fields"`
	ParentID   string    `json:"parent_id,omitempty"`
	Revision   int       `json:"revision"`
	Status     string    `json:"status"` // accepted | unconvertible | rejected
	Diagnostic string    `json:"diagnostic,omitempty"`
	ImportSeq  int64     `json:"import_seq"`
	CreatedAt  time.Time `json:"created_at"`
}

// Diagnostic severity.
const (
	SevError   = "error"   // layer unusable / half failure
	SevWarning = "warning" // value kept in place, excluded from drawing
	SevInfo    = "info"
)

// Diagnostic is a machine-readable import/parse problem pinned to raw input.
type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	LineNo   int    `json:"line_no"`
	Message  string `json:"message"`
	RawText  string `json:"raw_text,omitempty"`
}

// ImportBatch groups one submission. A half-failed batch is quarantined:
// records stay readable but never advance the current source version.
type ImportBatch struct {
	ProjectID   string `json:"project_id"`
	Filename    string `json:"filename"`
	Mode        string `json:"mode"` // full | amend
	Raw         string `json:"raw"`
	Records     []SourceRecord
	Diagnostics []Diagnostic `json:"diagnostics"`
	Status      string       `json:"status"` // accepted | quarantined
	Present     map[string]bool
}
