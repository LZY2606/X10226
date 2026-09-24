package domain

import (
	"fmt"
	"time"
)

// Layer is one of the stratified source layers. Each layer is submitted as an
// immutable version; revisions are expressed with a parent version link.
type Layer string

const (
	LayerSamples  Layer = "samples"  // raw sampled points (time, temperature, mass)
	LayerDrift    Layer = "drift"    // balance drift correction
	LayerFurnace  Layer = "furnace"  // furnace program segments
	LayerGasValve Layer = "gasvalve" // gas valve switch events
	LayerBlank    Layer = "blank"    // blank/baseline curve
	LayerMass0    Layer = "mass0"    // sample initial mass
)

// AllLayers is the canonical stratification order.
var AllLayers = []Layer{LayerSamples, LayerDrift, LayerFurnace, LayerGasValve, LayerBlank, LayerMass0}

func (l Layer) Valid() bool {
	for _, x := range AllLayers {
		if x == l {
			return true
		}
	}
	return false
}

// Diagnostic records an import-time problem without losing the offending row.
type Diagnostic struct {
	Layer   Layer  `json:"layer"`
	Row     int    `json:"row"`
	Field   string `json:"field,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Raw     string `json:"raw,omitempty"`
}

// RawLayer keeps exactly what the operator submitted: header, rows of cell
// strings, and the stated coordinate basis. Nothing here is normalized.
type RawLayer struct {
	Layer   Layer      `json:"layer"`
	Version int        `json:"version"`
	Parent  int        `json:"parent"`
	Units   LayerUnits `json:"units"`
	Header  []string   `json:"header"`
	Rows    [][]string `json:"rows"`
	Note    string     `json:"note,omitempty"`
}

type LayerUnits struct {
	Time string `json:"time,omitempty"`
	Temp string `json:"temp,omitempty"`
	Mass string `json:"mass,omitempty"`
}

// RawSet is one committed snapshot of the six stratified source layers.
type RawSet map[Layer]*RawLayer

func (s RawSet) Latest(l Layer) *RawLayer { return s[l] }

// ----- typed views (parsed on the derived layer; originals are retained) -----

type SamplePoint struct {
	Index       int         `json:"index"`
	Time        Measurement `json:"time"`
	Temp        Measurement `json:"temp"`
	Mass        Measurement `json:"mass"`
	HasGapAfter bool        `json:"has_gap_after,omitempty"`
	Diag        string      `json:"diag,omitempty"`
}

type DriftPoint struct {
	Index int         `json:"index"`
	Time  Measurement `json:"time"`
	Mass  Measurement `json:"mass"` // drift offset to subtract
}

type FurnaceSegment struct {
	Index      int         `json:"index"`
	Start      Measurement `json:"start_time"`
	End        Measurement `json:"end_time"`
	StartTemp  Measurement `json:"start_temp"`
	TargetTemp Measurement `json:"target_temp"`
	Kind       string      `json:"kind"` // ramp | hold
}

type ValveEvent struct {
	Index int         `json:"index"`
	Time  Measurement `json:"time"` // valve switch time (NOT sample reaction time)
	From  string      `json:"from_gas"`
	To    string      `json:"to_gas"`
}

type BlankPoint struct {
	Index int         `json:"index"`
	Time  Measurement `json:"time"`
	Mass  Measurement `json:"mass"` // blank buoyancy offset to subtract
}

type InitialMass struct {
	Revision int         `json:"revision"`
	Time     Measurement `json:"time"`
	Mass     Measurement `json:"mass"`
	Reason   string      `json:"reason,omitempty"`
}

// ParseTime parses the project's fixed virtual-time RFC3339 stamps.
func ParseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad timestamp %q: %w", s, err)
	}
	return t, nil
}
