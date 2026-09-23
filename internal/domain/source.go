package domain

// SourceLayer enumerates the six stratified raw input layers.
type SourceLayer string

const (
	LayerSamples   SourceLayer = "samples"
	LayerDrift     SourceLayer = "drift"
	LayerFurnace   SourceLayer = "furnace"
	LayerValves    SourceLayer = "valves"
	LayerBlank     SourceLayer = "blank"
	LayerInitMass  SourceLayer = "initial_mass"
)

func (l SourceLayer) Valid() bool {
	switch l {
	case LayerSamples, LayerDrift, LayerFurnace, LayerValves, LayerBlank, LayerInitMass:
		return true
	}
	return false
}

// Source is one committed, immutable revision of an input layer.
// New revisions never overwrite prior ones; Parent points to the replaced id.
type Source struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Layer     SourceLayer    `json:"layer"`
	Version   int            `json:"version"`
	ParentID  string         `json:"parent_id,omitempty"`
	Format    string         `json:"format"` // csv | json
	Raw       string         `json:"raw"`    // verbatim submitted text
	Payload   map[string]any `json:"payload"`
	Diag      []Diag         `json:"diag,omitempty"`
	Accepted  bool           `json:"accepted"`
	CreatedAt string         `json:"created_at"`
}

// Diag is a structured diagnostic attached to raw or derived data.
type Diag struct {
	Code    string `json:"code"`
	Row     int    `json:"row,omitempty"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	Severity string `json:"severity"` // error | warning
}

// Session records one import attempt: one or more layer fragments, each of
// which may be committed or rejected. A session with both accepted and rejected
// fragments is status "partial"; nothing accepted is "failed".
type Session struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Status    string         `json:"status"` // committed | partial | failed
	Fragments []Fragment     `json:"fragments"`
	Submitted string         `json:"submitted_at"`
}

type Fragment struct {
	Layer    SourceLayer `json:"layer"`
	Format   string      `json:"format"`
	Raw      string      `json:"raw"`
	Accepted bool        `json:"accepted"`
	SourceID string      `json:"source_id,omitempty"`
	Diag     []Diag      `json:"diag,omitempty"`
}
