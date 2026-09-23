package domain

// Event is the append-only project timeline entry. Every user action and
// automated action lands here; reverting to an event restores the visible
// state at that point in the log.
type Event struct {
	Seq        int            `json:"seq"`
	ProjectID  string         `json:"project_id"`
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Payload    map[string]any `json:"payload,omitempty"`
	CreatedAt  string         `json:"created_at"`
}

const (
	EvtProjectCreated = "project_created"
	EvtImport         = "import"
	EvtDerived        = "derived"
	EvtDecision       = "decision"
	EvtConflict       = "conflict"
	EvtConflictResolve = "conflict_resolved"
	EvtUndo           = "undo"
	EvtReviewComplete = "review_complete"
	EvtRuleJudgment   = "rule_judgment"
	EvtScenarios      = "scenario_run"
)

// RuleEvent is the machine-readable form of a domain judgment. These are the
// rules the counterexample archive is built around:
//
//	R1 atmosphere delay: valve switch time != sample reaction time
//	R2 temperature lag: furnace setpoint != sample temperature
//	R3 stages may overlap; their percentages are NOT forced to sum to 100
//	R4 data gaps must not be crossed by interpolation; integration is split
//	R5 non-monotonic noise is quarantined, not smoothed away
//	R6 initial mass revisions only invalidate mass-ratio downstream nodes
//	R7 partial import keeps committed fragments; rejected fragments stay visible
//	R8 overlap peaks keep all legal candidates until human adjudication
type RuleEvent struct {
	Rule       string         `json:"rule"`
	Subject    string         `json:"subject"`
	ObjectIDs  []string       `json:"object_ids"`
	Judgment   string         `json:"judgment"` // accepted | rejected | unknown
	Reason     string         `json:"reason"`
	Affected   []string       `json:"affected_node_ids"`
	Extra      map[string]any `json:"extra,omitempty"`
}

const (
	RuleAtmoDelay  = "R1_atmosphere_delay"
	RuleTempLag    = "R2_temperature_lag"
	RuleOverlap    = "R3_overlap_no_sum100"
	RuleGap        = "R4_no_interpolation_across_gap"
	RuleNoise      = "R5_nonmonotonic_quarantine"
	RuleMassRev    = "R6_initial_mass_locality"
	RuleHalfImport = "R7_partial_import_fragmentation"
	RulePeaks      = "R8_overlap_legal_candidates"
)
