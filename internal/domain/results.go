package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
)

// --- Corrected TG series ----------------------------------------------------

// Gap marks a sampling discontinuity that must never be interpolated across.
type Gap struct {
	AfterIndex int     `json:"after_index"`
	T1         float64 `json:"t1_s"`
	T2         float64 `json:"t2_s"`
	DtSec      float64 `json:"dt_s"`
	Reason     string  `json:"reason"`
}

// NoiseFlag records a non-monotonic excursion rather than smoothing it away.
type NoiseFlag struct {
	Index int     `json:"index"`
	T     float64 `json:"t_s"`
	Dmg   float64 `json:"dm_mg"`
	Note  string  `json:"note"`
}

type CorrectedSeries struct {
	TimeS        []float64 `json:"time_s"`
	TempC        []float64 `json:"temp_c"`
	MassMg       []float64 `json:"mass_mg"`     // after drift + blank correction
	RawMassMg    []float64 `json:"raw_mass_mg"` // normalized raw mass for comparison
	Missing      []bool    `json:"missing"`     // true where a value could not be converted
	Gaps         []Gap     `json:"gaps"`
	NoiseFlags   []NoiseFlag `json:"noise_flags"`
	CorrectionNote []string `json:"correction_notes"`
	Sources      []string  `json:"sources"`
}

// --- DTG --------------------------------------------------------------------

type DTGSeries struct {
	TimeS  []float64  `json:"time_s"`
	TempC  []float64  `json:"temp_c"`
	DmgMin []float64  `json:"dmg_min"`      // mg/min, central differences
	PctPerC []float64 `json:"pct_per_c"`    // -dm/dT normalized
	GapMask []bool     `json:"gap_mask"`    // derivative undefined adjacent to gaps
	Peaks   []DTGPeak  `json:"peaks"`
	Sources []string   `json:"sources"`
}

type DTGPeak struct {
	Index       int     `json:"index"`
	TimeS       float64 `json:"time_s"`
	TempC       float64 `json:"temp_c"`
	DmgMin      float64 `json:"dmg_min"`
	ProminencePct float64 `json:"prominence_pct"`
}

// --- Stages -----------------------------------------------------------------
// Stages may overlap and their mass losses are NOT forced to sum to 100%.

type StageCandidate struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	StartS         float64  `json:"start_s"`
	EndS           float64  `json:"end_s"`
	StartTempC     float64  `json:"start_temp_c"`
	EndTempC       float64  `json:"end_temp_c"`
	PeakS          float64  `json:"peak_s"`
	PeakTempC      float64  `json:"peak_temp_c"`
	MassLossMg     float64  `json:"mass_loss_mg"`
	MassLossPct    float64  `json:"mass_loss_pct"`
	OverlapsWith   []string `json:"overlaps_with,omitempty"`
	GapInside      bool     `json:"gap_inside"`
	// Gas attribution distinguishes valve time from effective sample reaction time.
	ValveSwitchS   *float64 `json:"valve_switch_s,omitempty"`
	EffectiveGasS  *float64 `json:"effective_gas_s,omitempty"`
	FromGas        string   `json:"from_gas,omitempty"`
	ToGas          string   `json:"to_gas,omitempty"`
	// Candidate bookkeeping.
	Status         string   `json:"status"`
	Ambiguity      []string `json:"ambiguity,omitempty"` // all legal alternatives kept
	RejectReason   string   `json:"reject_reason,omitempty"`
	Confidence     string   `json:"confidence"` // high | medium | low
	SourceIDs      []string `json:"source_ids"`
}

type StageSet struct {
	Candidates []StageCandidate `json:"candidates"`
	Notes      []string         `json:"notes"`
	Sources    []string         `json:"sources"`
}

// --- Residue with expandable computation chain ------------------------------

type ChainNode struct {
	Step      string  `json:"step"`
	Detail    string  `json:"detail"`
	ValueMg   float64 `json:"value_mg,omitempty"`
	Unit      string  `json:"unit,omitempty"`
	SourceID  string  `json:"source_id,omitempty"`
	GapBroken bool    `json:"gap_broken,omitempty"` // chain cannot be completed across gap
	Unknown   bool    `json:"unknown,omitempty"`
	Reason    string  `json:"reason,omitempty"`
}

type ResidueResult struct {
	InitialMassMg float64    `json:"initial_mass_mg"`
	FinalMassMg   float64    `json:"final_mass_mg"`
	ResiduePct    float64    `json:"residue_pct"`
	Unknown       bool       `json:"unknown"`
	Chain         []ChainNode `json:"chain"`
	// Alternate residues are retained when a gap or revision makes it non-unique.
	Alternatives  []ResidueResult `json:"alternatives,omitempty"`
	SourceIDs     []string        `json:"sources"`
}

// FingerprintParams returns a stable canonical fingerprint of all parameters.
func FingerprintParams(p Params) string {
	b, _ := json.Marshal(p)
	h := sha256.Sum256(b)
	return "fp_" + hex.EncodeToString(h[:])[:12]
}

func nanSafe(v float64) float64 {
	if math.IsNaN(v) {
		return 0
	}
	return v
}

var _ = nanSafe
