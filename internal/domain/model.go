package domain

// LayerKind identifies the six immutable, separately-versioned input layers.
type LayerKind string

const (
	LayerSample   LayerKind = "sample"    // raw sampled points: furnace time, T, raw balance mass
	LayerDrift    LayerKind = "drift"     // balance drift correction rows
	LayerFurnace  LayerKind = "furnace"   // furnace program segments
	LayerGas      LayerKind = "gas"       // gas valve events
	LayerBlank    LayerKind = "blank"     // blank curve points
	LayerInitial  LayerKind = "initial"   // sample initial mass (stratified)
)

// AllLayerKinds is the canonical layer order used for ordering and display.
func AllLayerKinds() []LayerKind {
	return []LayerKind{LayerSample, LayerDrift, LayerFurnace, LayerGas, LayerBlank, LayerInitial}
}

func (k LayerKind) Valid() bool {
	for _, x := range AllLayerKinds() {
		if x == k {
			return true
		}
	}
	return false
}

func (k LayerKind) Title() string {
	switch k {
	case LayerSample:
		return "原始采样点"
	case LayerDrift:
		return "天平漂移校正"
	case LayerFurnace:
		return "炉程序"
	case LayerGas:
		return "气体阀事件"
	case LayerBlank:
		return "空白曲线"
	case LayerInitial:
		return "样品初重"
	}
	return string(k)
}

// RowStatus marks the fate of each parsed input row.
type RowStatus string

const (
	RowAccepted RowStatus = "accepted"
	RowRejected RowStatus = "rejected" // row kept, parse/unit diagnostic attached
)

// LayerRow is one original line, preserved verbatim with its parse outcome.
type LayerRow struct {
	LineNo     int      `json:"line_no"`
	Raw        string   `json:"raw"`
	Status     RowStatus `json:"status"`
	Fields     []string `json:"fields,omitempty"`
	Diagnostic string   `json:"diagnostic,omitempty"`
	// Parsed typed values (accepted rows).
	TimeQ *Q `json:"time_q,omitempty"`
	TQ    *Q `json:"t_q,omitempty"`
	MassQ *Q `json:"mass_q,omitempty"`
	Extra map[string]string `json:"extra,omitempty"`
}

// ImportDiagnostic describes a non-fatal row problem or a fatal abort.
type ImportDiagnostic struct {
	LineNo  int    `json:"line_no,omitempty"`
	Level   string `json:"level"` // error | warning
	Code    string `json:"code"`
	Message string `json:"message"`
	Raw     string `json:"raw,omitempty"`
}

// Layer is one immutable version of one input source.
type Layer struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"project_id"`
	Kind      LayerKind       `json:"kind"`
	Version   int             `json:"version"`
	ParentID  string          `json:"parent_id,omitempty"` // previous version of same kind
	Origin    string          `json:"origin"`              // filename / text / archive
	SHA       string          `json:"sha"`                 // content hash of the raw payload
	Rows      []LayerRow      `json:"rows"`
	Diags     []ImportDiagnostic `json:"diags,omitempty"`
	Truncated bool            `json:"truncated,omitempty"` // import half-failure: stream ended mid-row/section
	AcceptedCount int        `json:"accepted_count"`
	RejectedCount int        `json:"rejected_count"`
	CreatedAt string          `json:"created_at"` // RFC3339 virtual time
}

// CurrentLayers maps kind -> latest layer version.
type CurrentLayers map[LayerKind]*Layer

// Series is an immutable numeric trace on a common time basis (canonical s).
type Series struct {
	ID      string    `json:"id"`
	TimeS   []float64 `json:"time_s"`
	// Parallel arrays; nil where the point is unknown (e.g. across a gap).
	TK       []float64 `json:"t_k,omitempty"`
	MassMG   []float64 `json:"mass_mg,omitempty"`
	RateMgMin []float64 `json:"rate_mg_min,omitempty"`
	Mask     []bool    `json:"mask,omitempty"` // false => unknown at that index
	GapAfter []int     `json:"gap_after,omitempty"` // indices after which a sampling gap begins
}

// Window is a time interval [StartS,EndS) on the sample time axis.
type Window struct {
	StartS float64 `json:"start_s"`
	EndS   float64 `json:"end_s"`
}

// Peak is one detected DTG peak.
type Peak struct {
	ID        string  `json:"id"`
	CenterS   float64 `json:"center_s"`
	CenterTK  float64 `json:"center_t_k"`
	Height    float64 `json:"height_mg_min"`
	Window    Window  `json:"window"`
	GroupID   string  `json:"group_id"`           // overlapping peaks share a group
	Overlaps  []string `json:"overlaps,omitempty"` // peak ids
}

// ReactionWindow is the atmosphere-attributed plausible reaction interval:
// valve time is NOT reaction time; the window expresses the delay/lag spread.
type ReactionWindow struct {
	ID            string   `json:"id"`
	FromEventID   string   `json:"from_event_id"`
	Gas           string   `json:"gas"`
	ValveS        float64  `json:"valve_s"`
	Window        Window   `json:"window"`
	DelayS        float64  `json:"delay_s"`
	LagS          float64  `json:"lag_s"`
	Note          string   `json:"note,omitempty"`
}

// StageCandidate is a legal (possibly overlapping) decomposition stage.
type StageCandidate struct {
	ID          string   `json:"id"`
	Window      Window   `json:"window"`
	PeakIDs     []string `json:"peak_ids"`
	GasWindowID string   `json:"gas_window_id,omitempty"`
	MassLossMG  float64  `json:"mass_loss_mg"`
	MassLossPct *float64 `json:"mass_loss_pct,omitempty"` // nil => unknown (no m0)
	StartTK     float64  `json:"start_t_k"`
	EndTK       float64  `json:"end_t_k"`
	Ambiguous   bool     `json:"ambiguous,omitempty"` // cannot uniquely attribute
	Reasons     []string `json:"reasons,omitempty"`   // why kept / rejected
}

// Residue is a residual-mass-ratio result with a full calculation chain.
type Residue struct {
	ID        string       `json:"id"`
	AtS       float64      `json:"at_s"`
	AtTK      float64      `json:"at_t_k"`
	Value     *float64     `json:"value"`   // fraction of chosen initial mass; nil unknown
	ValuePct  *float64     `json:"value_pct"`
	Status    string       `json:"status"`  // ok | unknown
	UnknownReasons []string `json:"unknown_reasons,omitempty"`
	Chain     []ChainStep  `json:"chain"`
}

// ChainStep is one step in a residue calculation chain.
type ChainStep struct {
	NodeID    string `json:"node_id"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Detail    string `json:"detail"`
	Value     string `json:"value,omitempty"`
	SourceRef string `json:"source_ref,omitempty"` // layer id / row / event id
}

// Decision is an adjudication on a candidate/residue.
type Decision struct {
	ID        string  `json:"id"`
	Seq       int64   `json:"seq"`
	EventID   string  `json:"event_id"`
	SubjectID string  `json:"subject_id"` // candidate or residue id
	Kind      string  `json:"kind"`       // accept | reject | adjust | conflict | resolve | undo
	Verdict   string  `json:"verdict"`    // accepted | rejected | adjusted
	Reason    string  `json:"reason"`
	Params    map[string]float64 `json:"params,omitempty"`
	At        string  `json:"at"`       // RFC3339
	SupersedesEventID string `json:"supersedes_event_id,omitempty"`
	ConflictWithEvent string `json:"conflict_with_event,omitempty"`
	Undone    bool    `json:"undone,omitempty"`
	UndoneBy  string  `json:"undone_by,omitempty"`
}
