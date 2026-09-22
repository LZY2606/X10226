package model

import "time"

// Layer kinds are the immutable raw source layers of a TGA project.
type LayerKind string

const (
	KindSamples LayerKind = "samples" // 原始采样点 (t, T, m; original representation retained)
	KindBlank   LayerKind = "blank"   // 空白曲线
	KindFurnace LayerKind = "furnace" // 炉程序
	KindGases   LayerKind = "gases"   // 气体阀事件
	KindDrift   LayerKind = "drift"   // 天平基线漂移校正参数
	KindInitial LayerKind = "initial" // 样品初重分层 (versioned)
)

// Coordinate/time unit conventions used for domain quantities.
const (
	TimeBase    = "s" // 炉时间 seconds since furnace start
	TempBase    = "K"
	MassBase    = "mg"
	MassPctUnit = "%"
	RateUnit    = "%/min"
)

// Point is one raw sample with original textual representation retained on
// import (raw*); parsed normalized values live in tSec/tK/mMg. When a value
// cannot be converted it stays at its original position with a diagnostic;
// we never drop a row merely to make plotting possible.
type Point struct {
	I       int     `json:"i"`
	RawT    string  `json:"rawT"`
	RawTemp string  `json:"rawTemp"`
	RawMass string  `json:"rawMass"`
	TSec    float64 `json:"tSec"`
	TK      float64 `json:"tK"`
	MMg     float64 `json:"mMg"`
	TOk     bool    `json:"tOk"`
	TempOk  bool    `json:"tempOk"`
	MassOk  bool    `json:"massOk"`
}

// Unit carries a physical unit together with the coordinate base it was
// converted from; original unit text is always retained.
type Unit struct {
	Original string `json:"original"`
	Base     string `json:"base"`
	Scale    float64 `json:"scale"` // multiply original by scale to reach base (0 => unknown)
}

type Samples struct {
	TimeUnit Unit   `json:"timeUnit"`
	TempUnit Unit   `json:"tempUnit"`
	MassUnit Unit   `json:"massUnit"`
	Points   []Point `json:"points"`
}

type BlankPoint struct {
	TSec float64 `json:"tSec"`
	MMg  float64 `json:"mMg"`
	Raw  string  `json:"raw"`
}

type Blank struct {
	MassUnit Unit         `json:"massUnit"`
	Points   []BlankPoint `json:"points"`
}

// FurnaceSeg is one furnace program segment: linear ramp from T0 to T1.
type FurnaceSeg struct {
	StartS float64 `json:"startS"`
	EndS   float64 `json:"endS"`
	T0K    float64 `json:"t0K"`
	T1K    float64 `json:"t1K"`
}

type Furnace struct {
	TempUnit Unit         `json:"tempUnit"`
	TimeUnit Unit         `json:"timeUnit"`
	Segments []FurnaceSeg `json:"segments"`
}

// GasAction describes a valve transition.
type GasAction string

const (
	GasOn  GasAction = "on"
	GasOff GasAction = "off"
)

// GasEvent is a single gas valve event (raw switch time). Reaction time is
// explicitly NOT assumed to equal the switch time (gas delay / temp lag).
type GasEvent struct {
	TSec     float64   `json:"tSec"`
	Action   GasAction `json:"action"`
	Gas      string    `json:"gas"`
	FlowSccm float64   `json:"flowSccm"`
	RawTime  string    `json:"rawTime"`
}

type Gases struct {
	TimeUnit Unit       `json:"timeUnit"`
	Events   []GasEvent `json:"events"`
}

// Drift describes a linear balance baseline drift m_drift(t) = a*t + b
// fitted/adopted for baseline correction.
type Drift struct {
	AMgPerS float64 `json:"aMgPerS"`
	BMg     float64 `json:"bMg"`
	Basis   string  `json:"basis"`
	Raw     string  `json:"raw"`
}

// Initial is the versioned sample initial mass layer.
type Initial struct {
	MassUnit Unit    `json:"massUnit"`
	M0Mg     float64 `json:"m0Mg"`
	RawMass  string  `json:"rawMass"`
}

// Layer is one immutable submitted raw layer. Revisions are expressed as new
// layers with ParentV pointing at the previous version of the same kind.
type Layer struct {
	ID        string    `json:"id"`
	Kind      LayerKind `json:"kind"`
	Version   int       `json:"version"`
	ParentV   int       `json:"parentV"`
	Creator   string    `json:"creator"`
	Submitted time.Time `json:"submitted"`
	Samples   *Samples  `json:"samples,omitempty"`
	Blank     *Blank    `json:"blank,omitempty"`
	Furnace   *Furnace  `json:"furnace,omitempty"`
	Gases     *Gases    `json:"gases,omitempty"`
	Drift     *Drift    `json:"drift,omitempty"`
	Initial   *Initial  `json:"initial,omitempty"`
}

// Severity for import diagnostics.
type Severity string

const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
)

// Diagnostic stays at the original position of an unconvertible/malformed
// value. Nothing is silently discarded.
type Diagnostic struct {
	LayerKind LayerKind `json:"layerKind"`
	LayerID   string    `json:"layerId"`
	Row       int       `json:"row"`
	Field     string    `json:"field"`
	Severity  Severity  `json:"severity"`
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Value     string    `json:"value"`
}
