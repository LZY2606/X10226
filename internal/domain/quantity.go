package domain

// Measure is a domain quantity that preserves its imported representation.
// Raw is the exact text submitted by the operator. Canonical conversion happens
// only in the derived layer; values that cannot be converted stay in place with
// a diagnostic instead of being dropped for plotting.
type Measure struct {
	Raw    string  `json:"raw"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Valid  bool    `json:"valid"`
	Diag   string  `json:"diag,omitempty"`
	Row    int     `json:"row,omitempty"`
	Column string  `json:"column,omitempty"`
}

// CoordBasis names the coordinate reference a series is expressed in.
// Examples: "furnace_seconds", "celsius".
type CoordBasis struct {
	Kind   string `json:"kind"` // "time" | "temperature"
	Unit   string `json:"unit"` // canonical unit
	Origin string `json:"origin"`
}

// SamplePoint is one original acquisition sample. Raw representations are kept
// verbatim; converted values live in T/M/D fields.
type SamplePoint struct {
	Seq   int      `json:"seq"`
	Row   int      `json:"row"`
	TRaw string   `json:"t_raw"`
	MRaw string   `json:"m_raw"`
	T    *float64 `json:"t,omitempty"`
	M    *float64 `json:"m,omitempty"`
	TUnit string  `json:"t_unit,omitempty"`
	MUnit string  `json:"m_unit,omitempty"`
	Diags []string `json:"diags,omitempty"`
}

// DocKind enumerates the stratified raw layers.
type DocKind string

const (
	KindSamples DocKind = "samples" // 原始采样点
	KindDrift   DocKind = "drift"   // 天平漂移校正
	KindFurnace DocKind = "furnace" // 炉程序
	KindGas     DocKind = "gas"     // 气体阀事件
	KindBlank   DocKind = "blank"   // 空白曲线
	KindMass0   DocKind = "mass0"   // 样品初重分层
)

// RawDoc is an immutable imported layer.
type RawDoc struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Kind      DocKind         `json:"kind"`
	Label     string          `json:"label"`
	Source    string          `json:"source"` // file name / text label
	Seq       int             `json:"seq"`
	ParentID  string          `json:"parent_id,omitempty"`
	RawText   string          `json:"raw_text"`
	Payload   string          `json:"payload"` // canonical-ish JSON, preserving raws
	Diags     []string        `json:"diags,omitempty"`
	IngestTS  string          `json:"ingest_ts"` // virtual-time stamp
}

// LineParse is one parsed record inside a document payload.
type LineParse struct {
	Row    int               `json:"row"`
	Fields map[string]string `json:"fields"`
	Diags  []string          `json:"diags,omitempty"`
}
