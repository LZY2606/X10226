package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
)

// AlgorithmID versions every derived result. Changing the algorithm logic
// bumps the id and invalidates cached nodes via fingerprint mismatch.
const AlgorithmID = "tga-decomp-v1"

// DefaultParams returns the fixed seed parameters. They are deterministic;
// tests override them through explicit revisions.
func DefaultParams() Params {
	return Params{
		Version:       1,
		Algorithm:     AlgorithmID,
		ValveDelayS:   30,
		TempLagS:      20,
		DTGWindow:     3,
		PeakMinProm:   0.05,
		GapThresholdS: 25,
		NoiseTolMg:    0.08,
	}
}

// Snapshot is the view of the latest accepted input version of each layer at
// computation time. Missing layers are nil.
type Snapshot struct {
	Layers map[string]*InputNode
	Params Params
}

// Point is one normalized series sample. Missing marks a true data gap; the
// analysis layer never interpolates across a gap.
type Point struct {
	T         float64  `json:"t_s"`
	Temp      *float64 `json:"T_C,omitempty"`
	MassRaw   *float64 `json:"m_raw_mg,omitempty"`
	MassCorr  *float64 `json:"m_corr_mg,omitempty"`
	DriftCorr *float64 `json:"drift_corr_mg,omitempty"`
	Missing   bool     `json:"missing,omitempty"`
	Noise     bool     `json:"noise,omitempty"`
	Note      string   `json:"note,omitempty"`
}

// Peak is a DTG extremum candidate.
type Peak struct {
	ID           string  `json:"id"`
	T            float64 `json:"t_s"`
	Temp         float64 `json:"T_C"`
	DTG          float64 `json:"dtg_pct_per_min"`
	Prominence   float64 `json:"prominence_pct_per_min"`
	GroupID      string  `json:"group"`
	Overlap      bool    `json:"overlap"`
	Status       string  `json:"status"` // candidate | rejected
	RejectReason string  `json:"reject_reason,omitempty"`
}

// AtmWindow is the interval in which a switched atmosphere can plausibly be
// the cause of a reaction. Valve instant != reaction interval.
type AtmWindow struct {
	ID            string  `json:"id"`
	ValveT        float64 `json:"valve_t_s"`
	GasFrom       string  `json:"gas_from"`
	GasTo         string  `json:"gas_to"`
	ReactionStart float64 `json:"reaction_start_s"`
	ReactionEnd   float64 `json:"reaction_end_s"`
	TempAtStart   float64 `json:"temp_at_start_C"`
}

// Stage is one legal stage-decomposition candidate.
type Stage struct {
	ID            string   `json:"id"`
	Label         string   `json:"label"`
	StartT        float64  `json:"start_t_s"`
	EndT          float64  `json:"end_t_s"`
	StartTemp     float64  `json:"start_T_C"`
	EndTemp       float64  `json:"end_T_C"`
	MassLossMg    float64  `json:"mass_loss_mg"`
	MassLossPct   *float64 `json:"mass_loss_pct,omitempty"`
	PeakIDs       []string `json:"peak_ids"`
	WindowID      string   `json:"window_id,omitempty"`
	Alternative   string   `json:"alternative,omitempty"` // split | merge | ""
	Status        string   `json:"status"`                // candidate | rejected
	RejectReason  string   `json:"reject_reason,omitempty"`
	Unknown       bool     `json:"unknown,omitempty"`
	UnknownReason string   `json:"unknown_reason,omitempty"`
}

// Residual is one residual-percentage value with a full calculation chain.
type Residual struct {
	ID            string      `json:"id"`
	Label         string      `json:"label"`
	T             float64     `json:"t_s"`
	Temp          *float64    `json:"T_C,omitempty"`
	MassMg        *float64    `json:"mass_mg,omitempty"`
	InitialMg     *float64    `json:"initial_mg,omitempty"`
	Percent       *float64    `json:"percent,omitempty"`
	Unknown       bool        `json:"unknown,omitempty"`
	UnknownReason string      `json:"unknown_reason,omitempty"`
	Chain         []ChainStep `json:"chain"`
}

// Analysis is the full set of computed families for one snapshot.
type Analysis struct {
	Series      []Point     `json:"series"`
	DTG         []Point     `json:"dtg"`
	Peaks       []Peak      `json:"peaks"`
	Windows     []AtmWindow `json:"windows"`
	Stages      []Stage     `json:"stages"`
	Residuals   []Residual  `json:"residuals"`
	Diagnostics []Diag      `json:"diagnostics"`
}

// Analyze computes all derived families for one immutable snapshot. It is a
// pure function: identical snapshot bytes always produce identical results and
// stable content-addressed identities.
func Analyze(snap Snapshot) Analysis {
	an := Analysis{}
	p := snap.Params

	samples := latestLayer(snap, LayerSamples)
	drift := latestLayer(snap, LayerDrift)
	blank := latestLayer(snap, LayerBlank)
	program := latestLayer(snap, LayerProgram)
	valve := latestLayer(snap, LayerValve)
	m0 := latestLayer(snap, LayerInitialMass)

	an.Series = buildSeries(samples, drift, blank, p, &an)
	an.DTG = buildDTG(an.Series, p, &an)
	an.Peaks = findPeaks(an.DTG, an.Series, p, &an)
	an.Windows = buildWindows(valve, an.Series, program, p)
	an.Stages = buildStages(an, p)
	an.Residuals = buildResiduals(an.Series, m0, p)
	return an
}

func latestLayer(snap Snapshot, layer string) *InputNode {
	if n, ok := snap.Layers[layer]; ok {
		return n
	}
	return nil
}
