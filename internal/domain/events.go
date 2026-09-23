package domain

import "encoding/json"

// Event kinds. The event log is append-only; nothing is ever overwritten.
const (
	EvProjectCreated  = "project_created"
	EvRawImported     = "raw_imported"
	EvParamsRevised   = "params_revised"
	EvResultPublished = "result_published"
	EvDecision        = "decision" // accept | reject | adjust
	EvDecisionUndone  = "decision_undone"
	EvConflict        = "conflict"
	EvConflictResolved = "conflict_resolved"
	EvArchiveBuilt    = "archive_built"
)

// Event is the single unit of history. StableID survives export/import.
type Event struct {
	ID         string          `json:"id"`
	ProjectID  string          `json:"project_id"`
	Seq        int64           `json:"seq"`
	Kind       string          `json:"kind"`
	AtVirtual  string          `json:"at_virtual"` // RFC3339 under virtual clock
	Version    int64           `json:"version"`
	ParentID   string          `json:"parent_id,omitempty"`
	Payload    json.RawMessage `json:"payload"`
}

// ProjectCreatedPayload is the first event of every project.
type ProjectCreatedPayload struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// RawImportedPayload records one immutable raw submission.
type RawImportedPayload struct {
	Layer       string       `json:"layer"`
	LayerSeq    int          `json:"layer_seq"`
	ParentEvent string       `json:"parent_event,omitempty"`
	RawText     string       `json:"raw_text"`
	Entries     []RawEntry   `json:"entries"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Status      string       `json:"status"`
	Source      string       `json:"source"` // manual|case_pack|reimport
	OriginID    string       `json:"origin_id,omitempty"`
}

// ParamsRevisedPayload links revisions to a parent param version.
type ParamsRevisedPayload struct {
	Params      Params `json:"params"`
	ParentEvent string `json:"parent_event,omitempty"`
	Note        string `json:"note,omitempty"`
}

// Boundary, Peak, StageWindow are the domain pieces of one candidate.
type Boundary struct {
	TimeMin float64 `json:"time_min"`
	TempC   Quantity `json:"temp_c"`
	Label   string   `json:"label"`
	Basis   string   `json:"basis"` // peak | valve | manual | gap_edge
}

type Peak struct {
	ID        string   `json:"id"`
	TimeMin   float64  `json:"time_min"`
	DTG       Quantity `json:"dtg"`
	TempC     Quantity `json:"temp_c"`
	Prominent bool     `json:"prominent"`
	Note      string   `json:"note,omitempty"`
}

type StageWindow struct {
	StartMin float64 `json:"start_min"`
	EndMin   float64 `json:"end_min"`
	Reason   string  `json:"reason,omitempty"`
}

// Candidate is a proposed stage decomposition result.
type Candidate struct {
	ID               string       `json:"id"`
	Label            string       `json:"label"`
	Start            Boundary     `json:"start"`
	End              Boundary     `json:"end"`
	Peaks            []Peak       `json:"peaks"`
	MassLoss         Quantity     `json:"mass_loss"`
	MassLossPctInit  Quantity     `json:"mass_loss_pct_initial"`
	ResiduePct       Quantity     `json:"residue_pct"`
	ResidueCalc      ResidueChain `json:"residue_calc"`
	Atmosphere       string       `json:"atmosphere"`
	GasWindows       []StageWindow `json:"gas_windows,omitempty"`
	OverlapsWith     []string     `json:"overlaps_with,omitempty"`
	Unknowns         []string     `json:"unknowns,omitempty"`
	RejectReasons    []string     `json:"reject_reasons,omitempty"`
	Judgments        []Judgment   `json:"judgments"`
	Verdict          string       `json:"verdict,omitempty"`
	DecisionEventID  string       `json:"decision_event_id,omitempty"`
	AdjustedFrom     string       `json:"adjusted_from,omitempty"`
}

// ResidueChain is the expandable calculation chain behind one residue %.
type ResidueChain struct {
	InitialMass Quantity     `json:"initial_mass"`
	BlankRef    string       `json:"blank_ref"`
	DriftRef    string       `json:"drift_ref"`
	Steps       []ChainStep  `json:"steps"`
	Result      Quantity     `json:"result"`
}

// ChainStep is one computation node with direct sources.
type ChainStep struct {
	Step    string   `json:"step"`
	Detail  string   `json:"detail"`
	Value   Quantity `json:"value"`
	Sources []string `json:"sources"`
}

// Judgment is a machine-readable justification/rule event.
type Judgment struct {
	RuleID  string `json:"rule_id"`
	Message string `json:"message"`
	Refs    []Ref  `json:"refs"`
}

// Ref points at an input, result or prior event.
type Ref struct {
	Kind   string `json:"kind"` // raw_entry|event|candidate|params|point
	ID     string `json:"id"`
	Label  string `json:"label,omitempty"`
}

// SeriesPoint is an immutable identity-bearing derived sample for plotting.
type SeriesPoint struct {
	ID          string   `json:"id"`
	TimeMin     float64  `json:"time_min"`
	TempC       Quantity `json:"temp_c"`
	MassRawMg   Quantity `json:"mass_raw_mg"`
	MassCorrMg  Quantity `json:"mass_corrected_mg"`
	DtgMgPerMin Quantity `json:"dtg_mg_per_min"`
	RawEntryIDs []string `json:"raw_entry_ids"`
	Flags       []string `json:"flags"`
}

// Gap marks a sampling hole; curves must never interpolate across it.
type Gap struct {
	ID      string  `json:"id"`
	FromMin float64 `json:"from_min"`
	ToMin   float64 `json:"to_min"`
	Reason  string  `json:"reason"`
}

// FurnaceSeg / ValveTimelineItem carry visible program & atmosphere tracks.
type FurnaceSeg struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	StartMin float64 `json:"start_min"`
	EndMin   float64 `json:"end_min"`
	FromC   Quantity `json:"from_c"`
	ToC     Quantity `json:"to_c"`
	RateCpm Quantity `json:"rate_c_per_min,omitempty"`
}

type ValveItem struct {
	ID            string   `json:"id"`
	SwitchTimeMin float64  `json:"switch_time_min"`
	Gas           string   `json:"gas"`
	WindowStart   float64  `json:"window_start_min"`
	WindowEnd     float64  `json:"window_end_min"`
	LagSec        float64  `json:"lag_sec"`
	Note          string   `json:"note,omitempty"`
}

// ResultPublishedPayload is the derived projection at one point in history.
type ResultPublishedPayload struct {
	AlgoVersion string          `json:"algo_version"`
	ParamEvent  string          `json:"param_event"`
	ParamFP     string          `json:"param_fingerprint"`
	Inputs      map[string]string `json:"inputs"` // layer -> raw event id
	Series      []SeriesPoint   `json:"series"`
	Gaps        []Gap           `json:"gaps"`
	Furnace     []FurnaceSeg    `json:"furnace"`
	Valves      []ValveItem     `json:"valves"`
	Candidates  []Candidate     `json:"candidates"`
	Diagnostics []Diagnostic    `json:"diagnostics"`
	Status      string          `json:"status"` // current | stale
	Supersedes  string          `json:"supersedes,omitempty"`
}

// DecisionPayload accepts, rejects or adjusts one candidate.
type DecisionPayload struct {
	CandidateID   string  `json:"candidate_id"`
	ResultEventID string  `json:"result_event_id"`
	Verdict       string  `json:"verdict"`
	Note          string  `json:"note,omitempty"`
	// adjust-only fields:
	NewStartMin *float64 `json:"new_start_min,omitempty"`
	NewEndMin   *float64 `json:"new_end_min,omitempty"`
	NewLabel    string   `json:"new_label,omitempty"`
	NewCandidateID string `json:"new_candidate_id,omitempty"`
}

// DecisionUndonePayload restores visibility of a prior verdict state.
type DecisionUndonePayload struct {
	DecisionEventID string `json:"decision_event_id"`
	CandidateID     string `json:"candidate_id"`
}

// ConflictPayload records conflicting human decisions on overlapping stages.
type ConflictPayload struct {
	CandidateA string   `json:"candidate_a"`
	CandidateB string   `json:"candidate_b"`
	Message    string   `json:"message"`
	Verdicts   []string `json:"verdicts"`
}

// ConflictResolvedPayload resolves a conflict with a chosen outcome.
type ConflictResolvedPayload struct {
	ConflictEventID string `json:"conflict_event_id"`
	Chosen          string `json:"chosen"`
	Note            string `json:"note,omitempty"`
}

// ArchiveBuiltPayload captures one counter-example scenario replay.
type ArchiveBuiltPayload struct {
	Scenario   string   `json:"scenario"`
	ChangedRef string   `json:"changed_ref"`
	ChangedFrom string  `json:"changed_from"`
	ChangedTo  string   `json:"changed_to"`
	KeptRefs   []string `json:"kept_refs"`
	Affected   []string `json:"affected_result_ids"`
	Unaffected []string `json:"unaffected_result_ids"`
	Adopted    []string `json:"adopted_ids"`
	Rejected   []string `json:"rejected_ids"`
	Judgments  []Judgment `json:"judgments"`
	Note       string   `json:"note"`
}
