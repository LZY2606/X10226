package domain

// Clock is the time source used for event metadata. Tests inject a fixed,
// manually advanced clock (virtual time); the server uses wall time.
type Clock interface {
	NowISO() string
}

// Layer kinds: the six input strata, kept separate and never overwritten.
const (
	LayerPoints  = "points"  // raw TGA samples: furnace time, temperature, measured mass
	LayerDrift   = "drift"   // balance drift correction coefficients
	LayerProgram = "program" // furnace program segments
	LayerGas     = "gas"     // gas valve switch events
	LayerBlank   = "blank"   // blank curve samples (empty-pan run)
	LayerInitial = "initial" // sample initial mass, versioned
)

var LayerKinds = []string{LayerPoints, LayerDrift, LayerProgram, LayerGas, LayerBlank, LayerInitial}

func LayerLabel(k string) string {
	switch k {
	case LayerPoints:
		return "原始采样点"
	case LayerDrift:
		return "天平漂移校正"
	case LayerProgram:
		return "炉程序"
	case LayerGas:
		return "气体阀事件"
	case LayerBlank:
		return "空白曲线"
	case LayerInitial:
		return "样品初重"
	}
	return k
}

// Quantity carries the canonical numeric value, its SI/derived reference unit,
// the original textual representation exactly as imported, and the original
// unit token. When conversion fails Value stays nil and a diagnostic is kept
// next to the row; the original representation is never discarded.
type Quantity struct {
	Value      *float64 `json:"v,omitempty"`
	Unit       string   `json:"u,omitempty"`     // normalized unit (coordinate reference)
	Original   string   `json:"raw"`             // exact source token
	SourceUnit string   `json:"srcu,omitempty"`  // unit token as authored
}

// Diagnostic explains an unconvertible value, a format problem, or an
// accepted-but-degraded condition. It is first-class data.
type Diagnostic struct {
	Code     string `json:"code"`
	Message  string `json:"msg"`
	Layer    string `json:"layer,omitempty"`
	Row      int    `json:"row,omitempty"` // 1-based source row, 0 = whole layer
	Field    string `json:"field,omitempty"`
	Severity string `json:"sev"` // "error" | "warning"
	Accepted bool   `json:"accepted"`
}

// ParsedRow is one record of an imported layer.
type ParsedRow struct {
	Index       int                 `json:"i"` // 0-based within layer
	Fields      map[string]Quantity `json:"f"` // canonical field name -> quantity
	Diagnostics []Diagnostic        `json:"diag,omitempty"`
	Valid       bool                `json:"ok"`
}

// ParsedLayer is the versioned parse result for one submitted blob.
type ParsedLayer struct {
	ID          string        `json:"id"`
	ProjectID   string        `json:"pid"`
	Kind        string        `json:"kind"`
	Version     int           `json:"ver"`
	ParentID    string        `json:"parent,omitempty"`
	BlobID      string        `json:"blob"`
	SchemaRev   string        `json:"schema"`    // import rule version
	MediaType   string        `json:"media"`     // e.g. text/csv
	Filename    string        `json:"file,omitempty"`
	Rows        []ParsedRow   `json:"rows"`
	Diagnostics []Diagnostic  `json:"diag,omitempty"`
	Accepted    bool          `json:"accepted"`  // fully valid layer release
	Partial     bool          `json:"partial"`   // some rows rejected/kept
	Headers     []string      `json:"headers"`
}
