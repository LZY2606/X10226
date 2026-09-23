package domain

// Provenance links one derived value to its direct sources.
type Provenance struct {
	Algorithm string   `json:"algorithm"`
	ParamFP  string   `json:"param_fp"`
	DocIDs   []string `json:"doc_ids"`
	EventSeq int64    `json:"event_seq"` // event under which it was current
	Formula  string   `json:"formula,omitempty"`
	Inputs   []ValueRef `json:"inputs,omitempty"`
}

// ValueRef references a stable value in the archive by ID.
type ValueRef struct {
	ID    string  `json:"id"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
}

// Point is a canonical derived point.
type Point struct {
	T float64 `json:"t"` // seconds
	M float64 `json:"m"` // mg (corrected)
	D float64 `json:"d"` // mg/min (DTG)
	Tmp float64 `json:"tmp"` // celsius
	Seg string `json:"seg,omitempty"` // segment id across a gap
}

// Series is a derived TG/DTG series with gaps rendered explicitly.
type Series struct {
	ID       string    `json:"id"`
	Basis    string    `json:"basis"`
	Segments [][]Point `json:"segments"`
	Gaps     []Gap     `json:"gaps"`
	Prov     Provenance `json:"prov"`
}

// Gap is an acquisition hole; interpolation never crosses it.
type Gap struct {
	FromT float64 `json:"from_t"`
	ToT   float64 `json:"to_t"`
	FromTmp float64 `json:"from_tmp"`
	ToTmp   float64 `json:"to_tmp"`
	Spanned bool    `json:"spanned"` // always false in this system
}

// NoiseRun is a non-monotonic noise annotation.
type NoiseRun struct {
	StartT  float64 `json:"start_t"`
	EndT    float64 `json:"end_t"`
	SignFlips int   `json:"sign_flips"`
	Kind    string  `json:"kind"` // "non_monotonic"
}

// Peak is one DTG peak candidate.
type Peak struct {
	ID        string  `json:"id"`
	T         float64 `json:"t"`
	Time      string  `json:"time"` // t +/−
	TempC     float64 `json:"temp_c"`
	Rate      float64 `json:"rate"` // mg/min
	Prominence float64 `json:"prominence"`
	GroupID   string  `json:"group_id"`
	AltOf    string   `json:"alt_of,omitempty"`
	AltIDs    []string `json:"alt_ids,omitempty"`
}

// StageCandidate is one legal decomposition-stage candidate. Stages may
// overlap; their losses are never forced to sum to 100%.
type StageCandidate struct {
	ID          string   `json:"id"`
	GroupID     string   `json:"group_id"`
	AltOf       string   `json:"alt_of,omitempty"`
	StartT      float64  `json:"start_t"`
	EndT        float64  `json:"end_t"`
	StartTempC  float64  `json:"start_temp_c"`
	EndTempC    float64  `json:"end_temp_c"`
	StartM      float64  `json:"start_m"`
	EndM        float64  `json:"end_m"`
	LossMG      float64  `json:"loss_mg"`
	LossPct     *float64 `json:"loss_pct,omitempty"` // nil when mass0 missing
	PeakID      string   `json:"peak_id"`
	ValveT      *float64 `json:"valve_t,omitempty"`
	ValveGas    string   `json:"valve_gas,omitempty"`
	ReactionT   *float64 `json:"reaction_t,omitempty"`
	ReactionTemp float64 `json:"reaction_temp_c"`
	OverlapsWith []string `json:"overlaps_with,omitempty"`
	Status      string   `json:"status"` // current | stale | rejected
	RejectReason string  `json:"reject_reason,omitempty"`
	JudgmentIDs []string `json:"judgment_ids"`
	Prov        Provenance `json:"prov"`
}

// Residue is the final residue fraction with an expandable calculation chain.
type Residue struct {
	ID         string     `json:"id"`
	Value      *float64   `json:"value"` // nil => unknown
	Unit       string     `json:"unit"`  // "%"
	Status     string     `json:"status"` // current | stale | unknown
	UnknownWhy string     `json:"unknown_why,omitempty"`
	Chain      []ChainStep `json:"chain"`
	JudgmentIDs []string  `json:"judgment_ids"`
	Prov       Provenance `json:"prov"`
}

// ChainStep is one node in a residue calculation chain.
type ChainStep struct {
	RefID string  `json:"ref_id"`
	Op    string  `json:"op"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Note  string  `json:"note"`
}

// Judgment is a machine-readable rationale event/axiom application.
type Judgment struct {
	ID        string `json:"id"`
	Axiom     string `json:"axiom"`
	SubjectID string `json:"subject_id"`
	SubjectKind string `json:"subject_kind"`
	Detail    string `json:"detail"`
	EventSeq  int64  `json:"event_seq"`
}

// Axiom identifiers emitted by the system.
const (
	AxiomValveReactionLag = "AXIOM_VALVE_TIME_NEQ_REACTION_TIME"
	AxiomOverlapNotSum100 = "AXIOM_STAGES_OVERLAP_NOT_SUM_100"
	AxiomGapNoInterp      = "AXIOM_GAP_NOT_INTERPOLATED"
	AxiomAmbiguityKept    = "AXIOM_AMBIGUITY_ALL_CANDIDATES_KEPT"
	AxiomUnconvertibleKept = "AXIOM_UNCONVERTIBLE_KEPT_IN_PLACE"
	AxiomImmutability      = "AXIOM_RAW_IMMUTABLE_REVISION_VERSIONED"
)

// ResultBundle is the complete derived view current at one event.
type ResultBundle struct {
	EventSeq   int64           `json:"event_seq"`
	ParamFP    string          `json:"param_fp"`
	Series     *Series         `json:"series"`
	Peaks      []Peak          `json:"peaks"`
	Stages     []StageCandidate `json:"stages"`
	Residues   []Residue       `json:"residues"`
	Judgments  []Judgment      `json:"judgments"`
	Noises     []NoiseRun      `json:"noises"`
	DriftNote  string          `json:"drift_note"`
	Diags      []string        `json:"diags"`
}

// Decision records the effective human verdict on a subject.
type Decision struct {
	ID          string `json:"id"`
	EventSeq    int64  `json:"event_seq"`
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Verdict     string `json:"verdict"`
	Reason      string `json:"reason"`
	Revoked     bool   `json:"revoked"`
	ConflictWith []string `json:"conflict_with,omitempty"`
	ResolvedBy  string `json:"resolved_by,omitempty"`
	Winner      bool   `json:"winner,omitempty"`
	Adjustment  *Params `json:"adjustment,omitempty"`
	TS          string `json:"ts"`
}
