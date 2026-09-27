package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// AlgoVersion identifies the derived-layer algorithm implementation. Every
// derived value carries this version so that a replay against an archived
// project shows which rule set produced it.
const AlgoVersion = "tga-derive-1.0.0"

// Layer identifies one of the six layered inputs the review narrative
// distinguishes. Layers are committed independently and never overwritten.
type Layer string

const (
	LayerRaw      Layer = "raw_samples"
	LayerDrift    Layer = "drift_correction"
	LayerFurnace  Layer = "furnace_program"
	LayerGas      Layer = "gas_valve_events"
	LayerBlank    Layer = "blank_curve"
	LayerInitMass Layer = "initial_mass"
)

func (l Layer) Valid() bool {
	switch l {
	case LayerRaw, LayerDrift, LayerFurnace, LayerGas, LayerBlank, LayerInitMass:
		return true
	}
	return false
}

// Value is a domain quantity. Raw is the exact imported representation and is
// never dropped; Value/Unit are the normalized layer, populated only when the
// representation is convertible. Unconvertible values stay in Raw with a
// diagnostic and Value/Unit empty.
type Value struct {
	Raw   string  `json:"raw"`
	Value float64 `json:"value,omitempty"`
	Unit  string  `json:"unit,omitempty"`
}

// RawPoint is one original sample row. Row is the stable 1-based position in
// the submitted file and survives across derived series indexing.
type RawPoint struct {
	Row   int    `json:"row"`
	Time  Value  `json:"time"`
	Temp  Value  `json:"temp"`
	Mass  Value  `json:"mass"`
	Valid bool   `json:"valid"`
	Diag  string `json:"diag,omitempty"`
}

// DriftSpec describes a balance baseline drift model anchored at a time.
type DriftSpec struct {
	Model     string `json:"model"`     // "linear" | "none"
	Rate      Value  `json:"rate"`      // e.g. ug/min, normalized to ug/min
	Intercept Value  `json:"intercept"` // mass at anchor, normalized to ug
	AnchorAt  Value  `json:"anchor_at"` // time anchor
	RawText   string `json:"raw_text,omitempty"`
}

// FurnaceSegment is one programmed temperature ramp/hold segment.
type FurnaceSegment struct {
	Index      int    `json:"index"`
	FromTime   Value  `json:"from_time"`
	ToTime     Value  `json:"to_time"`
	FromTemp   Value  `json:"from_temp"`
	ToTemp     Value  `json:"to_temp"`
	Kind       string `json:"kind"` // "ramp" | "hold"
	Programmed bool   `json:"programmed"`
}

// GasEvent is one gas valve switch as recorded by the controller clock.
type GasEvent struct {
	Index      int    `json:"index"`
	At         Value  `json:"at"`
	FromGas    string `json:"from_gas"`
	ToGas      string `json:"to_gas"`
	Controller string `json:"controller"`
	Flow       Value  `json:"flow,omitempty"`
	RawText    string `json:"raw_text,omitempty"`
}

// BlankPoint is one blank/baseline curve sample (empty pan run).
type BlankPoint struct {
	Row  int   `json:"row"`
	Time Value `json:"time"`
	Mass Value `json:"mass"`
	Temp Value `json:"temp,omitempty"`
}

// InitialMass is the layered sample initial weight record.
type InitialMass struct {
	Mass       Value  `json:"mass"`
	Basis      string `json:"basis"` // "sample" | "corrected"
	RecordedAt Value  `json:"recorded_at,omitempty"`
	Note       string `json:"note,omitempty"`
}

