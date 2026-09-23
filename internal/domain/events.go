package domain

// Event type constants. Events are append-only; raw inputs are never
// overwritten; revisions are new versions with a parent reference.
const (
	EvProjectCreated  = "project.created"
	EvBlobStored      = "blob.stored"
	EvLayerSubmitted  = "layer.submitted"
	EvLayerParsed     = "layer.parsed"
	EvLayerReleased   = "layer.released" // accepted (possibly partial) release becomes active input
	EvRunStarted      = "run.started"
	EvRunCompleted    = "run.completed"
	EvDecision        = "decision.recorded"
	EvConflict        = "decision.conflict"
	EvConflictResolved = "conflict.resolved"
	EvUndo            = "decision.undone"
	EvParameterRev    = "parameter.revised"
	EvNote            = "note"
	EvCheckoutView    = "checkout.view"
)

// Event is the single unit of history. id/seq are stable and survive
// export -> delete -> import; prev chains the full audit trail.
type Event struct {
	ID        string         `json:"id"`
	ProjectID string         `json:"pid"`
	Seq       int64          `json:"seq"`
	At        string         `json:"at"`
	Actor     string         `json:"actor"`
	Type      string         `json:"type"`
	Prev      string         `json:"prev,omitempty"`
	Payload   map[string]any `json:"p"`
}

// RunParams are the analyst-tunable analysis parameters. Fingerprints of
// these attach to every derived value.
type RunParams struct {
	AtmosphereDelayS      float64 `json:"atmo_delay_s"`      // gas valve -> sample reaction delay (seconds)
	TempLagC              float64 `json:"temp_lag_c"`        // furnace -> sample temperature lag (°C)
	DriftCorrect          bool    `json:"drift_correct"`     // apply linear balance drift
	BlankCorrect          bool    `json:"blank_correct"`     // subtract blank curve
	BlankInterpGapS       float64 `json:"blank_interp_gap_s"` // blank data are sparse: interpolation allowed only within this span
	SmoothingWindow       int     `json:"smooth_window"`     // central window for DTG smoothing
	PeakProminencePct     float64 `json:"peak_prominence_pct"` // min %/s prominence
	MinStageLossPct       float64 `json:"min_stage_loss_pct"`
	MaxGapSpanS           float64 `json:"max_gap_span_s"` // beyond this, adjacent intervals never bridge
}

func DefaultParams() RunParams {
	return RunParams{
		AtmosphereDelayS:      8,
		TempLagC:              4,
		DriftCorrect:          true,
		BlankCorrect:          true,
		BlankInterpGapS:       60,
		SmoothingWindow:       5,
		PeakProminencePct:     0.02,
		MinStageLossPct:       0.5,
		MaxGapSpanS:           30,
	}
}

func (p RunParams) Fingerprint() string {
	return NewCID("pf_", []any{
		p.AtmosphereDelayS, p.TempLagC, p.DriftCorrect, p.BlankCorrect,
		p.BlankInterpGapS, p.SmoothingWindow, p.PeakProminencePct,
		p.MinStageLossPct, p.MaxGapSpanS,
	})
}

const AlgoVersion = "tg-decomp-1.3.0"
