package domain

import "time"

// Derived slots. A slot holds the "current" derived result for a view; older
// versions remain viewable and never overwrite it.
const (
	SlotCorrected = "corrected_series"
	SlotTG        = "tg"
	SlotDTG       = "dtg"
	SlotWindow    = "reaction_windows"
	SlotStages    = "stages"
	SlotResidue   = "residue"
)

// SlotAlgo maps slot -> algorithm version that fills it.
var SlotAlgo = map[string]string{
	SlotCorrected: AlgoCorrectSeries,
	SlotTG:        AlgoTG,
	SlotDTG:       AlgoDTG,
	SlotWindow:    AlgoReactionWindow,
	SlotStages:    AlgoStages,
	SlotResidue:   AlgoResidue,
}

// DepRef points to one direct source/derived/decision input.
type DepRef struct {
	Kind string `json:"kind"` // source kind / slot / "decision"
	ID   string `json:"id"`
}

// Envelope is a content-addressed derived result. Its identity pins algorithm
// version, parameter fingerprint and direct sources.
type Envelope struct {
	ID         string    `json:"id"`
	Slot       string    `json:"slot"`
	AlgoVersion string   `json:"algo_version"`
	ParamFP    string    `json:"param_fp"`
	Deps       []DepRef  `json:"deps"`
	Payload    any       `json:"payload"`
	Diags      []Diag    `json:"diags,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// EnvelopeID is the stable content identity of a derived result.
func (e Envelope) EnvelopeID() string {
	return ShortID("der_", e.Slot, e.AlgoVersion, e.ParamFP, e.Deps, e.Payload, e.Diags)
}