// Diag is an import-time diagnostic. Severity is "error" or "warning".
type Diag struct {
	Severity string `json:"severity"`
	Layer    string `json:"layer,omitempty"`
	Row      int    `json:"row,omitempty"`
	Field    string `json:"field,omitempty"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Raw      string `json:"raw,omitempty"`
}

// ParsedLayer is the parse result of one submitted file/text.
type ParsedLayer struct {
	Layer    Layer            `json:"layer"`
	Raw      []RawPoint       `json:"raw,omitempty"`
	Drift    *DriftSpec       `json:"drift,omitempty"`
	Furnace  []FurnaceSegment `json:"furnace,omitempty"`
	Gas      []GasEvent       `json:"gas,omitempty"`
	Blank    []BlankPoint     `json:"blank,omitempty"`
	InitMass *InitialMass     `json:"initial_mass,omitempty"`
	Diags    []Diag           `json:"diags,omitempty"`
}

func (p *ParsedLayer) HasError() bool {
	for _, d := range p.Diags {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}

// Source is one committed input layer version. Sources are append-only; a
// revision is a new Source with ParentID set.
type Source struct {
	ID          string      `json:"id"`
	ProjectID   string      `json:"project_id"`
	Layer       Layer       `json:"layer"`
	Version     int         `json:"version"`
	ParentID    string      `json:"parent_id,omitempty"`
	Filename    string      `json:"filename"`
	MediaType   string      `json:"media_type"`
	RawText     string      `json:"raw_text"`
	Parsed      ParsedLayer `json:"parsed"`
	ImportBatch string      `json:"import_batch"`
	CommittedAt string      `json:"committed_at"` // virtual clock RFC3339
	Superseded  bool        `json:"superseded"`
}

// Params is the revision-controlled derivation parameter set.
type Params struct {
	Fingerprint    string  `json:"fingerprint"`
	WindowSec      float64 `json:"window_sec"`       // smoothing window for DTG
	PeakMinProm    float64 `json:"peak_min_prom"`    // min DTG prominence (%/min)
	ValveDelaySec  float64 `json:"valve_delay_sec"`  // gas arrival delay
	TempLagSec     float64 `json:"temp_lag_sec"`     // sample-vs-furnace temperature lag
	OverlapTolSec  float64 `json:"overlap_tol_sec"`  // overlap/non-uniqueness tolerance
	GapMaxFillSec  float64 `json:"gap_max_fill_sec"` // always 0: gaps are never bridged
	ResidueTailSec float64 `json:"residue_tail_sec"`
	DriftModel     string  `json:"drift_model"` // "none" | "linear" | "blank"
	Note           string  `json:"note,omitempty"`
}

// DefaultParams returns the frozen baseline parameter set.
func DefaultParams() Params {
	return Params{
		WindowSec: 12, PeakMinProm: 0.02,
		ValveDelaySec: 18, TempLagSec: 12,
		OverlapTolSec: 9, GapMaxFillSec: 0,
		ResidueTailSec: 30, DriftModel: "linear",
	}
}

// Ref is a typed stable reference to any object in the archive.
type Ref struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

// SeriesPoint is one derived TG/DTG sample keyed to the original row.
type SeriesPoint struct {
	Row       int     `json:"row"`
	Time      float64 `json:"t_sec"`
	FurnaceT  float64 `json:"t_furnace_c"`
	SampleT   float64 `json:"t_sample_c"`
	MassCorr  float64 `json:"mass_corr_mg"`
	TG        float64 `json:"tg_pct"` // percentage of initial mass
	DTG       float64 `json:"dtg_pct_per_min"`
	GapBefore bool    `json:"gap_before"`
	NonMono   bool    `json:"nonmono"`
	Present   bool    `json:"present"` // false inside unbridged gap
}

// NoiseFlag records a non-monotonic sample that was retained, not smoothed away.
type NoiseFlag struct {
	Row int     `json:"row"`
	T   float64 `json:"t_sec"`
	M   float64 `json:"mass_corr_mg"`
	D   float64 `json:"jump_mg"`
}

// Gap is a missing interval that no interpolation may cross.
type Gap struct {
	FromRow int     `json:"from_row"`
	ToRow   int     `json:"to_row"`
	FromT   float64 `json:"from_t_sec"`
	ToT     float64 `json:"to_t_sec"`
	Reason  string  `json:"reason"`
}

// ValveLink ties a controller valve event to the sample-reaction window.
type ValveLink struct {
	GasEvent      Ref     `json:"gas_event"`
	ValveT        float64 `json:"valve_t_sec"`
	ReactionFromT float64 `json:"reaction_from_t_sec"`
	ReactionToT   float64 `json:"reaction_to_t_sec"`
	ToGas         string  `json:"to_gas"`
	DelaySec      float64 `json:"delay_sec"`
}

// Stage is one mass-loss stage candidate.
type Stage struct {
	ID          string   `json:"id"`
	GroupID     string   `json:"group_id,omitempty"` // shared when overlap/non-unique
	OnsetT      float64  `json:"onset_t_sec"`
	PeakT       float64  `json:"peak_t_sec"`
	OffsetT     float64  `json:"offset_t_sec"`
	OnsetTemp   float64  `json:"onset_temp_c"`
	PeakTemp    float64  `json:"peak_temp_c"`
	OffsetTemp  float64  `json:"offset_temp_c"`
	MassLossPct float64  `json:"mass_loss_pct"`
	MassLossMg  Value    `json:"mass_loss_mg"`
	SigmaPct    float64  `json:"sigma_pct"`
	Uncertain   bool     `json:"uncertain"`
	Status      string   `json:"status"` // candidate | accepted | rejected
	Truncated   bool     `json:"truncated"`
	Overlaps    []string `json:"overlaps,omitempty"`
	Atmosphere  string   `json:"atmosphere,omitempty"`
	GasEvent    *Ref     `json:"gas_event,omitempty"`
	Basis       string   `json:"basis"` // on-baseline | regression | valley
	Notes       []string `json:"notes,omitempty"`
}

// ChainNode is one step of a residue calculation chain.
type ChainNode struct {
	Step     string  `json:"step"`
	Ref      *Ref    `json:"ref,omitempty"`
	Value    Value   `json:"value,omitempty"`
	Quantity float64 `json:"quantity,omitempty"`
	Unit     string  `json:"unit,omitempty"`
	Note     string  `json:"note,omitempty"`
}

// Residue is a final residue percentage with a full calculation chain.
type Residue struct {
	ID          string      `json:"id"`
	WindowFromT float64     `json:"window_from_t_sec"`
	WindowToT   float64     `json:"window_to_t_sec"`
	MeanMassMg  float64     `json:"mean_mass_mg"`
	Percent     float64     `json:"percent"`
	SigmaPct    float64     `json:"sigma_pct"`
	Truncated   bool        `json:"truncated"`
	Chain       []ChainNode `json:"chain"`
}

// Finding is a machine-readable judgment event emitted by derivation.
type Finding struct {
	Code     string         `json:"code"`
	Severity string         `json:"severity"` // info | warning | error
	Message  string         `json:"message"`
	T        float64        `json:"t_sec,omitempty"`
	Refs     []Ref          `json:"refs,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

// AnalysisResult is the immutable output of one derivation run.
type AnalysisResult struct {
	ParamsFingerprint string        `json:"params_fingerprint"`
	AlgoVersion       string        `json:"algo_version"`
	Series            []SeriesPoint `json:"series"`
	Stages            []Stage       `json:"stages"`
	Residues          []Residue     `json:"residues"`
	ValveLinks        []ValveLink   `json:"valve_links"`
	Noise             []NoiseFlag   `json:"noise"`
	Gaps              []Gap         `json:"gaps"`
	Findings          []Finding     `json:"findings"`
	StageSumPct       float64       `json:"stage_sum_pct"`
	StageSumEnforced  bool          `json:"stage_sum_enforced"` // always false
}

// Analysis binds a result to its exact direct sources.
type Analysis struct {
	ID        string            `json:"id"`
	ProjectID string            `json:"project_id"`
	Seq       int               `json:"seq"`
	SourceIDs map[string]string `json:"source_ids"` // layer -> source id
	Params    Params            `json:"params"`
	Result    AnalysisResult    `json:"result"`
	CreatedAt string            `json:"created_at"`
	Current   bool              `json:"current"`
}

// Decision is a human adjudication. TargetKind is "stage" or "residue".
type Decision struct {
	ID         string             `json:"id"`
	ProjectID  string             `json:"project_id"`
	AtEventSeq int                `json:"at_event_seq"`
	TargetKind string             `json:"target_kind"`
	TargetID   string             `json:"target_id"`
	AnalysisID string             `json:"analysis_id"`
	Action     string             `json:"action"` // accept | reject | adjust | note
	Reason     string             `json:"reason"`
	Reviewer   string             `json:"reviewer"`
	Adjust     map[string]float64 `json:"adjust,omitempty"`
	Undone     bool               `json:"undone"`
	UndoneBy   string             `json:"undone_by,omitempty"`
	CreatedAt  string             `json:"created_at"`
}

// Event is the append-only history record. Everything else is a projection.
type Event struct {
	Seq       int            `json:"seq"`
	ID        string         `json:"id"`
	ProjectID string         `json:"project_id"`
	Type      string         `json:"type"`
	At        string         `json:"at"` // virtual clock
	Actor     string         `json:"actor"`
	PrevHash  string         `json:"prev_hash"`
	Hash      string         `json:"hash"`
	Payload   map[string]any `json:"payload"`
}

// CanonicalJSON serializes a value deterministically (sorted object keys).
func CanonicalJSON(v any) []byte {
	b, err := json.Marshal(canon(v))
	if err != nil {
		panic(err)
	}
	return b
}

// Hash returns the hex SHA-256 of the canonical JSON of v.
func Hash(v any) string {
	sum := sha256.Sum256(CanonicalJSON(v))
	return hex.EncodeToString(sum[:])
}

// ShortID derives a stable content id "<prefix>_<16hex>".
func ShortID(prefix string, v any) string {
	return prefix + "_" + Hash(v)[:16]
}

func canon(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([][2]any, 0, len(keys))
		for _, k := range keys {
			out = append(out, [2]any{k, canon(t[k])})
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = canon(t[i])
		}
		return out
	default:
		return v
	}
}
