// Package tga contains the domain model, deck ingest and the versioned
// thermogravimetric analysis algorithms (TG/DTG, stage decomposition,
// atmosphere reaction windows and residual evidence chains).
package tga

// Canonical units / coordinate bases used in the derived layer.
const (
	UnitSecond   = "s"
	UnitMinute   = "min"
	UnitMg       = "mg"
	UnitGram     = "g"
	UnitCelsius  = "°C"
	UnitKelvin   = "K"
	UnitPercent  = "%"
	UnitMgPerMin = "mg/min"
	UnitPctPerMin = "%/min"
	UnitCPerMin  = "°C/min"
	UnitGas      = "gas"
	UnitText     = "text"
)

// Layers are the stratified raw inputs. Order here is also the canonical
// presentation order in the review UI.
const (
	LayerSamples     = "raw-samples"
	LayerDrift       = "drift"
	LayerProgram     = "program"
	LayerValve       = "valve"
	LayerBlank       = "blank"
	LayerInitialMass = "initial-mass"
)

// LayerOrder lists the stratified input layers in review order.
var LayerOrder = []string{
	LayerSamples, LayerDrift, LayerProgram, LayerValve, LayerBlank, LayerInitialMass,
}

// LayerDeps records which layers feed each derived family. It is the basis for
// the blast-radius / staleness containment guarantees.
var LayerDeps = map[string][]string{
	"series":    {LayerSamples, LayerDrift, LayerBlank},
	"dtg":       {LayerSamples, LayerDrift, LayerBlank},
	"peaks":     {LayerSamples, LayerDrift, LayerBlank},
	"windows":   {LayerValve, LayerProgram},
	"stages":    {LayerSamples, LayerDrift, LayerBlank, LayerValve, LayerProgram},
	"residuals": {LayerSamples, LayerDrift, LayerBlank, LayerInitialMass},
}

// Diag is a machine readable diagnostic attached at the exact location of a
// value. Unconvertible values stay in place with a diagnostic instead of being
// dropped for plotting.
type Diag struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // error | warning | info
	Message  string `json:"message"`
	Layer    string `json:"layer,omitempty"`
	Row      int    `json:"row,omitempty"` // 1-based row within the layer
	Column   string `json:"column,omitempty"`
}

// Cell keeps the original textual representation and, only when convertible,
// the normalized numeric value in a canonical unit.
type Cell struct {
	Raw        string  `json:"raw"`
	Value      float64 `json:"value,omitempty"`
	Unit       string  `json:"unit,omitempty"`
	Declared   string  `json:"declared_unit,omitempty"`
	OK         bool    `json:"ok"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

// InputNode is an immutable version of one stratified input layer.
type InputNode struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Layer       string    `json:"layer"`
	Version     int       `json:"version"`
	Kind        string    `json:"kind"` // import | revision
	ParentID    string    `json:"parent_id,omitempty"`
	ImportID    string    `json:"import_id"`
	Columns     []string  `json:"columns"`
	Units       []string  `json:"units"`
	Rows        [][]Cell  `json:"rows"`
	Diagnostics []Diag    `json:"diagnostics"`
	Note        string    `json:"note,omitempty"`
	ContentFP   string    `json:"content_fp"`
	CreatedSeq  int64     `json:"created_seq"`
	CreatedAt   string    `json:"created_at"`
}

// ImportRecord stores the full submitted fragment (even on rejection) and the
// per-layer judgement, which is what makes half-failed imports auditable.
type ImportRecord struct {
	ID          string                 `json:"id"`
	ProjectID   string                 `json:"project_id"`
	RawText     string                 `json:"raw_text"`
	Note        string                 `json:"note,omitempty"`
	Status      string                 `json:"status"` // accepted | partial | rejected
	Accepted    []string               `json:"accepted_layers"`
	Rejected    []RejectedLayer        `json:"rejected_layers"`
	Affected    map[string][]string    `json:"affected_objects,omitempty"`
	CreatedSeq  int64                  `json:"created_seq"`
	CreatedAt   string                 `json:"created_at"`
}

// RejectedLayer is a fragment that failed validation: it is preserved with its
// diagnostics and the objects it would have affected.
type RejectedLayer struct {
	Layer      string `json:"layer"`
	Kind       string `json:"kind"`
	RawText    string `json:"raw_text"`
	Diagnostics []Diag `json:"diagnostics"`
	Affects    []string `json:"affects"`
}

// Event is the append-only audit record. The event stream is the single source
// of truth for state reconstruction and time travel.
type Event struct {
	ID       string         `json:"id"`
	ProjectID string        `json:"project_id"`
	Seq      int64          `json:"seq"`
	Type     string         `json:"type"`
	At       string         `json:"at"`
	Actor    string         `json:"actor"`
	UndoOf   string         `json:"undo_of,omitempty"`
	Payload  map[string]any `json:"payload"`
}

// Params are the versioned analysis parameters.
type Params struct {
	Version        int     `json:"version"`
	Fingerprint    string  `json:"fingerprint"`
	Algorithm      string  `json:"algorithm"`
	ValveDelayS    float64 `json:"valve_delay_s"`
	TempLagS       float64 `json:"temp_lag_s"`
	DTGWindow      int     `json:"dtg_window"`
	PeakMinProm    float64 `json:"peak_min_prom_pct_per_min"`
	GapThresholdS  float64 `json:"gap_threshold_s"`
	NoiseTolMg     float64 `json:"noise_tol_mg"`
	Note           string  `json:"note,omitempty"`
}

// SourceRef points at a direct source of a derived value with the content
// fingerprint captured at computation time.
type SourceRef struct {
	Kind      string `json:"kind"` // input | param | derived | decision
	ID        string `json:"id"`
	Layer     string `json:"layer,omitempty"`
	ContentFP string `json:"content_fp,omitempty"`
}

// ChainStep is one human+machine readable step of a residual calculation chain.
type ChainStep struct {
	Op        string   `json:"op"`
	Label     string   `json:"label"`
	Value     string   `json:"value,omitempty"`
	Unit      string   `json:"unit,omitempty"`
	Sources   []string `json:"sources,omitempty"`
	Detail    string   `json:"detail,omitempty"`
}

// DerivedNode is one versioned family of computed results.
type DerivedNode struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	Family     string         `json:"family"` // series|dtg|peaks|windows|stages|residuals
	Algorithm  string         `json:"algorithm"`
	ParamsFP   string         `json:"params_fp"`
	Sources    []SourceRef    `json:"sources"`
	Data       map[string]any `json:"data"`
	Diagnostics []Diag        `json:"diagnostics"`
	CreatedSeq int64          `json:"created_seq"`
	CreatedAt  string         `json:"created_at"`
}

// Decision is a human adjudication on a stage candidate (or residual).
type Decision struct {
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	TargetID   string `json:"target_id"`
	TargetKind string `json:"target_kind"`
	Verdict    string `json:"verdict"` // accept | reject | adjust
	Adjustment map[string]float64 `json:"adjustment,omitempty"`
	Reason     string `json:"reason"`
	Seq        int64  `json:"seq"`
	At         string `json:"at"`
	Actor      string `json:"actor"`
	Undone     bool   `json:"undone"`
	UndoneBy   string `json:"undone_by,omitempty"`
	Active     bool   `json:"active"`
}
