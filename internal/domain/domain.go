// Package domain defines the immutable source layers, event log and review
// vocabulary of the thermogravimetric stage-decomposition system.
//
// Every physical quantity is carried as a Qty: the original textual
// representation is preserved at import time; normalization to float values
// happens only in the derived layer. Values that cannot be converted stay in
// place (Valid=false) and carry diagnostics instead of being dropped.
package domain

import "encoding/json"

// Qty is a domain quantity. Raw is the exact imported token. Value/Unit are
// populated by parsing; when Valid is false the raw token must remain visible.
type Qty struct {
	Raw        string  `json:"raw"`
	Value      float64 `json:"value,omitempty"`
	Unit       string  `json:"unit,omitempty"`
	Coordinate string  `json:"coordinate,omitempty"` // coordinate basis, e.g. furnace_time_s / sample_temperature_C
	Valid      bool    `json:"valid"`
}

// Source kinds: the seven separately versioned input layers.
const (
	KindPoints      = "raw_points"         // 原始采样点
	KindDrift       = "balance_drift"      // 天平漂移校正参数
	KindFurnace     = "furnace_program"    // 炉程序
	KindGas         = "gas_valve_events"   // 气体阀事件
	KindBlank       = "blank_curve"        // 空白曲线
	KindInitialMass = "initial_mass"       // 样品初重
	KindAlgoParams  = "algorithm_params"   // 算法参数（参数修订）
)

// SourceKinds is the canonical ordered list of layers.
var SourceKinds = []string{
	KindPoints, KindDrift, KindFurnace, KindGas, KindBlank, KindInitialMass, KindAlgoParams,
}

// Diagnostic is attached to the exact input position that caused it.
type Diagnostic struct {
	Level    string `json:"level"` // error | warning
	Code     string `json:"code"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Fragment string `json:"fragment,omitempty"`
}

// SourceRecord is one committed version of one input layer.
// Payload is the verbatim submitted text and is never overwritten: revisions
// create a new record with ParentID set.
type SourceRecord struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"projectId"`
	BatchID      string          `json:"batchId"`
	Kind         string          `json:"kind"`
	Name         string          `json:"name"`
	Payload      string          `json:"payload"`
	Version      int             `json:"version"`
	ParentID     string          `json:"parentId,omitempty"`
	Order        int             `json:"order"`
	Accepted     bool            `json:"accepted"` // false => rejected fragment from a half-failed import
	Diagnostics  []Diagnostic    `json:"diagnostics,omitempty"`
	Body         json.RawMessage `json:"body,omitempty"` // typed parse result (see Parse* types)
	ContentHash  string          `json:"contentHash"`
	CreatedAtMS  int64           `json:"createdAtMs"`
}

// Point is one raw sampling row. Gap marks a sampling hole: the time exists
// but temperature/mass values are missing and must never be interpolated.
type Point struct {
	Index int  `json:"index"`
	Time  Qty  `json:"time"`
	Temp  Qty  `json:"temp"`
	Mass  Qty  `json:"mass"`
	Gap   bool `json:"gap"`
}

// PointsFile is the raw_points layer.
type PointsFile struct {
	Points []Point `json:"points"`
}

// DriftAnchor calibrates balance baseline drift at a given time.
type DriftAnchor struct {
	At     Qty `json:"at"`
	Offset Qty `json:"offset"`
}

// DriftFile is the balance_drift layer.
type DriftFile struct {
	Mode      string        `json:"mode"` // none | linear | anchors
	Slope     Qty           `json:"slope"`
	Intercept Qty           `json:"intercept"`
	Anchors   []DriftAnchor `json:"anchors"`
	Basis     string        `json:"basis"`
}

// BlankPoint is one row of the empty-pan blank curve.
type BlankPoint struct {
	Index int  `json:"index"`
	Time  Qty  `json:"time"`
	Mass  Qty  `json:"mass"`
	Gap   bool `json:"gap"`
}

// BlankFile is the blank_curve layer.
type BlankFile struct {
	Points []BlankPoint `json:"points"`
}

// FurnaceSegment is one ramp/hold segment of the furnace program.
type FurnaceSegment struct {
	Index     int    `json:"index"`
	Mode      string `json:"mode"` // ramp | hold
	StartTime Qty    `json:"startTime"`
	EndTime   Qty    `json:"endTime"`
	StartTemp Qty    `json:"startTemp"`
	EndTemp   Qty    `json:"endTemp"`
	Rate      Qty    `json:"rate,omitempty"`
}

// FurnaceFile is the furnace_program layer.
type FurnaceFile struct {
	Segments []FurnaceSegment `json:"segments"`
}

// GasEvent is one valve switch with its measured transport delay.
type GasEvent struct {
	Index     int    `json:"index"`
	ValveTime Qty    `json:"valveTime"`
	From      string `json:"from"`
	To        string `json:"to"`
	Delay     Qty    `json:"delay"` // pipeline delay, valve -> sample zone
	Note      string `json:"note,omitempty"`
}

// GasFile is the gas_valve_events layer.
type GasFile struct {
	Events []GasEvent `json:"events"`
}

// InitialMassFile is the initial_mass layer. Revisions express mass correction.
type InitialMassFile struct {
	Mass   Qty    `json:"mass"`
	Method string `json:"method"`
	Note   string `json:"note,omitempty"`
}

// AlgoParamsFile is the algorithm_params layer: peak detection, lag handling.
type AlgoParamsFile struct {
	DTGWindowPoints       int    `json:"dtgWindowPoints"`
	PeakProminenceMGMin   Qty    `json:"peakhProminenceMgMin"`
	PeakMinSeparationS    Qty    `json:"peakMinSeparationS"`
	OverlapWindowS        Qty    `json:"overlapWindowS"`
	AtmosphereLagS        Qty    `json:"atmosphereLagS"`
	ThermalLagS           Qty    `json:"thermalLagS"`
	ResidueEndMarginS     Qty    `json:"residueEndMarginS"`
	Raw                   string `json:"-"`
}
